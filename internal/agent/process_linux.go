//go:build linux

package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"

	"github.com/Sao-Ali/vetch/internal/workload"
)

// HostCapacity reserves one CPU and 256 MiB for the agent and guest OS.
func HostCapacity() (int64, int64, error) {
	contents, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	var memoryKB int64
	if _, err := fmt.Sscanf(string(contents), "MemTotal: %d kB", &memoryKB); err != nil {
		return 0, 0, err
	}
	return max(int64(runtime.NumCPU()-1)*1000, 0), max(memoryKB*1024-(256<<20), 0), nil
}

type limitedProcess struct {
	command *exec.Cmd
	cgroup  string
	once    sync.Once
}

func (process *limitedProcess) Wait() error {
	err := process.command.Wait()
	process.Stop()
	_ = os.Remove(process.cgroup)
	return err
}

func (process *limitedProcess) Stop() {
	process.once.Do(func() {
		// The cgroup includes every descendant, even ones that create new sessions.
		_ = os.WriteFile(filepath.Join(process.cgroup, "cgroup.kill"), []byte("1"), 0o600)
		_ = syscall.Kill(-process.command.Process.Pid, syscall.SIGKILL)
	})
}

// RuntimeStarter requires a cgroup-v2 subtree delegated to the agent by systemd.
// Missing delegation fails closed; inference never runs without its CPU/RAM limits.
func RuntimeStarter(executable, cgroupRoot string) StartProcess {
	return func(ctx context.Context, model string, config workload.WorkerConfig, port int) (Process, error) {
		if cgroupRoot == "" || !filepath.IsAbs(cgroupRoot) {
			return nil, errors.New("an absolute delegated cgroup root is required")
		}
		if err := os.WriteFile(filepath.Join(cgroupRoot, "cgroup.subtree_control"), []byte("+cpu +memory"), 0o600); err != nil {
			return nil, fmt.Errorf("enable delegated resource controllers: %w", err)
		}
		path := filepath.Join(cgroupRoot, fmt.Sprintf("worker-%d", port))
		// Reuse only an empty subtree left by this agent after an earlier worker exit.
		if err := os.Mkdir(path, 0o700); err != nil {
			return nil, fmt.Errorf("create worker cgroup: %w", err)
		}
		started := false
		defer func() {
			if !started {
				_ = os.Remove(path)
			}
		}()
		for name, value := range map[string]string{
			"cpu.max":         fmt.Sprintf("%d 100000", int64(config.CPUMillis)*100),
			"memory.max":      strconv.FormatInt(config.MemoryBytes, 10),
			"memory.swap.max": "0", "memory.oom.group": "1",
		} {
			if err := os.WriteFile(filepath.Join(path, name), []byte(value), 0o600); err != nil {
				return nil, fmt.Errorf("set worker %s: %w", name, err)
			}
		}
		group, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = group.Close() }()
		command := exec.CommandContext(ctx, executable,
			"--model", model, "--host", "127.0.0.1", "--port", strconv.Itoa(port),
			"--ctx-size", strconv.Itoa(int(config.ContextTokens)),
			"--threads", strconv.Itoa(max(1, int(config.CPUMillis)/1000)),
			"--parallel", "1", "--n-gpu-layers", "0", "--load-mode", "none", "--no-webui")
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, UseCgroupFD: true, CgroupFD: int(group.Fd())}
		// Runtime logs can include prompts. Do not forward them to agent logs.
		if err := command.Start(); err != nil {
			return nil, fmt.Errorf("start limited runtime: %w", err)
		}
		started = true
		return &limitedProcess{command: command, cgroup: path}, nil
	}
}
