package manager

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
)

var leaseLog = slog.With("area", "lease")

// AgentInfo represents a registered agent
type AgentInfo struct {
	Name         string             `json:"name"`
	IP           string             `json:"ip"`
	LastSeen     time.Time          `json:"last_seen"`
	RegisteredAt time.Time          `json:"registered_at"`
	AppReports   []common.AppReport `json:"app_reports,omitempty"`
	Metrics      common.NodeMetrics `json:"metrics,omitempty"`
}

// LeaseManager manages agent leases
type LeaseManager struct {
	agents        map[string]*AgentInfo
	mu            sync.RWMutex
	leaseTTL      time.Duration
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}
}

// LeaseConfig holds lease configuration
type LeaseConfig struct {
	LeaseTTL        time.Duration
	CleanupInterval time.Duration
}

// DefaultLeaseConfig returns the default lease configuration
func DefaultLeaseConfig() LeaseConfig {
	return LeaseConfig{
		LeaseTTL:        30 * time.Second,
		CleanupInterval: 10 * time.Second,
	}
}

// NewLeaseManager creates a new LeaseManager
func NewLeaseManager(config LeaseConfig) *LeaseManager {
	lm := &LeaseManager{
		agents:      make(map[string]*AgentInfo),
		leaseTTL:    config.LeaseTTL,
		stopCleanup: make(chan struct{}),
	}

	// Start background cleanup goroutine
	lm.cleanupTicker = time.NewTicker(config.CleanupInterval)
	go lm.cleanupLoop()

	return lm
}

// cleanupLoop periodically removes expired agents
func (lm *LeaseManager) cleanupLoop() {
	for {
		select {
		case <-lm.cleanupTicker.C:
			lm.cleanupExpired()
		case <-lm.stopCleanup:
			lm.cleanupTicker.Stop()
			return
		}
	}
}

// staleAgentTTL is how long an offline agent's record is kept before deletion.
// This is much longer than leaseTTL so that the reconciler can distinguish
// "briefly offline during a rolling deploy" from "truly gone".
const staleAgentTTL = 24 * time.Hour

// cleanupExpired removes agents that have been offline longer than staleAgentTTL.
// The aliveAgents check in the reconciler uses leaseTTL (30s) to decide liveness;
// this only garbage-collects the record after 24h.
func (lm *LeaseManager) cleanupExpired() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	now := time.Now()
	for name, agent := range lm.agents {
		if now.Sub(agent.LastSeen) > staleAgentTTL {
			leaseLog.Info("Agent stale, removing", "agent", name, "last_seen", agent.LastSeen)
			delete(lm.agents, name)
		}
	}
}

// Register registers or renews an agent's lease
func (lm *LeaseManager) Heartbeat(name, ip string, reports []common.AppReport, metrics common.NodeMetrics) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	now := time.Now()
	if existing, ok := lm.agents[name]; ok {
		existing.LastSeen = now
		existing.IP = ip
		existing.AppReports = reports
		existing.Metrics = metrics
		leaseLog.Info("Agent renewed lease", "agent", name, "reports", len(reports))
	} else {
		lm.agents[name] = &AgentInfo{
			Name:         name,
			IP:           ip,
			LastSeen:     now,
			RegisteredAt: now,
			AppReports:   reports,
			Metrics:      metrics,
		}
		leaseLog.Info("Agent registered", "agent", name, "ip", ip, "reports", len(reports))
	}
}

// Unregister removes an agent
func (lm *LeaseManager) Unregister(name string) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if _, ok := lm.agents[name]; ok {
		delete(lm.agents, name)
		leaseLog.Info("Agent unregistered", "agent", name)
	}
}

// GetAgent returns info for a specific agent
func (lm *LeaseManager) GetAgent(name string) (*AgentInfo, bool) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	agent, ok := lm.agents[name]
	if !ok {
		return nil, false
	}
	// Return a copy to avoid race conditions
	copy := *agent
	return &copy, true
}

// ListAgents returns all active agents
func (lm *LeaseManager) ListAgents() []AgentInfo {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	agents := make([]AgentInfo, 0, len(lm.agents))
	for _, agent := range lm.agents {
		agents = append(agents, *agent)
	}
	return agents
}

// Stop stops the lease manager cleanup goroutine
func (lm *LeaseManager) Stop() {
	close(lm.stopCleanup)
}

// LeaseTTL returns the configured lease TTL used to determine agent liveness.
func (lm *LeaseManager) LeaseTTL() time.Duration {
	return lm.leaseTTL
}

// HeartbeatRequest is the request body for agent heartbeat
type HeartbeatRequest struct {
	Name    string             `json:"name"`
	IP      string             `json:"ip"`
	Reports []common.AppReport `json:"reports,omitempty"`
	Metrics common.NodeMetrics `json:"metrics,omitempty"`
}

// HTTP Handlers

func (m *Manager) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request: %s", err), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	m.leaseManager.Heartbeat(req.Name, req.IP, req.Reports, req.Metrics)

	// Adopt running apps that have no assignment (e.g. after manager restart)
	m.appRegistry.AdoptRunningApps(req.Name, req.Reports)

	// Update app registry with reported state
	now := time.Now()
	for _, report := range req.Reports {
		m.appRegistry.UpdateFromReport(report, req.Name, now)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
