package manager

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jatinparab/homectl/common"
)

// DashboardResponse contains aggregated stats for the dashboard
type DashboardResponse struct {
	TotalAgents     int `json:"total_agents"`
	AgentsOnline    int `json:"agents_online"`
	TotalApps       int `json:"total_apps"`
	AppsRunning     int `json:"apps_running"`
	AppsStopped     int `json:"apps_stopped"`
	AppsError       int `json:"apps_error"`
	AppsQuarantined int `json:"apps_quarantined"`
}

// APIAgentInfo is the API response format for an agent
type APIAgentInfo struct {
	Name         string             `json:"name"`
	IP           string             `json:"ip"`
	LastSeen     string             `json:"last_seen"`
	RegisteredAt string             `json:"registered_at"`
	Online       bool               `json:"online"`
	AppReports   []common.AppReport `json:"app_reports"`
}

// handleAPIDashboard returns aggregated stats for the dashboard
// GET /api/v1/dashboard
func (m *Manager) handleAPIDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agents := m.leaseManager.ListAgents()
	now := time.Now()

	response := DashboardResponse{}
	response.TotalAgents = len(agents)

	for _, agent := range agents {
		// Agent is online if last seen within 30 seconds
		if now.Sub(agent.LastSeen) < 30*time.Second {
			response.AgentsOnline++
		}

		for _, app := range agent.AppReports {
			response.TotalApps++

			switch app.ObservedState {
			case common.ObservedRunning:
				response.AppsRunning++
			case common.ObservedStopped:
				response.AppsStopped++
			case common.ObservedError:
				response.AppsError++
			}

			if app.Quarantined {
				response.AppsQuarantined++
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleAPIAgents returns all agents with their app reports
// GET /api/v1/agents
func (m *Manager) handleAPIAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agents := m.leaseManager.ListAgents()
	now := time.Now()

	apiAgents := make([]APIAgentInfo, 0, len(agents))
	for _, agent := range agents {
		appReports := agent.AppReports
		if appReports == nil {
			appReports = []common.AppReport{}
		}

		apiAgents = append(apiAgents, APIAgentInfo{
			Name:         agent.Name,
			IP:           agent.IP,
			LastSeen:     agent.LastSeen.Format(time.RFC3339),
			RegisteredAt: agent.RegisteredAt.Format(time.RFC3339),
			Online:       now.Sub(agent.LastSeen) < 30*time.Second,
			AppReports:   appReports,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(apiAgents)
}

// handleAPIAgentDetail returns a single agent by ID
// GET /api/v1/agents/{id}
func (m *Manager) handleAPIAgentDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract agent ID from path: /api/v1/agents/{id}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/agents/")
	agentID := strings.TrimSuffix(path, "/")

	if agentID == "" {
		http.Error(w, "Agent ID required", http.StatusBadRequest)
		return
	}

	agent, ok := m.leaseManager.GetAgent(agentID)
	if !ok {
		http.Error(w, "Agent not found", http.StatusNotFound)
		return
	}

	now := time.Now()
	appReports := agent.AppReports
	if appReports == nil {
		appReports = []common.AppReport{}
	}

	apiAgent := APIAgentInfo{
		Name:         agent.Name,
		IP:           agent.IP,
		LastSeen:     agent.LastSeen.Format(time.RFC3339),
		RegisteredAt: agent.RegisteredAt.Format(time.RFC3339),
		Online:       now.Sub(agent.LastSeen) < 30*time.Second,
		AppReports:   appReports,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(apiAgent)
}

// NodeReport represents a single node's report for an app
type NodeReport struct {
	Node          string `json:"node"`
	NodeOnline    bool   `json:"node_online"`
	ObservedState string `json:"observed_state"`
	Health        string `json:"health"`
	Phase         string `json:"phase"`
	LastError     string `json:"last_error"`
	RestartCount  int    `json:"restart_count"`
	Quarantined   bool   `json:"quarantined"`
	Port          int    `json:"port"`
}

// AppStatus combines desired state with observed state for UI display
type AppStatus struct {
	AppID         string       `json:"app_id"`
	DesiredState  string       `json:"desired_state"`
	ObservedState string       `json:"observed_state"` // Consolidated (worst case)
	Health        string       `json:"health"`         // Consolidated (worst case)
	Phase         string       `json:"phase"`          // Consolidated
	NodeReports   []NodeReport `json:"node_reports"`   // All node reports
}

// consolidateObservedState returns the worst observed state (error > restarting > stopped > unknown > running)
func consolidateObservedState(states []string) string {
	if len(states) == 0 {
		return "unknown"
	}
	priority := map[string]int{
		"error":      0,
		"restarting": 1,
		"stopped":    2,
		"unknown":    3,
		"running":    4,
	}
	// Start with best case, let loop find worst
	worst := "running"
	worstPriority := priority["running"]
	for _, state := range states {
		if p, ok := priority[state]; ok && p < worstPriority {
			worst = state
			worstPriority = p
		}
	}
	return worst
}

// consolidateHealth returns the worst health (unhealthy > unknown > healthy)
func consolidateHealth(healths []string) string {
	if len(healths) == 0 {
		return "unknown"
	}
	priority := map[string]int{
		"unhealthy": 0,
		"unknown":   1,
		"healthy":   2,
	}
	// Start with best case, let loop find worst
	worst := "healthy"
	worstPriority := priority["healthy"]
	for _, health := range healths {
		if p, ok := priority[health]; ok && p < worstPriority {
			worst = health
			worstPriority = p
		}
	}
	return worst
}

// consolidatePhase returns the worst phase (error > any other)
func consolidatePhase(phases []string) string {
	for _, phase := range phases {
		if phase == "error" {
			return "error"
		}
	}
	if len(phases) > 0 {
		return phases[0]
	}
	return ""
}

// handleAPIApps returns an app-centric view merging desired apps with observed state
// GET /api/v1/apps
func (m *Manager) handleAPIApps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get desired apps (hardcoded for now, same as handleDesiredApps)
	desiredApps := []common.DesiredApp{
		{AppID: "adguard", DesiredState: common.DesiredRunning},
		{AppID: "stash", DesiredState: common.DesiredRunning},
		{AppID: "traefik", DesiredState: common.DesiredRunning},
		{AppID: "immich", DesiredState: common.DesiredRunning},
		{AppID: "authentik", DesiredState: common.DesiredRunning},
		{AppID: "vaultwarden", DesiredState: common.DesiredRunning},
		{AppID: "budget", DesiredState: common.DesiredRunning},
		{AppID: "newt", DesiredState: common.DesiredRunning},
		{AppID: "jellyfin", DesiredState: common.DesiredRunning},
	}

	// Get all agents with their app reports
	agents := m.leaseManager.ListAgents()
	now := time.Now()

	// Build a map of app_id -> list of NodeReports for multi-node support
	appReportsMap := make(map[string][]NodeReport)

	for _, agent := range agents {
		agentOnline := now.Sub(agent.LastSeen) < 30*time.Second
		for _, report := range agent.AppReports {
			appID := string(report.AppID)
			nodeReport := NodeReport{
				Node:          agent.Name,
				NodeOnline:    agentOnline,
				ObservedState: string(report.ObservedState),
				Health:        string(report.Health),
				Phase:         string(report.Phase),
				LastError:     report.LastError,
				RestartCount:  report.RestartCount,
				Quarantined:   report.Quarantined,
				Port:          report.Port,
			}
			appReportsMap[appID] = append(appReportsMap[appID], nodeReport)
		}
	}

	// Build AppStatus list by merging desired with observed
	result := make([]AppStatus, 0, len(desiredApps))
	for _, desired := range desiredApps {
		appID := string(desired.AppID)
		nodeReports := appReportsMap[appID]
		if nodeReports == nil {
			nodeReports = []NodeReport{}
		}

		// Collect states/healths/phases for consolidation
		var states, healths, phases []string
		for _, nr := range nodeReports {
			states = append(states, nr.ObservedState)
			healths = append(healths, nr.Health)
			if nr.Phase != "" {
				phases = append(phases, nr.Phase)
			}
		}

		status := AppStatus{
			AppID:         appID,
			DesiredState:  string(desired.DesiredState),
			ObservedState: consolidateObservedState(states),
			Health:        consolidateHealth(healths),
			Phase:         consolidatePhase(phases),
			NodeReports:   nodeReports,
		}

		result = append(result, status)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleAPIAppLogs streams logs from an agent for a specific app via SSE
// GET /api/v1/apps/{appId}/logs?node={nodeName}
func (m *Manager) handleAPIAppLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract appId from path: /api/v1/apps/{appId}/logs
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/apps/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 || parts[1] != "logs" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	appID := parts[0]

	// Get node from query params
	nodeName := r.URL.Query().Get("node")
	if nodeName == "" {
		http.Error(w, "node query parameter is required", http.StatusBadRequest)
		return
	}

	// Find the agent
	agent, ok := m.leaseManager.GetAgent(nodeName)
	if !ok {
		http.Error(w, "Node not found", http.StatusNotFound)
		return
	}

	// Check if agent is online
	if time.Since(agent.LastSeen) > 30*time.Second {
		http.Error(w, "Node is offline", http.StatusServiceUnavailable)
		return
	}

	// Set up SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Proxy logs from agent
	agentLogsURL := "http://" + agent.IP + ":8081/logs/" + appID
	resp, err := m.httpClient.Get(agentLogsURL)
	if err != nil {
		// Send error as SSE event
		w.Write([]byte("data: Error connecting to agent: " + err.Error() + "\n\n"))
		flusher.Flush()
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		w.Write([]byte("data: Agent returned error: " + resp.Status + "\n\n"))
		flusher.Flush()
		return
	}

	// Stream the response from agent to client
	buf := make([]byte, 4096)
	for {
		select {
		case <-r.Context().Done():
			return
		default:
			n, err := resp.Body.Read(buf)
			if n > 0 {
				// Parse lines and send as SSE events
				lines := strings.Split(string(buf[:n]), "\n")
				for _, line := range lines {
					if line != "" {
						w.Write([]byte("data: " + line + "\n\n"))
					}
				}
				flusher.Flush()
			}
			if err != nil {
				return
			}
		}
	}
}
