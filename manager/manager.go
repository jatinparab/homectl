package manager

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
	"github.com/jatinparab/homectl/config"
)

var managerLog = slog.With("area", "manager")

// InitLoggers reinitializes all package-level loggers after logger.Init() has been called.
// Package-level vars capture slog.Default() at declaration time (before Init), so they must be
// refreshed to pick up the JSON handler.
func InitLoggers() {
	managerLog = slog.Default().With("area", "manager")
	apiLog = slog.Default().With("area", "api")
	appLog = slog.Default().With("area", "app")
	backupMgrLog = slog.Default().With("area", "backup")
	gitLog = slog.Default().With("area", "git")
	leaseLog = slog.Default().With("area", "lease")
	pruneStateLog = slog.Default().With("area", "backup")
	publicRouteLog = slog.Default().With("area", "public-routes")
	reconcileLog = slog.Default().With("area", "reconcile")
	repoMgrLog = slog.Default().With("area", "repo")
	repoStateLog = slog.Default().With("area", "repo")
	serverLog = slog.Default().With("area", "server")
	traefikMgrLog = slog.Default().With("area", "traefik")
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
	stateDir        string
	port            int
	leaseManager    *LeaseManager
	httpClient      *http.Client
	config          *config.Config
	appRegistry     *AppRegistry
	reconcileCancel context.CancelFunc

	backupMu              sync.Mutex
	backupAssignments     map[common.AppID]*BackupAssignment
	pruneAssignment       *BackupAssignment
	lastScheduledWorkKind string
	pendingGlobalRestores map[common.AppID]map[string]bool // appID -> agents needing restore
	pruneState            map[common.AppID]PruneState
	pruneStateSaving      bool
	pruneStateSavePending bool

	// Repo-wide maintenance (restic check/unlock), coordinated via gateway node.
	repoMu                      sync.Mutex
	repoCheckBusy               bool
	repoUnlockBusy              bool
	repoLastCheckAt             time.Time
	repoLastUnlockAt            time.Time
	repoLastCheckErr            string
	repoLastUnlockErr           string
	repoLastStatusSyncAt        time.Time
	repoLastStatusFetchErrLogAt time.Time

	gatewayNode       string
	manualGatewayNode string // User-selected gateway (overrides automatic selection)
	gatewayMu         sync.Mutex

	publicRoutesMu sync.RWMutex
	publicRoutes   []PublicRoute
	routeHealth    map[string]PublicRouteHealth

	linkMu      sync.RWMutex
	agentLinks  map[string]*agentLinkPeer // by agent name
	tcpListener net.Listener
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
	mgr := &Manager{
		repoDir:      cfg.RepoDir,
		stateDir:     stateDir,
		port:         cfg.Port,
		leaseManager: NewLeaseManager(cfg.Lease),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		config:                config.NewConfig(),
		appRegistry:           NewAppRegistry(),
		backupAssignments:     make(map[common.AppID]*BackupAssignment),
		pendingGlobalRestores: make(map[common.AppID]map[string]bool),
		pruneState:            make(map[common.AppID]PruneState),
		routeHealth:           make(map[string]PublicRouteHealth),
		agentLinks:            make(map[string]*agentLinkPeer),
	}
	mgr.loadRepoState()
	mgr.loadPruneState()
	mgr.loadPublicRoutes()
	return mgr
}

// RepoDir returns the repository directory
func (m *Manager) RepoDir() string {
	return m.repoDir
}

