package agent

import (
	"sync"
	"time"

	"github.com/jatinparab/homectl/common"
)

type BackupState struct {
	LastBackupAt time.Time
	BackupDefer  time.Time // Don't schedule first backup before this time
	InProgress   bool
	LastError    string
	JustBackedUp bool // set after successful local backup; cleared on next start
}

type PruneState struct {
	LastPruneAt time.Time
	InProgress  bool
	LastError   string
}

// AppRuntime tracks the runtime state of a single application
type AppRuntime struct {
	mu sync.RWMutex

	AppID             common.AppID
	Phase             common.Phase           // Stored lifecycle phase
	Running           common.RunningStatus   // From sensor: is container running?
	Health            common.HealthStatus    // From health check
	LastSeen          time.Time              // Last time sensor saw this app
	RestartCount      int                    // Incremented on each start/restart
	Quarantined       bool                   // Set when RestartCount >= MaxRestarts
	InFlight          bool                   // Action in progress
	InFlightAction    string                 // Description of the in-flight action
	LastError         string                 // Last error message
	Port              int                    // First exposed port (for gateway proxy routing)
	Progress          *common.ResticProgress // Restic backup/restore progress
	Backup            BackupState
	Prune             PruneState
	BackupSensitive   bool            // App is sensitive to backup interruptions (e.g. media streaming)
	BackupEnabled     bool            // Whether backups/restores are enabled for this app (default true)
	RestoreEnabled    bool            // Whether restore-on-start is enabled (default true)
	PhaseLog          *PhaseLogBuffer // Ring buffer for phase (pull) logs
	UnhealthyCount    int             // Consecutive unhealthy health checks
	LastActiveAt      time.Time       // Last time active TCP connections were seen
	PostBackupPhase   string          // Phase of post-backup steps (prune/check), separate from lifecycle Phase
	RestoreSnapshotID string          // Selected snapshot ID for restore override
}

// NewAppRuntime creates a new AppRuntime for the given app
func NewAppRuntime(appID common.AppID) *AppRuntime {
	return &AppRuntime{
		AppID:          appID,
		Health:         common.HealthUnknown,
		BackupEnabled:  true, // default enabled
		RestoreEnabled: true, // default enabled
		PhaseLog:       NewPhaseLogBuffer(),
		Backup: BackupState{
			BackupDefer: time.Now().Add(1 * time.Hour), // Defer first backup by 1 hour
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

// SetHealth updates the health status and tracks consecutive unhealthy checks.
func (r *AppRuntime) SetHealth(health common.HealthStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Health = health
	if health == common.HealthUnhealthy {
		r.UnhealthyCount++
	} else {
		r.UnhealthyCount = 0
	}
}

// GetUnhealthyCount returns the consecutive unhealthy check count (thread-safe).
func (r *AppRuntime) GetUnhealthyCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.UnhealthyCount
}

// IncrementRestarts increments the restart count
func (r *AppRuntime) IncrementRestarts() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.RestartCount++
}

// SetRestartCount sets the restart count
func (r *AppRuntime) SetRestartCount(count int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.RestartCount = count
}

// ResetForRestart atomically clears runtime state so the app gets a fresh start.
// It does NOT reset InFlight — if an action is executing, clearing it causes
// the reconcile loop to submit duplicate starts.
func (r *AppRuntime) ResetForRestart() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.RestartCount = 0
	r.Quarantined = false
	r.LastError = ""
	r.Phase = common.PhaseEmpty
	r.Health = common.HealthUnknown
	r.Progress = nil
}

// SetQuarantined sets the quarantine status
func (r *AppRuntime) SetQuarantined(quarantined bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Quarantined = quarantined
}

// SetInFlight sets the in-flight status with an optional action description.
func (r *AppRuntime) SetInFlight(inFlight bool, action ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.InFlight = inFlight
	if inFlight && len(action) > 0 {
		r.InFlightAction = action[0]
	} else if !inFlight {
		r.InFlightAction = ""
	}
}

// GetInFlightAction returns the description of the in-flight action (thread-safe).
func (r *AppRuntime) GetInFlightAction() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.InFlightAction
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

// SetProgress sets the restic progress
func (r *AppRuntime) SetProgress(p *common.ResticProgress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Progress = p
}

// ClearProgress clears the restic progress
func (r *AppRuntime) ClearProgress() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Progress = nil
}

// SetPostBackupPhase sets the post-backup phase (e.g. "backup_pruning", "backup_checking", "")
func (r *AppRuntime) SetPostBackupPhase(phase string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.PostBackupPhase = phase
}

// GetPostBackupPhase returns the current post-backup phase (thread-safe)
func (r *AppRuntime) GetPostBackupPhase() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.PostBackupPhase
}

// IsPostProcessing returns true if post-backup steps (prune/check) are in progress
func (r *AppRuntime) IsPostProcessing() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.PostBackupPhase != ""
}

// SetBackupSensitive sets the backup-sensitive flag
func (r *AppRuntime) SetBackupSensitive(sensitive bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.BackupSensitive = sensitive
}

// IsBackupSensitive returns the backup-sensitive flag (thread-safe)
func (r *AppRuntime) IsBackupSensitive() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.BackupSensitive
}

// SetBackupEnabled sets whether backups/restores are enabled
func (r *AppRuntime) SetBackupEnabled(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.BackupEnabled = enabled
}

// IsBackupEnabled returns whether backups/restores are enabled (thread-safe)
func (r *AppRuntime) IsBackupEnabled() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.BackupEnabled
}

// SetRestoreEnabled sets whether restore-on-start is enabled
func (r *AppRuntime) SetRestoreEnabled(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.RestoreEnabled = enabled
}

// IsRestoreEnabled returns whether restore-on-start is enabled (thread-safe)
func (r *AppRuntime) IsRestoreEnabled() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.RestoreEnabled
}

// SetRestoreSnapshotID sets the snapshot ID for restore override
func (r *AppRuntime) SetRestoreSnapshotID(snapshotID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.RestoreSnapshotID = snapshotID
}

// GetRestoreSnapshotID returns the snapshot ID for restore override (thread-safe)
func (r *AppRuntime) GetRestoreSnapshotID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.RestoreSnapshotID
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
		AppID:             r.AppID,
		Phase:             r.Phase,
		Running:           r.Running,
		Health:            r.Health,
		LastSeen:          r.LastSeen,
		RestartCount:      r.RestartCount,
		Quarantined:       r.Quarantined,
		InFlight:          r.InFlight,
		LastError:         r.LastError,
		Port:              r.Port,
		Progress:          r.Progress,
		Backup:            r.Backup,
		BackupSensitive:   r.BackupSensitive,
		BackupEnabled:     r.BackupEnabled,
		RestoreEnabled:    r.RestoreEnabled,
		RestoreSnapshotID: r.RestoreSnapshotID,
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
		AppID:           r.AppID,
		ObservedState:   observed,
		Health:          r.Health,
		Phase:           phase,
		LastError:       r.LastError,
		RestartCount:    r.RestartCount,
		Quarantined:     r.Quarantined,
		Port:            r.Port,
		BackupBusy:      r.Backup.InProgress,
		PruneBusy:       r.Prune.InProgress,
		Progress:        r.Progress,
		LastBackupAt:    r.Backup.LastBackupAt,
		LastPruneAt:     r.Prune.LastPruneAt,
		BackupDefer:     r.Backup.BackupDefer,
		LastActiveAt:    r.LastActiveAt,
		LastBackupError: r.Backup.LastError,
		LastPruneError:  r.Prune.LastError,
		PostBackupPhase: r.PostBackupPhase,
	}
}
