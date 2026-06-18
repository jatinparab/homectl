package manager

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jatinparab/homectl/common"
)

var serverLog = slog.With("area", "server")

// NewServer creates and returns an HTTP server (for graceful shutdown support)
func (m *Manager) NewServer() *http.Server {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", m.handleHealth)
	mux.HandleFunc("/api/join", m.handleAgentJoin)
	mux.HandleFunc("/auth/login", m.handleLogin)
	mux.HandleFunc("/auth/logout", m.handleLogout)
	mux.HandleFunc("/auth/onboarding/status", m.handleOnboardingStatus)
	mux.HandleFunc("/auth/onboarding/complete", m.handleOnboardingComplete)

	// File serving
	mux.HandleFunc("/files/", m.handleFiles)
	mux.HandleFunc("/sync", m.handleSync)
	mux.HandleFunc("/docker/sync", m.handleDockerSync)

	// Agent APIs (internal)
	mux.HandleFunc("/agents", m.handleListAgents)
	mux.HandleFunc("/agents/heartbeat", m.handleHeartbeat)
	mux.HandleFunc("/agents/", m.handleAgentRoutes)

	// UI APIs
	mux.HandleFunc("/api/v1/ws/state", m.handleAPIWebSocketState)
	mux.HandleFunc("/api/v1/auth/me", m.handleAuthMe)
	mux.HandleFunc("/api/v1/dashboard", m.handleAPIDashboard)
	mux.HandleFunc("/api/v1/agents", m.handleAPIAgents)
	mux.HandleFunc("/api/v1/agents/", m.handleAPIAgentDetail)
	mux.HandleFunc("/api/v1/agent-configs/", m.handleAPIAgentConfig)
	mux.HandleFunc("/api/v1/apps", m.handleAPIApps)
	mux.HandleFunc("/api/v1/apps/", m.handleAPIAppRoutes)
	mux.HandleFunc("/api/v1/system-logs", m.handleAPISystemLogSources)
	mux.HandleFunc("/api/v1/system-logs/", m.handleAPISystemLogStream)
	mux.HandleFunc("/api/v1/containers", m.handleAPIContainers)
	mux.HandleFunc("/api/v1/containers/", m.handleAPIContainerLogs)
	mux.HandleFunc("/api/v1/apps-desired-state/", m.handleAPIAppDesiredState)
	mux.HandleFunc("/api/v1/backup/upcoming", m.handleAPIUpcomingBackup)
	mux.HandleFunc("/api/v1/prune/upcoming", m.handleAPIUpcomingPrune)
	mux.HandleFunc("/api/v1/sync", m.handleAPISync)
	mux.HandleFunc("/api/v1/apps-restore/", m.handleAPIAppRestore)
	mux.HandleFunc("/api/v1/rebalance", m.handleAPIRebalance)
	mux.HandleFunc("/api/v1/snapshots/", m.handleAPISnapshotLs)
	mux.HandleFunc("/api/v1/repo/status", m.handleAPIRepoStatus)
	mux.HandleFunc("/api/v1/repo/check", m.handleAPIRepoCheck)
	mux.HandleFunc("/api/v1/repo/check-logs", m.handleAPIRepoCheckLogs)
	mux.HandleFunc("/api/v1/repo/unlock", m.handleAPIRepoUnlock)
	mux.HandleFunc("/api/v1/repo/force-unlock", m.handleAPIRepoForceUnlock)
	mux.HandleFunc("/api/v1/repo/unlock-logs", m.handleAPIRepoUnlockLogs)
	mux.HandleFunc("/api/v1/gateway", m.handleAPIGateway)
	mux.HandleFunc("/api/v1/crowdsec/status", m.handleAPICrowdSecStatus)
	mux.HandleFunc("/api/v1/crowdsec/metrics", m.handleAPICrowdSecMetrics)
	mux.HandleFunc("/api/v1/crowdsec/logs", m.handleAPICrowdSecLogs)
	mux.HandleFunc("/api/v1/crowdsec/bans", m.handleAPICrowdSecBans)
	mux.HandleFunc("/api/v1/crowdsec/bans/", m.handleAPICrowdSecBan)
	mux.HandleFunc("/api/v1/crowdsec/hub", m.handleAPICrowdSecHub)
	mux.HandleFunc("/api/v1/crowdsec/version", m.handleAPICrowdSecVersion)
	mux.HandleFunc("/api/v1/crowdsec/update", m.handleAPICrowdSecUpdate)
	mux.HandleFunc("/api/v1/public-route-logs", m.handleAPIPublicRouteLogs)
	mux.HandleFunc("/api/v1/ssh-logs", m.handleAPISSHLogs)
	mux.HandleFunc("/api/v1/public-routes", m.handleAPIPublicRoutes)
	mux.HandleFunc("/api/v1/public-routes/", m.handleAPIPublicRoute)
	mux.HandleFunc("/api/v1/storage", m.handleAPIStorage)
	mux.HandleFunc("/api/v1/storage/prune", m.handleAPIStoragePrune)

	// UI static files (catch-all)
	mux.Handle("/", m.ServeUI())

	handler := m.requireAuth(mux)
	return &http.Server{
		Addr:    fmt.Sprintf(":%d", m.port),
		Handler: handler,
	}
}

