package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
	"github.com/jatinparab/homectl/config"
	"github.com/jatinparab/homectl/docker"
	"github.com/jatinparab/homectl/keepalived"
	"github.com/jatinparab/homectl/logger"
	"github.com/jatinparab/homectl/manager/client"
	"github.com/jatinparab/homectl/utils"
	"github.com/jatinparab/homectl/wireguard"
)

const (
	// MaxRestarts is the maximum number of restarts before quarantining an app
	MaxRestarts = 5
	// UnhealthyThreshold is the number of consecutive unhealthy checks before triggering a restart
	UnhealthyThreshold = 5
)

var agentLog = slog.With("area", "agent")

// initPackageLoggers reinitializes all package-level loggers after logger.Init() has been called.
// Package-level vars capture slog.Default() at declaration time (before Init), so they must be
// refreshed to pick up the JSON handler.
func initPackageLoggers() {
	agentLog = slog.Default().With("area", "agent")
	executorLog = slog.Default().With("area", "executor")
	readyLog = slog.Default().With("area", "ready")
	repoLog = slog.Default().With("area", "repo")
	backupLog = slog.Default().With("area", "backup")
	sensorLog = slog.Default().With("area", "sensor")
	caddyLog = slog.Default().With("area", "caddy")
}

// Agent manages docker applications on a host
type Agent struct {
	name                 string
	ip                   string
	docker               *docker.Docker
	config               *config.Config
	agentConfig          Config
	managerClient        *client.Client
	managerURL           string
	bootstrapURL         string
	wireGuardReady       bool
	bootstrapMu          sync.Mutex
	lastBootstrapAttempt time.Time
	lastConnectedAt      time.Time
	dockerRootDir        string // Local directory for docker compose files
	appdataDir           string // Local directory for app data
	appdataHostDir       string // Host path mounted at appdataDir, passed to Docker Compose
	stateDir             string // Local directory for homectl-owned agent state
	composeExtraArgs     []string

	// Runtime state
	runtime      map[common.AppID]*AppRuntime
	assignedApps map[common.AppID]bool // apps assigned to this node by the manager
	runtimeMu    sync.RWMutex

	// Executor, Restic, Caddy, Ready
	executor    *Executor
	restic      Restic
	caddySync   *CaddySync
	readyServer *ReadyServer

	// Gateway state (set by manager)
	isGateway   bool
	isGatewayMu sync.RWMutex

	// Connection state
	connected   bool
	connectedMu sync.RWMutex
	appsReady   bool
	appsReadyMu sync.RWMutex

	// Background goroutines
	renewalTicker *time.Ticker
	stopChan      chan struct{}
	sensorCtx     context.Context
	sensorCancel  context.CancelFunc
	wg            sync.WaitGroup

	// Backup fields
	backupMu      sync.Mutex
	backupCond    *sync.Cond
	activeBackups int
	pruneActive   bool

	// Repo-wide maintenance (restic check/unlock)
	repoCheck  *RepoOpState
	repoUnlock *RepoOpState

	// Snapshot cache
	snapshotCache     map[common.AppID]*SnapshotCache
	snapshotCancelFns map[common.AppID]context.CancelFunc
	snapshotCacheMu   sync.RWMutex

	// Storage prune state
	storagePruneMu    sync.Mutex
	storagePruneState StoragePruneState

	// Runtime config synced from manager
	runtimeConfig   map[string]string
	runtimeConfigMu sync.RWMutex

	// Domain suffix for health check Host header
	domainSuffix string
}

// NewAgent creates a new Agent instance
func NewAgent(name, ip string, cfg *config.Config, agentCfg Config, managerURL, bootstrapURL string, wireGuardReady bool, appdataHostDir string, composeExtraArgs []string) *Agent {
	sensorCtx, sensorCancel := context.WithCancel(context.Background())
	a := &Agent{
		name:              name,
		ip:                ip,
		config:            cfg,
		agentConfig:       agentCfg,
		managerURL:        managerURL,
		bootstrapURL:      bootstrapURL,
		wireGuardReady:    wireGuardReady,
		dockerRootDir:     config.AgentDockerRootDir,
		appdataDir:        config.AppDataDir,
		appdataHostDir:    appdataHostDir,
		stateDir:          config.StateDir,
		composeExtraArgs:  append([]string(nil), composeExtraArgs...),
		managerClient:     client.NewClient(managerURL),
		runtime:           make(map[common.AppID]*AppRuntime),
		assignedApps:      make(map[common.AppID]bool),
		runtimeConfig:     make(map[string]string),
		restic:            nil,
		stopChan:          make(chan struct{}),
		sensorCtx:         sensorCtx,
		sensorCancel:      sensorCancel,
		repoCheck:         NewRepoOpState(),
		repoUnlock:        NewRepoOpState(),
		snapshotCache:     make(map[common.AppID]*SnapshotCache),
		snapshotCancelFns: make(map[common.AppID]context.CancelFunc),
		domainSuffix:      strings.TrimSpace(os.Getenv("HOMECTL_DOMAIN_SUFFIX")),
		lastConnectedAt:   time.Now(),
	}
	a.restic = NewRepoRestic(config.AppDataDir, os.Getenv("HOMECTL_RESTIC_USE_SUDO") == "true", a.configValue)
	a.managerClient.SetIdentity(name, ip)
	a.managerClient.SetJoinToken(strings.TrimSpace(os.Getenv("HOMECTL_JOIN_TOKEN")))
	a.managerClient.SetRequestHandler(a.handleManagerLinkRequest)
	a.backupCond = sync.NewCond(&a.backupMu)
	a.loadRepoOpsState()
	return a
}

