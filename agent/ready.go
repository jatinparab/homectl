package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jatinparab/homectl/common"
)

const (
	// ReadyServerPort is the port for the ready server
	ReadyServerPort = 1337
	// TraefikAppName is the app name to check for readiness
	TraefikAppName = "traefik"
)

// ReadyServer provides health check endpoints for keepalived
type ReadyServer struct {
	agent  *Agent
	server *http.Server
}

// NewReadyServer creates a new ready server
func NewReadyServer(agent *Agent) *ReadyServer {
	return &ReadyServer{
		agent: agent,
	}
}

// Start starts the ready server
func (rs *ReadyServer) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ready", rs.handleReady)
	mux.HandleFunc("/health", rs.handleHealth)

	// Backup trigger endpoint
	mux.HandleFunc("/backup/", rs.handleTriggerBackup)

	// Restart endpoint (clears runtime state and restarts the app)
	mux.HandleFunc("/restart/", rs.handleRestart)

	// Sync endpoint (re-pull docker configs from manager)
	mux.HandleFunc("/sync", rs.handleSync)

	// Snapshot browsing endpoints
	mux.HandleFunc("/snapshots/", rs.handleSnapshots)
	mux.HandleFunc("/snapshot-ls/", rs.handleSnapshotLs)
	mux.HandleFunc("/storage/status", rs.handleStorageStatus)
	mux.HandleFunc("/storage/prune", rs.handleStoragePrune)

	// Repo-wide maintenance endpoints
	mux.HandleFunc("/check", rs.handleRepoCheck)
	mux.HandleFunc("/check-status", rs.handleRepoCheckStatus)
	mux.HandleFunc("/check-logs", rs.handleRepoCheckLogs)
	mux.HandleFunc("/unlock", rs.handleRepoUnlock)
	mux.HandleFunc("/force-unlock", rs.handleRepoForceUnlock)
	mux.HandleFunc("/unlock-status", rs.handleRepoUnlockStatus)
	mux.HandleFunc("/unlock-logs", rs.handleRepoUnlockLogs)

	// Register logs, system-logs, containers, container-logs routes
	logsHandler := NewLogsHandler(rs.agent)
	logsHandler.RegisterRoutes(mux)

	rs.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", ReadyServerPort),
		Handler: mux,
	}

	log.Printf("Ready server starting on :%d", ReadyServerPort)
	return rs.server.ListenAndServe()
}

// handleRepoCheck triggers a repo-wide restic check.
// POST /check
func (rs *ReadyServer) handleRepoCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !rs.agent.IsGateway() {
		http.Error(w, "not gateway", http.StatusServiceUnavailable)
		return
	}

	if err := rs.agent.TriggerRepoCheck(); err != nil {
		if err == ErrRepoOpBusy {
			http.Error(w, "check already running", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "check_triggered"})
}

// handleRepoCheckStatus returns current repo check status.
// GET /check-status
func (rs *ReadyServer) handleRepoCheckStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rs.agent.RepoCheckStatus())
}

// handleRepoCheckLogs streams repo check logs from the ring buffer.
// GET /check-logs
func (rs *ReadyServer) handleRepoCheckLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rs.streamLogBuffer(w, r, rs.agent.repoCheck.Log)
}

// handleRepoUnlock triggers a repo-wide restic unlock.
// POST /unlock
func (rs *ReadyServer) handleRepoUnlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !rs.agent.IsGateway() {
		http.Error(w, "not gateway", http.StatusServiceUnavailable)
		return
	}

	if err := rs.agent.TriggerRepoUnlock(); err != nil {
		if err == ErrRepoOpBusy {
			http.Error(w, "unlock already running", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "unlock_triggered"})
}

// handleRepoForceUnlock triggers a repo-wide restic unlock --remove-all, bypassing busy checks.
// POST /force-unlock
func (rs *ReadyServer) handleRepoForceUnlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !rs.agent.IsGateway() {
		http.Error(w, "not gateway", http.StatusServiceUnavailable)
		return
	}

	if err := rs.agent.TriggerRepoForceUnlock(); err != nil {
		if err == ErrRepoOpBusy {
			http.Error(w, "unlock already running", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "force_unlock_triggered"})
}

// handleRepoUnlockStatus returns current repo unlock status.
// GET /unlock-status
func (rs *ReadyServer) handleRepoUnlockStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rs.agent.RepoUnlockStatus())
}

// handleRepoUnlockLogs streams repo unlock logs.
// GET /unlock-logs
func (rs *ReadyServer) handleRepoUnlockLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rs.streamLogBuffer(w, r, rs.agent.repoUnlock.Log)
}

func (rs *ReadyServer) streamLogBuffer(w http.ResponseWriter, r *http.Request, buf *PhaseLogBuffer) {
	if buf == nil {
		http.Error(w, "log buffer not available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	for _, line := range buf.Snapshot() {
		w.Write([]byte(line + "\n"))
	}
	flusher.Flush()

	ch := buf.Follow()
	defer buf.Unfollow(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case line, ok := <-ch:
			if !ok {
				return
			}
			w.Write([]byte(line + "\n"))
			flusher.Flush()
		}
	}
}

