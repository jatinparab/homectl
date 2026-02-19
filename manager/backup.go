package manager

import (
	"log"
	"time"

	"github.com/jatinparab/homectl/common"
)

// BackupInterval is how long since last backup before an app is considered stale.
const BackupInterval = 1 * time.Hour

// SensitiveIdleThreshold is how long since the last active connection before a
// backup-sensitive app is eligible for backup.
const SensitiveIdleThreshold = 1 * time.Hour

// BackupAssignment represents the current manager-assigned backup task.
type BackupAssignment struct {
	AgentID       string
	AppID         common.AppID
	AssignedAt    time.Time
	BackupStarted bool // set true when heartbeat shows BackupBusy=true
}

// resolveBackupAssignment is called from handleDesiredApps under backupMu.
// It returns the current assignment (possibly nil) after applying state transitions.
func (m *Manager) resolveBackupAssignment() *BackupAssignment {
	a := m.backupAssignment

	if a != nil {
		// Check if the assigned agent reports BackupBusy for this app
		busy := m.isAppBackupBusy(a.AgentID, a.AppID)

		if !a.BackupStarted && busy {
			// Agent started the backup
			a.BackupStarted = true
			log.Printf("Backup assignment: %s/%s started", a.AgentID, a.AppID)
			return a
		}

		if a.BackupStarted && !busy {
			// Backup completed (was busy, now not)
			log.Printf("Backup assignment: %s/%s completed", a.AgentID, a.AppID)
			m.triggerGlobalRestore(a.AgentID, a.AppID)
			m.backupAssignment = nil
			return nil
		}

		// Check if agent's LastBackupAt is newer than AssignedAt (completed)
		if m.appBackupAtNewer(a.AgentID, a.AppID, a.AssignedAt) {
			log.Printf("Backup assignment: %s/%s completed (LastBackupAt updated)", a.AgentID, a.AppID)
			m.triggerGlobalRestore(a.AgentID, a.AppID)
			m.backupAssignment = nil
			return nil
		}

		if !a.BackupStarted && time.Since(a.AssignedAt) > 5*time.Minute {
			// Not started within 5 minutes — timeout
			log.Printf("Backup assignment: %s/%s timed out (not started in 5m)", a.AgentID, a.AppID)
			m.backupAssignment = nil
			return nil
		}

		// Assignment still active
		return a
	}

	// No current assignment — check if any backup is busy globally
	if m.anyBackupBusy() {
		return nil
	}

	// Find the stalest app and create a new assignment
	agentID, appID, ok := m.findStalestApp()
	if !ok {
		return nil
	}

	m.backupAssignment = &BackupAssignment{
		AgentID:    agentID,
		AppID:      appID,
		AssignedAt: time.Now(),
	}
	log.Printf("Backup assignment: created %s/%s", agentID, appID)
	return m.backupAssignment
}

// findStalestApp iterates all online agents' AppReports and finds the running+healthy
// app with the oldest LastBackupAt that is older than 1 hour.
// Apps with backup_enabled=false in their config are excluded.
func (m *Manager) findStalestApp() (agentID string, appID common.AppID, ok bool) {
	agents := m.leaseManager.ListAgents()
	appConfigs := m.loadAppConfigs()
	now := time.Now()
	var stalestTime time.Time

	for _, agent := range agents {
		for _, report := range agent.AppReports {
			cfg := appConfigs[string(report.AppID)]

			// Skip apps with backup disabled
			if cfg.BackupEnabled != nil && !*cfg.BackupEnabled {
				continue
			}
			// For global apps, only the gateway node should create backups
			if cfg.IsGlobal {
				if agent.Name != m.gatewayNode {
					continue
				}
			}
			if report.ObservedState != common.ObservedRunning {
				continue
			}
			if report.Health != common.HealthHealthy {
				continue
			}
			if report.BackupBusy {
				continue
			}

			// Defer first backup until BackupDefer has passed
			if !report.BackupDefer.IsZero() && now.Before(report.BackupDefer) {
				continue
			}

			// Use per-app interval (falls back to default)
			interval := cfg.GetBackupInterval()
			if !report.LastBackupAt.IsZero() && report.LastBackupAt.After(now.Add(-interval)) {
				continue
			}

			// Skip backup-sensitive apps that were recently active
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

// invalidateBackupAssignment clears the current backup assignment if the app
// is no longer viable for backup (agent dead, app stopped/unhealthy, desired stopped).
// Called from reconcileOnce so stale assignments are cleared promptly rather than
// waiting for the 5-minute timeout.
func (m *Manager) invalidateBackupAssignment(alive map[string]bool) {
	m.backupMu.Lock()
	defer m.backupMu.Unlock()

	a := m.backupAssignment
	if a == nil {
		return
	}

	// If the backup is already in progress, let it finish
	if a.BackupStarted {
		return
	}

	// Agent went offline
	if !alive[a.AgentID] {
		log.Printf("Backup assignment: clearing %s/%s (agent dead)", a.AgentID, a.AppID)
		m.backupAssignment = nil
		return
	}

	// Check the agent's heartbeat report for this app
	agent, ok := m.leaseManager.GetAgent(a.AgentID)
	if !ok {
		log.Printf("Backup assignment: clearing %s/%s (agent not found)", a.AgentID, a.AppID)
		m.backupAssignment = nil
		return
	}

	for _, report := range agent.AppReports {
		if report.AppID != a.AppID {
			continue
		}
		// App stopped as part of backup flow — keep the assignment
		if report.Phase == common.PhaseBackupStopping || report.Phase == common.PhaseBackupStopped || report.Phase == common.PhaseBackingUp {
			return
		}
		if report.ObservedState != common.ObservedRunning {
			log.Printf("Backup assignment: clearing %s/%s (app is %s)", a.AgentID, a.AppID, report.ObservedState)
			m.backupAssignment = nil
			return
		}
		// App still running — assignment stays
		return
	}

	// App not found in agent reports (removed or never reported)
	log.Printf("Backup assignment: clearing %s/%s (app not in agent reports)", a.AgentID, a.AppID)
	m.backupAssignment = nil
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
	for _, a := range agents {
		if a.Name != gateway {
			pending[a.Name] = true
		}
	}

	if len(pending) > 0 {
		m.pendingGlobalRestores[appID] = pending
		log.Printf("Global restore: queued %s for %d non-gateway agents", appID, len(pending))
	}
}

// anyBackupBusy scans all agents' reports for BackupBusy=true.
func (m *Manager) anyBackupBusy() bool {
	agents := m.leaseManager.ListAgents()
	for _, agent := range agents {
		for _, report := range agent.AppReports {
			if report.BackupBusy {
				return true
			}
		}
	}
	return false
}

// isAppBackupBusy checks if a specific app on a specific agent reports BackupBusy.
func (m *Manager) isAppBackupBusy(agentID string, appID common.AppID) bool {
	agent, ok := m.leaseManager.GetAgent(agentID)
	if !ok {
		return false
	}
	for _, report := range agent.AppReports {
		if report.AppID == appID {
			return report.BackupBusy
		}
	}
	return false
}

// appBackupAtNewer checks if the agent's LastBackupAt for the app is newer than the given time.
func (m *Manager) appBackupAtNewer(agentID string, appID common.AppID, than time.Time) bool {
	agent, ok := m.leaseManager.GetAgent(agentID)
	if !ok {
		return false
	}
	for _, report := range agent.AppReports {
		if report.AppID == appID {
			return report.LastBackupAt.After(than)
		}
	}
	return false
}