// Init initializes an agent (best-effort, doesn't require manager to be up)
func Init() (*Agent, error) {
	return InitWithConfig(DefaultConfig())
}

// InitWithConfig initializes an agent with custom configuration
func InitWithConfig(agentCfg Config) (*Agent, error) {
	hostname := utils.GetHostname()
	logger.Init("agent", hostname)
	initPackageLoggers()
	ip := utils.GetLocalIP()
	cfg := config.NewConfig()
	hostCfg := cfg.GetAgentConfig(hostname)
	if hostCfg == nil {
		return nil, fmt.Errorf("agent config not found for hostname: %s", hostname)
	}
	appdataHostDir, err := resolveSelfBindMountSource(config.AppDataDir)
	if err != nil {
		return nil, err
	}

	managerURL := cfg.GetManagerURL()
	bootstrapURL := os.Getenv("HOMECTL_MANAGER_URL")
	agentName := hostCfg.Name
	wireGuardReady := false
	if tok := strings.TrimSpace(os.Getenv("HOMECTL_JOIN_TOKEN")); tok != "" {
		bootstrap, err := bootstrapWireGuard(agentName, bootstrapURL)
		if err != nil {
			agentLog.Warn("WireGuard bootstrap failed, continuing without overlay", "err", err)
		} else {
			agentName = bootstrap.AgentName
			ip = bootstrap.AgentIP
			managerURL = bootstrap.ManagerURL
			wireGuardReady = true
			hostCfg.IP = ip
		}
	}

	agent := NewAgent(agentName, ip, cfg, agentCfg, managerURL, bootstrapURL, wireGuardReady, appdataHostDir, hostCfg.ComposeExtraArgs)

	agentLog.Info("Agent initialized", "name", agentName, "ip", ip, "dockerRootDir", config.AgentDockerRootDir, "appdataDir", config.AppDataDir, "appdataHostDir", appdataHostDir, "composeExtraArgs", hostCfg.ComposeExtraArgs)

	// Try to connect and load apps (best-effort)
	agent.tryConnect()

	return agent, nil
}

// tryConnect attempts to connect to the manager and sync docker configs.
// TCP connection is independent from docker sync so the agent can heartbeat
// even when the homelab directory hasn't been set up yet.
func (a *Agent) tryConnect() {
	a.retryWireGuardBootstrap()

	if !a.managerClient.IsConnected() {
		if err := a.managerClient.Connect(a.agentConfig.ConnectionTimeout); err != nil {
			agentLog.Info("Manager TCP link unavailable (will retry)", "url", a.managerURL, "err", err)
			a.setConnected(false)
			a.setAppsReady(false)
			return
		}
	}

	if err := a.managerClient.Health(); err != nil {
		agentLog.Info("Manager not available (will retry)", "url", a.managerURL, "err", err)
		a.setConnected(false)
		a.setAppsReady(false)
		return
	}

	if err := a.seedAgentConfigFromEnv(); err != nil {
		agentLog.Info("Agent config env seed failed (will retry)", "err", err)
	}
	if err := a.syncAgentConfig(); err != nil {
		agentLog.Info("Initial agent config sync failed (will retry)", "err", err)
	}
	if !a.managerClient.IsConnected() {
		a.setConnected(false)
		a.setAppsReady(false)
		return
	}

	if !a.IsConnected() {
		a.setConnected(true)
		agentLog.Info("Connected to manager over TCP", "url", a.managerURL)
	}

	if err := a.SyncConfigs(); err != nil {
		agentLog.Info("Failed to sync configs (will retry)", "err", err)
		a.setAppsReady(false)
		return
	}

	a.setAppsReady(true)
	agentLog.Info("Docker configs synced and apps loaded")
}

