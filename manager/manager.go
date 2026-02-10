package manager

import (
	"log"
	"net/http"
	"sync"
	"time"

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
	repoDir      string
	port         int
	leaseManager *LeaseManager
	httpClient   *http.Client
	config       *config.Config
	backupLease  BackupLease
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
		config: config.NewConfig(),
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

// Stop stops the manager and cleans up resources
func (m *Manager) Stop() {
	if m.leaseManager != nil {
		m.leaseManager.Stop()
	}
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
