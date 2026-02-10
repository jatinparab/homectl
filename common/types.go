package common

import "time"

// AppID is a unique identifier for an application
type AppID string

// DesiredState represents the desired state of an application
type DesiredState string

const (
	DesiredRunning DesiredState = "running"
	DesiredStopped DesiredState = "stopped"
)

// ObservedState represents the observed state of an application
type ObservedState string

const (
	ObservedRunning    ObservedState = "running"
	ObservedStopped    ObservedState = "stopped"
	ObservedRestarting ObservedState = "restarting"
	ObservedError      ObservedState = "error"
)

// HealthStatus represents the health status of an application
type HealthStatus string

type RunningStatus string

const (
	RunningStatusRunning    RunningStatus = "running"
	RunningStatusStopped    RunningStatus = "stopped"
	RunningStatusRestarting RunningStatus = "restarting"
)

const (
	HealthUnknown   HealthStatus = "unknown"
	HealthHealthy   HealthStatus = "healthy"
	HealthUnhealthy HealthStatus = "unhealthy"
)

// Phase represents the lifecycle phase of an application
type Phase string

const (
	PhaseEmpty          Phase = "empty"
	PhaseRestoring      Phase = "restoring"
	PhaseReady          Phase = "ready"
	PhaseRunning        Phase = "running"
	PhaseError          Phase = "error"
	PhaseBackupStopping Phase = "backup_stopping"
	PhaseBackupStopped  Phase = "backup_stopped"
	PhaseBackingUp      Phase = "backing_up"
)

// AppReport is sent from agent to manager in heartbeats
type AppReport struct {
	AppID         AppID         `json:"app_id"`
	ObservedState ObservedState `json:"observed_state"`
	Health        HealthStatus  `json:"health"`
	Phase         Phase         `json:"phase"`
	LastError     string        `json:"last_error,omitempty"`
	RestartCount  int           `json:"restart_count"`
	Quarantined   bool          `json:"quarantined"`
	Port          int           `json:"port,omitempty"`
	BackupBusy    bool          `json:"backup_busy,omitempty"`
}

// BackupLeaseResponse is the manager's response to a backup lease request
type BackupLeaseResponse struct {
	Granted           bool      `json:"granted"`
	LeaseExpiresAt    time.Time `json:"lease_expires_at,omitempty"`
	Holder            string    `json:"holder,omitempty"`
	RetryAfterSeconds int       `json:"retry_after_seconds,omitempty"`
}

// DesiredApp is sent from manager to agent with desired state
type DesiredApp struct {
	AppID        AppID        `json:"app_id"`
	DesiredState DesiredState `json:"desired_state"`
}

// DesiredAppsResponse is the response from /agents/{id}/desired
type DesiredAppsResponse struct {
	Apps []DesiredApp `json:"apps"`
}