func (a *Agent) retryWireGuardBootstrap() {
	a.bootstrapMu.Lock()
	defer a.bootstrapMu.Unlock()

	tok := strings.TrimSpace(os.Getenv("HOMECTL_JOIN_TOKEN"))
	if tok == "" || strings.TrimSpace(a.bootstrapURL) == "" {
		return
	}

	if a.wireGuardReady {
		if a.IsConnected() {
			a.lastConnectedAt = time.Now()
			return
		}
		if time.Since(a.lastConnectedAt) < a.agentConfig.BootstrapCooldown {
			return
		}
		if time.Since(a.lastBootstrapAttempt) < a.agentConfig.BootstrapCooldown {
			return
		}
		a.lastBootstrapAttempt = time.Now()
		bootstrap, err := forceBootstrapWireGuard(a.name, a.bootstrapURL)
		if err != nil {
			agentLog.Info("WireGuard re-bootstrap failed (will retry)", "err", err)
			return
		}
		a.applyBootstrap(bootstrap)
		return
	}

	a.lastBootstrapAttempt = time.Now()
	bootstrap, err := bootstrapWireGuard(a.name, a.bootstrapURL)
	if err != nil {
		agentLog.Info("WireGuard bootstrap unavailable (will retry)", "err", err)
		return
	}
	a.applyBootstrap(bootstrap)
}

func (a *Agent) applyBootstrap(bootstrap wireGuardBootstrapResult) {
	a.name = bootstrap.AgentName
	a.ip = bootstrap.AgentIP
	a.managerURL = bootstrap.ManagerURL
	a.wireGuardReady = true
	a.lastConnectedAt = time.Now()
	a.managerClient.SetIdentity(a.name, a.ip)
	a.managerClient.SetBaseURL(a.managerURL)
	agentLog.Info("WireGuard bootstrap succeeded", "name", a.name, "ip", a.ip, "managerURL", a.managerURL)
}

// SyncConfigs pulls docker configs from the manager and reloads apps.
func (a *Agent) SyncConfigs() error {
	agentLog.Info("Syncing docker configs", "dir", a.dockerRootDir)
	if err := a.managerClient.SyncDockerConfigs(a.dockerRootDir); err != nil {
		return fmt.Errorf("sync docker configs: %w", err)
	}
	agentLog.Info("Docker configs synced successfully")

	// Create Docker instance using local files
	d := docker.NewDocker(a.dockerRootDir, a.appdataHostDir)
	d.SetComposeExtraArgs(a.composeExtraArgs)
	d.SetContext(a.sensorCtx)
	if err := d.LoadApps(); err != nil {
		return fmt.Errorf("load apps: %w", err)
	}
	docker.PruneStaleSharedNamespaceContainers()
	if err := docker.ConnectContainerToHomectlNetwork(utils.GetHostname()); err != nil {
		agentLog.Warn("Failed to connect agent to shared Docker network", "err", err)
	}
	if err := d.ConnectLoadedAppsToHomectlNetwork(); err != nil {
		agentLog.Warn("Failed to connect managed apps to shared Docker network", "err", err)
	}

	a.docker = d

	// Initialize executor if not already done
	if a.executor == nil {
		a.executor = NewExecutor(a.docker, a.restic, a)
		a.executor.Start()
	} else {
		a.executor.docker = a.docker
	}

	// Initialize Caddy sync if not already done
	if a.caddySync == nil {
		a.caddySync = NewCaddySync(config.CaddyDir, a.name, a.managerClient, a.caddyRuntimeEnv)
	}

	return nil
}

// setConnected updates the connection state
func (a *Agent) setConnected(connected bool) {
	a.connectedMu.Lock()
	defer a.connectedMu.Unlock()
	a.connected = connected
	if connected {
		a.lastConnectedAt = time.Now()
	}
}

// IsConnected returns whether the agent is connected to the manager via TCP
func (a *Agent) IsConnected() bool {
	a.connectedMu.RLock()
	defer a.connectedMu.RUnlock()
	return a.connected
}

// setAppsReady sets whether docker configs have been synced and apps are loaded.
func (a *Agent) setAppsReady(ready bool) {
	a.appsReadyMu.Lock()
	defer a.appsReadyMu.Unlock()
	a.appsReady = ready
}

// IsAppsReady returns whether docker configs are synced and the executor is initialized.
func (a *Agent) IsAppsReady() bool {
	a.appsReadyMu.RLock()
	defer a.appsReadyMu.RUnlock()
	return a.appsReady
}

