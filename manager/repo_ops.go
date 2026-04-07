package manager

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	agentpkg "github.com/jatinparab/homectl/agent"
)

var repoMgrLog = slog.With("area", "repo")

// RepoCheckInterval is the minimum interval between repo-wide restic checks.
const RepoCheckInterval = 24 * time.Hour

// enableAutoRepoChecks controls whether the manager auto-triggers periodic repo checks.
// Keep disabled for now; checks can still be triggered manually via the API/UI.
const enableAutoRepoChecks = false

type agentRepoOpStatus struct {
	Busy            bool       `json:"busy"`
	LastAt          *time.Time `json:"last_at,omitempty"`
	LastError       string     `json:"last_error,omitempty"`
	ProgressPercent *float64   `json:"progress_percent,omitempty"`
}

func (m *Manager) reconcileRepoOps(now time.Time) {
	// Refresh cached busy state from gateway (best-effort)
	gateway := m.getGatewayNode()
	if gateway == "" {
		return
	}
	if !m.isAgentOnline(gateway) {
		m.repoMu.Lock()
		shouldLog := m.repoLastStatusFetchErrLogAt.IsZero() || now.Sub(m.repoLastStatusFetchErrLogAt) > 2*time.Minute
		if shouldLog {
			m.repoLastStatusFetchErrLogAt = now
		}
		m.repoMu.Unlock()
		if shouldLog {
			repoMgrLog.Info("Gateway offline, skipping status sync", "gateway", gateway)
		}
		return
	}

	agentCfg := m.config.GetAgentConfig(gateway)
	if agentCfg == nil {
		repoMgrLog.Info("Gateway not in config, skipping", "gateway", gateway)
		return
	}

	check, unlock, ok := m.fetchRepoStatusesFromAgent(agentCfg.IP)
	if ok {
		m.repoMu.Lock()
		prevCheckBusy := m.repoCheckBusy
		prevUnlockBusy := m.repoUnlockBusy
		prevLastCheckAt := m.repoLastCheckAt
		prevLastUnlockAt := m.repoLastUnlockAt
		prevCheckErr := m.repoLastCheckErr
		prevUnlockErr := m.repoLastUnlockErr

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
		if check.LastError != m.repoLastCheckErr {
			m.repoLastCheckErr = check.LastError
			changed = true
		}
		if unlock.LastError != m.repoLastUnlockErr {
			m.repoLastUnlockErr = unlock.LastError
			changed = true
		}
		m.repoLastStatusSyncAt = now
		m.repoMu.Unlock()

		if prevCheckBusy != check.Busy || prevUnlockBusy != unlock.Busy {
			repoMgrLog.Info("Status busy changed", "check", check.Busy, "unlock", unlock.Busy)
		}
		if check.LastAt != nil && prevLastCheckAt.Before(*check.LastAt) {
			repoMgrLog.Info("Check completed", "at", check.LastAt.Format(time.RFC3339), "err", check.LastError)
		}
		if unlock.LastAt != nil && prevLastUnlockAt.Before(*unlock.LastAt) {
			repoMgrLog.Info("Unlock completed", "at", unlock.LastAt.Format(time.RFC3339), "err", unlock.LastError)
		}
		if prevCheckErr != check.LastError && check.LastError != "" {
			repoMgrLog.Info("Check error updated", "err", check.LastError)
		}
		if prevUnlockErr != unlock.LastError && unlock.LastError != "" {
			repoMgrLog.Info("Unlock error updated", "err", unlock.LastError)
		}

		if changed {
			m.saveRepoState()
		}
	} else {
		m.repoMu.Lock()
		shouldLog := m.repoLastStatusFetchErrLogAt.IsZero() || now.Sub(m.repoLastStatusFetchErrLogAt) > 2*time.Minute
		if shouldLog {
			m.repoLastStatusFetchErrLogAt = now
		}
		m.repoMu.Unlock()
		if shouldLog {
			repoMgrLog.Info("Failed to fetch status from gateway", "gateway", gateway, "ip", agentCfg.IP)
		}
	}

	// Don't schedule unless we have fresh status from gateway.
	if !ok {
		return
	}
	if !enableAutoRepoChecks {
		return
	}

	// Schedule repo check (manager-owned) if due and the repo is idle.
	m.repoMu.Lock()
	busy := m.repoCheckBusy || m.repoUnlockBusy
	last := m.repoLastCheckAt
	m.repoMu.Unlock()

	if busy {
		repoMgrLog.Info("Repo busy, not scheduling", "check", check.Busy, "unlock", unlock.Busy)
		return
	}
	if !last.IsZero() && now.Sub(last) < RepoCheckInterval {
		return
	}
	if m.anyBackupBusy() || m.anyPruneBusy() || m.anyRestoreBusy() {
		repoMgrLog.Info("Backup in progress, not scheduling repo check")
		return
	}
	// Avoid starting a repo check while a backup assignment is pending.
	m.backupMu.Lock()
	hasAssignment := len(m.backupAssignments) > 0 || m.pruneAssignment != nil
	m.backupMu.Unlock()
	if hasAssignment {
		repoMgrLog.Info("Backup scheduled, not scheduling repo check")
		return
	}

	if err := m.triggerRepoCheckOnAgent(agentCfg.IP); err != nil {
		repoMgrLog.Info("Repo check trigger failed", "err", err)
		return
	}

	m.repoMu.Lock()
	m.repoCheckBusy = true
	m.repoMu.Unlock()
	repoMgrLog.Info("Repo check triggered on gateway", "gateway", gateway)
}

func (m *Manager) getGatewayNode() string {
	m.gatewayMu.Lock()
	defer m.gatewayMu.Unlock()
	return m.gatewayNode
}

func (m *Manager) isAgentOnline(agentName string) bool {
	agent, ok := m.leaseManager.GetAgent(agentName)
	if !ok {
		return false
	}
	return time.Since(agent.LastSeen) < 30*time.Second
}

func (m *Manager) fetchRepoStatusesFromAgent(ip string) (check agentRepoOpStatus, unlock agentRepoOpStatus, ok bool) {
	checkURL := fmt.Sprintf("http://%s:%d/check-status", ip, agentpkg.ReadyServerPort)
	resp, err := m.httpClient.Get(checkURL)
	if err != nil {
		return agentRepoOpStatus{}, agentRepoOpStatus{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return agentRepoOpStatus{}, agentRepoOpStatus{}, false
	}
	if err := json.NewDecoder(resp.Body).Decode(&check); err != nil {
		return agentRepoOpStatus{}, agentRepoOpStatus{}, false
	}

	unlockURL := fmt.Sprintf("http://%s:%d/unlock-status", ip, agentpkg.ReadyServerPort)
	resp2, err := m.httpClient.Get(unlockURL)
	if err != nil {
		return agentRepoOpStatus{}, agentRepoOpStatus{}, false
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		return agentRepoOpStatus{}, agentRepoOpStatus{}, false
	}
	if err := json.NewDecoder(resp2.Body).Decode(&unlock); err != nil {
		return agentRepoOpStatus{}, agentRepoOpStatus{}, false
	}

	return check, unlock, true
}

func (m *Manager) triggerRepoCheckOnAgent(ip string) error {
	url := fmt.Sprintf("http://%s:%d/check", ip, agentpkg.ReadyServerPort)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("agent returned %s", resp.Status)
	}
	return nil
}
