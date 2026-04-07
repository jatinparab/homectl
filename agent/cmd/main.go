package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jatinparab/homectl/agent"
	"github.com/jatinparab/homectl/utils"
)

func main() {
	// Load .env file if present (local dev)
	if err := utils.LoadEnv(".env"); err != nil {
		slog.Info("Warning: failed to load .env", "err", err)
	}

	a, err := agent.Init()
	if err != nil {
		slog.Error("Failed to initialize agent", "err", err)
		os.Exit(1)
	}

	// Start background loops (heartbeat + reconnection)
	a.Start()

	// Show initial status
	if a.IsConnected() {
		slog.Info("Discovered apps")
		for _, app := range a.ListApps() {
			fmt.Printf("  %s: ports %v\n", app.Name, app.Ports)
		}
	} else {
		slog.Info("Not connected to manager yet, will retry in background")
	}

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	slog.Info("Agent running. Press Ctrl+C to stop.", "name", a.Name(), "ip", a.IP())
	<-sigCh

	a.Stop()
}
