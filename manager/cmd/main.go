package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jatinparab/homectl/config"
	"github.com/jatinparab/homectl/logger"
	"github.com/jatinparab/homectl/manager"
	"github.com/jatinparab/homectl/utils"
	"github.com/jatinparab/homectl/wireguard"
)

func main() {
	// Load .env file if present (local dev)
	if err := utils.LoadEnv(".env"); err != nil {
		slog.Info("Warning: failed to load .env", "err", err)
	}
	if err := utils.LoadEnv("/etc/homectl/secrets.env"); err != nil {
		slog.Info("Warning: failed to load manager secrets", "err", err)
	}

	hostname := utils.GetHostname()
	logger.Init("manager", hostname)
	manager.InitLoggers()
	repoDir := os.Getenv("HOMECTL_REPO_DIR")
	if repoDir == "" {
		repoDir = config.GetGitRepoDir(hostname)
	}
	port := 1338

	mgr := manager.NewManager(repoDir, port)
	tokenSet := true
	if _, err := manager.RequireJoinTokenForStartup(); err != nil {
		slog.Warn("WireGuard not configured, running without overlay", "err", err)
		tokenSet = false
	}
	if tokenSet {
		if err := mgr.EnsureWireGuard(); err != nil {
			slog.Warn("Failed to start WireGuard overlay (continuing anyway)", "err", err)
		}
	}
	if err := mgr.StartAgentTCPServer(); err != nil {
		slog.Error("Failed to start agent TCP server", "err", err)
		os.Exit(1)
	}

	// Best-effort initial git sync
	slog.Info("Performing initial git sync")
	if err := mgr.GitSync(); err != nil {
		slog.Info("Warning: Initial git sync failed (continuing anyway)", "err", err)
	}

	// Discover apps from docker and start reconcile loop
	mgr.DiscoverApps()

	// Start server before public route health checks. The generated Caddy config
	// proxies the public manager host back to this listener.
	server := mgr.NewServer()
	go func() {
		slog.Info("Manager server starting", "port", port)
		slog.Info("Serving files from", "dir", repoDir)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed", "err", err)
			os.Exit(1)
		}
	}()

	reconcileCtx, reconcileCancel := context.WithCancel(context.Background())
	mgr.SetReconcileCancel(reconcileCancel)
	mgr.StartReconcileLoop(reconcileCtx)
	mgr.StartPublicRouteLoop(reconcileCtx)

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

	mgr.Stop()
	wireguard.Teardown()
	slog.Info("Manager stopped")
}
