package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jatinparab/homectl/buildinfo"
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
	leaseLog = slog.Default().With("area", "lease")
	pruneStateLog = slog.Default().With("area", "backup")
	publicRouteLog = slog.Default().With("area", "public-routes")
	crowdSecLog = slog.Default().With("area", "crowdsec")
	reconcileLog = slog.Default().With("area", "reconcile")
	repoMgrLog = slog.Default().With("area", "repo")
	repoStateLog = slog.Default().With("area", "repo")
	serverLog = slog.Default().With("area", "server")
	caddyContainerLog = slog.Default().With("area", "caddy-container")
	caddyMgrLog = slog.Default().With("area", "caddy")
	authLog = slog.Default().With("area", "auth")
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

// Manager handles app management and file serving for agents
type Manager struct {
	repoDir         string
	stateDir        string
	port            int
	store           *Store
	auth            *AuthService
	agentConfig     *AgentConfigService
	leaseManager    *LeaseManager
	httpClient      *http.Client
	config          *config.Config
	appRegistry     *AppRegistry
	reconcileCancel context.CancelFunc
	runtimeCtx      context.Context
	runtimeOnce     sync.Once

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

	crowdSecMu     sync.RWMutex
	crowdSecStatus CrowdSecStatus
	crowdSecBans   crowdSecCachedResult[[]CrowdSecDecision]
	crowdSecStats  crowdSecCachedResult[CrowdSecMetrics]

	linkMu      sync.RWMutex
	agentLinks  map[string]*agentLinkPeer // by agent name
	tcpListener net.Listener

	wgCleanupStop chan struct{}
	startedAt     time.Time

	// Rate limiters
	loginRateLimiter      *rateLimiter        // per-IP rate limiter for login
	mutatingActionLimiter *perUserRateLimiter // per-user rate limiter for mutations
}

// NewManager creates a new Manager instance with default lease config.
// Returns an error if any initialization step fails (store, auth, encryption key).
func NewManager(repoDir string, port int) (*Manager, error) {
	return NewManagerWithConfig(Config{
		RepoDir: repoDir,
		Port:    port,
		Lease:   DefaultLeaseConfig(),
	})
}

// NewManagerWithConfig creates a new Manager instance with custom config.
// Returns an error if any initialization step fails.
func NewManagerWithConfig(cfg Config) (*Manager, error) {
	stateDir := config.StateDir
	if cfg.RepoDir != "" && cfg.RepoDir != config.RepoDir {
		stateDir = filepath.Join(cfg.RepoDir, ".homectl")
	}
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
		loginRateLimiter:      newRateLimiter(5, 1*time.Minute),         // 5 login attempts per IP per minute
		mutatingActionLimiter: newPerUserRateLimiter(30, 1*time.Minute), // 30 mutations per user per minute
	}
	store, err := OpenStore(stateDir)
	if err != nil {
		return nil, fmt.Errorf("open store: %w", err)
	}
	authSvc, err := NewAuthService(context.Background(), store.Users, store.Sessions, store.APIKeys)
	if err != nil {
		return nil, fmt.Errorf("init auth service: %w", err)
	}
	// Deliver onboarding token to a dedicated file to avoid plaintext in logs.
	tokenFile := filepath.Join(stateDir, "onboarding-token")
	authSvc.SetTokenDelivered(func(token string) {
		if err := os.WriteFile(tokenFile, []byte(token), 0600); err != nil {
			managerLog.Error("Failed to write onboarding token file", "err", err)
		}
		managerLog.Warn("First user onboarding — the admin token has been written to " + tokenFile +
			". Anyone with access to that file can create the first admin account.")
	})
	agentConfigKey, err := ensureAgentConfigEncryptionKey(stateDir)
	if err != nil {
		return nil, fmt.Errorf("ensure agent config encryption key: %w", err)
	}
	agentConfigSvc, err := NewAgentConfigServiceWithKey(store.AgentConfigs, agentConfigKey)
	if err != nil {
		return nil, fmt.Errorf("init agent config service: %w", err)
	}
	if err := agentConfigSvc.SeedFromEnv(context.Background()); err != nil {
		managerLog.Warn("Failed to seed agent config from environment", "err", err)
	}
	mgr.store = store
	mgr.auth = authSvc
	mgr.agentConfig = agentConfigSvc
	mgr.loadRepoState()
	mgr.loadPruneState()
	mgr.loadPublicRoutes()
	return mgr, nil
}