// Start starts all background loops.
func (a *Agent) Start() {
	a.wg.Add(7)

	if a.readyServer == nil {
		a.readyServer = NewReadyServer(a)
	}

	go a.heartbeatLoop()
	go a.reconnectLoop()
	go a.reconcileLoop()
	go a.agentConfigSyncLoop()
	go a.sensorLoop(a.sensorCtx)
	go a.snapshotCacheLoop()
	go func() {
		defer a.wg.Done()
		if err := a.readyServer.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			agentLog.Error("Ready server stopped", "err", err)
		}
	}()

	agentLog.Info("Agent started", "heartbeat", a.agentConfig.RenewalInterval, "retry", a.agentConfig.RetryInterval, "reconcile", a.agentConfig.ReconcileInterval)
}

func (a *Agent) agentConfigSyncLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if a.IsConnected() {
				if err := a.syncAgentConfig(); err != nil {
					agentLog.Info("Agent config sync failed", "err", err)
				}
			}
		case <-a.stopChan:
			return
		}
	}
}

func (a *Agent) seedAgentConfigFromEnv() error {
	seed := common.NodeConfigDocument{}
	for _, key := range common.SharedAgentConfigKeys {
		envKey := key
		if key == common.ConfigKeyLocalHAIP {
			envKey = "HOMECTL_LOCAL_HA_IP"
		}
		if val := strings.TrimSpace(os.Getenv(envKey)); val != "" {
			seed[key] = val
		}
	}
	if val := strings.TrimSpace(os.Getenv(common.ConfigKeyAgentName)); val != "" {
		seed[common.ConfigKeyAgentName] = val
	}
	return a.managerClient.SeedAgentConfig(seed)
}

func (a *Agent) syncAgentConfig() error {
	resp, err := a.managerClient.FetchAgentConfig()
	if err != nil {
		return err
	}
	if resp.Version == 0 || len(resp.Config) == 0 {
		return nil
	}
	a.runtimeConfigMu.RLock()
	existingVersion, _ := strconv.ParseInt(a.runtimeConfig["__version"], 10, 64)
	a.runtimeConfigMu.RUnlock()
	if existingVersion >= resp.Version {
		return nil
	}
	var cfg map[string]string
	if err := json.Unmarshal(resp.Config, &cfg); err != nil {
		return err
	}
	changed := a.setRuntimeConfig(resp.Version, cfg)
	if changed {
		a.applyRuntimeConfig()
	}
	return nil
}

func (a *Agent) setRuntimeConfig(version int64, cfg map[string]string) bool {
	a.runtimeConfigMu.Lock()
	defer a.runtimeConfigMu.Unlock()
	changed := false
	for _, key := range append(common.SharedAgentConfigKeys, common.AgentSpecificConfigKeys...) {
		newVal := strings.TrimSpace(cfg[key])
		if a.runtimeConfig[key] != newVal {
			changed = true
		}
		if newVal == "" {
			delete(a.runtimeConfig, key)
		} else {
			a.runtimeConfig[key] = newVal
		}
	}
	a.runtimeConfig["__version"] = strconv.FormatInt(version, 10)
	return changed
}

func (a *Agent) configValue(key string) string {
	a.runtimeConfigMu.RLock()
	value := strings.TrimSpace(a.runtimeConfig[key])
	a.runtimeConfigMu.RUnlock()
	if value != "" {
		return value
	}
	return ""
}

func (a *Agent) runtimeConfigSnapshot() map[string]string {
	a.runtimeConfigMu.RLock()
	defer a.runtimeConfigMu.RUnlock()
	out := make(map[string]string, len(a.runtimeConfig))
	for k, v := range a.runtimeConfig {
		if !strings.HasPrefix(k, "__") {
			out[k] = v
		}
	}
	return out
}

func sanitizedAgentEnviron() []string {
	managed := map[string]bool{"HOMECTL_LOCAL_HA_IP": true}
	for _, key := range common.SharedAgentConfigKeys {
		managed[key] = true
	}
	for _, key := range common.AgentSpecificConfigKeys {
		managed[key] = true
	}
	out := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !managed[key] {
			out = append(out, entry)
		}
	}
	return out
}

func (a *Agent) caddyRuntimeEnv() map[string]string {
	cfg := a.runtimeConfigSnapshot()
	out := map[string]string{}
	for _, key := range []string{common.ConfigKeyCFDNSAPIToken} {
		if val := strings.TrimSpace(cfg[key]); val != "" {
			out[key] = val
		}
	}
	return out
}

