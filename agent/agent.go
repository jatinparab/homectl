package agent

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
	"github.com/jatinparab/homectl/config"
	"github.com/jatinparab/homectl/docker"
	"github.com/jatinparab/homectl/manager/client"
	"github.com/jatinparab/homectl/utils"
)

const (
	// MaxRestarts is the maximum number of restarts before quarantining an app
	MaxRestarts = 5
	// UnhealthyThreshold is the number of consecutive unhealthy checks before triggering a restart
	UnhealthyThreshold = 5
)

// Agent manages docker applications on a host
type Agent struct {
	name          string
	ip            string
	docker        *docker.Docker
	config        *config.Config
	agentConfig   Config
	managerClient *client.Client
	managerURL    string
	dockerRootDir string // Local directory for docker compose files
	appdataDir    string // Local directory for app data
	user          string // User for docker compose commands

	// Runtime state
	runtime      map[common.AppID]*AppRuntime
	assignedApps map[common.AppID]bool // apps assigned to this node by the manager
	runtimeMu    sync.RWMutex

	// Executor, Restic, Traefik, Ready, and Logs
	executor    *Executor
	restic      Restic
	traefikSync *TraefikSync
	readyServer *ReadyServer

	// Gateway state (set by manager)
	isGateway   bool
	isGatewayMu sync.RWMutex

	// Connection state
	connected   bool
	connectedMu sync.RWMutex

	// Background goroutines
	renewalTicker *time.Ticker
	stopChan      chan struct{}
	sensorCtx     context.Context
	sensorCancel  context.CancelFunc
	wg            sync.WaitGroup

	// Backup fields
	backupMu sync.Mutex

	// Repo-wide maintenance (restic check/unlock)
	repoCheck  *RepoOpState
	repoUnlock *RepoOpState

	// Snapshot cache
	snapshotCache     map[common.AppID]*SnapshotCache
	snapshotCancelFns map[common.AppID]context.CancelFunc
	snapshotCacheMu   sync.RWMutex
}

// NewAgent creates a new Agent instance
func NewAgent(name, ip string, cfg *config.Config, agentCfg Config, managerURL, dockerRootDir, appdataDir, user string) *Agent {
	sensorCtx, sensorCancel := context.WithCancel(context.Background())
	a := &Agent{
		name:              name,
		ip:                ip,
		config:            cfg,
		agentConfig:       agentCfg,
		managerURL:        managerURL,
		dockerRootDir:     dockerRootDir,
		appdataDir:        appdataDir,
		user:              user,
		managerClient:     client.NewClient(managerURL),
		runtime:           make(map[common.AppID]*AppRuntime),
		assignedApps:      make(map[common.AppID]bool),
		restic:            NewRepoRestic(appdataDir, name != "Jatins-MacBook-Pro.local"),
		stopChan:          make(chan struct{}),
		sensorCtx:         sensorCtx,
		sensorCancel:      sensorCancel,
		repoCheck:         NewRepoOpState(),
		repoUnlock:        NewRepoOpState(),
		snapshotCache:     make(map[common.AppID]*SnapshotCache),
		snapshotCancelFns: make(map[common.AppID]context.CancelFunc),
	}
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
	ip := utils.GetLocalIP()
	cfg := config.NewConfig()
	hostCfg := cfg.GetAgentConfig(hostname)
	if hostCfg == nil {
		return nil, fmt.Errorf("agent config not found for hostname: %s", hostname)
	}

	managerURL := cfg.GetManagerURL()
	agent := NewAgent(hostname, ip, cfg, agentCfg, managerURL, hostCfg.DockerRootDir, hostCfg.AppdataDir, hostCfg.User)

	log.Printf("Agent initialized: name=%s, dockerRootDir=%s, appdataDir=%s, user=%s",
		hostname, hostCfg.DockerRootDir, hostCfg.AppdataDir, hostCfg.User)

	// Try to connect and load apps (best-effort)
	agent.tryConnect()

	return agent, nil
}

// tryConnect attempts to connect to the manager and sync docker configs
func (a *Agent) tryConnect() {
	if err := a.managerClient.Health(); err != nil {
		log.Printf("Manager not available at %s: %s (will retry)", a.managerURL, err)
		a.setConnected(false)
		return
	}

	if err := a.SyncConfigs(); err != nil {
		log.Printf("Failed to sync configs: %s (will retry)", err)
		a.setConnected(false)
		return
	}

	a.setConnected(true)
	log.Printf("Connected to manager at %s", a.managerURL)
}

