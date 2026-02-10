package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jatinparab/homectl/config"
	"github.com/jatinparab/homectl/manager"
	"github.com/jatinparab/homectl/utils"
)

func main() {
	// Load .env file if present (local dev)
	if err := utils.LoadEnv(".env"); err != nil {
		log.Printf("Warning: failed to load .env: %s", err)
	}

	hostname := utils.GetHostname()
	repoDir := config.GetGitRepoDir(hostname)
	port := 1338

	mgr := manager.NewManager(repoDir, port)

	// Best-effort initial git sync
	log.Println("Performing initial git sync...")
	if err := mgr.GitSync(); err != nil {
		log.Printf("Warning: Initial git sync failed: %s (continuing anyway)", err)
	}

	// Start server in goroutine
	server := mgr.NewServer()
	go func() {
		log.Printf("Manager server starting on :%d", port)
		log.Printf("Serving files from: %s", repoDir)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %s", err)
		}
	}()

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Manager running. Press Ctrl+C to stop.")
	<-sigCh

	// Graceful shutdown
	log.Println("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server shutdown error: %s", err)
	}

	mgr.Stop()
	log.Println("Manager stopped")
}
