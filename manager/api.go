package manager

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	agentpkg "github.com/jatinparab/homectl/agent"
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

// handleAPIAppRoutes dispatches /api/v1/apps/{appId}/... sub-routes
func (m *Manager) handleAPIAppRoutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/apps/")
	parts := strings.SplitN(path, "/", 2)

	if len(parts) < 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	switch parts[1] {
	case "logs":
		m.handleAPIAppLogs(w, r)
	case "backup":
		m.handleAPITriggerBackup(w, r)
	default:
		http.Error(w, "Unknown action", http.StatusNotFound)
	}
}

// handleAPITriggerBackup proxies a backup trigger request to the appropriate agent
// POST /api/v1/apps/{appId}/backup?node={nodeName}
func (m *Manager) handleAPITriggerBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract appId from path
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/apps/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	appID := parts[0]

	nodeName := r.URL.Query().Get("node")
	if nodeName == "" {
		http.Error(w, "node query parameter is required", http.StatusBadRequest)
		return
	}

	agentCfg := m.config.GetAgentConfig(nodeName)
	if agentCfg == nil {
		http.Error(w, "Node not found in config", http.StatusNotFound)
		return
	}

	// Check if agent is online
	agent, ok := m.leaseManager.GetAgent(nodeName)
	if !ok || time.Since(agent.LastSeen) > 30*time.Second {
		http.Error(w, "Node is offline", http.StatusServiceUnavailable)
		return
	}

	// Forward to agent's ready server
	agentURL := fmt.Sprintf("http://%s:%d/backup/%s", agentCfg.IP, agentpkg.ReadyServerPort, appID)
	req, err := http.NewRequest(http.MethodPost, agentURL, nil)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		http.Error(w, "Failed to reach agent: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
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

	// Look up agent IP from config
	agentCfg := m.config.GetAgentConfig(nodeName)
	if agentCfg == nil {
		http.Error(w, "Node not found in config", http.StatusNotFound)
		return
	}

	// Check if agent is online
	agent, ok := m.leaseManager.GetAgent(nodeName)
	if !ok || time.Since(agent.LastSeen) > 30*time.Second {
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

	// Proxy logs from agent using config IP
	agentLogsURL := fmt.Sprintf("http://%s:%d/logs/%s", agentCfg.IP, agentpkg.ReadyServerPort, appID)
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

// handleAPISystemLogSources returns a list of available system log sources
// GET /api/v1/system-logs
func (m *Manager) handleAPISystemLogSources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agents := m.leaseManager.ListAgents()
	now := time.Now()

	sources := []string{"manager"}
	for _, a := range agents {
		if now.Sub(a.LastSeen) < 30*time.Second {
			sources = append(sources, a.Name)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(sources)
}

// handleAPISystemLogStream streams system logs from a source as SSE
// GET /api/v1/system-logs/{source}
func (m *Manager) handleAPISystemLogStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	source := strings.TrimPrefix(r.URL.Path, "/api/v1/system-logs/")
	source = strings.TrimSuffix(source, "/")

	if source == "" {
		http.Error(w, "Source required", http.StatusBadRequest)
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

	if source == "manager" {
		// Run journalctl locally for manager
		cmd := exec.CommandContext(r.Context(), "journalctl", "-u", "homectl-manager", "-f", "--output=short-iso", "-n", "100")
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			w.Write([]byte("data: Error creating pipe: " + err.Error() + "\n\n"))
			flusher.Flush()
			return
		}

		if err := cmd.Start(); err != nil {
			w.Write([]byte("data: Error starting journalctl: " + err.Error() + "\n\n"))
			flusher.Flush()
			return
		}

		done := make(chan struct{})
		go func() {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				w.Write([]byte("data: " + scanner.Text() + "\n\n"))
				flusher.Flush()
			}
			close(done)
		}()

		select {
		case <-r.Context().Done():
			cmd.Process.Kill()
		case <-done:
		}
		cmd.Wait()
		return
	}

	// Proxy to agent
	agentCfg := m.config.GetAgentConfig(source)
	if agentCfg == nil {
		w.Write([]byte("data: Error: source not found in config\n\n"))
		flusher.Flush()
		return
	}

	agentURL := fmt.Sprintf("http://%s:%d/system-logs", agentCfg.IP, agentpkg.ReadyServerPort)
	resp, err := m.httpClient.Get(agentURL)
	if err != nil {
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

	buf := make([]byte, 4096)
	for {
		select {
		case <-r.Context().Done():
			return
		default:
			n, err := resp.Body.Read(buf)
			if n > 0 {
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

// NodeContainers represents containers from a single node
type NodeContainers struct {
	Node       string                   `json:"node"`
	Online     bool                     `json:"online"`
	Containers []agentpkg.ContainerInfo `json:"containers"`
	Error      string                   `json:"error,omitempty"`
}

// handleAPIContainers returns containers from all agents
// GET /api/v1/containers
func (m *Manager) handleAPIContainers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agents := m.leaseManager.ListAgents()
	now := time.Now()

	results := make([]NodeContainers, len(agents))
	var wg sync.WaitGroup

	for i, a := range agents {
		online := now.Sub(a.LastSeen) < 30*time.Second
		results[i] = NodeContainers{
			Node:       a.Name,
			Online:     online,
			Containers: []agentpkg.ContainerInfo{},
		}

		if !online {
			continue
		}

		agentCfg := m.config.GetAgentConfig(a.Name)
		if agentCfg == nil {
			results[i].Error = "not in config"
			continue
		}

		wg.Add(1)
		go func(idx int, ip string) {
			defer wg.Done()

			url := fmt.Sprintf("http://%s:%d/containers", ip, agentpkg.ReadyServerPort)
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Get(url)
			if err != nil {
				results[idx].Error = err.Error()
				return
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				results[idx].Error = err.Error()
				return
			}

			var containers []agentpkg.ContainerInfo
			if err := json.Unmarshal(body, &containers); err != nil {
				results[idx].Error = err.Error()
				return
			}
			results[idx].Containers = containers
		}(i, agentCfg.IP)
	}

	wg.Wait()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

// handleAPIContainerLogs streams container logs from a specific agent as SSE
// GET /api/v1/containers/{containerId}/logs?node={node}&all=false
func (m *Manager) handleAPIContainerLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract containerId from path: /api/v1/containers/{containerId}/logs
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/containers/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 || parts[1] != "logs" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	containerID := parts[0]

	nodeName := r.URL.Query().Get("node")
	if nodeName == "" {
		http.Error(w, "node query parameter is required", http.StatusBadRequest)
		return
	}

	showAll := r.URL.Query().Get("all")

	agentCfg := m.config.GetAgentConfig(nodeName)
	if agentCfg == nil {
		http.Error(w, "Node not found in config", http.StatusNotFound)
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

	agentURL := fmt.Sprintf("http://%s:%d/container-logs/%s?all=%s", agentCfg.IP, agentpkg.ReadyServerPort, containerID, showAll)
	resp, err := m.httpClient.Get(agentURL)
	if err != nil {
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

	buf := make([]byte, 4096)
	for {
		select {
		case <-r.Context().Done():
			return
		default:
			n, err := resp.Body.Read(buf)
			if n > 0 {
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