func (a *Agent) applyRuntimeConfig() {
	if configuredName := a.configValue(common.ConfigKeyAgentName); configuredName != "" && configuredName != a.name {
		oldName := a.name
		a.name = configuredName
		a.managerClient.SetIdentity(a.name, a.ip)
		a.managerClient.SetBaseURL(a.managerURL)
		a.setConnected(false)
		a.setAppsReady(false)
		if a.caddySync != nil {
			a.caddySync.agentID = a.name
		}
		agentLog.Info("Agent name updated from manager config; reconnecting with new identity", "old", oldName, "new", a.name)
	}
	if haIP := a.configValue(common.ConfigKeyLocalHAIP); haIP != "" {
		a.ensureKeepalived(haIP)
	}
	if IsCaddyRunning() {
		StopCaddy()
		if err := EnsureCaddy(a.caddyRuntimeEnv); err != nil {
			agentLog.Warn("Failed to restart Caddy after config sync", "err", err)
		}
	}
}

func (a *Agent) ensureKeepalived(haIP string) {
	hostConfigRoot, err := resolveSelfBindMountSource(config.ConfigDir)
	if err != nil {
		agentLog.Error("Failed to resolve /config host path for keepalived", "err", err)
		return
	}
	configDir := config.KeepalivedDir
	hostConfigDir := filepath.Join(hostConfigRoot, "keepalived")
	if err := keepalived.Ensure(configDir, hostConfigDir, haIP); err != nil {
		agentLog.Error("Failed to start keepalived container", "err", err)
	} else {
		agentLog.Info("Keepalived container ensured", "vip", haIP)
	}
}

// heartbeatLoop sends periodic heartbeats to the manager.
// This loop must never block on long operations.
func (a *Agent) heartbeatLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(a.agentConfig.RenewalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if a.IsConnected() {
				reports := a.GetAppReports()
				metrics := collectNodeMetrics(a.appdataDir)
				if err := a.managerClient.Heartbeat(a.name, a.ip, reports, metrics); err != nil {
					agentLog.Info("Heartbeat failed", "err", err)
					a.setConnected(false)
				}
			}
		case <-a.stopChan:
			return
		}
	}
}

// reconnectLoop attempts to reconnect when disconnected or when apps are not ready.
// May block during connection attempts.
func (a *Agent) reconnectLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(a.agentConfig.RetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !a.IsConnected() || !a.IsAppsReady() {
				a.tryConnect()
			}
		case <-a.stopChan:
			return
		}
	}
}

// reconcileLoop periodically reconciles desired state with actual state.
// This loop may run long operations without affecting heartbeats.
func (a *Agent) reconcileLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(a.agentConfig.ReconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if a.IsAppsReady() {
				a.reconcileOnce()
			}
		case <-a.stopChan:
			return
		}
	}
}

// reconcileOnce performs a single reconciliation cycle.
func (a *Agent) reconcileOnce() {
	agentLog.Debug("Reconcile: fetching desired apps from manager")

	// Fetch desired apps from manager
	response, err := a.managerClient.FetchDesiredApps(a.name)
	if err != nil {
		agentLog.Info("Reconcile: failed to fetch desired apps", "err", err)
		return
	}

	desiredApps := response.Apps

	// Update gateway status from manager
	a.isGatewayMu.Lock()
	a.isGateway = response.IsGateway
	a.isGatewayMu.Unlock()

	agentLog.Debug("Reconcile: got desired apps", "count", len(desiredApps), "gateway", response.IsGateway)

	// Track which apps are expected on this node (running or explicitly stopped).
	// Always include the gateway proxy (caddy runs in-process).
	assigned := make(map[common.AppID]bool, len(desiredApps)+1)
	assigned[common.GatewayProxyAppID] = true
	for _, da := range desiredApps {
		assigned[da.AppID] = true
	}
	a.runtimeMu.Lock()
	a.assignedApps = assigned
	a.runtimeMu.Unlock()
	a.cancelUnassignedSnapshotRefreshes(assigned)

	// Sync Caddy config before app actions
	if a.caddySync != nil {
		if err := a.caddySync.Sync(); err != nil {
			agentLog.Info("Reconcile: caddy sync failed", "err", err)
		}
	}

	// Ensure caddy is running as a process (not a Docker container).
	// Gracefully skip if caddy is not installed (e.g., e2e environment).
	if _, cerr := exec.LookPath("caddy"); cerr == nil {
		if err := EnsureCaddy(a.caddyRuntimeEnv); err != nil {
			agentLog.Warn("Reconcile: caddy process not running", "err", err)
		}
	}

	// Always report caddy as running+healthy (it runs in-process)
	a.setCaddyRuntimeHealthy()

	managedApps := a.ListApps()
	for _, app := range managedApps {
		found := false
		for _, desiredApp := range desiredApps {
			if desiredApp.AppID == common.AppID(app.Name) {
				found = true
				break
			}
		}
		if !found {
			desiredApps = append(
				desiredApps,
				common.DesiredApp{
					AppID:        common.AppID(app.Name),
					DesiredState: common.DesiredStopped,
				},
			)
		}
	}
	// Reconcile each app
	for _, app := range desiredApps {
		// Skip caddy — it runs in-process, not via Docker Compose
		if app.AppID == common.GatewayProxyAppID {
			continue
		}

		rt := a.getOrCreateRuntime(app.AppID)
		rt.SetBackupSensitive(app.BackupSensitive)
		rt.SetBackupEnabled(app.BackupEnabled)
		rt.SetRestoreEnabled(app.RestoreEnabled)

		// Skip if action already in flight
		if rt.IsInFlight() {
			continue
		}

		action := a.reconcileApp(app, rt)
		if action.Type != ActionTypeNone {
			a.executor.Submit(action)
		}
	}
}

