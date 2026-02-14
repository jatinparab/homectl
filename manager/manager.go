package manager

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
	"github.com/jatinparab/homectl/config"
)

const BackupLeaseDuration = 15 * time.Minute

// BackupLease represents the global backup lease
type BackupLease struct {
	mu        sync.Mutex
	Holder    string
	ExpiresAt time.Time
}

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
	port            int
	leaseManager    *LeaseManager
	httpClient      *http.Client
	config          *config.Config
	backupLease     BackupLease
	appRegistry     *AppRegistry
	reconcileCancel context.CancelFunc
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
	return &Manager{
		repoDir:      cfg.RepoDir,
		port:         cfg.Port,
		leaseManager: NewLeaseManager(cfg.Lease),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		config:      config.NewConfig(),
		appRegistry: NewAppRegistry(),
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

// DiscoverApps scans docker/v2 subdirectories for apps and registers them.
// By default, traefik and adguard have DesiredRunning state, others have DesiredStopped.
// The Register method is idempotent so repeated calls are safe.
func (m *Manager) DiscoverApps() {
	dockerV2Dir := filepath.Join(m.repoDir, "docker", "v2")
	entries, err := os.ReadDir(dockerV2Dir)
	if err != nil {
		log.Printf("Failed to read docker/v2 directory for app discovery: %s", err)
		return
	}

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

		m.appRegistry.Register(appID, desiredState)
		count++
	}
	log.Printf("App discovery: found %d apps in docker/v2", count)
}

// TryAcquireBackupLease attempts to grant the backup lease to an agent.
// Returns true if the lease was granted (or already held by this agent).
func (m *Manager) TryAcquireBackupLease(agentID string) bool {
	m.backupLease.mu.Lock()
	defer m.backupLease.mu.Unlock()

	now := time.Now()

	// Check if existing lease has expired
	if m.backupLease.Holder != "" && now.After(m.backupLease.ExpiresAt) {
		log.Printf("Backup lease expired (was held by %s)", m.backupLease.Holder)
		m.backupLease.Holder = ""
	}

	// Free → grant
	if m.backupLease.Holder == "" {
		m.backupLease.Holder = agentID
		m.backupLease.ExpiresAt = now.Add(BackupLeaseDuration)
		log.Printf("Backup lease granted to %s (expires %s)", agentID, m.backupLease.ExpiresAt.Format(time.RFC3339))
		return true
	}

	// Already held by this agent → extend
	if m.backupLease.Holder == agentID {
		m.backupLease.ExpiresAt = now.Add(BackupLeaseDuration)
		return true
	}

	// Held by another agent
	return false
}

// ReleaseBackupLease releases the backup lease if held by the specified agent.
func (m *Manager) ReleaseBackupLease(agentID string) {
	m.backupLease.mu.Lock()
	defer m.backupLease.mu.Unlock()

	if m.backupLease.Holder == agentID {
		log.Printf("Backup lease released by %s", agentID)
		m.backupLease.Holder = ""
		m.backupLease.ExpiresAt = time.Time{}
	}
}

// ExtendBackupLeaseIfHolder extends the lease if the agent is the current holder.
func (m *Manager) ExtendBackupLeaseIfHolder(agentID string) {
	m.backupLease.mu.Lock()
	defer m.backupLease.mu.Unlock()

	if m.backupLease.Holder == agentID {
		m.backupLease.ExpiresAt = time.Now().Add(BackupLeaseDuration)
	}
}
