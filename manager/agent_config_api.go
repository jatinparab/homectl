package manager

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jatinparab/homectl/common"
)

func (m *Manager) handleAPIAgentConfig(w http.ResponseWriter, r *http.Request) {
	agentName := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/agent-configs/"), "/")
	if agentName == "" {
		http.Error(w, "agent name required", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodPut:
		var req common.NodeConfigUIRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid config request", http.StatusBadRequest)
			return
		}
		resp, err := m.agentConfig.UpsertNodeConfig(r.Context(), agentName, req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := m.SyncCaddyConfig(); err != nil {
			apiLog.Warn("Failed to sync Caddy after config update", "err", err)
		}
		writeJSON(w, resp)
	case http.MethodGet:
		resp, err := m.agentConfig.GetForUI(r.Context(), agentName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, resp)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}