// Stop stops the ready server
func (rs *ReadyServer) Stop() error {
	if rs.server != nil {
		return rs.server.Close()
	}
	return nil
}

// handleHealth is a basic health check for the agent ready server itself
func (rs *ReadyServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok\n"))
}

// handleReady checks if this node is the gateway and traefik is healthy
func (rs *ReadyServer) handleReady(w http.ResponseWriter, r *http.Request) {
	if !rs.agent.IsGateway() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("not ready: not gateway\n"))
		return
	}

	// Still check traefik as a safety net
	isReady, message := rs.checkTraefikReady()
	if isReady {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok\n"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(fmt.Sprintf("not ready: %s\n", message)))
	}
}

// handleTriggerBackup triggers an immediate backup for an app
// POST /backup/{appId}
func (rs *ReadyServer) handleTriggerBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	appID := strings.TrimPrefix(r.URL.Path, "/backup/")
	if appID == "" {
		http.Error(w, "app_id is required", http.StatusBadRequest)
		return
	}

	rt := rs.agent.getRuntime(common.AppID(appID))
	if rt == nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	// Reset LastBackupAt to zero so shouldBackup() returns true immediately
	rs.agent.runtimeMu.Lock()
	rt.Backup.LastBackupAt = time.Time{}
	rs.agent.runtimeMu.Unlock()

	log.Printf("Backup triggered for %s via API", appID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "backup_triggered"})
}

// handleRestart clears the runtime state for an app and triggers a stop+start cycle.
// POST /restart/{appId}
func (rs *ReadyServer) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	appID := strings.TrimPrefix(r.URL.Path, "/restart/")
	if appID == "" {
		http.Error(w, "app_id is required", http.StatusBadRequest)
		return
	}

	rt := rs.agent.getRuntime(common.AppID(appID))
	if rt == nil {
		http.Error(w, "app not found", http.StatusNotFound)
		return
	}

	rt.ResetForRestart()

	// If currently running, stop it — the reconcile loop will start it again
	if rt.RunningStatus() == common.RunningStatusRunning || rt.RunningStatus() == common.RunningStatusRestarting {
		rs.agent.executor.Submit(ActionStop(common.AppID(appID)))
		log.Printf("Restart triggered for %s via API (stopping, will restart)", appID)
	} else {
		log.Printf("Restart triggered for %s via API (not running, will start on next reconcile)", appID)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "restart_triggered"})
}

// handleSync re-pulls docker configs from the manager and reloads apps.
// POST /sync
func (rs *ReadyServer) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := rs.agent.SyncConfigs(); err != nil {
		log.Printf("Sync failed: %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Sync completed via API")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "synced"})
}

// handleSnapshots handles snapshot requests:
// GET /snapshots/{appId} — return cached snapshot data
// POST /snapshots/{appId}/refresh — invalidate cache and trigger async refresh
func (rs *ReadyServer) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/snapshots/")

	// Check for /snapshots/{appId}/refresh
	if strings.HasSuffix(path, "/refresh") {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		appID := strings.TrimSuffix(path, "/refresh")
		if appID == "" {
			http.Error(w, "app_id is required", http.StatusBadRequest)
			return
		}

		// Kick off async refresh (cancels any in-flight refresh automatically)
		go rs.agent.refreshSnapshotCache(common.AppID(appID))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"loading": true})
		return
	}

	// GET /snapshots/{appId}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	appID := path
	if appID == "" {
		http.Error(w, "app_id is required", http.StatusBadRequest)
		return
	}

	cache := rs.agent.getSnapshotCache(common.AppID(appID))
	if cache == nil {
		// Never fetched — do a synchronous refresh
		rs.agent.refreshSnapshotCache(common.AppID(appID))
		cache = rs.agent.getSnapshotCache(common.AppID(appID))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cache)
}

// handleSnapshotLs lists files inside a snapshot at a given path
// GET /snapshot-ls/{snapshotId}?path=/
func (rs *ReadyServer) handleSnapshotLs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	snapshotID := strings.TrimPrefix(r.URL.Path, "/snapshot-ls/")
	if snapshotID == "" {
		http.Error(w, "snapshot_id is required", http.StatusBadRequest)
		return
	}

	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}

	entries, err := rs.agent.restic.ListSnapshotFiles(r.Context(), snapshotID, path)
	if err != nil {
		log.Printf("ListSnapshotFiles(%s, %s) error: %s", snapshotID, path, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(entries)
}

// checkTraefikReady checks if traefik app is running and healthy
func (rs *ReadyServer) checkTraefikReady() (bool, string) {
	if rs.agent == nil {
		return false, "agent not initialized"
	}

	rt := rs.agent.getRuntime(common.AppID(TraefikAppName))
	if rt == nil {
		return false, fmt.Sprintf("'%s' not tracked", TraefikAppName)
	}

	runningStatus := rt.RunningStatus()
	if runningStatus != common.RunningStatusRunning {
		return false, fmt.Sprintf("'%s' status: %s", TraefikAppName, runningStatus)
	}

	health := rt.GetHealth()
	if health == common.HealthUnhealthy {
		return false, fmt.Sprintf("'%s' unhealthy", TraefikAppName)
	}

	return true, "ok"
}
