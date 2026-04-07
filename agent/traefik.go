package agent

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/jatinparab/homectl/manager/client"
)

var traefikLog = slog.With("area", "traefik")

// TraefikSync handles syncing traefik config from manager
type TraefikSync struct {
	configPath    string // Path to write traefik.yml
	currentConfig string // Cached current config for diff check
	client        *client.Client
	agentID       string
}

// NewTraefikSync creates a new TraefikSync instance
func NewTraefikSync(appdataDir, agentID string, c *client.Client) *TraefikSync {
	// Config path: {appdataDir}/traefik/config/traefik.yml
	configPath := filepath.Join(appdataDir, "traefik", "config", "traefik.yml")
	ts := &TraefikSync{
		configPath: configPath,
		client:     c,
		agentID:    agentID,
	}
	// Seed currentConfig from disk so the first Sync won't rewrite an identical file
	if data, err := os.ReadFile(configPath); err == nil {
		ts.currentConfig = string(data)
	}
	return ts
}

// FetchConfig fetches traefik config from manager
func (t *TraefikSync) FetchConfig() (string, error) {
	return t.client.FetchTraefikConfig(t.agentID)
}

// WriteConfig writes the config to disk only if the content differs from what
// is already on disk. This avoids unnecessary file-change events that cause
// traefik to reload its config and momentarily return 404s.
func (t *TraefikSync) WriteConfig(content string) error {
	// Ensure parent directory exists
	dir := filepath.Dir(t.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Read existing file and skip write if identical
	if existing, err := os.ReadFile(t.configPath); err == nil && string(existing) == content {
		traefikLog.Info("Config unchanged, skipping write", "path", t.configPath)
		return nil
	}

	// Write config file
	if err := os.WriteFile(t.configPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// Sync fetches config from manager and writes if changed
func (t *TraefikSync) Sync() error {
	config, err := t.FetchConfig()
	if err != nil {
		return fmt.Errorf("failed to fetch traefik config: %w", err)
	}

	// Check if config has changed
	if config == t.currentConfig {
		return nil // No change
	}

	// Write new config
	if err := t.WriteConfig(config); err != nil {
		return err
	}

	t.currentConfig = config
	traefikLog.Info("Config updated", "path", t.configPath)
	return nil
}
