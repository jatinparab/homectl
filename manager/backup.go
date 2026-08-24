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

// BackupAssignmentState tracks the lifecycle phase of a work assignment.
type BackupAssignmentState int

const (
	AssignmentPending   BackupAssignmentState = iota // Waiting for agent to start
	AssignmentRunning                                // Agent is executing
	AssignmentUncertain                              // Agent went offline during execution
	AssignmentFailed                                 // Explicit failure or timeout
	AssignmentCompleted                              // Positive success evidence observed
)

func (s BackupAssignmentState) String() string {
	switch s {
	case AssignmentPending:
		return "pending"
	case AssignmentRunning:
		return "running"
	case AssignmentUncertain:
		return "uncertain"
	case AssignmentFailed:
		return "failed"
	case AssignmentCompleted:
		return "completed"
	default:
		return "unknown"
	}
}

// BackupAssignment represents a manager-assigned backup/prune task.
type BackupAssignment struct {
	Kind          string
	AgentID       string
	AppID         common.AppID
	AssignedAt    time.Time
	State         BackupAssignmentState
	FailedAt      time.Time // When the assignment was marked failed
	FailCount     int       // Number of consecutive failures
	NextRetryAt   time.Time // When the next retry is allowed
	BackupStarted bool      // Deprecated: kept for migration compatibility
}

