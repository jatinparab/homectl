package common

import "encoding/json"

type LinkRequestType string

const (
	LinkTypeHello    = "hello"
	LinkTypeRequest  = "request"
	LinkTypeResponse = "response"
)

const (
	LinkReqAgentHeartbeat   LinkRequestType = "agent_heartbeat"
	LinkReqAgentDesired     LinkRequestType = "agent_desired"
	LinkReqAgentTraefik     LinkRequestType = "agent_traefik_config"
	LinkReqAgentDockerSync  LinkRequestType = "agent_docker_sync"
	LinkReqAgentManagerSync LinkRequestType = "agent_manager_sync"
	LinkReqRestartApp       LinkRequestType = "restart_app"
	LinkReqAppLogs          LinkRequestType = "app_logs"
	LinkReqAppPhaseLogs     LinkRequestType = "app_phase_logs"
	LinkReqSystemLogs       LinkRequestType = "system_logs"
	LinkReqContainerLogs    LinkRequestType = "container_logs"
	LinkReqContainers       LinkRequestType = "containers"
	LinkReqStorageStatus    LinkRequestType = "storage_status"
	LinkReqStoragePrune     LinkRequestType = "storage_prune"
	LinkReqSnapshots        LinkRequestType = "snapshots"
	LinkReqSnapshotRefresh  LinkRequestType = "snapshot_refresh"
	LinkReqSnapshotLs       LinkRequestType = "snapshot_ls"
	LinkReqRepoCheck        LinkRequestType = "repo_check"
	LinkReqRepoUnlock       LinkRequestType = "repo_unlock"
	LinkReqRepoForceUnlock  LinkRequestType = "repo_force_unlock"
	LinkReqRepoCheckStatus  LinkRequestType = "repo_check_status"
	LinkReqRepoUnlockStatus LinkRequestType = "repo_unlock_status"
	LinkReqRepoCheckLogs    LinkRequestType = "repo_check_logs"
	LinkReqRepoUnlockLogs   LinkRequestType = "repo_unlock_logs"
)

type LinkMessage struct {
	Type        string          `json:"type"`
	ID          string          `json:"id,omitempty"`
	Source      string          `json:"source,omitempty"`
	AgentName   string          `json:"agent_name,omitempty"`
	AgentIP     string          `json:"agent_ip,omitempty"`
	RequestType LinkRequestType `json:"request_type,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Status      int             `json:"status,omitempty"`
	Body        []byte          `json:"body,omitempty"`
	Error       string          `json:"error,omitempty"`
	Done        bool            `json:"done,omitempty"`
}

type AgentHeartbeatPayload struct {
	Name    string      `json:"name"`
	IP      string      `json:"ip"`
	Reports []AppReport `json:"reports,omitempty"`
	Metrics NodeMetrics `json:"metrics,omitempty"`
}

type RestartAppPayload struct {
	AppID string `json:"app_id"`
}

type AppLogsPayload struct {
	AppID string `json:"app_id"`
}

type AppPhaseLogsPayload struct {
	AppID string `json:"app_id"`
}

type ContainerLogsPayload struct {
	ContainerID string `json:"container_id"`
	ShowAll     bool   `json:"show_all"`
}

type SnapshotsPayload struct {
	AppID string `json:"app_id"`
}

type SnapshotRefreshPayload struct {
	AppID string `json:"app_id"`
}

type SnapshotLsPayload struct {
	SnapshotID string `json:"snapshot_id"`
	Path       string `json:"path"`
}

func EncodePayload(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func DecodePayload[T any](msg LinkMessage) (T, error) {
	var out T
	if len(msg.Payload) == 0 {
		return out, nil
	}
	err := json.Unmarshal(msg.Payload, &out)
	return out, err
}
