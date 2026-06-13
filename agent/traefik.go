package agent

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
	"github.com/jatinparab/homectl/manager/client"
)

var caddyLog = slog.With("area", "caddy")

const caddyBinary = "caddy"

var (
	agentCaddyCmd   *exec.Cmd
	agentCaddyCmdMu sync.Mutex
)

type CaddySync struct {
	configPath    string
	configDir     string
	currentConfig string
	client        *client.Client
	agentID       string
	envProvider   func() map[string]string
}

func NewCaddySync(configDir, agentID string, c *client.Client, envProvider func() map[string]string) *CaddySync {
	configPath := filepath.Join(configDir, "Caddyfile")
	cs := &CaddySync{
		configPath:  configPath,
		configDir:   filepath.Dir(configPath),
		client:      c,
		agentID:     agentID,
		envProvider: envProvider,
	}
	if data, err := os.ReadFile(configPath); err == nil {
		cs.currentConfig = string(data)
	}
	return cs
}

func (c *CaddySync) FetchConfig() (string, error) {
	return c.client.FetchCaddyConfig(c.agentID)
}

func (c *CaddySync) WriteConfig(content string) error {
	dir := filepath.Dir(c.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	if existing, err := os.ReadFile(c.configPath); err == nil && string(existing) == content {
		caddyLog.Info("Config unchanged, skipping write", "path", c.configPath)
		return nil
	}
	if err := os.WriteFile(c.configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

func (c *CaddySync) Sync() error {
	config, err := c.FetchConfig()
	if err != nil {
		return fmt.Errorf("failed to fetch caddy config: %w", err)
	}
	if config == c.currentConfig {
		return nil
	}
	if err := c.WriteConfig(config); err != nil {
		return err
	}
	c.currentConfig = config
	return reloadLocalCaddy(c.envProvider)
}

func EnsureCaddy(envProvider func() map[string]string) error {
	agentCaddyCmdMu.Lock()
	defer agentCaddyCmdMu.Unlock()
	if agentCaddyCmd != nil && agentCaddyCmd.Process != nil {
		return nil
	}
	return startCaddyLocked(envProvider)
}

func startCaddyLocked(envProvider func() map[string]string) error {
	caddyDir := resolveCaddyConfigDir()
	caddyfilePath := filepath.Join(caddyDir, "Caddyfile")
	for _, dir := range []string{caddyDir, filepath.Join(caddyDir, "data"), filepath.Join(caddyDir, "config")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create caddy dir %s: %w", dir, err)
		}
	}
	if _, err := os.Stat(caddyfilePath); os.IsNotExist(err) {
		os.WriteFile(caddyfilePath, []byte("# placeholder\n"), 0644)
	}

	cmd := exec.Command(caddyBinary, "run", "--config", caddyfilePath, "--adapter", "caddyfile")
	cmd.Env = caddyEnv(envProvider)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start caddy: %w", err)
	}
	agentCaddyCmd = cmd
	caddyLog.Info("Caddy started", "pid", cmd.Process.Pid)
	return nil
}

func caddyEnv(envProvider func() map[string]string) []string {
	caddyDir := resolveCaddyConfigDir()
	env := append(sanitizedAgentEnviron(),
		"XDG_DATA_HOME="+filepath.Join(caddyDir, "data"),
		"XDG_CONFIG_HOME="+filepath.Join(caddyDir, "config"),
	)
	if envProvider != nil {
		for key, value := range envProvider() {
			if strings.TrimSpace(value) != "" {
				env = append(env, fmt.Sprintf("%s=%s", key, value))
			}
		}
	}
	return env
}

func StopCaddy() {
	agentCaddyCmdMu.Lock()
	defer agentCaddyCmdMu.Unlock()
	if agentCaddyCmd != nil && agentCaddyCmd.Process != nil {
		_ = agentCaddyCmd.Process.Kill()
		_ = agentCaddyCmd.Wait()
		agentCaddyCmd = nil
	}
}

func IsCaddyRunning() bool {
	agentCaddyCmdMu.Lock()
	defer agentCaddyCmdMu.Unlock()
	return agentCaddyCmd != nil && agentCaddyCmd.Process != nil
}

func reloadLocalCaddy(envProvider func() map[string]string) error {
	agentCaddyCmdMu.Lock()
	defer agentCaddyCmdMu.Unlock()

	if agentCaddyCmd == nil || agentCaddyCmd.Process == nil {
		caddyLog.Debug("Caddy not running yet, reload deferred")
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	caddyDir := resolveCaddyConfigDir()
	caddyfilePath := filepath.Join(caddyDir, "Caddyfile")
	cmd := exec.CommandContext(ctx, caddyBinary, "reload", "--config", caddyfilePath, "--adapter", "caddyfile")
	output, err := cmd.CombinedOutput()
	if err == nil {
		caddyLog.Debug("Caddy reloaded")
		return nil
	}
	msg := strings.TrimSpace(string(output))

	stopCaddyLocked()
	if startErr := startCaddyLocked(envProvider); startErr != nil {
		return fmt.Errorf("caddy reload+restart failed: reload: %w: %s; restart: %w", err, msg, startErr)
	}
	caddyLog.Info("Caddy restarted after reload failure")
	return nil
}

func stopCaddyLocked() {
	if agentCaddyCmd != nil && agentCaddyCmd.Process != nil {
		_ = agentCaddyCmd.Process.Kill()
		_ = agentCaddyCmd.Wait()
		agentCaddyCmd = nil
	}
}

func resolveCaddyConfigDir() string {
	return config.CaddyDir
}
