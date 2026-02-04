package agent

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/jatinparab/homectl/manager/client"
)

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
	return &TraefikSync{
		configPath: configPath,
		client:     c,
		agentID:    agentID,
	}
}

// FetchConfig fetches traefik config from manager
func (t *TraefikSync) FetchConfig() (string, error) {
	return t.client.FetchTraefikConfig(t.agentID)
}

// WriteConfig writes the config to disk
func (t *TraefikSync) WriteConfig(content string) error {
	// Ensure parent directory exists
	dir := filepath.Dir(t.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
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
	log.Printf("Traefik config updated at %s", t.configPath)
	return nil
}
