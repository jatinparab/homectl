package manager

import (
	"log/slog"
	"time"

	"github.com/jatinparab/homectl/common"
)

var backupMgrLog = slog.With("area", "backup")

// BackupInterval is how long since last backup before an app is considered stale.
const BackupInterval = 1 * time.Hour

// PruneInterval is how long since last prune before an app is considered stale.
const PruneInterval = 24 * time.Hour

// SensitiveIdleThreshold is how long since the last active connection before a
// backup-sensitive app is eligible for backup.
const SensitiveIdleThreshold = 1 * time.Hour

const (
	AssignmentKindBackup = "backup"
	AssignmentKindPrune  = "prune"
)

// BackupAssignment represents a manager-assigned backup/prune task.
type BackupAssignment struct {
	Kind          string
	AgentID       string
	AppID         common.AppID
	AssignedAt    time.Time
	BackupStarted bool // set true when heartbeat shows corresponding busy=true
}

// resolveWorkAssignments is called from handleDesiredApps under backupMu.
// It resolves state transitions for in-flight assignments and may create new
// backup or prune assignments when allowed.
func (m *Manager) resolveWorkAssignments() (backups map[common.AppID]*BackupAssignment, prune *BackupAssignment) {
	for appID, a := range m.backupAssignments {
		busy := m.isAppBackupBusy(a.AgentID, a.AppID)
		if !a.BackupStarted && busy {
			a.BackupStarted = true
			backupMgrLog.Info("Backup assignment started", "agent", a.AgentID, "app", a.AppID)
			continue
		}
		if a.BackupStarted && !busy {
			backupMgrLog.Info("Backup assignment completed", "agent", a.AgentID, "app", a.AppID)
			m.triggerGlobalRestore(a.AgentID, a.AppID)
			delete(m.backupAssignments, appID)
			continue
		}
		if m.appBackupAtNewer(a.AgentID, a.AppID, a.AssignedAt) {
			backupMgrLog.Info("Backup assignment completed (LastBackupAt updated)", "agent", a.AgentID, "app", a.AppID)
			m.triggerGlobalRestore(a.AgentID, a.AppID)
			delete(m.backupAssignments, appID)
			continue
		}
		if !a.BackupStarted && time.Since(a.AssignedAt) > 5*time.Minute {
			backupMgrLog.Info("Backup assignment timed out (not started in 5m)", "agent", a.AgentID, "app", a.AppID)
			delete(m.backupAssignments, appID)
		}
	}

	// Existing prune assignment next
	if p := m.pruneAssignment; p != nil {
		busy := m.isAppPruneBusy(p.AgentID, p.AppID)
		if !p.BackupStarted && busy {
			p.BackupStarted = true
			backupMgrLog.Info("Prune assignment started", "agent", p.AgentID, "app", p.AppID)
			return m.backupAssignments, p
		}
		if p.BackupStarted && !busy {
			backupMgrLog.Info("Prune assignment completed", "agent", p.AgentID, "app", p.AppID)
			m.pruneAssignment = nil
		} else if m.appPruneAtNewer(p.AgentID, p.AppID, p.AssignedAt) {
			backupMgrLog.Info("Prune assignment completed (LastPruneAt updated)", "agent", p.AgentID, "app", p.AppID)
			m.pruneAssignment = nil
		} else if !p.BackupStarted && time.Since(p.AssignedAt) > 5*time.Minute {
			backupMgrLog.Info("Prune assignment timed out (not started in 5m)", "agent", p.AgentID, "app", p.AppID)
			m.pruneAssignment = nil
		} else {
			return m.backupAssignments, p
		}
	}

	// Prune and backup remain mutually exclusive.
	if m.pruneAssignment != nil || m.anyPruneBusy() || m.anyRestoreBusy() || m.isRepoBusy() {
		return m.backupAssignments, m.pruneAssignment
	}

	// Find candidates.
	backupCandidates := m.findStaleBackupApps()
	pruneAgent, pruneApp, pruneOK := m.findStalestPruneApp()

	if len(backupCandidates) > 0 {
		now := time.Now()
		for _, c := range backupCandidates {
			if _, ok := m.backupAssignments[c.AppID]; ok {
				continue
			}
			m.backupAssignments[c.AppID] = &BackupAssignment{
				Kind:       AssignmentKindBackup,
				AgentID:    c.AgentID,
				AppID:      c.AppID,
				AssignedAt: now,
			}
			backupMgrLog.Info("Backup assignment created", "agent", c.AgentID, "app", c.AppID)
		}
		m.lastScheduledWorkKind = AssignmentKindBackup
		return m.backupAssignments, m.pruneAssignment
	}

	if pruneOK {
		if len(m.backupAssignments) > 0 || m.anyBackupBusy() {
			return m.backupAssignments, nil
		}
		m.pruneAssignment = &BackupAssignment{
			Kind:       AssignmentKindPrune,
			AgentID:    pruneAgent,
			AppID:      pruneApp,
			AssignedAt: time.Now(),
		}
		m.lastScheduledWorkKind = AssignmentKindPrune
		backupMgrLog.Info("Prune assignment created", "agent", pruneAgent, "app", pruneApp)
		return m.backupAssignments, m.pruneAssignment
	}

	return m.backupAssignments, m.pruneAssignment
}

