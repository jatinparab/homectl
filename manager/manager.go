package manager

import (
	"net/http"
	"time"
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
	repoDir      string
	port         int
	leaseManager *LeaseManager
	httpClient   *http.Client
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
