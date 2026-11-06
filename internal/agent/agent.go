package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultListenAddress = ":8080"
	shutdownTimeout      = 5 * time.Second
)

// Version identifies the agent build. Release builds may set it with -ldflags.
var Version = "dev"

// Config contains the values the agent receives when the guest boots.
type Config struct {
	ListenAddress string `json:"listenAddress,omitempty"`
	Model         string `json:"model"`
	WorkersPerVM  int32  `json:"workersPerVM"`
}

// LoadConfig reads one complete JSON config and validates it before startup.
func LoadConfig(reader io.Reader) (Config, error) {
	var config Config
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode agent config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, errors.New("agent config must contain one JSON object")
		}
		return Config{}, fmt.Errorf("decode agent config: %w", err)
	}
	if config.ListenAddress == "" {
		config.ListenAddress = DefaultListenAddress
	}
	if err := config.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// Validate checks startup fields only. Worker runtime validation is separate.
func (config Config) Validate() error {
	if strings.TrimSpace(config.Model) == "" {
		return errors.New("model must not be empty")
	}
	if config.WorkersPerVM < 1 {
		return errors.New("workersPerVM must be at least 1")
	}
	host, portText, err := net.SplitHostPort(config.ListenAddress)
	if err != nil {
		return fmt.Errorf("invalid listenAddress %q: %w", config.ListenAddress, err)
	}
	if strings.ContainsAny(host, " \t\r\n") {
		return fmt.Errorf("invalid listenAddress host %q", host)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("listenAddress port must be between 1 and 65535: %q", portText)
	}
	return nil
}

// Handler exposes only process health and build version in the skeleton.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /version", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, map[string]string{"version": Version})
	})
	return mux
}

func writeJSON(writer http.ResponseWriter, value any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(value)
}

// Serve runs until the context is cancelled, then shuts down active requests.
func Serve(ctx context.Context, listener net.Listener) error {
	server := &http.Server{
		Handler:           Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(listener) }()
	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("shut down agent HTTP server: %w", err)
		}
		err := <-serveResult
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
