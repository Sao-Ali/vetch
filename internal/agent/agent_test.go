package agent

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "existing cloud-init config", input: `{"model":"example/model-v1","workersPerVM":2}`},
		{name: "custom address", input: `{"listenAddress":"127.0.0.1:9000","model":"model-a","workersPerVM":1}`},
		{name: "empty model", input: `{"model":" ","workersPerVM":2}`, wantErr: "model must not be empty"},
		{name: "zero workers", input: `{"model":"model-a","workersPerVM":0}`, wantErr: "workersPerVM must be at least 1"},
		{name: "invalid port", input: `{"listenAddress":":70000","model":"model-a","workersPerVM":1}`, wantErr: "listenAddress port must be between 1 and 65535"},
		{name: "unknown field", input: `{"model":"model-a","workersPerVM":1,"workerCount":2}`, wantErr: "unknown field"},
		{name: "trailing object", input: `{"model":"model-a","workersPerVM":1} {}`, wantErr: "one JSON object"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, err := LoadConfig(strings.NewReader(test.input))
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("LoadConfig() error = %v, want substring %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadConfig() error = %v", err)
			}
			if config.ListenAddress == "" {
				t.Fatal("LoadConfig() did not set listen address")
			}
		})
	}
}

func TestHandler(t *testing.T) {
	server := httptest.NewServer(Handler())
	defer server.Close()
	for _, test := range []struct {
		path string
		key  string
		want string
	}{
		{path: "/healthz", key: "status", want: "ok"},
		{path: "/version", key: "version", want: Version},
	} {
		response, err := server.Client().Get(server.URL + test.path)
		if err != nil {
			t.Fatalf("GET %s: %v", test.path, err)
		}
		if response.StatusCode != http.StatusOK {
			_ = response.Body.Close()
			t.Fatalf("GET %s status = %d", test.path, response.StatusCode)
		}
		if response.Header.Get("Content-Type") != "application/json" {
			_ = response.Body.Close()
			t.Fatalf("GET %s content type = %q", test.path, response.Header.Get("Content-Type"))
		}
		var body map[string]string
		decodeErr := json.NewDecoder(response.Body).Decode(&body)
		_ = response.Body.Close()
		if decodeErr != nil || body[test.key] != test.want {
			t.Fatalf("GET %s body = %v, decode error = %v", test.path, body, decodeErr)
		}
	}
}

func TestServeShutsDown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- Serve(ctx, listener) }()

	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + listener.Addr().String() + "/healthz")
	if err != nil {
		cancel()
		t.Fatalf("agent did not serve health: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	cancel()
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("Serve() shutdown error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve() did not shut down after cancellation")
	}
}
