package common

import "time"

// NodeMetrics represents host-level runtime metrics reported by an agent.
type NodeMetrics struct {
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryTotal   uint64  `json:"memory_total"`
	MemoryUsed    uint64  `json:"memory_used"`
	MemoryPercent float64 `json:"memory_percent"`
	DiskTotal     uint64  `json:"disk_total"`
	DiskUsed      uint64  `json:"disk_used"`
	DiskFree      uint64  `json:"disk_free"`
	DiskPercent   float64 `json:"disk_percent"`
}

// AppID is a unique identifier for an application
type AppID string

const GatewayProxyAppID AppID = "caddy"

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
	PhasePulling        Phase = "pulling"
)

// ResticProgress tracks progress of a restic backup/restore operation
type ResticProgress struct {
	PercentDone float64 `json:"percent_done"`
	TotalBytes  uint64  `json:"total_bytes"`
	BytesDone   uint64  `json:"bytes_done"`
	TotalFiles  uint64  `json:"total_files"`
	FilesDone   uint64  `json:"files_done"`
}

// AppReport is sent from agent to manager in heartbeats
type AppReport struct {
	AppID             AppID           `json:"app_id"`
	ObservedState     ObservedState   `json:"observed_state"`
	Health            HealthStatus    `json:"health"`
	Phase             Phase           `json:"phase"`
	LastError         string          `json:"last_error,omitempty"`
	RestartCount      int             `json:"restart_count"`
	Quarantined       bool            `json:"quarantined"`
	Port              int             `json:"port,omitempty"`
	BackupBusy        bool            `json:"backup_busy,omitempty"`
	PruneBusy         bool            `json:"prune_busy,omitempty"`
	Progress          *ResticProgress `json:"progress,omitempty"`
	LastBackupAt      time.Time       `json:"last_backup_at,omitempty"`
	LastPruneAt       time.Time       `json:"last_prune_at,omitempty"`
	BackupDefer       time.Time       `json:"backup_defer,omitempty"`
	LastBackupError   string          `json:"last_backup_error,omitempty"`
	LastPruneError    string          `json:"last_prune_error,omitempty"`
	ActiveConnections bool            `json:"active_connections,omitempty"`
	LastActiveAt      time.Time       `json:"last_active_at,omitempty"`
	PostBackupPhase   string          `json:"post_backup_phase,omitempty"`
}

// RestoreSnapshotOverride carries metadata about a user-selected snapshot
// to restore from instead of latest.
type RestoreSnapshotOverride struct {
	SnapshotID string    `json:"snapshot_id"`
	ShortID    string    `json:"short_id,omitempty"`
	Time       time.Time `json:"time,omitempty"`
	Node       string    `json:"node,omitempty"`
}

// DesiredApp is sent from manager to agent with desired state
type DesiredApp struct {
	AppID                   AppID                    `json:"app_id"`
	DesiredState            DesiredState             `json:"desired_state"`
	BackupSensitive         bool                     `json:"backup_sensitive,omitempty"`
	BackupNow               bool                     `json:"backup_now,omitempty"`
	PruneNow                bool                     `json:"prune_now,omitempty"`
	BackupEnabled           bool                     `json:"backup_enabled"`
	RestoreEnabled          bool                     `json:"restore_enabled"`
	RestoreNow              bool                     `json:"restore_now,omitempty"`
	RestoreSnapshotID       string                   `json:"restore_snapshot_id,omitempty"`
	RestoreSnapshotOverride *RestoreSnapshotOverride `json:"restore_snapshot_override,omitempty"`
}

// DesiredAppsResponse is the response from /agents/{id}/desired
type DesiredAppsResponse struct {
	Apps      []DesiredApp `json:"apps"`
	IsGateway bool         `json:"is_gateway,omitempty"`
}

const (
	ConfigKeyLocalHAIP        = "LOCAL_HA_IP"
	ConfigKeyCaddyDNSProvider = "CADDY_DNS_PROVIDER"
	ConfigKeyCaddyACMEEmail   = "CADDY_ACME_EMAIL"
	ConfigKeyCaddyACMECA      = "CADDY_ACME_CA"
	ConfigKeyCaddyDNSTokenEnv = "CADDY_DNS_TOKEN_ENV"
	ConfigKeyCFDNSAPIToken    = "CF_DNS_API_TOKEN"
	ConfigKeyResticRepository = "RESTIC_REPOSITORY"
	ConfigKeyB2AccountID      = "B2_ACCOUNT_ID"
	ConfigKeyB2AccountKey     = "B2_ACCOUNT_KEY"
	ConfigKeyResticPassword   = "RESTIC_PASSWORD"
	ConfigKeyAgentName        = "HOMECTL_AGENT_NAME"
)

var SharedAgentConfigKeys = []string{
	ConfigKeyLocalHAIP,
	ConfigKeyCaddyDNSProvider,
	ConfigKeyCaddyACMEEmail,
	ConfigKeyCaddyACMECA,
	ConfigKeyResticRepository,
}

var EditableAgentEnvConfigKeys = []string{
	ConfigKeyCaddyDNSTokenEnv,
	ConfigKeyCFDNSAPIToken,
	ConfigKeyB2AccountID,
	ConfigKeyB2AccountKey,
	ConfigKeyResticPassword,
}

var AgentSpecificConfigKeys = []string{
	ConfigKeyAgentName,
}

type NodeConfigDocument map[string]string

type NodeConfigUIResponse struct {
	AgentName string             `json:"agent_name"`
	Version   int64              `json:"version"`
	Shared    NodeConfigDocument `json:"shared"`
	Agent     NodeConfigDocument `json:"agent"`
	UpdatedAt string             `json:"updated_at,omitempty"`
}

type NodeConfigUIRequest struct {
	Shared NodeConfigDocument `json:"shared"`
	Agent  NodeConfigDocument `json:"agent"`
}
