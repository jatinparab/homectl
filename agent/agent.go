package agent

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jatinparab/homectl/config"
	"github.com/jatinparab/homectl/docker"
	"github.com/jatinparab/homectl/manager/client"
	"github.com/jatinparab/homectl/utils"
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

	// Connection state
	connected   bool
	connectedMu sync.RWMutex

	// Background goroutines
	renewalTicker *time.Ticker
	stopChan      chan struct{}
	wg            sync.WaitGroup
}

// NewAgent creates a new Agent instance
func NewAgent(name, ip string, cfg *config.Config, agentCfg Config, managerURL string) *Agent {
	return &Agent{
		name:          name,
		ip:            ip,
		config:        cfg,
		agentConfig:   agentCfg,
		managerURL:    managerURL,
		managerClient: client.NewClient(managerURL),
		stopChan:      make(chan struct{}),
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
	agent := NewAgent(hostname, ip, cfg, agentCfg, managerURL)

	// Try to connect and load apps (best-effort)
	agent.tryConnect()

	return agent, nil
}

// tryConnect attempts to connect to the manager and load apps
func (a *Agent) tryConnect() {
	if err := a.managerClient.Health(); err != nil {
		log.Printf("Manager not available at %s: %s (will retry)", a.managerURL, err)
		a.setConnected(false)
		return
	}

	// Create Docker instance that reads from manager
	d := docker.NewDockerWithRemote("docker/v2", a.managerURL)
	if err := d.LoadApps(); err != nil {
		log.Printf("Failed to load apps: %s (will retry)", err)
		a.setConnected(false)
		return
	}

	a.docker = d
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

// Start starts all background loops (heartbeat, reconnection, reconcile)
func (a *Agent) Start() {
	a.wg.Add(3)

	go a.heartbeatLoop()
	go a.reconnectLoop()
	go a.reconcileLoop()

	log.Printf("Agent started (heartbeat: %v, retry: %v, reconcile: %v)",
		a.agentConfig.RenewalInterval,
		a.agentConfig.RetryInterval,
		a.agentConfig.ReconcileInterval)
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
				if err := a.managerClient.Heartbeat(a.name, a.ip); err != nil {
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
	log.Println("Reconcile: fetching apps from manager...")

	apps := a.ListApps()
	if apps == nil {
		log.Println("Reconcile: no apps available")
		return
	}

	log.Printf("Reconcile: found %d apps", len(apps))
	for _, app := range apps {
		log.Printf("  %s: ports %v", app.Name, app.Ports)
	}
}

// Stop stops the agent gracefully
func (a *Agent) Stop() {
	log.Println("Stopping agent...")
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
