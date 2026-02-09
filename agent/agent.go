package agent

import (
	"context"
	"fmt"
	"log"
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
	MaxRestarts = 2
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
	runtime   map[common.AppID]*AppRuntime
	runtimeMu sync.RWMutex

	// Executor, Restic, Traefik, Ready, and Logs
	executor    *Executor
	restic      Restic
	traefikSync *TraefikSync
	readyServer *ReadyServer
	logsServer  *LogsServer

	// Connection state
	connected   bool
	connectedMu sync.RWMutex

	// Background goroutines
	renewalTicker *time.Ticker
	stopChan      chan struct{}
	sensorCtx     context.Context
	sensorCancel  context.CancelFunc
	wg            sync.WaitGroup
}

// NewAgent creates a new Agent instance
func NewAgent(name, ip string, cfg *config.Config, agentCfg Config, managerURL, dockerRootDir, appdataDir, user string) *Agent {
	sensorCtx, sensorCancel := context.WithCancel(context.Background())
	return &Agent{
		name:          name,
		ip:            ip,
		config:        cfg,
		agentConfig:   agentCfg,
		managerURL:    managerURL,
		dockerRootDir: dockerRootDir,
		appdataDir:    appdataDir,
		user:          user,
		managerClient: client.NewClient(managerURL),
		runtime:       make(map[common.AppID]*AppRuntime),
		restic:        NewDummyRestic(),
		stopChan:      make(chan struct{}),
		sensorCtx:     sensorCtx,
		sensorCancel:  sensorCancel,
	}
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

	// Sync docker configs from manager to local directory
	log.Printf("Syncing docker configs to %s...", a.dockerRootDir)
	if err := a.managerClient.SyncDockerConfigs(a.dockerRootDir); err != nil {
		log.Printf("Failed to sync docker configs: %s (will retry)", err)
		a.setConnected(false)
		return
	}
	log.Printf("Docker configs synced successfully")

	// Create Docker instance using local files
	d := docker.NewDocker(a.dockerRootDir, a.appdataDir, a.user)
	d.SetContext(a.sensorCtx) // Use agent's context for cancellation
	if err := d.LoadApps(); err != nil {
		log.Printf("Failed to load apps: %s (will retry)", err)
		a.setConnected(false)
		return
	}

	a.docker = d

	// Initialize executor if not already done
	if a.executor == nil {
		a.executor = NewExecutor(a.docker, a.restic, a)
		a.executor.Start()
	} else {
		// Update docker reference if reconnecting
		a.executor.docker = a.docker
	}

	// Initialize traefik sync if not already done
	if a.traefikSync == nil {
		a.traefikSync = NewTraefikSync(a.appdataDir, a.name, a.managerClient)
	}

	a.setConnected(true)
	log.Printf("Connected to manager at %s", a.managerURL)
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
	go a.logsServerLoop()

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

// logsServerLoop runs the logs server for log streaming
func (a *Agent) logsServerLoop() {
	defer a.wg.Done()

	a.logsServer = NewLogsServer(a)
	if err := a.logsServer.Start(); err != nil {
		log.Printf("Logs server stopped: %s", err)
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
	desiredApps, err := a.managerClient.FetchDesiredApps(a.name)
	if err != nil {
		log.Printf("Reconcile: failed to fetch desired apps: %s", err)
		return
	}

	log.Printf("Reconcile: got %d desired apps", len(desiredApps))
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

		// Skip if action already in flight
		if rt.IsInFlight() {
			log.Printf("Reconcile: skipping %s (action in flight)", app.AppID)
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

	// Running but unhealthy
	if rt.GetHealth() == common.HealthUnhealthy {
		if a.canRestart(rt) {
			log.Printf("Reconcile: %s is unhealthy, restarting", app.AppID)
			return ActionRestart(app.AppID)
		}
		log.Printf("Reconcile: %s is unhealthy and exceeded restart limit, quarantining", app.AppID)
		return ActionQuarantine(app.AppID)
	}

	return ActionNone()
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

// Stop stops the agent gracefully
func (a *Agent) Stop() {
	log.Println("Stopping agent...")

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

	// Stop logs server
	if a.logsServer != nil {
		a.logsServer.Stop()
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

// GetAppReports returns reports for all tracked apps
func (a *Agent) GetAppReports() []common.AppReport {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()

	reports := make([]common.AppReport, 0, len(a.runtime))
	for _, rt := range a.runtime {
		reports = append(reports, rt.ToReport())
	}
	return reports
}