func (m *Manager) SetRuntimeContext(ctx context.Context) {
	m.runtimeCtx = ctx
}

func (m *Manager) OnboardingRequired() bool {
	return m.auth != nil && m.auth.OnboardingRequired()
}

func (m *Manager) StartRuntimeOnce(ctx context.Context) {
	m.runtimeOnce.Do(func() {
		tokenSet := true
		if _, err := RequireJoinTokenForStartup(); err != nil {
			managerLog.Warn("WireGuard not configured, running without overlay", "err", err)
			tokenSet = false
		}
		if tokenSet {
			if err := m.EnsureWireGuard(); err != nil {
				managerLog.Warn("Failed to start WireGuard overlay (continuing anyway)", "err", err)
			}
		}
		m.leaseManager.Start()
		m.startWireGuardCleanup()
		if err := m.StartAgentTCPServer(); err != nil {
			managerLog.Error("Failed to start agent TCP server", "err", err)
			return
		}
		m.DiscoverApps()
		m.StartReconcileLoop(ctx)
		m.StartCrowdSecBootstrap(ctx)
		m.StartPublicRouteLoop(ctx)
		managerLog.Info("Manager runtime started")
	})
}

func (m *Manager) startWireGuardCleanup() {
	m.startedAt = time.Now()
	m.wgCleanupStop = make(chan struct{})
	go m.wireGuardCleanupLoop()
}

func (m *Manager) wireGuardCleanupLoop() {
	const wgCleanupInterval = 10 * time.Minute
	const startupGracePeriod = 5 * time.Minute

	ticker := time.NewTicker(wgCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if time.Since(m.startedAt) < startupGracePeriod {
				continue
			}
			if err := m.CleanupStaleWireGuardPeers(); err != nil {
				managerLog.Warn("WireGuard peer cleanup failed", "err", err)
			}
		case <-m.wgCleanupStop:
			return
		}
	}
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
	if m.wgCleanupStop != nil {
		close(m.wgCleanupStop)
	}
	if m.reconcileCancel != nil {
		m.reconcileCancel()
	}
	if m.leaseManager != nil {
		m.leaseManager.Stop()
	}
	if m.tcpListener != nil {
		_ = m.tcpListener.Close()
	}
	if m.store != nil {
		_ = m.store.Close()
	}
	m.waitForPruneStateSaves()
	stopCaddy()
	stopCrowdSec()
}

