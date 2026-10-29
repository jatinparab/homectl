package manager

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
)

var appLog = slog.With("area", "app")

// App is the manager's view of a single application.
type App struct {
	ID               common.AppID
	DesiredState     common.DesiredState
	AssignedAgent    *string
	ManuallyAssigned bool // If true, skip automatic rebalancing/migration for this app
	IsGlobal         bool // Run on all connected nodes
	RestoreEnabled   bool // Whether restore-on-start is enabled (default true)

	// Observational fields updated from heartbeats
	ObservedState common.ObservedState
	Health        common.HealthStatus
	RestartCount  int
	Quarantined   bool
	LastSeen      time.Time

	// Migration guards
	LastMigratedAt time.Time
	MigrationCount int
	QuarantinedOn  []string

	// RestoreSnapshotOverride stores a user-selected snapshot to restore from
	// instead of latest. Nil means restore from latest.
	RestoreSnapshotOverride *common.RestoreSnapshotOverride
}

// AppRegistry is a thread-safe registry of apps known to the manager.
type AppRegistry struct {
	mu   sync.RWMutex
	apps map[common.AppID]*App
}

// NewAppRegistry creates an empty AppRegistry.
func NewAppRegistry() *AppRegistry {
	return &AppRegistry{
		apps: make(map[common.AppID]*App),
	}
}

// Register adds an app with the given desired state. Idempotent: won't overwrite existing apps.
func (r *AppRegistry) Register(appID common.AppID, desiredState common.DesiredState, isGlobal bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, exists := r.apps[appID]; exists {
		// Update IsGlobal on re-discovery (config may have changed)
		existing.IsGlobal = isGlobal
		return
	}

	r.apps[appID] = &App{
		ID:             appID,
		DesiredState:   desiredState,
		IsGlobal:       isGlobal,
		RestoreEnabled: true,
	}
}

// Get returns a copy of the app, or nil if not found.
func (r *AppRegistry) Get(appID common.AppID) *App {
	r.mu.RLock()
	defer r.mu.RUnlock()

	app, ok := r.apps[appID]
	if !ok {
		return nil
	}
	cp := *app
	cp.QuarantinedOn = append([]string(nil), app.QuarantinedOn...)
	if app.RestoreSnapshotOverride != nil {
		o := *app.RestoreSnapshotOverride
		cp.RestoreSnapshotOverride = &o
	}
	return &cp
}

// ListAll returns copies of all apps.
func (r *AppRegistry) ListAll() []App {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]App, 0, len(r.apps))
	for _, app := range r.apps {
		cp := *app
		cp.QuarantinedOn = append([]string(nil), app.QuarantinedOn...)
		if app.RestoreSnapshotOverride != nil {
			o := *app.RestoreSnapshotOverride
			cp.RestoreSnapshotOverride = &o
		}
		result = append(result, cp)
	}
	return result
}

// AppsForAgent returns copies of all apps assigned to the given agent,
// including global apps (which run on all agents).
func (r *AppRegistry) AppsForAgent(agentName string) []App {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []App
	for _, app := range r.apps {
		assigned := app.AssignedAgent != nil && *app.AssignedAgent == agentName
		global := app.IsGlobal && app.DesiredState == common.DesiredRunning
		if assigned || global {
			cp := *app
			cp.QuarantinedOn = append([]string(nil), app.QuarantinedOn...)
			if app.RestoreSnapshotOverride != nil {
				o := *app.RestoreSnapshotOverride
				cp.RestoreSnapshotOverride = &o
			}
			result = append(result, cp)
		}
	}
	return result
}

// UpdateFromReport updates observational fields for a known app from a heartbeat report.
// Ignores unknown apps (REQ-HB-2).
func (r *AppRegistry) UpdateFromReport(report common.AppReport, agentName string, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	app, ok := r.apps[report.AppID]
	if !ok {
		return
	}

	// Old nodes keep reporting stopped/unknown apps after migration. Only
	// the owner can update singleton state; otherwise a stale report can
	// make a healthy workload look stopped and eligible for rebalancing.
	if !app.IsGlobal && (app.AssignedAgent == nil || *app.AssignedAgent != agentName) {
		return
	}
	app.ObservedState = report.ObservedState
	app.Health = report.Health
	app.RestartCount = report.RestartCount
	// Only accept quarantine status from the currently assigned agent to prevent
	// a stale report from the old agent re-quarantining an app after migration.
	if app.AssignedAgent != nil && *app.AssignedAgent == agentName {
		app.Quarantined = report.Quarantined
	}
	app.LastSeen = now
}

// AdoptRunningApps claims assignment for unassigned non-global workloads that
// are running or temporarily stopped for backup/restore. This preserves existing placement after
// a manager restart instead of redistributing from scratch.
func (r *AppRegistry) AdoptRunningApps(agentName string, reports []common.AppReport) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, report := range reports {
		app, ok := r.apps[report.AppID]
		if !ok {
			continue
		}
		if app.IsGlobal {
			continue
		}
		if app.AssignedAgent != nil {
			continue
		}
		if !reportOwnsWorkload(report) {
			continue
		}
		agent := agentName
		app.AssignedAgent = &agent
		appLog.Info("Adopted app from heartbeat", "app", report.AppID, "agent", agentName)
	}
}

// UpdateDesiredState updates the desired state for an app.
func (r *AppRegistry) UpdateDesiredState(appID common.AppID, desiredState common.DesiredState) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	app, ok := r.apps[appID]
	if !ok {
		return fmt.Errorf("app not found: %s", appID)
	}

	app.DesiredState = desiredState
	return nil
}

// UpdateRestoreEnabled sets whether restore-on-start is enabled for an app.
func (r *AppRegistry) UpdateRestoreEnabled(appID common.AppID, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	app, ok := r.apps[appID]
	if !ok {
		return fmt.Errorf("app not found: %s", appID)
	}

	app.RestoreEnabled = enabled
	return nil
}

// ReassignAgent manually moves an app to a specific agent and marks it as
// manually assigned so the reconciler will not rebalance or migrate it.
func (r *AppRegistry) ReassignAgent(appID common.AppID, agentName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	app, ok := r.apps[appID]
	if !ok {
		return fmt.Errorf("app not found: %s", appID)
	}

	agent := agentName
	app.AssignedAgent = &agent
	app.ManuallyAssigned = true
	return nil
}

// ClearManualAssignment removes the manual assignment flag, allowing the
// reconciler to rebalance or migrate the app freely.
func (r *AppRegistry) ClearManualAssignment(appID common.AppID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	app, ok := r.apps[appID]
	if !ok {
		return fmt.Errorf("app not found: %s", appID)
	}

	app.ManuallyAssigned = false
	return nil
}

// SetRestoreSnapshotOverride sets the restore snapshot override for an app.
func (r *AppRegistry) SetRestoreSnapshotOverride(appID common.AppID, override common.RestoreSnapshotOverride) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	app, ok := r.apps[appID]
	if !ok {
		return fmt.Errorf("app not found: %s", appID)
	}

	app.RestoreSnapshotOverride = &override
	return nil
}

// ClearRestoreSnapshotOverride clears the restore snapshot override for an app.
func (r *AppRegistry) ClearRestoreSnapshotOverride(appID common.AppID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	app, ok := r.apps[appID]
	if !ok {
		return fmt.Errorf("app not found: %s", appID)
	}

	app.RestoreSnapshotOverride = nil
	return nil
}

// WithLock gives the reconcile loop raw access to the apps map under a write lock.
func (r *AppRegistry) WithLock(fn func(apps map[common.AppID]*App)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(r.apps)
}
