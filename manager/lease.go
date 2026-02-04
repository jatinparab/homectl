package manager

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// AgentInfo represents a registered agent
type AgentInfo struct {
	Name         string    `json:"name"`
	IP           string    `json:"ip"`
	LastSeen     time.Time `json:"last_seen"`
	RegisteredAt time.Time `json:"registered_at"`
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

// cleanupExpired removes agents that haven't renewed their lease
func (lm *LeaseManager) cleanupExpired() {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	now := time.Now()
	for name, agent := range lm.agents {
		if now.Sub(agent.LastSeen) > lm.leaseTTL {
			log.Printf("Agent %s lease expired (last seen: %v)", name, agent.LastSeen)
			delete(lm.agents, name)
		}
	}
}

// Register registers or renews an agent's lease
func (lm *LeaseManager) Heartbeat(name, ip string) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	now := time.Now()
	if existing, ok := lm.agents[name]; ok {
		existing.LastSeen = now
		existing.IP = ip
		log.Printf("Agent %s renewed lease", name)
	} else {
		lm.agents[name] = &AgentInfo{
			Name:         name,
			IP:           ip,
			LastSeen:     now,
			RegisteredAt: now,
		}
		log.Printf("Agent %s registered (IP: %s)", name, ip)
	}
}

// Unregister removes an agent
func (lm *LeaseManager) Unregister(name string) {
	lm.mu.Lock()
	defer lm.mu.Unlock()

	if _, ok := lm.agents[name]; ok {
		delete(lm.agents, name)
		log.Printf("Agent %s unregistered", name)
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

// HeartbeatRequest is the request body for agent heartbeat
type HeartbeatRequest struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
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

	m.leaseManager.Heartbeat(req.Name, req.IP)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
