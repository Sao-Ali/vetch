package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sao-Ali/vetch/internal/workload"
)

type fakeProcess struct {
	done chan struct{}
	once sync.Once
}

func (process *fakeProcess) Wait() error { <-process.done; return errors.New("test process exited") }
func (process *fakeProcess) Stop()       { process.once.Do(func() { close(process.done) }) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (transport roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func testConfig(t *testing.T) (string, workload.Config) {
	t.Helper()
	root := t.TempDir()
	contents := []byte("test model artifact")
	if err := os.WriteFile(filepath.Join(root, "model.gguf"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(contents)
	return root, workload.Config{Generation: 1, Model: "test-model", WorkersPerVM: 1,
		Worker: &workload.WorkerConfig{ModelFile: "model.gguf", ModelSHA256: hex.EncodeToString(hash[:]), CPUMillis: 1000,
			MemoryBytes: 2 << 30, ContextTokens: 2048}}
}

func waitState(t *testing.T, manager *Manager, state string) {
	t.Helper()
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		status := manager.Status()
		if len(status.Workers) == 1 && status.Workers[0].State == state {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("wanted %s, got %+v", state, status)
		case <-ticker.C:
		}
	}
}

func TestWorkerLifecycleAndAdmission(t *testing.T) {
	root, config := testConfig(t)
	process := &fakeProcess{done: make(chan struct{})}
	started := make(chan struct{}, 1)
	manager := NewManager(context.Background(), root, func(context.Context, string, workload.WorkerConfig, int) (Process, error) {
		started <- struct{}{}
		return process, nil
	}, 1000, 3<<30)
	t.Cleanup(manager.Close)
	inferenceStarted := make(chan struct{}, 1)
	release := make(chan struct{})
	manager.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/health" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}
		var input map[string]any
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if input["max_tokens"] != float64(16) || input["stream"] != false {
			t.Errorf("invalid runtime request: %v", input)
		}
		inferenceStarted <- struct{}{}
		select {
		case <-release:
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"hello"}}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`))}, nil
	})
	if err := manager.Apply(config); err != nil {
		t.Fatal(err)
	}
	<-started
	if status := manager.Status(); status.AppliedGeneration != 0 {
		t.Fatal("configuration applied before model readiness")
	}
	waitState(t, manager, workload.Ready)
	if manager.Status().AppliedGeneration != config.Generation {
		t.Fatal("ready generation not applied")
	}
	if err := manager.Apply(config); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
		t.Fatal("duplicate configuration restarted runtime")
	default:
	}
	body, _ := json.Marshal(workload.Request{ID: "test", Model: config.Model, Prompt: "hello", MaxTokens: 16, Deadline: time.Now().Add(time.Minute)})
	handler := manager.Handler()
	first := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() {
		handler.ServeHTTP(first, httptest.NewRequest(http.MethodPost, "/infer", bytes.NewReader(body)))
		close(finished)
	}()
	<-inferenceStarted
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/infer", bytes.NewReader(body)))
	if second.Code != http.StatusServiceUnavailable {
		t.Fatalf("busy worker accepted concurrent request: %d", second.Code)
	}
	close(release)
	<-finished
	if first.Code != 200 || !strings.Contains(first.Body.String(), `"text":"hello"`) {
		t.Fatalf("inference response: %d %s", first.Code, first.Body)
	}
	process.Stop()
	waitState(t, manager, workload.Error)
}

func TestWorkerConfigurationValidation(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*workload.Config)
	}{
		{"path traversal", func(c *workload.Config) { c.Worker.ModelFile = "../model.gguf" }},
		{"checksum mismatch", func(c *workload.Config) { c.Worker.ModelSHA256 = strings.Repeat("0", 64) }},
		{"invalid checksum", func(c *workload.Config) { c.Worker.ModelSHA256 = "bad" }},
		{"cpu overcommit", func(c *workload.Config) { c.Worker.CPUMillis = 2000 }},
		{"memory overcommit", func(c *workload.Config) { c.Worker.MemoryBytes = 4 << 30 }},
		{"count overcommit", func(c *workload.Config) { c.WorkersPerVM = 2 }},
		{"context budget", func(c *workload.Config) { c.Worker.ContextTokens = 4096 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, config := testConfig(t)
			manager := NewManager(context.Background(), root, func(context.Context, string, workload.WorkerConfig, int) (Process, error) {
				t.Error("invalid configuration started process")
				return nil, errors.New("unexpected start")
			}, 1000, 3<<30)
			defer manager.Close()
			test.change(&config)
			if err := manager.Apply(config); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestConfigurationGenerationAndShutdown(t *testing.T) {
	root, config := testConfig(t)
	process := &fakeProcess{done: make(chan struct{})}
	manager := NewManager(context.Background(), root, func(context.Context, string, workload.WorkerConfig, int) (Process, error) { return process, nil }, 1000, 3<<30)
	if err := manager.Apply(config); err != nil {
		t.Fatal(err)
	}
	changed := config
	changed.Model = "another-model"
	if err := manager.Apply(changed); err == nil {
		t.Fatal("accepted different configuration at same generation")
	}
	changed.Generation = 0
	if err := manager.Apply(changed); err == nil {
		t.Fatal("accepted stale generation")
	}
	manager.Close()
	if err := manager.Apply(config); err == nil {
		t.Fatal("accepted configuration after shutdown")
	}
}

func TestHealthOnlyConfigurationPreservesDeclaredCounts(t *testing.T) {
	manager := NewManager(context.Background(), t.TempDir(), nil, 0, 0)
	defer manager.Close()
	if err := manager.Apply(workload.Config{Model: "legacy-model", WorkersPerVM: 1000}); err != nil {
		t.Fatal(err)
	}
	if len(manager.Status().Workers) != 0 {
		t.Fatal("health-only configuration started workers")
	}
}

func TestInferenceDeadlineCancelsRuntime(t *testing.T) {
	root, config := testConfig(t)
	process := &fakeProcess{done: make(chan struct{})}
	manager := NewManager(context.Background(), root, func(context.Context, string, workload.WorkerConfig, int) (Process, error) { return process, nil }, 1000, 3<<30)
	defer manager.Close()
	cancelled := make(chan struct{})
	manager.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/health" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}
		<-request.Context().Done()
		close(cancelled)
		return nil, request.Context().Err()
	})
	if err := manager.Apply(config); err != nil {
		t.Fatal(err)
	}
	waitState(t, manager, workload.Ready)
	body, _ := json.Marshal(workload.Request{ID: "deadline", Model: config.Model, Prompt: "hello", MaxTokens: 1, Deadline: time.Now().Add(50 * time.Millisecond)})
	response := httptest.NewRecorder()
	manager.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/infer", bytes.NewReader(body)))
	if response.Code != http.StatusGatewayTimeout {
		t.Fatalf("deadline status: %d", response.Code)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("runtime context was not cancelled")
	}
}

func TestFailedWorkerRetryDoesNotRestartHealthyWorkers(t *testing.T) {
	root, config := testConfig(t)
	config.WorkersPerVM = 2
	processes := make(chan *fakeProcess, 3)
	manager := NewManager(context.Background(), root,
		func(context.Context, string, workload.WorkerConfig, int) (Process, error) {
			process := &fakeProcess{done: make(chan struct{})}
			processes <- process
			return process, nil
		}, 2000, 5<<30)
	defer manager.Close()
	manager.client.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	if err := manager.Apply(config); err != nil {
		t.Fatal(err)
	}
	first, second := <-processes, <-processes
	first.Stop()
	// Wait for process observation without changing real retry timers.
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		manager.mu.Lock()
		failed := false
		for _, worker := range manager.workers {
			if worker.State == workload.Error {
				select {
				case <-worker.done:
					worker.retryAfter = time.Now().Add(-time.Second)
					failed = true
				default:
				}
			}
		}
		manager.mu.Unlock()
		if failed {
			break
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("failed worker was not observed")
		}
	}
	if err := manager.Apply(config); err != nil {
		t.Fatal(err)
	}
	select {
	case <-processes:
	case <-time.After(time.Second):
		t.Fatal("failed worker was not restarted")
	}
	select {
	case <-second.done:
		t.Fatal("healthy worker was stopped")
	default:
	}
}