// SyncConfigs pulls docker configs from the manager and reloads apps.
func (a *Agent) SyncConfigs() error {
	log.Printf("Syncing docker configs to %s...", a.dockerRootDir)
	if err := a.managerClient.SyncDockerConfigs(a.dockerRootDir); err != nil {
		return fmt.Errorf("sync docker configs: %w", err)
	}
	log.Printf("Docker configs synced successfully")

	// Create Docker instance using local files
	d := docker.NewDocker(a.dockerRootDir, a.appdataDir, a.user)
	d.SetContext(a.sensorCtx)
	if err := d.LoadApps(); err != nil {
		return fmt.Errorf("load apps: %w", err)
	}

	a.docker = d

	// Initialize executor if not already done
	if a.executor == nil {
		a.executor = NewExecutor(a.docker, a.restic, a)
		a.executor.Start()
	} else {
		a.executor.docker = a.docker
	}

	// Initialize traefik sync if not already done
	if a.traefikSync == nil {
		a.traefikSync = NewTraefikSync(a.appdataDir, a.name, a.managerClient)
	}

	return nil
}

// setConnected updates the connection state
func (a *Agent) setConnected(connected bool) {
	a.connectedMu.Lock()
	defer a.connectedMu.Unlock()
	a.connected = connected
}

// IsConnected returns whether the agent is connected to the manager
func (a *Agent) IsConnected() bool {
	a.connectedMu.RLock()
	defer a.connectedMu.RUnlock()
	return a.connected
}

// Start starts all background loops (heartbeat, reconnection, reconcile, sensor, ready server, logs server)
func (a *Agent) Start() {
	a.wg.Add(6)

	go a.heartbeatLoop()
	go a.reconnectLoop()
	go a.reconcileLoop()
	go a.sensorLoop(a.sensorCtx)
	go a.readyServerLoop()
	go a.snapshotCacheLoop()

	log.Printf("Agent started (heartbeat: %v, retry: %v, reconcile: %v)",
		a.agentConfig.RenewalInterval,
		a.agentConfig.RetryInterval,
		a.agentConfig.ReconcileInterval)
}

// readyServerLoop runs the ready server for keepalived health checks
func (a *Agent) readyServerLoop() {
	defer a.wg.Done()

	a.readyServer = NewReadyServer(a)
	if err := a.readyServer.Start(); err != nil {
		log.Printf("Ready server stopped: %s", err)
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
				if err := a.managerClient.Heartbeat(a.name, a.ip, reports); err != nil {
					log.Printf("Heartbeat failed: %s", err)
					a.setConnected(false)
				}
			}
		case <-a.stopChan:
			return
		}
	}
}

// reconnectLoop attempts to reconnect when disconnected.
// May block during connection attempts.
func (a *Agent) reconnectLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(a.agentConfig.RetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !a.IsConnected() {
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
			if a.IsConnected() {
				a.reconcileOnce()
			}
		case <-a.stopChan:
			return
		}
	}
}

// reconcileOnce performs a single reconciliation cycle.
func (a *Agent) reconcileOnce() {
	log.Println("Reconcile: fetching desired apps from manager...")

	// Fetch desired apps from manager
	response, err := a.managerClient.FetchDesiredApps(a.name)
	if err != nil {
		log.Printf("Reconcile: failed to fetch desired apps: %s", err)
		return
	}

	desiredApps := response.Apps

	// Update gateway status from manager
	a.isGatewayMu.Lock()
	a.isGateway = response.IsGateway
	a.isGatewayMu.Unlock()

	log.Printf("Reconcile: got %d desired apps (gateway=%v)", len(desiredApps), response.IsGateway)

	// Track which apps the manager wants running on this node
	assigned := make(map[common.AppID]bool, len(desiredApps))
	for _, da := range desiredApps {
		if da.DesiredState == common.DesiredRunning {
			assigned[da.AppID] = true
		}
	}
	a.runtimeMu.Lock()
	a.assignedApps = assigned
	a.runtimeMu.Unlock()
	a.cancelUnassignedSnapshotRefreshes(assigned)

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
		rt := a.getOrCreateRuntime(app.AppID)
		rt.SetBackupSensitive(app.BackupSensitive)
		rt.SetBackupEnabled(app.BackupEnabled)
		rt.SetRestoreEnabled(app.RestoreEnabled)

		// Skip if action already in flight
		if rt.IsInFlight() {
			// log.Printf("Reconcile: skipping %s (action in flight: %s)", app.AppID, rt.GetInFlightAction())
			continue
		}

		action := a.reconcileApp(app, rt)
		if action.Type != ActionTypeNone {
			a.executor.Submit(action)
		}
	}

	// Sync traefik config
	if a.traefikSync != nil {
		if err := a.traefikSync.Sync(); err != nil {
			log.Printf("Reconcile: traefik sync failed: %s", err)
		}
	}
}

