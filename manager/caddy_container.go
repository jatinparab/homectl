package manager

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jatinparab/homectl/config"
)

var caddyContainerLog = slog.With("area", "caddy")

const (
	caddyBinary = "caddy"
)

var (
	caddyCmd      *exec.Cmd
	caddyCmdMu    sync.Mutex
	caddySyncMu   sync.Mutex // protects generation → validation → atomic rename
	caddyRestartW sync.Mutex // prevents concurrent restarts after failed reload
)

func caddyHostDir() string {
	return config.CaddyDir
}

func managerCaddyfilePath() string {
	return filepath.Join(caddyHostDir(), "Caddyfile")
}

func managerCaddyAccessLogPath() string {
	return filepath.Join(caddyHostDir(), "data", "logs", "access.log")
}

func caddyLogDir() string {
	return filepath.Join(caddyHostDir(), "data", "logs")
}

func caddyConfigDir() string {
	return filepath.Join(caddyHostDir(), "config")
}

func ensureCaddy() error {
	caddyCmdMu.Lock()
	defer caddyCmdMu.Unlock()

	if caddyCmd != nil && caddyCmd.Process != nil {
		return nil
	}

	for _, dir := range []string{caddyConfigDir(), caddyLogDir(), filepath.Dir(managerCaddyfilePath())} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create caddy directory %s: %w", dir, err)
		}
	}

	cmd := exec.Command(caddyBinary, "run", "--config", managerCaddyfilePath(), "--adapter", "caddyfile")
	cmd.Env = caddyEnv()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start caddy: %w", err)
	}
	caddyCmd = cmd
	caddyContainerLog.Info("Caddy started", "pid", cmd.Process.Pid)
	return nil
}

func reloadCaddy() error {
	caddyCmdMu.Lock()
	defer caddyCmdMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, caddyBinary, "reload", "--config", managerCaddyfilePath(), "--adapter", "caddyfile")
	output, err := cmd.CombinedOutput()
	if err == nil {
		caddyContainerLog.Debug("Caddy reloaded")
		return nil
	}

	out := strings.TrimSpace(string(output))
	caddyContainerLog.Warn("Caddy reload failed, restarting", "err", out)

	stopCaddyLocked()
	if startErr := ensureCaddyLocked(); startErr != nil {
		return fmt.Errorf("caddy reload+restart failed: reload: %w: %s; restart: %w", err, out, startErr)
	}
	return nil
}

func ensureCaddyLocked() error {
	cmd := exec.Command(caddyBinary, "run", "--config", managerCaddyfilePath(), "--adapter", "caddyfile")
	cmd.Env = caddyEnv()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	caddyCmd = cmd
	return nil
}

func caddyEnv() []string {
	return append(os.Environ(),
		"XDG_DATA_HOME="+filepath.Join(caddyHostDir(), "data"),
		"XDG_CONFIG_HOME="+filepath.Join(caddyHostDir(), "config"),
	)
}

func stopCaddyLocked() {
	if caddyCmd != nil && caddyCmd.Process != nil {
		_ = caddyCmd.Process.Kill()
		_ = caddyCmd.Wait()
		caddyCmd = nil
	}
}

func stopCaddy() {
	caddyCmdMu.Lock()
	defer caddyCmdMu.Unlock()
	stopCaddyLocked()
	caddyContainerLog.Info("Caddy stopped")
}

func isCaddyRunning() bool {
	caddyCmdMu.Lock()
	defer caddyCmdMu.Unlock()
	return caddyCmd != nil && caddyCmd.Process != nil
}
