// Package workload defines the shared, non-streaming agent and inference contract.
package workload

import "time"

const (
	Provisioning = "PROVISIONING"
	Ready        = "READY"
	Busy         = "BUSY"
	Offline      = "OFFLINE"
	Error        = "ERROR"
)

// WorkerConfig describes one independently limited llama.cpp process.
type WorkerConfig struct {
	ModelFile     string `json:"modelFile"`
	ModelSHA256   string `json:"modelSHA256"`
	CPUMillis     int32  `json:"cpuMillis"`
	MemoryBytes   int64  `json:"memoryBytes"`
	ContextTokens int32  `json:"contextTokens"`
}

type Config struct {
	Generation   int64         `json:"generation,omitempty"`
	Model        string        `json:"model"`
	WorkersPerVM int32         `json:"workersPerVM"`
	Worker       *WorkerConfig `json:"worker,omitempty"`
}

type Worker struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
}

type Status struct {
	DesiredGeneration int64    `json:"desiredGeneration"`
	AppliedGeneration int64    `json:"appliedGeneration"`
	Model             string   `json:"model"`
	Workers           []Worker `json:"workers"`
}

type Request struct {
	ID        string    `json:"id"`
	Model     string    `json:"model"`
	Prompt    string    `json:"prompt"`
	MaxTokens int       `json:"maxTokens"`
	Deadline  time.Time `json:"deadline"`
}

type Result struct {
	ID           string `json:"id"`
	Model        string `json:"model"`
	BackendID    string `json:"backendID"`
	WorkerID     string `json:"workerID"`
	Device       string `json:"device"`
	Text         string `json:"text"`
	PromptTokens int    `json:"promptTokens"`
	OutputTokens int    `json:"outputTokens"`
	DurationMS   int64  `json:"durationMs"`
}

type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