func (a *Agent) setCaddyRuntimeHealthy() {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	rt := a.getOrCreateRuntimeLocked(common.GatewayProxyAppID)
	rt.SetRunning(common.RunningStatusRunning)
	rt.SetHealth(common.HealthHealthy)
	rt.SetPhase(common.PhaseRunning)
}

func (a *Agent) isAppExpected(appID common.AppID) bool {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.assignedApps[appID]
}

// reconcileApp determines what action to take for an app
func (a *Agent) reconcileApp(app common.DesiredApp, rt *AppRuntime) Action {
	// === BACKUP PHASE HANDLING (takes priority) ===
	phase := rt.GetPhase()

	switch phase {
	case common.PhaseBackupStopping:
		// Waiting for sensor to confirm container stopped
		agentLog.Info("Reconcile: in BackupStopping, waiting for sensor confirmation", "app", app.AppID)
		return ActionNone()

	case common.PhaseBackupStopped:
		// Container confirmed stopped by sensor — emit backup action
		agentLog.Info("Reconcile: in BackupStopped, emitting backup action", "app", app.AppID)
		return ActionBackup(app.AppID)

	case common.PhaseBackingUp:
		// Backup execution in progress
		agentLog.Info("Reconcile: in BackingUp, waiting for completion", "app", app.AppID)
		return ActionNone()
	}

	// === RESTORE (non-gateway global app with stale data) ===
	if app.RestoreNow && app.RestoreEnabled && rt.RunningStatus() == common.RunningStatusRunning {
		agentLog.Info("Reconcile: restore requested, stopping for restore cycle", "app", app.AppID)
		return ActionStop(app.AppID)
	}

	// === NORMAL LIFECYCLE ===

	// Desired stopped
	if app.DesiredState == common.DesiredStopped {
		if !a.isAppExpected(app.AppID) {
			if rt.RunningStatus() == common.RunningStatusRunning || rt.RunningStatus() == common.RunningStatusRestarting {
				agentLog.Info("Reconcile: app no longer expected on this node, removing compose resources", "app", app.AppID)
				return ActionDown(app.AppID)
			}
			return ActionNone()
		}
		if rt.RunningStatus() == common.RunningStatusRunning || rt.RunningStatus() == common.RunningStatusRestarting {
			agentLog.Info("Reconcile: should be stopped but is running", "app", app.AppID)
			return ActionStop(app.AppID)
		}
		return ActionNone()
	}

	// Desired running
	if rt.IsQuarantined() {
		agentLog.Debug("Reconcile: is quarantined, skipping", "app", app.AppID)
		if rt.RunningStatus() == common.RunningStatusRunning || rt.RunningStatus() == common.RunningStatusRestarting {
			agentLog.Info("Reconcile: is quarantined but is running, stopping", "app", app.AppID)
			return ActionStop(app.AppID)
		}
		return ActionNone()
	}

	if rt.RunningStatus() != common.RunningStatusRunning {
		if a.canRestart(rt) {
			agentLog.Info("Reconcile: should be running but is not", "app", app.AppID)
			return ActionStart(app.AppID)
		}
		agentLog.Info("Reconcile: exceeded restart limit, quarantining", "app", app.AppID)
		return ActionQuarantine(app.AppID)
	}

	// Running but unhealthy — only act after consecutive failures reach the threshold
	if rt.GetHealth() == common.HealthUnhealthy && rt.GetUnhealthyCount() >= UnhealthyThreshold {
		if a.canRestart(rt) {
			agentLog.Info("Reconcile: is unhealthy, restarting", "app", app.AppID, "unhealthy_count", rt.GetUnhealthyCount())
			return ActionRestart(app.AppID)
		}
		agentLog.Info("Reconcile: is unhealthy and exceeded restart limit, quarantining", "app", app.AppID)
		return ActionQuarantine(app.AppID)
	}

	// Running + healthy: check if manager assigned BackupNow
	if app.BackupEnabled && app.BackupNow && rt.RunningStatus() == common.RunningStatusRunning && rt.GetHealth() == common.HealthHealthy && !rt.IsPostProcessing() {
		agentLog.Info("Reconcile: manager requested backup, transitioning to BackupStopping", "app", app.AppID)
		rt.SetPhase(common.PhaseBackupStopping)
		return ActionStop(app.AppID)
	}

	// Running + healthy: check if manager assigned PruneNow
	if app.BackupEnabled && app.PruneNow && rt.RunningStatus() == common.RunningStatusRunning && rt.GetHealth() == common.HealthHealthy && !rt.IsPostProcessing() {
		agentLog.Info("Reconcile: manager requested prune, executing prune", "app", app.AppID)
		return ActionPrune(app.AppID)
	}

	return ActionNone()
}

