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
	repoDir := config.GetGitRepoDir(hostname)
	port := 1338

	mgr, err := manager.NewManager(repoDir, port)
	if err != nil {
		slog.Error("Failed to initialize manager", "err", err)
		os.Exit(1)
	}
	reconcileCtx, reconcileCancel := context.WithCancel(context.Background())
	mgr.SetRuntimeContext(reconcileCtx)
	mgr.SetReconcileCancel(reconcileCancel)

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

	if !mgr.OnboardingRequired() {
		mgr.StartRuntimeOnce(reconcileCtx)
	} else {
		if err := mgr.SyncManagerHostCaddyConfig(); err != nil {
			slog.Warn("Failed to start onboarding public host proxy", "err", err)
		}
		slog.Info("Manager waiting for first user onboarding before starting runtime services")
	}

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
