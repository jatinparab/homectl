package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/jatinparab/homectl/agent"
	"github.com/jatinparab/homectl/utils"
)

func main() {
	// Load .env file if present (local dev)
	if err := utils.LoadEnv(".env"); err != nil {
		log.Printf("Warning: failed to load .env: %s", err)
	}

	a, err := agent.Init()
	if err != nil {
		log.Fatalf("Failed to initialize agent: %s", err)
	}

	// Start background loops (heartbeat + reconnection)
	a.Start()

	// Show initial status
	if a.IsConnected() {
		log.Println("Discovered apps:")
		for _, app := range a.ListApps() {
			fmt.Printf("  %s: ports %v\n", app.Name, app.Ports)
		}
	} else {
		log.Println("Not connected to manager yet, will retry in background")
	}

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Printf("Agent %s (%s) running. Press Ctrl+C to stop.", a.Name(), a.IP())
	<-sigCh

	a.Stop()
}