// findStaleBackupApps returns all running+healthy apps with stale backup data.
func (m *Manager) findStaleBackupApps() []BackupAssignment {
	agents := m.leaseManager.ListAgents()
	appConfigs := m.loadAppConfigs()
	now := time.Now()
	candidates := make([]BackupAssignment, 0)

	for _, agent := range agents {
		if now.Sub(agent.LastSeen) >= 30*time.Second {
			continue
		}
		for _, report := range agent.AppReports {
			cfg := appConfigs[string(report.AppID)]
			if cfg.BackupEnabled != nil && !*cfg.BackupEnabled {
				continue
			}
			if cfg.IsGlobal && agent.Name != m.gatewayNode {
				continue
			}
			if report.ObservedState != common.ObservedRunning || report.Health != common.HealthHealthy {
				continue
			}
			if report.BackupBusy || report.PruneBusy {
				continue
			}
			if !report.BackupDefer.IsZero() && now.Before(report.BackupDefer) {
				continue
			}
			interval := cfg.GetBackupInterval()
			if !report.LastBackupAt.IsZero() && report.LastBackupAt.After(now.Add(-interval)) {
				continue
			}
			if cfg.BackupSensitive {
				if !report.LastActiveAt.IsZero() && now.Sub(report.LastActiveAt) < SensitiveIdleThreshold {
					continue
				}
			}
			if _, scheduled := m.backupAssignments[report.AppID]; scheduled {
				continue
			}
			candidates = append(candidates, BackupAssignment{AgentID: agent.Name, AppID: report.AppID})
		}
	}

	return candidates
}

// findStalestPruneApp finds the app most overdue for prune.
func (m *Manager) findStalestPruneApp() (agentID string, appID common.AppID, ok bool) {
	agents := m.leaseManager.ListAgents()
	appConfigs := m.loadAppConfigs()
	now := time.Now()
	var stalestTime time.Time

	for _, agent := range agents {
		if now.Sub(agent.LastSeen) >= 30*time.Second {
			continue
		}
		for _, report := range agent.AppReports {
			cfg := appConfigs[string(report.AppID)]
			if cfg.BackupEnabled != nil && !*cfg.BackupEnabled {
				continue
			}
			if cfg.IsGlobal && agent.Name != m.gatewayNode {
				continue
			}
			if report.ObservedState != common.ObservedRunning || report.Health != common.HealthHealthy {
				continue
			}
			if report.BackupBusy || report.PruneBusy {
				continue
			}
			lastPruneAt := report.LastPruneAt
			if st, ok := m.pruneState[report.AppID]; ok && st.LastPruneAt.After(lastPruneAt) {
				lastPruneAt = st.LastPruneAt
			}
			if !lastPruneAt.IsZero() && lastPruneAt.After(now.Add(-PruneInterval)) {
				continue
			}
			if !ok || lastPruneAt.Before(stalestTime) {
				stalestTime = lastPruneAt
				agentID = agent.Name
				appID = report.AppID
				ok = true
			}
		}
	}
	return
}

// invalidateWorkAssignments clears stale assignments if their agent/app is no
// longer viable before they start.
func (m *Manager) invalidateWorkAssignments(alive map[string]bool) {
	m.backupMu.Lock()
	defer m.backupMu.Unlock()

	invalidate := func(prefix string, a **BackupAssignment, allowPhases map[common.Phase]bool) {
		cur := *a
		if cur == nil || cur.BackupStarted {
			return
		}
		if !alive[cur.AgentID] {
			backupMgrLog.Info("Clearing assignment (agent dead)", "kind", prefix, "agent", cur.AgentID, "app", cur.AppID)
			*a = nil
			return
		}
		agent, ok := m.leaseManager.GetAgent(cur.AgentID)
		if !ok {
			backupMgrLog.Info("Clearing assignment (agent not found)", "kind", prefix, "agent", cur.AgentID, "app", cur.AppID)
			*a = nil
			return
		}
		for _, report := range agent.AppReports {
			if report.AppID != cur.AppID {
				continue
			}
			if allowPhases[report.Phase] {
				return
			}
			if report.ObservedState != common.ObservedRunning {
				backupMgrLog.Debug("Clearing assignment (app not running)", "kind", prefix, "agent", cur.AgentID, "app", cur.AppID, "state", report.ObservedState)
				*a = nil
				return
			}
			return
		}
		backupMgrLog.Info("Clearing assignment (app not in agent reports)", "kind", prefix, "agent", cur.AgentID, "app", cur.AppID)
		*a = nil
	}

	for appID := range m.backupAssignments {
		a := m.backupAssignments[appID]
		ptr := &a
		invalidate("Backup", ptr, map[common.Phase]bool{
			common.PhaseBackupStopping: true,
			common.PhaseBackupStopped:  true,
			common.PhaseBackingUp:      true,
		})
		if *ptr == nil {
			delete(m.backupAssignments, appID)
		} else {
			m.backupAssignments[appID] = *ptr
		}
	}
	invalidate("Prune", &m.pruneAssignment, map[common.Phase]bool{})
}