// hasActiveTCPConnections checks if there are established TCP connections to the given port.
// Uses `ss` on Linux. Returns false on error (e.g. macOS dev) to allow backup.
func hasActiveTCPConnections(port int) bool {
	cmd := exec.Command("ss", "-tn", "state", "established", fmt.Sprintf("dst :%d", port))
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	// ss outputs a header line; any additional lines indicate active connections
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	return len(lines) > 1
}

// canRestart checks if an app can be restarted
func (a *Agent) canRestart(rt *AppRuntime) bool {
	return rt.GetRestartCount() < MaxRestarts
}

func (a *Agent) beginBackup() {
	a.backupMu.Lock()
	for a.pruneActive {
		a.backupCond.Wait()
	}
	a.activeBackups++
	a.backupMu.Unlock()
}

func (a *Agent) endBackup() {
	a.backupMu.Lock()
	if a.activeBackups > 0 {
		a.activeBackups--
	}
	a.backupCond.Broadcast()
	a.backupMu.Unlock()
}

func (a *Agent) beginExclusiveBackupLane() {
	a.backupMu.Lock()
	for a.pruneActive || a.activeBackups > 0 {
		a.backupCond.Wait()
	}
	a.pruneActive = true
	a.backupMu.Unlock()
}

func (a *Agent) endExclusiveBackupLane() {
	a.backupMu.Lock()
	a.pruneActive = false
	a.backupCond.Broadcast()
	a.backupMu.Unlock()
}

// getRuntime returns the runtime for an app (thread-safe)
func (a *Agent) getRuntime(appID common.AppID) *AppRuntime {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.runtime[appID]
}

// getOrCreateRuntime gets or creates a runtime for an app (thread-safe)
func (a *Agent) getOrCreateRuntime(appID common.AppID) *AppRuntime {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	return a.getOrCreateRuntimeLocked(appID)
}

// snapshotCacheLoop refreshes snapshot caches for all backup-enabled apps every 30 minutes
func (a *Agent) snapshotCacheLoop() {
	defer a.wg.Done()

	// Initial refresh
	a.refreshAllSnapshotCaches()

	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.refreshAllSnapshotCaches()
		case <-a.stopChan:
			return
		}
	}
}

// refreshAllSnapshotCaches refreshes snapshot caches for all backup-enabled apps
func (a *Agent) refreshAllSnapshotCaches() {
	a.runtimeMu.RLock()
	var appIDs []common.AppID
	for appID, rt := range a.runtime {
		if rt.IsBackupEnabled() && a.assignedApps[appID] {
			appIDs = append(appIDs, appID)
		}
	}
	a.runtimeMu.RUnlock()

	for _, appID := range appIDs {
		a.refreshSnapshotCache(appID)
	}
}

func (a *Agent) cancelUnassignedSnapshotRefreshes(assigned map[common.AppID]bool) {
	a.snapshotCacheMu.Lock()
	defer a.snapshotCacheMu.Unlock()
	for appID, cancel := range a.snapshotCancelFns {
		if assigned[appID] {
			continue
		}
		cancel()
		delete(a.snapshotCancelFns, appID)
		if cache, ok := a.snapshotCache[appID]; ok {
			cache.Loading = false
		}
	}
}