// Start starts the HTTP server (blocking)
func (m *Manager) Start() error {
	server := m.NewServer()
	serverLog.Info("Manager server starting", "addr", server.Addr)
	serverLog.Info("Serving files from", "dir", m.repoDir)
	return server.ListenAndServe()
}

func (m *Manager) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (m *Manager) handleListAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agents := m.leaseManager.ListAgents()
	w.Header().Set("Content-Type", "application/json")

	// Simple JSON encoding
	w.Write([]byte("["))
	for i, agent := range agents {
		if i > 0 {
			w.Write([]byte(","))
		}
		fmt.Fprintf(w, `{"name":%q,"ip":%q,"last_seen":%q,"registered_at":%q}`,
			agent.Name, agent.IP, agent.LastSeen.Format("2006-01-02T15:04:05Z07:00"), agent.RegisteredAt.Format("2006-01-02T15:04:05Z07:00"))
	}
	w.Write([]byte("]"))
}

func (m *Manager) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	m.DiscoverApps()

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Sync completed"))
}

func (m *Manager) handleFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/files/")
	if path == "" {
		http.Error(w, "Path required", http.StatusBadRequest)
		return
	}

	// Prevent directory traversal attacks
	cleanPath := filepath.Clean(path)
	if strings.HasPrefix(cleanPath, "..") {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(m.repoDir, cleanPath)

	info, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Error accessing file", http.StatusInternalServerError)
		return
	}

	if info.IsDir() {
		m.serveDirectory(w, fullPath, cleanPath)
		return
	}

	m.serveFile(w, fullPath)
}

