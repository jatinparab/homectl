package manager

import (
	"log"
	"time"

	"github.com/jatinparab/homectl/common"
)

// BackupInterval is how long since last backup before an app is considered stale.
const BackupInterval = 1 * time.Hour

// PruneInterval is how long since last prune before an app is considered stale.
const PruneInterval = 3 * time.Hour

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
// It resolves state transitions for in-flight assignments and may create a new
// backup or prune assignment when the global lane is idle.
func (m *Manager) resolveWorkAssignments() (backup *BackupAssignment, prune *BackupAssignment) {
	// Existing backup assignment first
	if a := m.backupAssignment; a != nil {
		busy := m.isAppBackupBusy(a.AgentID, a.AppID)
		if !a.BackupStarted && busy {
			a.BackupStarted = true
			log.Printf("Backup assignment: %s/%s started", a.AgentID, a.AppID)
			return a, m.pruneAssignment
		}
		if a.BackupStarted && !busy {
			log.Printf("Backup assignment: %s/%s completed", a.AgentID, a.AppID)
			m.triggerGlobalRestore(a.AgentID, a.AppID)
			m.backupAssignment = nil
		} else if m.appBackupAtNewer(a.AgentID, a.AppID, a.AssignedAt) {
			log.Printf("Backup assignment: %s/%s completed (LastBackupAt updated)", a.AgentID, a.AppID)
			m.triggerGlobalRestore(a.AgentID, a.AppID)
			m.backupAssignment = nil
		} else if !a.BackupStarted && time.Since(a.AssignedAt) > 5*time.Minute {
			log.Printf("Backup assignment: %s/%s timed out (not started in 5m)", a.AgentID, a.AppID)
			m.backupAssignment = nil
		} else {
			return a, m.pruneAssignment
		}
	}

	// Existing prune assignment next
	if p := m.pruneAssignment; p != nil {
		busy := m.isAppPruneBusy(p.AgentID, p.AppID)
		if !p.BackupStarted && busy {
			p.BackupStarted = true
			log.Printf("Prune assignment: %s/%s started", p.AgentID, p.AppID)
			return nil, p
		}
		if p.BackupStarted && !busy {
			log.Printf("Prune assignment: %s/%s completed", p.AgentID, p.AppID)
			m.pruneAssignment = nil
		} else if m.appPruneAtNewer(p.AgentID, p.AppID, p.AssignedAt) {
			log.Printf("Prune assignment: %s/%s completed (LastPruneAt updated)", p.AgentID, p.AppID)
			m.pruneAssignment = nil
		} else if !p.BackupStarted && time.Since(p.AssignedAt) > 5*time.Minute {
			log.Printf("Prune assignment: %s/%s timed out (not started in 5m)", p.AgentID, p.AppID)
			m.pruneAssignment = nil
		} else {
			return m.backupAssignment, p
		}
	}

	// No current assignment — globally serialize backup/prune/restore/repo-ops.
	if m.anyBackupBusy() || m.anyPruneBusy() || m.anyRestoreBusy() || m.isRepoBusy() {
		return m.backupAssignment, m.pruneAssignment
	}

	// Find candidates.
	backupAgent, backupApp, backupOK := m.findStalestBackupApp()
	pruneAgent, pruneApp, pruneOK := m.findStalestPruneApp()

	if backupOK && pruneOK {
		// Alternate priority when both queues are pending.
		if m.lastScheduledWorkKind == AssignmentKindBackup {
			m.pruneAssignment = &BackupAssignment{
				Kind:       AssignmentKindPrune,
				AgentID:    pruneAgent,
				AppID:      pruneApp,
				AssignedAt: time.Now(),
			}
			m.lastScheduledWorkKind = AssignmentKindPrune
			log.Printf("Prune assignment: created %s/%s (alternating)", pruneAgent, pruneApp)
			return nil, m.pruneAssignment
		}

		m.backupAssignment = &BackupAssignment{
			Kind:       AssignmentKindBackup,
			AgentID:    backupAgent,
			AppID:      backupApp,
			AssignedAt: time.Now(),
		}
		m.lastScheduledWorkKind = AssignmentKindBackup
		log.Printf("Backup assignment: created %s/%s (alternating)", backupAgent, backupApp)
		return m.backupAssignment, nil
	}

	if backupOK {
		m.backupAssignment = &BackupAssignment{
			Kind:       AssignmentKindBackup,
			AgentID:    backupAgent,
			AppID:      backupApp,
			AssignedAt: time.Now(),
		}
		m.lastScheduledWorkKind = AssignmentKindBackup
		log.Printf("Backup assignment: created %s/%s", backupAgent, backupApp)
		return m.backupAssignment, nil
	}

	if pruneOK {
		m.pruneAssignment = &BackupAssignment{
			Kind:       AssignmentKindPrune,
			AgentID:    pruneAgent,
			AppID:      pruneApp,
			AssignedAt: time.Now(),
		}
		m.lastScheduledWorkKind = AssignmentKindPrune
		log.Printf("Prune assignment: created %s/%s", pruneAgent, pruneApp)
		return nil, m.pruneAssignment
	}

	return nil, nil
}

// findStalestBackupApp iterates all online agents' AppReports and finds the
// running+healthy app with stale backup data.
func (m *Manager) findStalestBackupApp() (agentID string, appID common.AppID, ok bool) {
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
			if !ok || report.LastBackupAt.Before(stalestTime) {
				stalestTime = report.LastBackupAt
				agentID = agent.Name
				appID = report.AppID
				ok = true
			}
		}
	}
	return
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
			log.Printf("%s assignment: clearing %s/%s (agent dead)", prefix, cur.AgentID, cur.AppID)
			*a = nil
			return
		}
		agent, ok := m.leaseManager.GetAgent(cur.AgentID)
		if !ok {
			log.Printf("%s assignment: clearing %s/%s (agent not found)", prefix, cur.AgentID, cur.AppID)
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
				log.Printf("%s assignment: clearing %s/%s (app is %s)", prefix, cur.AgentID, cur.AppID, report.ObservedState)
				*a = nil
				return
			}
			return
		}
		log.Printf("%s assignment: clearing %s/%s (app not in agent reports)", prefix, cur.AgentID, cur.AppID)
		*a = nil
	}

	invalidate("Backup", &m.backupAssignment, map[common.Phase]bool{
		common.PhaseBackupStopping: true,
		common.PhaseBackupStopped:  true,
		common.PhaseBackingUp:      true,
	})
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
		log.Printf("Global restore: queued %s for %d non-gateway agents", appID, len(pending))
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