// reconcileApp determines what action to take for an app
func (a *Agent) reconcileApp(app common.DesiredApp, rt *AppRuntime) Action {
	// === BACKUP PHASE HANDLING (takes priority) ===
	phase := rt.GetPhase()

	switch phase {
	case common.PhaseBackupStopping:
		// Waiting for sensor to confirm container stopped
		log.Printf("Reconcile: %s in BackupStopping, waiting for sensor confirmation", app.AppID)
		return ActionNone()

	case common.PhaseBackupStopped:
		// Container confirmed stopped by sensor — emit backup action
		log.Printf("Reconcile: %s in BackupStopped, emitting backup action", app.AppID)
		return ActionBackup(app.AppID)

	case common.PhaseBackingUp:
		// Backup execution in progress
		log.Printf("Reconcile: %s in BackingUp, waiting for completion", app.AppID)
		return ActionNone()
	}

	// === RESTORE (non-gateway global app with stale data) ===
	if app.RestoreNow && app.RestoreEnabled && rt.RunningStatus() == common.RunningStatusRunning {
		log.Printf("Reconcile: %s restore requested, stopping for restore cycle", app.AppID)
		return ActionStop(app.AppID)
	}

	// === NORMAL LIFECYCLE ===

	// Desired stopped
	if app.DesiredState == common.DesiredStopped {
		if rt.RunningStatus() == common.RunningStatusRunning || rt.RunningStatus() == common.RunningStatusRestarting {
			log.Printf("Reconcile: %s should be stopped but is running", app.AppID)
			return ActionStop(app.AppID)
		}
		return ActionNone()
	}

	// Desired running
	if rt.IsQuarantined() {
		log.Printf("Reconcile: %s is quarantined, skipping", app.AppID)
		if rt.RunningStatus() == common.RunningStatusRunning || rt.RunningStatus() == common.RunningStatusRestarting {
			log.Printf("Reconcile: %s is quarantined but is running, stopping", app.AppID)
			return ActionStop(app.AppID)
		}
		return ActionNone()
	}

	if rt.RunningStatus() != common.RunningStatusRunning {
		if a.canRestart(rt) {
			log.Printf("Reconcile: %s should be running but is not", app.AppID)
			return ActionStart(app.AppID)
		}
		log.Printf("Reconcile: %s exceeded restart limit, quarantining", app.AppID)
		return ActionQuarantine(app.AppID)
	}

	// Running but unhealthy — only act after consecutive failures reach the threshold
	if rt.GetHealth() == common.HealthUnhealthy && rt.GetUnhealthyCount() >= UnhealthyThreshold {
		if a.canRestart(rt) {
			log.Printf("Reconcile: %s is unhealthy (%d consecutive checks), restarting", app.AppID, rt.GetUnhealthyCount())
			return ActionRestart(app.AppID)
		}
		log.Printf("Reconcile: %s is unhealthy and exceeded restart limit, quarantining", app.AppID)
		return ActionQuarantine(app.AppID)
	}

	// Running + healthy: check if manager assigned BackupNow
	if app.BackupEnabled && app.BackupNow && rt.RunningStatus() == common.RunningStatusRunning && rt.GetHealth() == common.HealthHealthy && !rt.IsPostProcessing() {
		log.Printf("Reconcile: %s manager requested backup, transitioning to BackupStopping", app.AppID)
		rt.SetPhase(common.PhaseBackupStopping)
		return ActionStop(app.AppID)
	}

	// Running + healthy: check if manager assigned PruneNow
	if app.BackupEnabled && app.PruneNow && rt.RunningStatus() == common.RunningStatusRunning && rt.GetHealth() == common.HealthHealthy && !rt.IsPostProcessing() {
		log.Printf("Reconcile: %s manager requested prune, executing prune", app.AppID)
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
		log.Printf("Snapshot cache refresh failed for %s: %s", appID, err)
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
	log.Println("Stopping agent...")

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
	log.Println("Agent stopped")
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
