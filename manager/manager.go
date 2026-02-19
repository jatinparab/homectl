package manager

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
	"github.com/jatinparab/homectl/config"
)

// Config holds manager configuration
type Config struct {
	RepoDir string
	Port    int
	Lease   LeaseConfig
}

// DefaultConfig returns the default manager configuration
func DefaultConfig() Config {
	return Config{
		Port:  1338,
		Lease: DefaultLeaseConfig(),
	}
}

// Manager handles git sync and file serving for agents
type Manager struct {
	repoDir         string
	stateDir        string
	port            int
	leaseManager    *LeaseManager
	httpClient      *http.Client
	config          *config.Config
	appRegistry     *AppRegistry
	reconcileCancel context.CancelFunc

	backupMu              sync.Mutex
	backupAssignment      *BackupAssignment
	pendingGlobalRestores map[common.AppID]map[string]bool // appID -> agents needing restore

	gatewayNode string
	gatewayMu   sync.Mutex
}

// NewManager creates a new Manager instance with default lease config
func NewManager(repoDir string, port int) *Manager {
	return NewManagerWithConfig(Config{
		RepoDir: repoDir,
		Port:    port,
		Lease:   DefaultLeaseConfig(),
	})
}

// NewManagerWithConfig creates a new Manager instance with custom config
func NewManagerWithConfig(cfg Config) *Manager {
	stateDir := filepath.Join(filepath.Dir(cfg.RepoDir), ".homectl")
	return &Manager{
		repoDir:      cfg.RepoDir,
		stateDir:     stateDir,
		port:         cfg.Port,
		leaseManager: NewLeaseManager(cfg.Lease),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		config:                config.NewConfig(),
		appRegistry:           NewAppRegistry(),
		pendingGlobalRestores: make(map[common.AppID]map[string]bool),
	}
}

// RepoDir returns the repository directory
func (m *Manager) RepoDir() string {
	return m.repoDir
}

// Port returns the server port
func (m *Manager) Port() int {
	return m.port
}

// LeaseManager returns the lease manager
func (m *Manager) LeaseManager() *LeaseManager {
	return m.leaseManager
}

// SetReconcileCancel sets the cancel function for the reconcile loop.
func (m *Manager) SetReconcileCancel(cancel context.CancelFunc) {
	m.reconcileCancel = cancel
}

// Stop stops the manager and cleans up resources
func (m *Manager) Stop() {
	if m.reconcileCancel != nil {
		m.reconcileCancel()
	}
	if m.leaseManager != nil {
		m.leaseManager.Stop()
	}
}

// desiredStatePath returns the path to the persisted desired-state file.
func (m *Manager) desiredStatePath() string {
	return filepath.Join(m.stateDir, "desired-state.json")
}

// loadDesiredState reads the persisted desired-state file.
// Returns nil map if the file doesn't exist (not an error).
func (m *Manager) loadDesiredState() map[string]common.DesiredState {
	data, err := os.ReadFile(m.desiredStatePath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Failed to read desired-state file: %s", err)
		}
		return nil
	}

	var states map[string]common.DesiredState
	if err := json.Unmarshal(data, &states); err != nil {
		log.Printf("Failed to parse desired-state file: %s", err)
		return nil
	}
	return states
}

// saveDesiredState persists the current desired state of all apps.
// Only stores apps whose state differs from the default (stopped).
func (m *Manager) saveDesiredState() {
	runByDefault := map[string]bool{
		"traefik": true,
		"adguard": true,
	}

	allApps := m.appRegistry.ListAll()
	states := make(map[string]common.DesiredState)
	for _, app := range allApps {
		defaultState := common.DesiredStopped
		if runByDefault[string(app.ID)] {
			defaultState = common.DesiredRunning
		}
		if app.DesiredState != defaultState {
			states[string(app.ID)] = app.DesiredState
		}
	}

	if err := os.MkdirAll(m.stateDir, 0755); err != nil {
		log.Printf("Failed to create state dir: %s", err)
		return
	}

	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal desired-state: %s", err)
		return
	}

	if err := os.WriteFile(m.desiredStatePath(), data, 0644); err != nil {
		log.Printf("Failed to write desired-state file: %s", err)
	}
}

// restoreDisabledPath returns the path to the persisted restore-disabled file.
func (m *Manager) restoreDisabledPath() string {
	return filepath.Join(m.stateDir, "restore-disabled.json")
}

// loadRestoreDisabled reads the persisted set of apps with restore disabled.
func (m *Manager) loadRestoreDisabled() map[string]bool {
	data, err := os.ReadFile(m.restoreDisabledPath())
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Failed to read restore-disabled file: %s", err)
		}
		return nil
	}

	var disabled []string
	if err := json.Unmarshal(data, &disabled); err != nil {
		log.Printf("Failed to parse restore-disabled file: %s", err)
		return nil
	}
	result := make(map[string]bool, len(disabled))
	for _, id := range disabled {
		result[id] = true
	}
	return result
}

// saveRestoreDisabled persists which apps have restore disabled.
func (m *Manager) saveRestoreDisabled() {
	allApps := m.appRegistry.ListAll()
	var disabled []string
	for _, app := range allApps {
		if !app.RestoreEnabled {
			disabled = append(disabled, string(app.ID))
		}
	}

	if err := os.MkdirAll(m.stateDir, 0755); err != nil {
		log.Printf("Failed to create state dir: %s", err)
		return
	}

	data, err := json.MarshalIndent(disabled, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal restore-disabled: %s", err)
		return
	}

	if err := os.WriteFile(m.restoreDisabledPath(), data, 0644); err != nil {
		log.Printf("Failed to write restore-disabled file: %s", err)
	}
}

// DiscoverApps scans docker/v2 subdirectories for apps and registers them.
// By default, traefik and adguard have DesiredRunning state, others have DesiredStopped.
// Persisted state from the UI overrides defaults. The Register method is idempotent
// so repeated calls are safe.
func (m *Manager) DiscoverApps() {
	dockerV2Dir := filepath.Join(m.repoDir, "docker", "v2")
	entries, err := os.ReadDir(dockerV2Dir)
	if err != nil {
		log.Printf("Failed to read docker/v2 directory for app discovery: %s", err)
		return
	}

	appConfigs := m.loadAppConfigs()
	persisted := m.loadDesiredState()
	restoreDisabled := m.loadRestoreDisabled()

	count := 0
	runByDefault := map[string]bool{
		"traefik": true,
		"adguard": true,
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		appID := common.AppID(entry.Name())
		desiredState := common.DesiredStopped
		if runByDefault[entry.Name()] {
			desiredState = common.DesiredRunning
		}
		// Override with persisted state if present
		if ps, ok := persisted[entry.Name()]; ok {
			desiredState = ps
		}

		isGlobal := false
		if cfg, ok := appConfigs[entry.Name()]; ok {
			isGlobal = cfg.IsGlobal
		}

		m.appRegistry.Register(appID, desiredState, isGlobal)

		// Apply restore-disabled state
		if restoreDisabled[entry.Name()] {
			m.appRegistry.UpdateRestoreEnabled(appID, false)
		}

		count++
	}
	log.Printf("App discovery: found %d apps in docker/v2", count)
}

