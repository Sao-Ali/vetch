package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/Sao-Ali/vetch/internal/workload"
)

// Process starts inside its resource limits, before executing runtime code.
type Process interface {
	Wait() error
	Stop()
}

type StartProcess func(context.Context, string, workload.WorkerConfig, int) (Process, error)

type managedWorker struct {
	workload.Worker
	url        string
	process    Process
	done       chan struct{}
	retryAfter time.Time
}

// Manager owns the worker processes for the lifetime of the agent.
type Manager struct {
	opMu      sync.Mutex
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	config    workload.Config
	applied   int64
	workers   []*managedWorker
	modelRoot string
	start     StartProcess
	client    *http.Client
	cpuMillis int64
	memory    int64
}

func NewManager(ctx context.Context, modelRoot string, start StartProcess, cpuMillis, memory int64) *Manager {
	ctx, cancel := context.WithCancel(ctx)
	return &Manager{ctx: ctx, cancel: cancel, modelRoot: modelRoot, start: start,
		cpuMillis: cpuMillis, memory: memory, client: &http.Client{Timeout: 2 * time.Second}}
}

func (manager *Manager) validate(config workload.Config) (string, error) {
	if strings.TrimSpace(config.Model) == "" || config.Generation < 0 || config.WorkersPerVM < 1 {
		return "", errors.New("model, nonnegative generation, and positive workersPerVM are required")
	}
	worker := config.Worker
	if worker == nil {
		return "", nil // Backwards-compatible process-health-only configuration.
	}
	if config.WorkersPerVM > 16 {
		return "", errors.New("managed inference supports at most 16 workers")
	}
	if worker.CPUMillis < 100 || worker.CPUMillis > 64000 || worker.MemoryBytes < 128<<20 ||
		worker.ContextTokens < 256 || worker.ContextTokens > 2048 {
		return "", errors.New("invalid worker CPU, memory, or context budget")
	}
	count := int64(config.WorkersPerVM)
	if int64(worker.CPUMillis) > manager.cpuMillis/count || worker.MemoryBytes > manager.memory/count {
		return "", errors.New("worker budgets exceed available guest capacity (including agent reserve)")
	}
	if filepath.Base(worker.ModelFile) != worker.ModelFile || worker.ModelFile == "." || worker.ModelFile == "" {
		return "", errors.New("modelFile must be a file name within the configured model directory")
	}
	wantHash, err := hex.DecodeString(worker.ModelSHA256)
	if err != nil || len(wantHash) != sha256.Size {
		return "", errors.New("modelSHA256 must contain 64 hexadecimal characters")
	}
	path := filepath.Join(manager.modelRoot, worker.ModelFile)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("modelFile must be a readable regular file (symlinks are not accepted)")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open model artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash model artifact: %w", err)
	}
	if !bytes.Equal(hash.Sum(nil), wantHash) {
		return "", errors.New("model artifact checksum mismatch")
	}
	return path, nil
}

// Apply validates before stopping existing workers. Duplicate generations do not restart workers.
func (manager *Manager) Apply(config workload.Config) error {
	manager.opMu.Lock()
	defer manager.opMu.Unlock()
	manager.mu.Lock()
	previous := manager.config
	same := reflect.DeepEqual(previous, config)
	manager.mu.Unlock()
	if manager.ctx.Err() != nil {
		return errors.New("agent is stopping")
	}
	if same {
		manager.retryFailed(config)
		return nil
	}
	if config.Generation < previous.Generation || (config.Generation == previous.Generation && previous.Model != "") {
		return errors.New("configuration changes require a newer generation")
	}
	path, err := manager.validate(config)
	if err != nil {
		return err
	}
	manager.stopWorkers()
	manager.mu.Lock()
	manager.config = config
	manager.applied = 0
	manager.workers = nil
	if config.Worker == nil {
		manager.applied = config.Generation
	}
	for index := range config.WorkersPerVM {
		if config.Worker == nil {
			break
		}
		worker := &managedWorker{Worker: workload.Worker{ID: fmt.Sprintf("worker-%d", index), State: workload.Provisioning},
			url: fmt.Sprintf("http://127.0.0.1:%d", 18080+index), done: make(chan struct{})}
		manager.workers = append(manager.workers, worker)
		go manager.runWorker(worker, path, *config.Worker, 18080+int(index))
	}
	manager.mu.Unlock()
	return nil
}

