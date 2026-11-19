package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/Sao-Ali/vetch/internal/agent"
)

const defaultConfigPath = "/etc/vetch/config.json"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	configPath := os.Getenv("VETCH_AGENT_CONFIG")
	if configPath == "" {
		configPath = defaultConfigPath
	}
	configFile, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("open agent config: %w", err)
	}
	config, decodeErr := agent.LoadConfig(configFile)
	closeErr := configFile.Close()
	if decodeErr != nil {
		return decodeErr
	}
	if closeErr != nil {
		return fmt.Errorf("close agent config: %w", closeErr)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	modelRoot := os.Getenv("VETCH_MODEL_ROOT")
	if modelRoot == "" {
		modelRoot = "/var/lib/vetch/models"
	}
	executable := os.Getenv("VETCH_RUNTIME")
	if executable == "" {
		executable = "llama-server"
	}
	if config.Worker != nil {
		if _, err := exec.LookPath(executable); err != nil {
			return fmt.Errorf("find runtime: %w", err)
		}
	}
	cpu, memory, err := agent.HostCapacity()
	if err != nil {
		return fmt.Errorf("read guest capacity: %w", err)
	}
	start := agent.RuntimeStarter(executable, os.Getenv("VETCH_CGROUP_ROOT"))
	manager := agent.NewManager(ctx, modelRoot, start, cpu, memory)
	defer manager.Close()
	if err := manager.Apply(config.Workload()); err != nil {
		return fmt.Errorf("apply bootstrap config: %w", err)
	}

	listener, err := net.Listen("tcp", config.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen for agent requests: %w", err)
	}
	if err := agent.Serve(ctx, listener, manager.Handler()); err != nil {
		return fmt.Errorf("serve agent requests: %w", err)
	}
	return nil
}