// refreshSnapshotCache fetches fresh snapshots from restic and updates the cache.
// Any in-flight refresh for the same app is cancelled first.
func (a *Agent) refreshSnapshotCache(appID common.AppID) {
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel any previous in-flight refresh and mark loading
	a.snapshotCacheMu.Lock()
	if prevCancel, ok := a.snapshotCancelFns[appID]; ok {
		prevCancel()
	}
	a.snapshotCancelFns[appID] = cancel
	existing := a.snapshotCache[appID]
	if existing == nil {
		a.snapshotCache[appID] = &SnapshotCache{Loading: true}
	} else {
		existing.Loading = true
	}
	a.snapshotCacheMu.Unlock()

	snapshots, err := a.restic.ListSnapshots(ctx, appID)

	a.snapshotCacheMu.Lock()
	defer a.snapshotCacheMu.Unlock()

	// If this context was cancelled, another refresh superseded us — don't write stale results
	if ctx.Err() != nil {
		return
	}

	// Clean up cancel func
	delete(a.snapshotCancelFns, appID)

	cache := a.snapshotCache[appID]
	if cache == nil {
		cache = &SnapshotCache{}
		a.snapshotCache[appID] = cache
	}
	cache.Loading = false
	cache.CachedAt = time.Now()
	if err != nil {
		cache.Error = err.Error()
		agentLog.Info("Snapshot cache refresh failed", "app", appID, "err", err)
	} else {
		cache.Snapshots = snapshots
		cache.Error = ""
	}
}

// cancelSnapshotRefresh cancels any in-flight snapshot refresh and resets loading state
func (a *Agent) cancelSnapshotRefresh(appID common.AppID) {
	a.snapshotCacheMu.Lock()
	defer a.snapshotCacheMu.Unlock()
	if cancel, ok := a.snapshotCancelFns[appID]; ok {
		cancel()
		delete(a.snapshotCancelFns, appID)
	}
	if cache, ok := a.snapshotCache[appID]; ok {
		cache.Loading = false
	}
}

// getSnapshotCache returns the cached snapshot data for an app (thread-safe)
func (a *Agent) getSnapshotCache(appID common.AppID) *SnapshotCache {
	a.snapshotCacheMu.RLock()
	defer a.snapshotCacheMu.RUnlock()
	return a.snapshotCache[appID]
}

// Stop stops the agent gracefully
func (a *Agent) Stop() {
	agentLog.Info("Stopping agent")

	// Cancel all in-flight snapshot refreshes
	a.snapshotCacheMu.Lock()
	for appID, cancel := range a.snapshotCancelFns {
		cancel()
		delete(a.snapshotCancelFns, appID)
	}
	a.snapshotCacheMu.Unlock()

	// Stop sensor
	a.sensorCancel()

	// Stop executor
	if a.executor != nil {
		a.executor.Stop()
	}

	// Stop ready server
	if a.readyServer != nil {
		a.readyServer.Stop()
	}

	close(a.stopChan)
	a.wg.Wait()

	// Stop caddy process
	StopCaddy()

	// Teardown WireGuard interface
	wireguard.Teardown()

	agentLog.Info("Agent stopped")
}

// Name returns the agent name
func (a *Agent) Name() string {
	return a.name
}

// IP returns the agent IP
func (a *Agent) IP() string {
	return a.ip
}

// IsGateway returns whether this agent is the current gateway node
func (a *Agent) IsGateway() bool {
	a.isGatewayMu.RLock()
	defer a.isGatewayMu.RUnlock()
	return a.isGateway
}

// Docker returns the docker instance
func (a *Agent) Docker() *docker.Docker {
	return a.docker
}

// Config returns the config instance
func (a *Agent) Config() *config.Config {
	return a.config
}

// ManagerClient returns the manager client
func (a *Agent) ManagerClient() *client.Client {
	return a.managerClient
}

// ListApps returns all discovered docker apps (empty if not connected)
func (a *Agent) ListApps() []docker.DockerApp {
	if a.docker == nil {
		return nil
	}
	return a.docker.ListApps()
}

// GetAppReports returns reports for all tracked apps.
// Idle stopped apps (stopped, no active phase, no error) are omitted to reduce noise.
func (a *Agent) GetAppReports() []common.AppReport {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()

	reports := make([]common.AppReport, 0, len(a.runtime))
	for appID, rt := range a.runtime {
		report := rt.ToReport()
		if !a.assignedApps[appID] {
			continue
		}
		if report.Port > 0 {
			report.ActiveConnections = hasActiveTCPConnections(report.Port)
			if report.ActiveConnections {
				now := time.Now()
				rt.mu.Lock()
				rt.LastActiveAt = now
				rt.mu.Unlock()
				report.LastActiveAt = now
			}
		}
		reports = append(reports, report)
	}
	return reports
}