// Retry failed workers on configuration reconciliation, with a bounded backoff.
// Healthy workers and their active requests are left alone.
func (manager *Manager) retryFailed(config workload.Config) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	for index, worker := range manager.workers {
		if worker.State != workload.Error || time.Now().Before(worker.retryAfter) {
			continue
		}
		select {
		case <-worker.done:
		default:
			continue
		}
		worker.State, worker.Message = workload.Provisioning, ""
		worker.process = nil
		worker.done = make(chan struct{})
		go manager.runWorker(worker, filepath.Join(manager.modelRoot, config.Worker.ModelFile), *config.Worker, 18080+index)
	}
}

func (manager *Manager) stopWorkers() {
	manager.mu.Lock()
	workers := manager.workers
	for _, worker := range workers {
		worker.State = workload.Offline
		if worker.process != nil {
			worker.process.Stop()
		}
	}
	manager.mu.Unlock()
	for _, worker := range workers {
		<-worker.done
	}
}

func (manager *Manager) Close() {
	manager.cancel()
	manager.opMu.Lock()
	defer manager.opMu.Unlock()
	manager.stopWorkers()
}

func (manager *Manager) runWorker(worker *managedWorker, path string, config workload.WorkerConfig, port int) {
	defer close(worker.done)
	manager.mu.Lock()
	if worker.State == workload.Offline || manager.ctx.Err() != nil {
		manager.mu.Unlock()
		return
	}
	process, err := manager.start(manager.ctx, path, config, port)
	if err != nil {
		worker.State, worker.Message = workload.Error, err.Error()
		worker.retryAfter = time.Now().Add(30 * time.Second)
		manager.mu.Unlock()
		return
	}
	worker.process = process
	manager.mu.Unlock()
	finished := make(chan error, 1)
	go func() { finished <- process.Wait() }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	startupDeadline := time.Now().Add(2 * time.Minute)
	for {
		select {
		case <-manager.ctx.Done():
			process.Stop()
			<-finished
			return
		case err := <-finished:
			process.Stop() // Kill any remaining descendants and remove the cgroup.
			manager.mu.Lock()
			if worker.State != workload.Offline {
				worker.State, worker.Message = workload.Error, fmt.Sprintf("runtime exited: %v", err)
				worker.retryAfter = time.Now().Add(30 * time.Second)
			}
			manager.mu.Unlock()
			return
		case <-ticker.C:
			manager.mu.Lock()
			offline := worker.State == workload.Offline
			manager.mu.Unlock()
			if offline {
				process.Stop()
				<-finished
				return
			}
			healthy := manager.healthy(worker.url)
			manager.mu.Lock()
			if worker.State != workload.Offline {
				if healthy && worker.State == workload.Provisioning {
					worker.State = workload.Ready
				} else if !healthy && (worker.State != workload.Provisioning || time.Now().After(startupDeadline)) {
					worker.State, worker.Message = workload.Error, "runtime health check failed"
					process.Stop()
				}
				allReady := true
				for _, current := range manager.workers {
					allReady = allReady && (current.State == workload.Ready || current.State == workload.Busy)
				}
				if allReady {
					manager.applied = manager.config.Generation
				}
			}
			manager.mu.Unlock()
		}
	}
}

func (manager *Manager) healthy(url string) bool {
	request, err := http.NewRequestWithContext(manager.ctx, http.MethodGet, url+"/health", nil)
	if err != nil {
		return false
	}
	response, err := manager.client.Do(request)
	if err != nil {
		return false
	}
	_ = response.Body.Close()
	return response.StatusCode == http.StatusOK
}

func (manager *Manager) Status() workload.Status {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	status := workload.Status{DesiredGeneration: manager.config.Generation, AppliedGeneration: manager.applied,
		Model: manager.config.Model, Workers: []workload.Worker{}}
	for _, worker := range manager.workers {
		status.Workers = append(status.Workers, worker.Worker)
	}
	return status
}

