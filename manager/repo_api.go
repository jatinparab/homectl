package manager

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type RepoOpStatusResponse struct {
	Busy            bool     `json:"busy"`
	LastAt          string   `json:"last_at,omitempty"`
	LastError       string   `json:"last_error,omitempty"`
	ProgressPercent *float64 `json:"progress_percent,omitempty"`
}

type RepoStatusResponse struct {
	GatewayNode      string               `json:"gateway_node"`
	GatewayOnline    bool                 `json:"gateway_online"`
	LastStatusSyncAt string               `json:"last_status_sync_at,omitempty"`
	Check            RepoOpStatusResponse `json:"check"`
	Unlock           RepoOpStatusResponse `json:"unlock"`
}

// handleAPIRepoStatus returns gateway repo-maintenance status (check/unlock).
// GET /api/v1/repo/status
func (m *Manager) handleAPIRepoStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	gateway := m.getGatewayNode()
	online := false
	var check agentRepoOpStatus
	var unlock agentRepoOpStatus
	var ok bool

	if gateway != "" {
		online = m.isAgentOnline(gateway)
		if online {
			agentCfg := m.config.GetAgentConfig(gateway)
			if agentCfg != nil {
				check, unlock, ok = m.fetchRepoStatusesFromAgent(agentCfg.IP)
				if ok {
					m.repoMu.Lock()
					m.repoCheckBusy = check.Busy
					m.repoUnlockBusy = unlock.Busy
					changed := false
					if check.LastAt != nil && check.LastAt.After(m.repoLastCheckAt) {
						m.repoLastCheckAt = *check.LastAt
						changed = true
					}
					if unlock.LastAt != nil && unlock.LastAt.After(m.repoLastUnlockAt) {
						m.repoLastUnlockAt = *unlock.LastAt
						changed = true
					}
					m.repoMu.Unlock()
					if changed {
						m.saveRepoState()
					}
				}
			}
		}
	}

	resp := RepoStatusResponse{GatewayNode: gateway, GatewayOnline: online}

	if ok {
		resp.Check = toRepoOpStatusResponse(check)
		resp.Unlock = toRepoOpStatusResponse(unlock)
		m.repoMu.Lock()
		if !m.repoLastStatusSyncAt.IsZero() {
			resp.LastStatusSyncAt = m.repoLastStatusSyncAt.Format(time.RFC3339)
		}
		m.repoMu.Unlock()
	} else {
		// Fallback to manager-cached state
		m.repoMu.Lock()
		busyCheck := m.repoCheckBusy
		busyUnlock := m.repoUnlockBusy
		lastCheck := m.repoLastCheckAt
		lastUnlock := m.repoLastUnlockAt
		lastCheckErr := m.repoLastCheckErr
		lastUnlockErr := m.repoLastUnlockErr
		lastSync := m.repoLastStatusSyncAt
		m.repoMu.Unlock()
		resp.Check = RepoOpStatusResponse{Busy: busyCheck}
		if !lastCheck.IsZero() {
			resp.Check.LastAt = lastCheck.Format(time.RFC3339)
		}
		resp.Check.LastError = lastCheckErr
		resp.Unlock = RepoOpStatusResponse{Busy: busyUnlock}
		if !lastUnlock.IsZero() {
			resp.Unlock.LastAt = lastUnlock.Format(time.RFC3339)
		}
		resp.Unlock.LastError = lastUnlockErr
		if !lastSync.IsZero() {
			resp.LastStatusSyncAt = lastSync.Format(time.RFC3339)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func toRepoOpStatusResponse(st agentRepoOpStatus) RepoOpStatusResponse {
	resp := RepoOpStatusResponse{Busy: st.Busy, LastError: st.LastError, ProgressPercent: st.ProgressPercent}
	if st.LastAt != nil {
		resp.LastAt = st.LastAt.Format(time.RFC3339)
	}
	return resp
}

// handleAPIRepoCheck triggers a repo-wide restic check on the gateway.
// POST /api/v1/repo/check
func (m *Manager) handleAPIRepoCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	gateway := m.getGatewayNode()
	if gateway == "" {
		http.Error(w, "no gateway", http.StatusServiceUnavailable)
		return
	}
	if !m.isAgentOnline(gateway) {
		http.Error(w, "gateway offline", http.StatusServiceUnavailable)
		return
	}
	if m.anyBackupBusy() || m.anyPruneBusy() || m.anyRestoreBusy() {
		http.Error(w, "backup running", http.StatusConflict)
		return
	}
	m.backupMu.Lock()
	hasAssignment := len(m.backupAssignments) > 0 || m.pruneAssignment != nil
	m.backupMu.Unlock()
	if hasAssignment {
		http.Error(w, "backup scheduled", http.StatusConflict)
		return
	}
	if m.isRepoBusy() {
		http.Error(w, "repo busy", http.StatusConflict)
		return
	}

	agentCfg := m.config.GetAgentConfig(gateway)
	if agentCfg == nil {
		http.Error(w, "gateway not in config", http.StatusNotFound)
		return
	}

	if err := m.triggerRepoCheckOnAgent(agentCfg.IP); err != nil {
		http.Error(w, "failed to reach agent: "+err.Error(), http.StatusBadGateway)
		return
	}

	m.repoMu.Lock()
	m.repoCheckBusy = true
	m.repoMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleAPIRepoUnlock triggers a repo-wide restic unlock on the gateway.
// POST /api/v1/repo/unlock
func (m *Manager) handleAPIRepoUnlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	gateway := m.getGatewayNode()
	if gateway == "" {
		http.Error(w, "no gateway", http.StatusServiceUnavailable)
		return
	}
	if !m.isAgentOnline(gateway) {
		http.Error(w, "gateway offline", http.StatusServiceUnavailable)
		return
	}
	agentCfg := m.config.GetAgentConfig(gateway)
	if agentCfg == nil {
		http.Error(w, "gateway not in config", http.StatusNotFound)
		return
	}

	status, body, err := m.requestAgentUnaryByIP(context.Background(), agentCfg.IP, "/unlock", nil)
	if err != nil {
		http.Error(w, "Failed to reach agent: "+err.Error(), http.StatusBadGateway)
		return
	}
	if status != http.StatusOK {
		http.Error(w, string(body), status)
		return
	}

	m.repoMu.Lock()
	m.repoUnlockBusy = true
	m.repoMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleAPIRepoForceUnlock triggers restic unlock --remove-all on the gateway,
// bypassing all busy checks.
// POST /api/v1/repo/force-unlock
func (m *Manager) handleAPIRepoForceUnlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	gateway := m.getGatewayNode()
	if gateway == "" {
		http.Error(w, "no gateway", http.StatusServiceUnavailable)
		return
	}
	if !m.isAgentOnline(gateway) {
		http.Error(w, "gateway offline", http.StatusServiceUnavailable)
		return
	}

	agentCfg := m.config.GetAgentConfig(gateway)
	if agentCfg == nil {
		http.Error(w, "gateway not in config", http.StatusNotFound)
		return
	}

	status, body, err := m.requestAgentUnaryByIP(context.Background(), agentCfg.IP, "/force-unlock", nil)
	if err != nil {
		http.Error(w, "Failed to reach agent: "+err.Error(), http.StatusBadGateway)
		return
	}
	if status != http.StatusOK {
		http.Error(w, string(body), status)
		return
	}

	m.repoMu.Lock()
	m.repoUnlockBusy = true
	m.repoMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleAPIRepoCheckLogs streams repo check logs from the gateway as SSE.
// GET /api/v1/repo/check-logs
func (m *Manager) handleAPIRepoCheckLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	m.proxyRepoLogs(w, r, "check-logs")
}

// handleAPIRepoUnlockLogs streams repo unlock logs from the gateway as SSE.
// GET /api/v1/repo/unlock-logs
func (m *Manager) handleAPIRepoUnlockLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	m.proxyRepoLogs(w, r, "unlock-logs")
}

func (m *Manager) proxyRepoLogs(w http.ResponseWriter, r *http.Request, path string) {
	gateway := m.getGatewayNode()
	if gateway == "" {
		http.Error(w, "no gateway", http.StatusServiceUnavailable)
		return
	}
	if !m.isAgentOnline(gateway) {
		http.Error(w, "gateway offline", http.StatusServiceUnavailable)
		return
	}
	agentCfg := m.config.GetAgentConfig(gateway)
	if agentCfg == nil {
		http.Error(w, "gateway not in config", http.StatusNotFound)
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

	stream, status, err := m.streamAgentByIP(agentCfg.IP, "/"+path, nil)
	if err != nil {
		w.Write([]byte("data: Error connecting to agent: " + err.Error() + "\n\n"))
		flusher.Flush()
		return
	}
	defer stream.Close()

	if status != http.StatusOK {
		w.Write([]byte("data: Agent returned error\n\n"))
		flusher.Flush()
		return
	}

	buf := make([]byte, 4096)
	for {
		select {
		case <-r.Context().Done():
			return
		default:
			n, err := stream.Read(buf)
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