func (m *Manager) serveFile(w http.ResponseWriter, fullPath string) {
	file, err := os.Open(fullPath)
	if err != nil {
		http.Error(w, "Error reading file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	io.Copy(w, file)
}

func (m *Manager) serveDirectory(w http.ResponseWriter, fullPath, relativePath string) {
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		http.Error(w, "Error reading directory", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	for _, entry := range entries {
		entryPath := filepath.Join(relativePath, entry.Name())
		if entry.IsDir() {
			fmt.Fprintf(w, "%s/\n", entryPath)
		} else {
			fmt.Fprintf(w, "%s\n", entryPath)
		}
	}
}

// handleAgentRoutes handles routes under /agents/{id}/...
func (m *Manager) handleAgentRoutes(w http.ResponseWriter, r *http.Request) {
	// Parse path: /agents/{id}/desired
	path := strings.TrimPrefix(r.URL.Path, "/agents/")
	parts := strings.SplitN(path, "/", 2)

	if len(parts) < 2 {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}

	agentID := parts[0]
	action := parts[1]

	switch action {
	case "desired":
		m.handleDesiredApps(w, r, agentID)
	case "caddy-config":
		m.handleCaddyConfig(w, r, agentID)
	case "traefik-config":
		m.handleCaddyConfig(w, r, agentID)
	default:
		http.Error(w, "Unknown action", http.StatusNotFound)
	}
}

// handleDesiredApps returns the desired apps for an agent
// GET /agents/{id}/desired
func (m *Manager) handleDesiredApps(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Don't send desired apps until the agent has heartbeated at least once.
	// The first heartbeat triggers AdoptRunningApps which preserves existing
	// placement. Without this gate the manager could reassign apps before
	// learning what the agent is already running.
	if _, ok := m.leaseManager.GetAgent(agentID); !ok {
		http.Error(w, "Waiting for first heartbeat", http.StatusServiceUnavailable)
		return
	}

	agentApps := m.appRegistry.AppsForAgent(agentID)
	appConfigs := m.loadAppConfigs()

	// Resolve scheduled backup/prune assignments under lock
	m.backupMu.Lock()
	resolvedBackups, pruneAssignment := m.resolveWorkAssignments()
	backupAssignments := make(map[common.AppID]*BackupAssignment, len(resolvedBackups))
	for appID, assignment := range resolvedBackups {
		backupAssignments[appID] = assignment
	}
	m.backupMu.Unlock()

	// Check pending global restores under backupMu (do not clear yet; clear after dispatch)
	m.backupMu.Lock()
	pendingRestoreApps := make(map[common.AppID]bool)
	for appID, agents := range m.pendingGlobalRestores {
		if agents[agentID] {
			pendingRestoreApps[appID] = true
		}
	}
	m.backupMu.Unlock()

	desiredApps := make([]common.DesiredApp, 0, len(agentApps))
	restoreSent := make(map[common.AppID]bool)
	for _, app := range agentApps {
		da := common.DesiredApp{
			AppID:          app.ID,
			DesiredState:   app.DesiredState,
			BackupEnabled:  backupEnabledForApp(app.ID, appConfigs),
			RestoreEnabled: app.RestoreEnabled,
		}
		if cfg, ok := appConfigs[string(app.ID)]; ok {
			da.BackupSensitive = cfg.BackupSensitive
		}
		if da.BackupEnabled {
			if ba, ok := backupAssignments[app.ID]; ok && ba.AgentID == agentID {
				da.BackupNow = true
			}
		}
		if da.BackupEnabled && pruneAssignment != nil && pruneAssignment.AgentID == agentID && pruneAssignment.AppID == app.ID {
			da.PruneNow = true
		}
		if pendingRestoreApps[app.ID] {
			if !m.anyBackupBusy() && !m.anyPruneBusy() && !m.isRepoBusy() {
				da.RestoreNow = true
				restoreSent[app.ID] = true
			}
		}
		desiredApps = append(desiredApps, da)
	}

	if len(restoreSent) > 0 {
		m.backupMu.Lock()
		for appID := range restoreSent {
			agents := m.pendingGlobalRestores[appID]
			if agents == nil {
				continue
			}
			delete(agents, agentID)
			if len(agents) == 0 {
				delete(m.pendingGlobalRestores, appID)
			}
		}
		m.backupMu.Unlock()
	}

	response := common.DesiredAppsResponse{Apps: desiredApps}

	m.gatewayMu.Lock()
	response.IsGateway = m.gatewayNode == agentID
	m.gatewayMu.Unlock()

	serverLog.Debug("Returning desired apps", "agent", agentID, "count", len(response.Apps))

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		serverLog.Error("Failed to encode desired apps response", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// handleCaddyConfig returns the Caddy config for an agent.
// GET /agents/{id}/caddy-config
func (m *Manager) handleCaddyConfig(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	config, err := m.GenerateCaddyConfig(agentID)
	if err != nil {
		serverLog.Error("Failed to generate Caddy config", "agent", agentID, "err", err)
		http.Error(w, "Failed to generate config", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(config))
}

// handleDockerSync returns the docker folder as a tar.gz archive
// GET /docker/sync
func (m *Manager) handleDockerSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	dockerDir := filepath.Join(m.repoDir, "docker")
	if _, err := os.Stat(dockerDir); os.IsNotExist(err) {
		http.Error(w, "Docker directory not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", "attachment; filename=docker.tar.gz")

	gw := gzip.NewWriter(w)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	err := filepath.Walk(dockerDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path from dockerDir
		relPath, err := filepath.Rel(dockerDir, path)
		if err != nil {
			return err
		}

		// Skip the root directory itself
		if relPath == "." {
			return nil
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// If it's a file, write its contents
		if !info.IsDir() {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()

			if _, err := io.Copy(tw, file); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		serverLog.Error("Error creating docker sync archive", "err", err)
		// Can't send error to client as we've already started writing
		return
	}

	serverLog.Info("Docker sync archive sent successfully")
}
