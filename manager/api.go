package manager

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	agentpkg "github.com/jatinparab/homectl/agent"
	"github.com/jatinparab/homectl/common"
)

var apiLog = slog.With("area", "api")

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
	IsGateway    bool               `json:"is_gateway"`
	Metrics      common.NodeMetrics `json:"metrics,omitempty"`
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
		if now.Sub(agent.LastSeen) < 30*time.Second {
			response.AgentsOnline++
		}
	}

	allApps := m.appRegistry.ListAll()
	response.TotalApps = len(allApps)
	for _, app := range allApps {
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

	m.gatewayMu.Lock()
	gatewayNode := m.gatewayNode
	m.gatewayMu.Unlock()

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
			IsGateway:    gatewayNode == agent.Name,
			Metrics:      agent.Metrics,
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

	m.gatewayMu.Lock()
	isGateway := m.gatewayNode == agent.Name
	m.gatewayMu.Unlock()

	apiAgent := APIAgentInfo{
		Name:         agent.Name,
		IP:           agent.IP,
		LastSeen:     agent.LastSeen.Format(time.RFC3339),
		RegisteredAt: agent.RegisteredAt.Format(time.RFC3339),
		Online:       now.Sub(agent.LastSeen) < 30*time.Second,
		IsGateway:    isGateway,
		Metrics:      agent.Metrics,
		AppReports:   appReports,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(apiAgent)
}

// NodeReport represents a single node's report for an app
type NodeReport struct {
	Node              string                 `json:"node"`
	NodeOnline        bool                   `json:"node_online"`
	ObservedState     string                 `json:"observed_state"`
	Health            string                 `json:"health"`
	Phase             string                 `json:"phase"`
	LastError         string                 `json:"last_error"`
	RestartCount      int                    `json:"restart_count"`
	Quarantined       bool                   `json:"quarantined"`
	Port              int                    `json:"port"`
	Progress          *common.ResticProgress `json:"progress,omitempty"`
	PruneBusy         bool                   `json:"prune_busy,omitempty"`
	LastBackupAt      string                 `json:"last_backup_at,omitempty"`
	LastBackupError   string                 `json:"last_backup_error,omitempty"`
	BackupStatus      string                 `json:"backup_status"`
	LastPruneAt       string                 `json:"last_prune_at,omitempty"`
	LastPruneError    string                 `json:"last_prune_error,omitempty"`
	PruneStatus       string                 `json:"prune_status"`
	ActiveConnections bool                   `json:"active_connections"`
	LastActiveAt      string                 `json:"last_active_at,omitempty"`
	PostBackupPhase   string                 `json:"post_backup_phase,omitempty"`
}

// UpcomingBackupInfo describes the next scheduled backup
type UpcomingBackupInfo struct {
	AgentID    string `json:"agent_id"`
	AppID      string `json:"app_id"`
	AssignedAt string `json:"assigned_at"`
	Started    bool   `json:"started"`
}

// UpcomingPruneInfo describes the next scheduled prune
type UpcomingPruneInfo struct {
	AgentID    string `json:"agent_id"`
	AppID      string `json:"app_id"`
	AssignedAt string `json:"assigned_at"`
	Started    bool   `json:"started"`
}

// AppStatus combines desired state with observed state for UI display
type AppStatus struct {
	AppID           string       `json:"app_id"`
	DesiredState    string       `json:"desired_state"`
	AssignedAgent   *string      `json:"assigned_agent"`
	IsGlobal        bool         `json:"is_global"`        // Runs on all connected nodes
	ObservedState   string       `json:"observed_state"`   // Consolidated (worst case)
	Health          string       `json:"health"`           // Consolidated (worst case)
	Phase           string       `json:"phase"`            // Consolidated
	BackupStatus    string       `json:"backup_status"`    // Consolidated worst backup status
	PruneStatus     string       `json:"prune_status"`     // Consolidated worst prune status
	BackupEnabled   bool         `json:"backup_enabled"`   // Whether backup is enabled for this app
	BackupSensitive bool         `json:"backup_sensitive"` // App is sensitive to backup interruptions
	BackupInterval  string       `json:"backup_interval"`  // Human-readable backup interval
	PruneInterval   string       `json:"prune_interval"`   // Human-readable prune interval
	RestoreEnabled  bool         `json:"restore_enabled"`  // Whether restore-on-start is enabled
	NodeReports     []NodeReport `json:"node_reports"`     // All node reports
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

// consolidateBackupStatus returns the worst backup status (never > needs_backup > ok)
func consolidateBackupStatus(statuses []string) string {
	if len(statuses) == 0 {
		return ""
	}
	priority := map[string]int{
		"never":        0,
		"needs_backup": 1,
		"ok":           2,
	}
	worst := "ok"
	worstP := priority["ok"]
	for _, s := range statuses {
		if p, ok := priority[s]; ok && p < worstP {
			worst = s
			worstP = p
		}
	}
	return worst
}

// consolidatePruneStatus returns the worst prune status (never > needs_prune > ok)
func consolidatePruneStatus(statuses []string) string {
	if len(statuses) == 0 {
		return ""
	}
	priority := map[string]int{
		"never":       0,
		"needs_prune": 1,
		"ok":          2,
	}
	worst := "ok"
	worstP := priority["ok"]
	for _, s := range statuses {
		if p, ok := priority[s]; ok && p < worstP {
			worst = s
			worstP = p
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

	allApps := m.appRegistry.ListAll()
	appConfigs := m.loadAppConfigs()

	// Get all agents with their app reports for per-node detail
	agents := m.leaseManager.ListAgents()
	now := time.Now()

	// Build a map of app_id -> list of NodeReports for multi-node support
	appReportsMap := make(map[string][]NodeReport)
	pruneStateChanged := false
	pruneStateUpdates := make(map[common.AppID]PruneState)
	m.backupMu.Lock()
	pruneStateSnapshot := make(map[common.AppID]PruneState, len(m.pruneState))
	for k, v := range m.pruneState {
		pruneStateSnapshot[k] = v
	}
	m.backupMu.Unlock()

	for _, agent := range agents {
		agentOnline := now.Sub(agent.LastSeen) < 30*time.Second
		for _, report := range agent.AppReports {
			appID := string(report.AppID)
			cfg := appConfigs[appID]
			interval := cfg.GetBackupInterval()

			var lastBackupStr string
			var backupStatus string
			if !report.LastBackupAt.IsZero() {
				lastBackupStr = report.LastBackupAt.Format(time.RFC3339)
				if now.Sub(report.LastBackupAt) > interval {
					backupStatus = "needs_backup"
				} else {
					backupStatus = "ok"
				}
			} else {
				backupStatus = "never"
			}

			lastPruneAt := report.LastPruneAt
			st, hasState := pruneStateSnapshot[report.AppID]
			if hasState && st.LastPruneAt.After(lastPruneAt) {
				lastPruneAt = st.LastPruneAt
				if report.LastPruneError == "" {
					report.LastPruneError = st.LastPruneError
				}
			} else if !report.LastPruneAt.IsZero() && (!hasState || report.LastPruneAt.After(st.LastPruneAt)) {
				pruneStateUpdates[report.AppID] = PruneState{LastPruneAt: report.LastPruneAt, LastPruneError: report.LastPruneError}
				pruneStateChanged = true
			}
			var lastPruneStr string
			var pruneStatus string
			if !lastPruneAt.IsZero() {
				lastPruneStr = lastPruneAt.Format(time.RFC3339)
				if now.Sub(lastPruneAt) > PruneInterval {
					pruneStatus = "needs_prune"
				} else {
					pruneStatus = "ok"
				}
			} else {
				pruneStatus = "never"
			}

			var lastActiveStr string
			if !report.LastActiveAt.IsZero() {
				lastActiveStr = report.LastActiveAt.Format(time.RFC3339)
			}
			nodeReport := NodeReport{
				Node:              agent.Name,
				NodeOnline:        agentOnline,
				ObservedState:     string(report.ObservedState),
				Health:            string(report.Health),
				Phase:             string(report.Phase),
				LastError:         report.LastError,
				RestartCount:      report.RestartCount,
				Quarantined:       report.Quarantined,
				Port:              report.Port,
				Progress:          report.Progress,
				PruneBusy:         report.PruneBusy,
				LastBackupAt:      lastBackupStr,
				LastBackupError:   report.LastBackupError,
				BackupStatus:      backupStatus,
				LastPruneAt:       lastPruneStr,
				LastPruneError:    report.LastPruneError,
				PruneStatus:       pruneStatus,
				ActiveConnections: report.ActiveConnections,
				LastActiveAt:      lastActiveStr,
				PostBackupPhase:   report.PostBackupPhase,
			}
			if !agentOnline {
				// Agent is offline; treat any phase/progress/busy-style fields as stale.
				nodeReport.ObservedState = "unknown"
				nodeReport.Health = "unknown"
				nodeReport.Phase = ""
				nodeReport.LastError = ""
				nodeReport.Progress = nil
				nodeReport.ActiveConnections = false
				nodeReport.LastActiveAt = ""
				nodeReport.PostBackupPhase = ""
				nodeReport.PruneBusy = false
			}
			appReportsMap[appID] = append(appReportsMap[appID], nodeReport)
		}
	}
	if pruneStateChanged {
		m.backupMu.Lock()
		for appID, st := range pruneStateUpdates {
			m.pruneState[appID] = st
		}
		m.backupMu.Unlock()
		go m.savePruneState()
	}

	// Build AppStatus list by merging registry apps with observed state
	result := make([]AppStatus, 0, len(allApps))
	for _, app := range allApps {
		appID := string(app.ID)
		nodeReports := appReportsMap[appID]
		if nodeReports == nil {
			nodeReports = []NodeReport{}
		}

		// Collect states/healths/phases for consolidation
		var states, healths, phases, backupStatuses, pruneStatuses []string
		for _, nr := range nodeReports {
			states = append(states, nr.ObservedState)
			healths = append(healths, nr.Health)
			if nr.Phase != "" {
				phases = append(phases, nr.Phase)
			}
			backupStatuses = append(backupStatuses, nr.BackupStatus)
			pruneStatuses = append(pruneStatuses, nr.PruneStatus)
		}

		backupEnabled := true
		var backupSensitive bool
		backupInterval := BackupInterval.String()
		pruneInterval := PruneInterval.String()
		if cfg, ok := appConfigs[appID]; ok {
			backupEnabled = cfg.IsBackupEnabled()
			backupSensitive = cfg.BackupSensitive
			backupInterval = cfg.GetBackupInterval().String()
		}

		status := AppStatus{
			AppID:           appID,
			DesiredState:    string(app.DesiredState),
			AssignedAgent:   app.AssignedAgent,
			IsGlobal:        app.IsGlobal,
			ObservedState:   consolidateObservedState(states),
			Health:          consolidateHealth(healths),
			Phase:           consolidatePhase(phases),
			BackupStatus:    consolidateBackupStatus(backupStatuses),
			PruneStatus:     consolidatePruneStatus(pruneStatuses),
			BackupEnabled:   backupEnabled,
			BackupSensitive: backupSensitive,
			BackupInterval:  backupInterval,
			PruneInterval:   pruneInterval,
			RestoreEnabled:  app.RestoreEnabled,
			NodeReports:     nodeReports,
		}

		result = append(result, status)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleAPIUpcomingBackup returns the current backup assignment
// GET /api/v1/backup/upcoming
func (m *Manager) handleAPIUpcomingBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	m.backupMu.Lock()
	var a *BackupAssignment
	for _, candidate := range m.backupAssignments {
		if a == nil || candidate.AssignedAt.Before(a.AssignedAt) {
			a = candidate
		}
	}
	m.backupMu.Unlock()

	if a == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("null"))
		return
	}

	info := UpcomingBackupInfo{
		AgentID:    a.AgentID,
		AppID:      string(a.AppID),
		AssignedAt: a.AssignedAt.Format(time.RFC3339),
		Started:    a.BackupStarted,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// handleAPIUpcomingPrune returns the current prune assignment
// GET /api/v1/prune/upcoming
func (m *Manager) handleAPIUpcomingPrune(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	m.backupMu.Lock()
	a := m.pruneAssignment
	m.backupMu.Unlock()

	if a == nil {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("null"))
		return
	}

	info := UpcomingPruneInfo{
		AgentID:    a.AgentID,
		AppID:      string(a.AppID),
		AssignedAt: a.AssignedAt.Format(time.RFC3339),
		Started:    a.BackupStarted,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// handleAPIAppRestore handles PUT requests to toggle an app's restore-enabled flag
// PUT /api/v1/apps-restore/{appId}?enabled=true|false
func (m *Manager) handleAPIAppRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/apps-restore/")
	appID := strings.TrimSuffix(path, "/")
	if appID == "" {
		http.Error(w, "App ID required", http.StatusBadRequest)
		return
	}

	enabledStr := r.URL.Query().Get("enabled")
	if enabledStr == "" {
		http.Error(w, "enabled query parameter is required (true or false)", http.StatusBadRequest)
		return
	}

	enabled := enabledStr == "true"

	if err := m.appRegistry.UpdateRestoreEnabled(common.AppID(appID), enabled); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	m.saveRestoreDisabled()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"app_id": appID, "restore_enabled": enabled})
}

// handleAPISync performs a git pull, re-discovers apps, and tells all online agents to re-sync.
// POST /api/v1/sync
func (m *Manager) handleAPISync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Step 1: Git pull
	if err := m.GitSync(); err != nil {
		http.Error(w, fmt.Sprintf("Git sync failed: %s", err), http.StatusInternalServerError)
		return
	}

	// Step 2: Re-discover apps
	m.DiscoverApps()

	// Step 3: Fan out to all online agents
	agents := m.leaseManager.ListAgents()
	now := time.Now()
	var syncErrors []string

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, agent := range agents {
		if now.Sub(agent.LastSeen) > 30*time.Second {
			continue
		}
		agentCfg := m.config.GetAgentConfig(agent.Name)
		if agentCfg == nil {
			continue
		}

		wg.Add(1)
		go func(name, ip string) {
			defer wg.Done()
			url := fmt.Sprintf("http://%s:%d/sync", ip, agentpkg.ReadyServerPort)
			req, err := http.NewRequest(http.MethodPost, url, nil)
			if err != nil {
				mu.Lock()
				syncErrors = append(syncErrors, fmt.Sprintf("%s: %s", name, err))
				mu.Unlock()
				return
			}
			resp, err := m.httpClient.Do(req)
			if err != nil {
				mu.Lock()
				syncErrors = append(syncErrors, fmt.Sprintf("%s: %s", name, err))
				mu.Unlock()
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				mu.Lock()
				syncErrors = append(syncErrors, fmt.Sprintf("%s: HTTP %d", name, resp.StatusCode))
				mu.Unlock()
				return
			}
			apiLog.Info("Agent synced successfully", "agent", name)
		}(agent.Name, agentCfg.IP)
	}

	wg.Wait()

	response := map[string]any{
		"status": "ok",
	}
	if len(syncErrors) > 0 {
		response["errors"] = syncErrors
		apiLog.Warn("Sync completed with errors", "errors", syncErrors)
	} else {
		apiLog.Info("Sync completed successfully for all agents")
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleAPIRebalance triggers a force rebalance of all apps
// POST /api/v1/rebalance
func (m *Manager) handleAPIRebalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	m.ForceRebalance()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
	case "phase-logs":
		m.handleAPIAppPhaseLogs(w, r)
	case "backup":
		m.handleAPITriggerBackup(w, r)
	case "prune":
		m.handleAPITriggerPrune(w, r)
	case "restart":
		m.handleAPIRestartApp(w, r)
	case "snapshots":
		m.handleAPIAppSnapshots(w, r)
	case "snapshot-refresh":
		m.handleAPIAppSnapshotRefresh(w, r)
	case "reassign":
		m.handleAPIAppReassign(w, r)
	default:
		http.Error(w, "Unknown action", http.StatusNotFound)
	}
}

// handleAPIAppReassign handles POST requests to manually move an app to a different node.
// POST /api/v1/apps/{appId}/reassign?node={nodeName}
func (m *Manager) handleAPIAppReassign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/apps/")
	appID := common.AppID(strings.SplitN(path, "/", 2)[0])

	node := r.URL.Query().Get("node")
	if node == "" {
		http.Error(w, "node query parameter is required", http.StatusBadRequest)
		return
	}

	// Verify the target node is currently online
	alive := m.aliveAgents()
	if !alive[node] {
		http.Error(w, "target node is not online", http.StatusBadRequest)
		return
	}

	if err := m.appRegistry.ReassignAgent(appID, node); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	apiLog.Info("App manually reassigned", "app", appID, "node", node)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleAPIAppDesiredState handles PUT requests to update an app's desired state
// PUT /api/v1/apps-desired-state/{appId}?state=running|stopped
func (m *Manager) handleAPIAppDesiredState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract appId from path: /api/v1/apps-desired-state/{appId}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/apps-desired-state/")
	appID := strings.TrimSuffix(path, "/")

	if appID == "" {
		http.Error(w, "App ID required", http.StatusBadRequest)
		return
	}

	// Get desired state from query parameter
	stateStr := r.URL.Query().Get("state")
	if stateStr == "" {
		http.Error(w, "state query parameter is required (running or stopped)", http.StatusBadRequest)
		return
	}

	var desiredState common.DesiredState
	switch stateStr {
	case "running":
		desiredState = common.DesiredRunning
	case "stopped":
		desiredState = common.DesiredStopped
	default:
		http.Error(w, "Invalid state, must be 'running' or 'stopped'", http.StatusBadRequest)
		return
	}

	// Update the app's desired state and persist
	err := m.appRegistry.UpdateDesiredState(common.AppID(appID), desiredState)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	m.saveDesiredState()

	// Return the updated app
	app := m.appRegistry.Get(common.AppID(appID))
	if app == nil {
		http.Error(w, "App not found", http.StatusNotFound)
		return
	}

	response := map[string]interface{}{
		"app_id":        app.ID,
		"desired_state": app.DesiredState,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleAPITriggerBackup creates a manager-approved backup assignment.
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

	if m.config.GetAgentConfig(nodeName) == nil {
		http.Error(w, "Node not found in config", http.StatusNotFound)
		return
	}

	// Check if agent is online
	agent, ok := m.leaseManager.GetAgent(nodeName)
	if !ok || time.Since(agent.LastSeen) > 30*time.Second {
		http.Error(w, "Node is offline", http.StatusServiceUnavailable)
		return
	}

	m.backupMu.Lock()
	defer m.backupMu.Unlock()

	if m.pruneAssignment != nil || m.anyPruneBusy() || m.anyRestoreBusy() || m.isRepoBusy() {
		http.Error(w, "global work lane busy", http.StatusConflict)
		return
	}
	if _, ok := m.backupAssignments[common.AppID(appID)]; ok {
		http.Error(w, "backup already scheduled", http.StatusConflict)
		return
	}

	m.backupAssignments[common.AppID(appID)] = &BackupAssignment{
		Kind:       AssignmentKindBackup,
		AgentID:    nodeName,
		AppID:      common.AppID(appID),
		AssignedAt: time.Now(),
	}
	m.lastScheduledWorkKind = AssignmentKindBackup

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "backup_scheduled"})
}

