package agent

import (
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
)

type BackupState struct {
	LastBackupAt time.Time
	InProgress   bool
	LastError    string
}

// AppRuntime tracks the runtime state of a single application
type AppRuntime struct {
	mu sync.RWMutex

	AppID        common.AppID
	Phase        common.Phase         // Stored lifecycle phase
	Running      common.RunningStatus // From sensor: is container running?
	Health       common.HealthStatus  // From health check
	LastSeen     time.Time            // Last time sensor saw this app
	RestartCount int                  // Incremented on each start/restart
	Quarantined  bool                 // Set when RestartCount >= MaxRestarts
	InFlight     bool                 // Action in progress
	LastError    string               // Last error message
	Port         int                  // First exposed port (for traefik routing)
	Backup       BackupState
}

// NewAppRuntime creates a new AppRuntime for the given app
func NewAppRuntime(appID common.AppID) *AppRuntime {
	return &AppRuntime{
		AppID:  appID,
		Health: common.HealthUnknown,
		Backup: BackupState{
			LastBackupAt: time.Now(), // Defer first backup by 1 hour
		},
	}
}

// SetPhase updates the lifecycle phase
func (r *AppRuntime) SetPhase(phase common.Phase) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Phase = phase
}

// GetPhase returns the current lifecycle phase (thread-safe)
func (r *AppRuntime) GetPhase() common.Phase {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.Phase
}

// SetRunning updates the running state
func (r *AppRuntime) SetRunning(running common.RunningStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Running = running
	r.LastSeen = time.Now()
}

func (r *AppRuntime) ResetRestartCount() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.RestartCount = 0
}

// SetHealth updates the health status
func (r *AppRuntime) SetHealth(health common.HealthStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Health = health
}

// IncrementRestarts increments the restart count
func (r *AppRuntime) IncrementRestarts() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.RestartCount++
}

// SetQuarantined sets the quarantine status
func (r *AppRuntime) SetQuarantined(quarantined bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Quarantined = quarantined
}

// SetInFlight sets the in-flight status
func (r *AppRuntime) SetInFlight(inFlight bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.InFlight = inFlight
}

// SetLastError sets the last error
func (r *AppRuntime) SetLastError(err string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.LastError = err
}

// SetPort sets the exposed port
func (r *AppRuntime) SetPort(port int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Port = port
}

// RunningStatus returns whether the app is running (thread-safe)
func (r *AppRuntime) RunningStatus() common.RunningStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.Running
}

// IsQuarantined returns whether the app is quarantined (thread-safe)
func (r *AppRuntime) IsQuarantined() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.Quarantined
}

// IsInFlight returns whether an action is in flight (thread-safe)
func (r *AppRuntime) IsInFlight() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.InFlight
}

// GetHealth returns the health status (thread-safe)
func (r *AppRuntime) GetHealth() common.HealthStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.Health
}

// GetRestartCount returns the restart count (thread-safe)
func (r *AppRuntime) GetRestartCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.RestartCount
}

// Snapshot returns a copy of the runtime state (thread-safe)
func (r *AppRuntime) Snapshot() AppRuntime {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return AppRuntime{
		AppID:        r.AppID,
		Phase:        r.Phase,
		Running:      r.Running,
		Health:       r.Health,
		LastSeen:     r.LastSeen,
		RestartCount: r.RestartCount,
		Quarantined:  r.Quarantined,
		InFlight:     r.InFlight,
		LastError:    r.LastError,
		Port:         r.Port,
		Backup:       r.Backup,
	}
}

// ToReport converts runtime state to an AppReport for heartbeat
func (r *AppRuntime) ToReport() common.AppReport {
	r.mu.RLock()
	defer r.mu.RUnlock()

	observed := common.ObservedStopped
	switch r.Running {
	case common.RunningStatusRunning:
		observed = common.ObservedRunning
	case common.RunningStatusRestarting:
		observed = common.ObservedRestarting
	}
	if r.LastError != "" {
		observed = common.ObservedError
	}

	phase := r.Phase
	if phase == "" || phase == common.PhaseEmpty {
		if r.Running == common.RunningStatusRunning {
			phase = common.PhaseRunning
		} else if r.LastError != "" {
			phase = common.PhaseError
		} else {
			phase = common.PhaseEmpty
		}
	}

	return common.AppReport{
		AppID:         r.AppID,
		ObservedState: observed,
		Health:        r.Health,
		Phase:         phase,
		LastError:     r.LastError,
		RestartCount:  r.RestartCount,
		Quarantined:   r.Quarantined,
		Port:          r.Port,
		BackupBusy:    r.Backup.InProgress,
	}
}
