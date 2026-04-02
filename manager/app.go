package manager

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
)

// App is the manager's view of a single application.
type App struct {
	ID             common.AppID
	DesiredState   common.DesiredState
	AssignedAgent  *string
	IsGlobal       bool // Run on all connected nodes
	RestoreEnabled bool // Whether restore-on-start is enabled (default true)

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
	copy := *app
	copy.QuarantinedOn = append([]string(nil), app.QuarantinedOn...)
	return &copy
}

// ListAll returns copies of all apps.
func (r *AppRegistry) ListAll() []App {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]App, 0, len(r.apps))
	for _, app := range r.apps {
		copy := *app
		copy.QuarantinedOn = append([]string(nil), app.QuarantinedOn...)
		result = append(result, copy)
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
			copy := *app
			copy.QuarantinedOn = append([]string(nil), app.QuarantinedOn...)
			result = append(result, copy)
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

// AdoptRunningApps claims assignment for non-global apps that an agent reports as
// running but that have no AssignedAgent. This preserves existing placement after
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
		if report.ObservedState != common.ObservedRunning {
			continue
		}
		agent := agentName
		app.AssignedAgent = &agent
		log.Printf("Adopted app %s on agent %s (from heartbeat)", report.AppID, agentName)
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

// WithLock gives the reconcile loop raw access to the apps map under a write lock.
func (r *AppRegistry) WithLock(fn func(apps map[common.AppID]*App)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(r.apps)
}