// resolveWorkAssignments is called from handleDesiredApps under backupMu.
// It resolves state transitions for in-flight assignments and may create new
// backup or prune assignments when allowed.
//
// Completion semantics: only positive evidence (LastBackupAt > AssignedAt)
// triggers success actions. A stopped or offline agent is never mistaken
// for a completed backup.
func (m *Manager) resolveWorkAssignments() (backups map[common.AppID]*BackupAssignment, prune *BackupAssignment) {
	m.cleanupBackupStateLocked()
	now := time.Now()

	for appID, a := range m.backupAssignments {
		switch a.State {
		case AssignmentPending:
			if m.isAppBackupBusy(a.AgentID, a.AppID) {
				a.State = AssignmentRunning
				a.BackupStarted = true
				backupMgrLog.Info("Backup assignment started", "agent", a.AgentID, "app", a.AppID)
				continue
			}
			// Timed out before starting
			if now.Sub(a.AssignedAt) > 5*time.Minute {
				a.State = AssignmentFailed
				a.FailedAt = now
				a.FailCount++
				a.NextRetryAt = now.Add(5 * time.Minute)
				backupMgrLog.Info("Backup assignment timed out (not started)", "agent", a.AgentID, "app", a.AppID, "fail_count", a.FailCount)
			}

		case AssignmentRunning:
			// Check for positive success evidence first
			if m.appBackupAtNewer(a.AgentID, a.AppID, a.AssignedAt) {
				a.State = AssignmentCompleted
				backupMgrLog.Info("Backup assignment completed (positive evidence)", "agent", a.AgentID, "app", a.AppID)
				m.triggerGlobalRestore(a.AgentID, a.AppID)
				delete(m.backupAssignments, appID)
				continue
			}
			// The agent is no longer reporting the backup as busy, but completion
			// evidence has not arrived yet. Stop dispatching the assignment and
			// allow time for a delayed heartbeat before treating it as failed.
			if !m.isAppBackupBusy(a.AgentID, a.AppID) {
				a.State = AssignmentUncertain
				a.FailedAt = now
				backupMgrLog.Info("Backup assignment uncertain (no completion evidence)", "agent", a.AgentID, "app", a.AppID)
			}

		case AssignmentUncertain:
			// Agent came back online and is still busy -> back to running
			if m.isAppBackupBusy(a.AgentID, a.AppID) {
				a.State = AssignmentRunning
				backupMgrLog.Info("Backup assignment back to running (agent reconnected)", "agent", a.AgentID, "app", a.AppID)
				continue
			}
			// Still offline or finished: check for positive evidence
			if m.appBackupAtNewer(a.AgentID, a.AppID, a.AssignedAt) {
				a.State = AssignmentCompleted
				backupMgrLog.Info("Backup assignment completed (positive evidence after reconnect)", "agent", a.AgentID, "app", a.AppID)
				m.triggerGlobalRestore(a.AgentID, a.AppID)
				delete(m.backupAssignments, appID)
				continue
			}
			// Uncertain for too long -> fail and retry
			if now.Sub(a.FailedAt) > 10*time.Minute {
				a.State = AssignmentFailed
				a.FailCount++
				a.NextRetryAt = now.Add(5 * time.Minute)
				backupMgrLog.Info("Backup assignment uncertain -> failed (timeout)", "agent", a.AgentID, "app", a.AppID, "fail_count", a.FailCount)
			}

		case AssignmentFailed:
			// Expired or app removed -> clean up
			if a.NextRetryAt.IsZero() || now.After(a.NextRetryAt) {
				backupMgrLog.Info("Backup assignment expired", "agent", a.AgentID, "app", a.AppID)
				delete(m.backupAssignments, appID)
			}
		}
	}

	// Existing prune assignment next
	if p := m.pruneAssignment; p != nil {
		switch p.State {
		case AssignmentPending:
			if m.isAppPruneBusy(p.AgentID, p.AppID) {
				p.State = AssignmentRunning
				p.BackupStarted = true
				backupMgrLog.Info("Prune assignment started", "agent", p.AgentID, "app", p.AppID)
				return m.backupAssignments, p
			}
			if now.Sub(p.AssignedAt) > 5*time.Minute {
				p.State = AssignmentFailed
				p.FailedAt = now
				p.FailCount++
				p.NextRetryAt = now.Add(5 * time.Minute)
				backupMgrLog.Info("Prune assignment timed out (not started)", "agent", p.AgentID, "app", p.AppID)
			}

		case AssignmentRunning:
			if m.appPruneAtNewer(p.AgentID, p.AppID, p.AssignedAt) {
				p.State = AssignmentCompleted
				backupMgrLog.Info("Prune assignment completed", "agent", p.AgentID, "app", p.AppID)
				m.pruneAssignment = nil
			} else if !m.isAppPruneBusy(p.AgentID, p.AppID) {
				p.State = AssignmentUncertain
				p.FailedAt = now
				backupMgrLog.Info("Prune assignment uncertain (no completion evidence)", "agent", p.AgentID, "app", p.AppID)
			}

		case AssignmentUncertain:
			if m.isAppPruneBusy(p.AgentID, p.AppID) {
				p.State = AssignmentRunning
			} else if m.appPruneAtNewer(p.AgentID, p.AppID, p.AssignedAt) {
				p.State = AssignmentCompleted
				m.pruneAssignment = nil
			} else if now.Sub(p.FailedAt) > 10*time.Minute {
				p.State = AssignmentFailed
				p.FailCount++
				p.NextRetryAt = now.Add(5 * time.Minute)
			}

		case AssignmentFailed:
			if p.NextRetryAt.IsZero() || now.After(p.NextRetryAt) {
				backupMgrLog.Info("Prune assignment expired", "agent", p.AgentID, "app", p.AppID)
				m.pruneAssignment = nil
			}
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
		for _, c := range backupCandidates {
			if _, ok := m.backupAssignments[c.AppID]; ok {
				continue
			}
			m.backupAssignments[c.AppID] = &BackupAssignment{
				Kind:       AssignmentKindBackup,
				AgentID:    c.AgentID,
				AppID:      c.AppID,
				AssignedAt: now,
				State:      AssignmentPending,
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
			AssignedAt: now,
			State:      AssignmentPending,
		}
		m.lastScheduledWorkKind = AssignmentKindPrune
		backupMgrLog.Info("Prune assignment created", "agent", pruneAgent, "app", pruneApp)
		return m.backupAssignments, m.pruneAssignment
	}

	return m.backupAssignments, m.pruneAssignment
}

// cleanupBackupStateLocked prunes stale bookkeeping entries.
// Must be called with backupMu held.
func (m *Manager) cleanupBackupStateLocked() {
	validApps := make(map[common.AppID]bool)
	for _, app := range m.appRegistry.ListAll() {
		validApps[app.ID] = true
	}
	appConfigs, err := m.LoadAppConfigs(nil, false)
	if err != nil {
		backupMgrLog.Warn("Failed to load app configs; using defaults",
			"err", err)
		appConfigs = make(map[string]HomectlConfig)
	}

	knownAgents := make(map[string]bool)
	for _, agent := range m.leaseManager.ListAgents() {
		knownAgents[agent.Name] = true
	}

	for appID := range m.pruneState {
		if !validApps[appID] || !backupEnabledForApp(appID, appConfigs) {
			delete(m.pruneState, appID)
		}
	}
	gateway := m.getGatewayNode()
	for appID, assignment := range m.backupAssignments {
		cfg := appConfigs[string(appID)]
		if !validApps[appID] || !backupEnabledForApp(appID, appConfigs) ||
			(cfg.UsesGatewayOnlyBackups() && assignment.State == AssignmentPending && assignment.AgentID != gateway) {
			delete(m.backupAssignments, appID)
		}
	}
	if m.pruneAssignment != nil {
		cfg := appConfigs[string(m.pruneAssignment.AppID)]
		if !validApps[m.pruneAssignment.AppID] || !backupEnabledForApp(m.pruneAssignment.AppID, appConfigs) ||
			(cfg.UsesGatewayOnlyBackups() && m.pruneAssignment.State == AssignmentPending && m.pruneAssignment.AgentID != gateway) {
			m.pruneAssignment = nil
		}
	}
	for appID, pending := range m.pendingGlobalRestores {
		cfg := appConfigs[string(appID)]
		if !validApps[appID] || !backupEnabledForApp(appID, appConfigs) || cfg.UsesGatewayOnlyBackups() {
			delete(m.pendingGlobalRestores, appID)
			continue
		}
		for agentID := range pending {
			if !knownAgents[agentID] {
				delete(pending, agentID)
			}
		}
		if len(pending) == 0 {
			delete(m.pendingGlobalRestores, appID)
		}
	}
}

// backupNodeAllowed applies node-specific backup policies to manual work.
// Automatic work for global apps is already assigned to the current gateway.
func (m *Manager) backupNodeAllowed(appID common.AppID, agentID string) (allowed bool, gateway string) {
	appConfigs, err := m.LoadAppConfigs(nil, false)
	if err != nil {
		backupMgrLog.Warn("Failed to load app configs while checking backup policy", "app", appID, "err", err)
		return true, ""
	}
	cfg, ok := appConfigs[string(appID)]
	if !ok || !cfg.UsesGatewayOnlyBackups() {
		return true, ""
	}
	gateway = m.getGatewayNode()
	return gateway != "" && agentID == gateway, gateway
}

// findStaleBackupApps returns all running+healthy apps with stale backup data.
func (m *Manager) findStaleBackupApps() []BackupAssignment {
	agents := m.leaseManager.ListAgents()
	restoreOverrides := make(map[common.AppID]bool)
	for _, app := range m.appRegistry.ListAll() {
		if app.RestoreSnapshotOverride != nil {
			restoreOverrides[app.ID] = true
		}
	}
	appConfigs, err := m.LoadAppConfigs(nil, false)
	if err != nil {
		backupMgrLog.Warn("Failed to load app configs; using defaults",
			"err", err)
		appConfigs = make(map[string]HomectlConfig)
	}
	now := time.Now()
	gateway := m.getGatewayNode()
	candidates := make([]BackupAssignment, 0)

	for _, agent := range agents {
		if now.Sub(agent.LastSeen) >= 30*time.Second {
			continue
		}
		for _, report := range agent.AppReports {
			cfg := appConfigs[string(report.AppID)]
			if !backupEnabledForApp(report.AppID, appConfigs) {
				continue
			}
			if cfg.IsGlobal && agent.Name != gateway {
				continue
			}
			if report.ObservedState != common.ObservedRunning || report.Health != common.HealthHealthy {
				continue
			}
			if restoreOverrides[report.AppID] {
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
	appConfigs, err := m.LoadAppConfigs(nil, false)
	if err != nil {
		backupMgrLog.Warn("Failed to load app configs; using defaults",
			"err", err)
		appConfigs = make(map[string]HomectlConfig)
	}
	now := time.Now()
	gateway := m.getGatewayNode()
	var stalestTime time.Time

	for _, agent := range agents {
		if now.Sub(agent.LastSeen) >= 30*time.Second {
			continue
		}
		for _, report := range agent.AppReports {
			cfg := appConfigs[string(report.AppID)]
			if !backupEnabledForApp(report.AppID, appConfigs) {
				continue
			}
			if cfg.IsGlobal && agent.Name != gateway {
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

// invalidateWorkAssignments clears stale pending assignments if their agent/app
// is no longer viable. Running, uncertain, and failed assignments are left alone.
func (m *Manager) invalidateWorkAssignments(alive map[string]bool) {
	m.backupMu.Lock()
	defer m.backupMu.Unlock()

	invalidate := func(prefix string, a **BackupAssignment) {
		cur := *a
		if cur == nil || cur.State != AssignmentPending {
			return
		}
		if !alive[cur.AgentID] {
			backupMgrLog.Info("Clearing pending assignment (agent dead)", "kind", prefix, "agent", cur.AgentID, "app", cur.AppID)
			*a = nil
			return
		}
		agent, ok := m.leaseManager.GetAgent(cur.AgentID)
		if !ok {
			backupMgrLog.Info("Clearing pending assignment (agent not found)", "kind", prefix, "agent", cur.AgentID, "app", cur.AppID)
			*a = nil
			return
		}
		for _, report := range agent.AppReports {
			if report.AppID != cur.AppID {
				continue
			}
			if report.ObservedState != common.ObservedRunning {
				backupMgrLog.Debug("Clearing pending assignment (app not running)", "kind", prefix, "agent", cur.AgentID, "app", cur.AppID, "state", report.ObservedState)
				*a = nil
				return
			}
			return
		}
		backupMgrLog.Info("Clearing pending assignment (app not in agent reports)", "kind", prefix, "agent", cur.AgentID, "app", cur.AppID)
		*a = nil
	}

	for appID := range m.backupAssignments {
		a := m.backupAssignments[appID]
		ptr := &a
		invalidate("Backup", ptr)
		if *ptr == nil {
			delete(m.backupAssignments, appID)
		} else {
			m.backupAssignments[appID] = *ptr
		}
	}
	invalidate("Prune", &m.pruneAssignment)
}

// triggerGlobalRestore checks if the completed backup was for a global app on the
// gateway node. If so, it flags all other alive agents to restore that app.
// Called under backupMu.
func (m *Manager) triggerGlobalRestore(agentID string, appID common.AppID) {
	appConfigs, err := m.LoadAppConfigs(nil, false)
	if err != nil {
		backupMgrLog.Warn("Failed to load app configs; using defaults",
			"err", err)
		appConfigs = make(map[string]HomectlConfig)
	}
	cfg, ok := appConfigs[string(appID)]
	if !ok || !cfg.IsGlobal || cfg.UsesGatewayOnlyBackups() {
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

func assignmentShouldDispatch(a *BackupAssignment) bool {
	if a == nil {
		return false
	}
	return a.State == AssignmentPending || a.State == AssignmentRunning
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
				m.requestPruneStateSaveLocked()
				return true
			}
			return false
		}
	}
	return false
}