// triggerGlobalRestore checks if the completed backup was for a global app on the
// gateway node. If so, it flags all other alive agents to restore that app.
// Called under backupMu.
func (m *Manager) triggerGlobalRestore(agentID string, appID common.AppID) {
	appConfigs := m.loadAppConfigs()
	cfg, ok := appConfigs[string(appID)]
	if !ok || !cfg.IsGlobal {
		return
	}

	m.gatewayMu.Lock()
	gateway := m.gatewayNode
	m.gatewayMu.Unlock()

	if agentID != gateway {
		return
	}

	agents := m.leaseManager.ListAgents()
	pending := make(map[string]bool)
	now := time.Now()
	for _, a := range agents {
		if a.Name == gateway || now.Sub(a.LastSeen) >= 30*time.Second {
			continue
		}
		pending[a.Name] = true
	}

	if len(pending) > 0 {
		m.pendingGlobalRestores[appID] = pending
		backupMgrLog.Info("Global restore queued", "app", appID, "agents", len(pending))
	}
}

// anyBackupBusy scans all online agents' reports for BackupBusy=true.
func (m *Manager) anyBackupBusy() bool {
	agents := m.leaseManager.ListAgents()
	now := time.Now()
	for _, agent := range agents {
		if now.Sub(agent.LastSeen) >= 30*time.Second {
			continue
		}
		for _, report := range agent.AppReports {
			if report.BackupBusy {
				return true
			}
		}
	}
	return false
}

// anyPruneBusy scans all online agents' reports for PruneBusy=true.
func (m *Manager) anyPruneBusy() bool {
	agents := m.leaseManager.ListAgents()
	now := time.Now()
	for _, agent := range agents {
		if now.Sub(agent.LastSeen) >= 30*time.Second {
			continue
		}
		for _, report := range agent.AppReports {
			if report.PruneBusy {
				return true
			}
		}
	}
	return false
}

// anyRestoreBusy detects active restores on online agents.
func (m *Manager) anyRestoreBusy() bool {
	agents := m.leaseManager.ListAgents()
	now := time.Now()
	for _, agent := range agents {
		if now.Sub(agent.LastSeen) >= 30*time.Second {
			continue
		}
		for _, report := range agent.AppReports {
			if report.Phase == common.PhaseRestoring {
				return true
			}
		}
	}
	return false
}

func (m *Manager) isAppBackupBusy(agentID string, appID common.AppID) bool {
	agent, ok := m.leaseManager.GetAgent(agentID)
	if !ok || time.Since(agent.LastSeen) >= 30*time.Second {
		return false
	}
	for _, report := range agent.AppReports {
		if report.AppID == appID {
			return report.BackupBusy
		}
	}
	return false
}

func (m *Manager) isAppPruneBusy(agentID string, appID common.AppID) bool {
	agent, ok := m.leaseManager.GetAgent(agentID)
	if !ok || time.Since(agent.LastSeen) >= 30*time.Second {
		return false
	}
	for _, report := range agent.AppReports {
		if report.AppID == appID {
			return report.PruneBusy
		}
	}
	return false
}

func (m *Manager) appBackupAtNewer(agentID string, appID common.AppID, than time.Time) bool {
	agent, ok := m.leaseManager.GetAgent(agentID)
	if !ok || time.Since(agent.LastSeen) >= 30*time.Second {
		return false
	}
	for _, report := range agent.AppReports {
		if report.AppID == appID {
			return report.LastBackupAt.After(than)
		}
	}
	return false
}

func (m *Manager) appPruneAtNewer(agentID string, appID common.AppID, than time.Time) bool {
	agent, ok := m.leaseManager.GetAgent(agentID)
	if !ok || time.Since(agent.LastSeen) >= 30*time.Second {
		return false
	}
	for _, report := range agent.AppReports {
		if report.AppID == appID {
			if report.LastPruneAt.After(than) {
				m.pruneState[appID] = PruneState{LastPruneAt: report.LastPruneAt, LastPruneError: report.LastPruneError}
				go m.savePruneState()
				return true
			}
			return false
		}
	}
	return false
}