// handleAPITriggerPrune creates a manager-approved prune assignment.
// POST /api/v1/apps/{appId}/prune?node={nodeName}
func (m *Manager) handleAPITriggerPrune(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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

	if m.config.GetAgentConfig(nodeName) == nil {
		http.Error(w, "Node not found in config", http.StatusNotFound)
		return
	}
	agent, ok := m.leaseManager.GetAgent(nodeName)
	if !ok || time.Since(agent.LastSeen) > 30*time.Second {
		http.Error(w, "Node is offline", http.StatusServiceUnavailable)
		return
	}

	m.backupMu.Lock()
	defer m.backupMu.Unlock()

	if len(m.backupAssignments) > 0 || m.pruneAssignment != nil || m.anyBackupBusy() || m.anyPruneBusy() || m.anyRestoreBusy() || m.isRepoBusy() {
		http.Error(w, "global work lane busy", http.StatusConflict)
		return
	}

	m.pruneAssignment = &BackupAssignment{
		Kind:       AssignmentKindPrune,
		AgentID:    nodeName,
		AppID:      common.AppID(appID),
		AssignedAt: time.Now(),
	}
	m.lastScheduledWorkKind = AssignmentKindPrune

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "prune_scheduled"})
}

// handleAPIRestartApp proxies a restart request to the appropriate agent
// POST /api/v1/apps/{appId}/restart?node={nodeName}
func (m *Manager) handleAPIRestartApp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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

	agent, ok := m.leaseManager.GetAgent(nodeName)
	if !ok || time.Since(agent.LastSeen) > 30*time.Second {
		http.Error(w, "Node is offline", http.StatusServiceUnavailable)
		return
	}

	agentURL := fmt.Sprintf("http://%s:%d/restart/%s", agentCfg.IP, agentpkg.ReadyServerPort, appID)
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
	resp, err := m.streamHTTPClient.Get(agentLogsURL)
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