func (m *Manager) waitForPruneStateSaves() {
	deadline := time.Now().Add(2 * time.Second)
	for {
		m.backupMu.Lock()
		done := !m.pruneStateSaving && !m.pruneStateSavePending
		m.backupMu.Unlock()
		if done || time.Now().After(deadline) {
			return
		}
		time.Sleep(10 * time.Millisecond)
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
		string(common.DNSAppID): true,
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

// restoreSnapshotOverridesPath returns the path to the persisted restore snapshot overrides file.
func (m *Manager) restoreSnapshotOverridesPath() string {
	return filepath.Join(m.stateDir, "restore-snapshot-overrides.json")
}

// loadRestoreSnapshotOverrides reads the persisted restore snapshot overrides.
// Returns nil map if the file doesn't exist (not an error).
func (m *Manager) loadRestoreSnapshotOverrides() map[string]common.RestoreSnapshotOverride {
	data, err := os.ReadFile(m.restoreSnapshotOverridesPath())
	if err != nil {
		if !os.IsNotExist(err) {
			managerLog.Error("Failed to read restore-snapshot-overrides file", "err", err)
		}
		return nil
	}

	var overrides map[string]common.RestoreSnapshotOverride
	if err := json.Unmarshal(data, &overrides); err != nil {
		managerLog.Error("Failed to parse restore-snapshot-overrides file", "err", err)
		return nil
	}
	return overrides
}

// saveRestoreSnapshotOverrides persists the current restore snapshot overrides.
// Only stores apps that have an override set.
func (m *Manager) saveRestoreSnapshotOverrides() {
	allApps := m.appRegistry.ListAll()
	overrides := make(map[string]common.RestoreSnapshotOverride)
	for _, app := range allApps {
		if app.RestoreSnapshotOverride != nil {
			overrides[string(app.ID)] = *app.RestoreSnapshotOverride
		}
	}

	if err := os.MkdirAll(m.stateDir, 0755); err != nil {
		managerLog.Error("Failed to create state dir", "err", err)
		return
	}

	data, err := json.MarshalIndent(overrides, "", "  ")
	if err != nil {
		managerLog.Error("Failed to marshal restore-snapshot-overrides", "err", err)
		return
	}

	if err := os.WriteFile(m.restoreSnapshotOverridesPath(), data, 0644); err != nil {
		managerLog.Error("Failed to write restore-snapshot-overrides file", "err", err)
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
// By default, caddy and Technitium DNS have DesiredRunning state; other apps are stopped.
// Persisted state from the UI overrides defaults. The Register method is idempotent
// so repeated calls are safe.
func (m *Manager) DiscoverApps() {
	// Always register the built-in gateway proxy (caddy runs in-process on each agent).
	m.appRegistry.Register(common.GatewayProxyAppID, common.DesiredRunning, true)

	dockerDir := filepath.Join(m.repoDir, "docker")
	entries, err := os.ReadDir(dockerDir)
	if err != nil {
		managerLog.Error("Failed to read docker directory for app discovery", "err", err)
		return
	}

	// Build include list from entries already read to avoid a second directory scan
	includeApps := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			includeApps = append(includeApps, entry.Name())
		}
	}

	appConfigs, err := m.LoadAppConfigs(includeApps, false)
	if err != nil {
		managerLog.Error("Failed to load app configs; using defaults for all apps",
			"err", err)
	}

	persisted := m.loadDesiredState()
	restoreDisabled := m.loadRestoreDisabled()
	restoreOverrides := m.loadRestoreSnapshotOverrides()
	manualAssignments := m.loadManualAssignments()

	count := 0
	runByDefault := map[string]bool{
		string(common.DNSAppID): true,
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == string(common.DNSAppID) {
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
		// If config loading failed, isGlobal defaults to false (safe default)

		m.appRegistry.Register(appID, desiredState, isGlobal)

		// Apply restore-disabled state
		if restoreDisabled[entry.Name()] {
			m.appRegistry.UpdateRestoreEnabled(appID, false)
		}

		// Restore restore snapshot override
		if override, ok := restoreOverrides[entry.Name()]; ok {
			_ = m.appRegistry.SetRestoreSnapshotOverride(appID, override)
		}

		// Restore manual assignment
		if node, ok := manualAssignments[entry.Name()]; ok {
			m.appRegistry.ReassignAgent(appID, node)
		}

		count++
	}

	technitiumState := common.DesiredRunning
	if state, ok := persisted[string(common.DNSAppID)]; ok {
		technitiumState = state
	}
	m.appRegistry.Register(common.DNSAppID, technitiumState, true)
	count++
	managerLog.Info("App discovery complete", "count", count)
}

// versionInfo returns the manager's build-time version info.
func (m *Manager) versionInfo() buildinfo.Info {
	return buildinfo.ForComponent("manager")
}
