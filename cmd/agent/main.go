package main

import (
	"context"
	"fmt"
	"net"
	"os"
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

	listener, err := net.Listen("tcp", config.ListenAddress)
	if err != nil {
		return fmt.Errorf("listen for agent requests: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := agent.Serve(ctx, listener); err != nil {
		return fmt.Errorf("serve agent requests: %w", err)
	}
	return nil
}