// isAgentKnown returns true if the agent is statically configured OR has
// dynamically registered via heartbeat. This allows e2e / containerised
// agents whose names are not in the static config to use all manager APIs.
func (m *Manager) isAgentKnown(nodeName string) bool {
	if m.config.GetAgentConfig(nodeName) != nil {
		return true
	}
	_, ok := m.leaseManager.GetAgent(nodeName)
	return ok
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
	if m.tcpListener != nil {
		_ = m.tcpListener.Close()
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
			managerLog.Error("Failed to read desired-state file", "err", err)
		}
		return nil
	}

	var states map[string]common.DesiredState
	if err := json.Unmarshal(data, &states); err != nil {
		managerLog.Error("Failed to parse desired-state file", "err", err)
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
		managerLog.Error("Failed to create state dir", "err", err)
		return
	}

	data, err := json.MarshalIndent(states, "", "  ")
	if err != nil {
		managerLog.Error("Failed to marshal desired-state", "err", err)
		return
	}

	if err := os.WriteFile(m.desiredStatePath(), data, 0644); err != nil {
		managerLog.Error("Failed to write desired-state file", "err", err)
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
			managerLog.Error("Failed to read restore-disabled file", "err", err)
		}
		return nil
	}

	var disabled []string
	if err := json.Unmarshal(data, &disabled); err != nil {
		managerLog.Error("Failed to parse restore-disabled file", "err", err)
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
		managerLog.Error("Failed to create state dir", "err", err)
		return
	}

	data, err := json.MarshalIndent(disabled, "", "  ")
	if err != nil {
		managerLog.Error("Failed to marshal restore-disabled", "err", err)
		return
	}

	if err := os.WriteFile(m.restoreDisabledPath(), data, 0644); err != nil {
		managerLog.Error("Failed to write restore-disabled file", "err", err)
	}
}

// manualAssignmentsPath returns the path to the persisted manual-assignments file.
func (m *Manager) manualAssignmentsPath() string {
	return filepath.Join(m.stateDir, "manual-assignments.json")
}

// loadManualAssignments reads the persisted manual assignments file.
// Returns a map of appID -> nodeName for apps that were manually assigned.
func (m *Manager) loadManualAssignments() map[string]string {
	data, err := os.ReadFile(m.manualAssignmentsPath())
	if err != nil {
		if !os.IsNotExist(err) {
			managerLog.Error("Failed to read manual-assignments file", "err", err)
		}
		return nil
	}

	var assignments map[string]string
	if err := json.Unmarshal(data, &assignments); err != nil {
		managerLog.Error("Failed to parse manual-assignments file", "err", err)
		return nil
	}
	return assignments
}

// saveManualAssignments persists the current manually-assigned apps.
func (m *Manager) saveManualAssignments() {
	allApps := m.appRegistry.ListAll()
	assignments := make(map[string]string)
	for _, app := range allApps {
		if app.ManuallyAssigned && app.AssignedAgent != nil {
			assignments[string(app.ID)] = *app.AssignedAgent
		}
	}

	if err := os.MkdirAll(m.stateDir, 0755); err != nil {
		managerLog.Error("Failed to create state dir", "err", err)
		return
	}

	data, err := json.MarshalIndent(assignments, "", "  ")
	if err != nil {
		managerLog.Error("Failed to marshal manual-assignments", "err", err)
		return
	}

	if err := os.WriteFile(m.manualAssignmentsPath(), data, 0644); err != nil {
		managerLog.Error("Failed to write manual-assignments file", "err", err)
	}
}

// DiscoverApps scans docker subdirectories for apps and registers them.
// By default, traefik and adguard have DesiredRunning state, others have DesiredStopped.
// Persisted state from the UI overrides defaults. The Register method is idempotent
// so repeated calls are safe.
func (m *Manager) DiscoverApps() {
	dockerDir := filepath.Join(m.repoDir, "docker")
	entries, err := os.ReadDir(dockerDir)
	if err != nil {
		managerLog.Error("Failed to read docker directory for app discovery", "err", err)
		return
	}

	appConfigs := m.loadAppConfigs()
	persisted := m.loadDesiredState()
	restoreDisabled := m.loadRestoreDisabled()
	manualAssignments := m.loadManualAssignments()

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

		// Restore manual assignment
		if node, ok := manualAssignments[entry.Name()]; ok {
			m.appRegistry.ReassignAgent(appID, node)
		}

		count++
	}
	managerLog.Info("App discovery complete", "count", count)
}
