package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jatinparab/homectl/config"
	"github.com/jatinparab/homectl/logger"
	"github.com/jatinparab/homectl/manager"
	"github.com/jatinparab/homectl/utils"
	"github.com/soheilhy/cmux"
)

func main() {
	// Load .env file if present (local dev)
	if err := utils.LoadEnv(".env"); err != nil {
		slog.Info("Warning: failed to load .env", "err", err)
	}

	hostname := utils.GetHostname()
	logger.Init("manager", hostname)
	repoDir := config.GetGitRepoDir(hostname)
	port := 1338

	mgr := manager.NewManager(repoDir, port)

	baseListener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		slog.Error("Failed to listen on manager port", "port", port, "err", err)
		os.Exit(1)
	}

	m := cmux.New(baseListener)
	httpListener := m.Match(cmux.HTTP1Fast())
	agentListener := m.Match(cmux.Any())

	if err := mgr.StartAgentTCPServer(agentListener); err != nil {
		slog.Error("Failed to start agent TCP server", "err", err)
		os.Exit(1)
	}

	// Best-effort initial git sync
	slog.Info("Performing initial git sync")
	if err := mgr.GitSync(); err != nil {
		slog.Info("Warning: Initial git sync failed (continuing anyway)", "err", err)
	}

	// Discover apps from docker/v2 and start reconcile loop
	mgr.DiscoverApps()

	reconcileCtx, reconcileCancel := context.WithCancel(context.Background())
	mgr.SetReconcileCancel(reconcileCancel)
	mgr.StartReconcileLoop(reconcileCtx)

	// Start server in goroutine
	server := mgr.NewServer()
	go func() {
		slog.Info("Manager server starting", "port", port)
		slog.Info("Serving files from", "dir", repoDir)
		if err := server.Serve(httpListener); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed", "err", err)
			os.Exit(1)
		}
	}()

	go func() {
		if err := m.Serve(); err != nil {
			slog.Error("Port multiplexer failed", "err", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	slog.Info("Manager running. Press Ctrl+C to stop.")
	<-sigCh

	// Graceful shutdown
	slog.Info("Shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Info("Server shutdown error", "err", err)
	}
	_ = baseListener.Close()

	mgr.Stop()
	slog.Info("Manager stopped")
}