func (manager *Manager) infer(writer http.ResponseWriter, request *http.Request) {
	var input workload.Request
	if err := workload.Decode(writer, request, &input); err != nil {
		workload.Fail(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := input.Validate(); err != nil {
		workload.Fail(writer, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	manager.mu.Lock()
	if input.Model != manager.config.Model {
		manager.mu.Unlock()
		workload.Fail(writer, http.StatusNotFound, "unsupported_model", "model is not served by this backend")
		return
	}
	var selected *managedWorker
	for _, worker := range manager.workers {
		if worker.State == workload.Ready {
			selected = worker
			worker.State = workload.Busy
			break
		}
	}
	manager.mu.Unlock()
	if selected == nil {
		workload.Fail(writer, http.StatusServiceUnavailable, "capacity_exhausted", "no ready worker is available")
		return
	}
	defer func() {
		manager.mu.Lock()
		if selected.State == workload.Busy {
			selected.State = workload.Ready
		}
		manager.mu.Unlock()
	}()
	ctx, cancel := context.WithDeadline(request.Context(), input.Deadline)
	defer cancel()
	started := time.Now()
	// Qwen3's non-thinking chat template is applied by llama.cpp.
	body, _ := json.Marshal(map[string]any{"messages": []map[string]string{{"role": "user", "content": input.Prompt}},
		"max_tokens": input.MaxTokens, "temperature": 0, "stream": false,
		"chat_template_kwargs": map[string]bool{"enable_thinking": false}})
	upstream, err := http.NewRequestWithContext(ctx, http.MethodPost, selected.url+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		workload.Fail(writer, http.StatusBadGateway, "runtime_error", "cannot construct runtime request")
		return
	}
	upstream.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: manager.client.Transport}
	response, err := client.Do(upstream)
	if err != nil {
		code := http.StatusBadGateway
		if ctx.Err() != nil {
			code = http.StatusGatewayTimeout
		}
		workload.Fail(writer, code, "runtime_error", "runtime request failed or deadline expired")
		return
	}
	defer func() { _ = response.Body.Close() }()
	var output struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, workload.MaxBodyBytes)).Decode(&output) != nil || len(output.Choices) != 1 {
		workload.Fail(writer, http.StatusBadGateway, "runtime_error", "runtime returned an unsuccessful or invalid response")
		return
	}
	workload.Write(writer, http.StatusOK, workload.Result{ID: input.ID, Model: input.Model,
		WorkerID: selected.ID, Device: "cpu", Text: output.Choices[0].Message.Content,
		PromptTokens: output.Usage.PromptTokens, OutputTokens: output.Usage.CompletionTokens,
		DurationMS: time.Since(started).Milliseconds()})
}

func (manager *Manager) Handler(configToken string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", Handler())
	mux.Handle("GET /version", Handler())
	status := func(writer http.ResponseWriter, _ *http.Request) {
		workload.Write(writer, http.StatusOK, manager.Status())
	}
	mux.HandleFunc("GET /config", status)
	mux.HandleFunc("GET /workers", status)
	mux.HandleFunc("GET /capacity", status)
	mux.HandleFunc("PUT /config", func(writer http.ResponseWriter, request *http.Request) {
		if !authorizedConfig(request.Header.Get("Authorization"), configToken) {
			writer.Header().Set("WWW-Authenticate", "Bearer")
			workload.Fail(writer, http.StatusUnauthorized, "unauthorized", "configuration credential required")
			return
		}
		var config workload.Config
		if err := workload.Decode(writer, request, &config); err != nil {
			workload.Fail(writer, http.StatusBadRequest, "invalid_config", err.Error())
			return
		}
		if err := manager.Apply(config); err != nil {
			workload.Fail(writer, http.StatusUnprocessableEntity, "invalid_config", err.Error())
			return
		}
		workload.Write(writer, http.StatusAccepted, manager.Status())
	})
	mux.HandleFunc("POST /infer", manager.infer)
	return mux
}