// handleAPIAppPhaseLogs streams phase logs (e.g. pull output) from an agent as SSE
// GET /api/v1/apps/{appId}/phase-logs?node={nodeName}
func (m *Manager) handleAPIAppPhaseLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1/apps/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 || parts[1] != "phase-logs" {
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

	agent, ok := m.leaseManager.GetAgent(nodeName)
	if !ok || time.Since(agent.LastSeen) > 30*time.Second {
		http.Error(w, "Node is offline", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	agentURL := fmt.Sprintf("http://%s:%d/phase-logs/%s", agentCfg.IP, agentpkg.ReadyServerPort, appID)
	resp, err := m.streamHTTPClient.Get(agentURL)
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
	resp, err := m.streamHTTPClient.Get(agentURL)
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

type APIStorageNode struct {
	Node        string                     `json:"node"`
	Online      bool                       `json:"online"`
	AppdataPath string                     `json:"appdata_path,omitempty"`
	DiskTotal   uint64                     `json:"disk_total"`
	DiskUsed    uint64                     `json:"disk_used"`
	DiskFree    uint64                     `json:"disk_free"`
	DiskPercent float64                    `json:"disk_percent"`
	Reclaimable uint64                     `json:"reclaimable"`
	ReclaimPct  float64                    `json:"reclaim_pct"`
	Prune       agentpkg.StoragePruneState `json:"prune"`
	Error       string                     `json:"error,omitempty"`
}

type APIStorageAggregate struct {
	DiskTotal   uint64  `json:"disk_total"`
	DiskUsed    uint64  `json:"disk_used"`
	DiskFree    uint64  `json:"disk_free"`
	DiskPercent float64 `json:"disk_percent"`
	Reclaimable uint64  `json:"reclaimable"`
	ReclaimPct  float64 `json:"reclaim_pct"`
}

type APIStorageResponse struct {
	Nodes      []APIStorageNode    `json:"nodes"`
	Aggregate  APIStorageAggregate `json:"aggregate"`
	AnyPruning bool                `json:"any_pruning"`
}

type APIStoragePruneResult struct {
	Node    string `json:"node"`
	Started bool   `json:"started"`
	Error   string `json:"error,omitempty"`
}

type APIStoragePruneResponse struct {
	Results []APIStoragePruneResult `json:"results"`
}

// handleAPIStorage returns per-node and aggregate storage status.
// GET /api/v1/storage
func (m *Manager) handleAPIStorage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agents := m.leaseManager.ListAgents()
	now := time.Now()
	results := make([]APIStorageNode, len(agents))
	var wg sync.WaitGroup

	for i, a := range agents {
		online := now.Sub(a.LastSeen) < 30*time.Second
		results[i] = APIStorageNode{Node: a.Name, Online: online}
		if !online {
			results[i].Error = "offline"
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
			url := fmt.Sprintf("http://%s:%d/storage/status", ip, agentpkg.ReadyServerPort)
			resp, err := m.httpClient.Get(url)
			if err != nil {
				results[idx].Error = err.Error()
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				results[idx].Error = fmt.Sprintf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
				return
			}

			var st agentpkg.StorageStatusResponse
			if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
				results[idx].Error = err.Error()
				return
			}

			results[idx].AppdataPath = st.AppdataPath
			results[idx].DiskTotal = st.DiskTotal
			results[idx].DiskUsed = st.DiskUsed
			results[idx].DiskFree = st.DiskFree
			results[idx].DiskPercent = st.DiskPercent
			results[idx].Reclaimable = st.Reclaimable
			results[idx].ReclaimPct = st.ReclaimPct
			results[idx].Prune = st.Prune
		}(i, agentCfg.IP)
	}

	wg.Wait()

	resp := APIStorageResponse{Nodes: results}
	for _, node := range results {
		if !node.Online || node.Error != "" {
			continue
		}
		resp.Aggregate.DiskTotal += node.DiskTotal
		resp.Aggregate.DiskUsed += node.DiskUsed
		resp.Aggregate.DiskFree += node.DiskFree
		resp.Aggregate.Reclaimable += node.Reclaimable
		if node.Prune.Busy {
			resp.AnyPruning = true
		}
	}
	if resp.Aggregate.DiskTotal > 0 {
		resp.Aggregate.DiskPercent = float64(resp.Aggregate.DiskUsed) / float64(resp.Aggregate.DiskTotal) * 100
		resp.Aggregate.ReclaimPct = float64(resp.Aggregate.Reclaimable) / float64(resp.Aggregate.DiskTotal) * 100
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleAPIStoragePrune triggers storage prune on one node or all online nodes.
// POST /api/v1/storage/prune?node={nodeName|all}
func (m *Manager) handleAPIStoragePrune(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	node := r.URL.Query().Get("node")
	if node == "" {
		http.Error(w, "node query parameter is required", http.StatusBadRequest)
		return
	}

	now := time.Now()
	var targetAgents []AgentInfo
	if node == "all" {
		for _, a := range m.leaseManager.ListAgents() {
			if now.Sub(a.LastSeen) < 30*time.Second {
				targetAgents = append(targetAgents, a)
			}
		}
	} else {
		a, ok := m.leaseManager.GetAgent(node)
		if !ok || now.Sub(a.LastSeen) >= 30*time.Second {
			http.Error(w, "Node is offline", http.StatusServiceUnavailable)
			return
		}
		targetAgents = []AgentInfo{*a}
	}

	results := make([]APIStoragePruneResult, 0, len(targetAgents))
	for _, a := range targetAgents {
		result := APIStoragePruneResult{Node: a.Name}
		agentCfg := m.config.GetAgentConfig(a.Name)
		if agentCfg == nil {
			result.Error = "not in config"
			results = append(results, result)
			continue
		}

		url := fmt.Sprintf("http://%s:%d/storage/prune", agentCfg.IP, agentpkg.ReadyServerPort)
		req, err := http.NewRequest(http.MethodPost, url, nil)
		if err != nil {
			result.Error = err.Error()
			results = append(results, result)
			continue
		}

		resp, err := m.httpClient.Do(req)
		if err != nil {
			result.Error = err.Error()
			results = append(results, result)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			result.Error = fmt.Sprintf("%s: %s", resp.Status, strings.TrimSpace(string(body)))
			results = append(results, result)
			continue
		}

		result.Started = true
		results = append(results, result)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(APIStoragePruneResponse{Results: results})
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

// handleAPIAppSnapshots proxies a snapshot list request to an agent
// GET /api/v1/apps/{appId}/snapshots?node={nodeName}
func (m *Manager) handleAPIAppSnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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

	agent, ok := m.leaseManager.GetAgent(nodeName)
	if !ok || time.Since(agent.LastSeen) > 30*time.Second {
		http.Error(w, "Node is offline", http.StatusServiceUnavailable)
		return
	}

	agentURL := fmt.Sprintf("http://%s:%d/snapshots/%s", agentCfg.IP, agentpkg.ReadyServerPort, appID)
	resp, err := m.httpClient.Get(agentURL)
	if err != nil {
		http.Error(w, "Failed to reach agent: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handleAPIAppSnapshotRefresh proxies a snapshot refresh request to an agent
// POST /api/v1/apps/{appId}/snapshot-refresh?node={nodeName}
func (m *Manager) handleAPIAppSnapshotRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

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

	agent, ok := m.leaseManager.GetAgent(nodeName)
	if !ok || time.Since(agent.LastSeen) > 30*time.Second {
		http.Error(w, "Node is offline", http.StatusServiceUnavailable)
		return
	}

	agentURL := fmt.Sprintf("http://%s:%d/snapshots/%s/refresh", agentCfg.IP, agentpkg.ReadyServerPort, appID)
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

// handleAPISnapshotLs proxies a snapshot file listing request to an agent
// GET /api/v1/snapshots/{snapshotId}/ls?node={nodeName}&path=/
func (m *Manager) handleAPISnapshotLs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse: /api/v1/snapshots/{snapshotId}/ls
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/snapshots/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) < 2 || parts[1] != "ls" {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	snapshotID := parts[0]

	nodeName := r.URL.Query().Get("node")
	if nodeName == "" {
		http.Error(w, "node query parameter is required", http.StatusBadRequest)
		return
	}

	browsePath := r.URL.Query().Get("path")
	if browsePath == "" {
		browsePath = "/"
	}

	agentCfg := m.config.GetAgentConfig(nodeName)
	if agentCfg == nil {
		http.Error(w, "Node not found in config", http.StatusNotFound)
		return
	}

	agent, ok := m.leaseManager.GetAgent(nodeName)
	if !ok || time.Since(agent.LastSeen) > 30*time.Second {
		http.Error(w, "Node is offline", http.StatusServiceUnavailable)
		return
	}

	agentURL := fmt.Sprintf("http://%s:%d/snapshot-ls/%s?path=%s", agentCfg.IP, agentpkg.ReadyServerPort, snapshotID, browsePath)
	resp, err := m.httpClient.Get(agentURL)
	if err != nil {
		http.Error(w, "Failed to reach agent: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
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
	resp, err := m.streamHTTPClient.Get(agentURL)
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

// GatewayInfo contains current gateway information
type GatewayInfo struct {
	CurrentNode string `json:"current_node"`
	ManualNode  string `json:"manual_node,omitempty"`
	IsManual    bool   `json:"is_manual"`
}

// handleAPIGateway handles GET (fetch gateway info) and POST/DELETE (set/clear manual gateway)
func (m *Manager) handleAPIGateway(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	m.gatewayMu.Lock()
	currentNode := m.gatewayNode
	manualNode := m.manualGatewayNode
	m.gatewayMu.Unlock()

	if r.Method == http.MethodGet {
		// GET /api/v1/gateway - return current gateway info
		info := GatewayInfo{
			CurrentNode: currentNode,
			ManualNode:  manualNode,
			IsManual:    manualNode != "",
		}
		json.NewEncoder(w).Encode(info)
	} else if r.Method == http.MethodPost {
		// POST /api/v1/gateway?node={node} - set manual gateway
		node := r.URL.Query().Get("node")
		if node == "" {
			http.Error(w, "node parameter is required", http.StatusBadRequest)
			return
		}

		// Verify the node exists and is online
		agent, ok := m.leaseManager.GetAgent(node)
		if !ok {
			http.Error(w, "node not found", http.StatusNotFound)
			return
		}
		if time.Since(agent.LastSeen) > 30*time.Second {
			http.Error(w, "node is offline", http.StatusConflict)
			return
		}

		// Set manual gateway
		m.gatewayMu.Lock()
		m.manualGatewayNode = node
		m.gatewayMu.Unlock()

		apiLog.Info("Manual gateway set", "node", node)

		// Return updated info
		info := GatewayInfo{
			CurrentNode: node,
			ManualNode:  node,
			IsManual:    true,
		}
		json.NewEncoder(w).Encode(info)
	} else if r.Method == http.MethodDelete {
		// DELETE /api/v1/gateway - clear manual gateway (revert to automatic)
		m.gatewayMu.Lock()
		m.manualGatewayNode = ""
		m.gatewayMu.Unlock()

		apiLog.Info("Manual gateway cleared, reverting to automatic")

		// Return updated info
		info := GatewayInfo{
			CurrentNode: currentNode,
			ManualNode:  "",
			IsManual:    false,
		}
		json.NewEncoder(w).Encode(info)
	} else {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
