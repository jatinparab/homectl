export type ObservedState =
  | "running"
  | "stopped"
  | "restarting"
  | "error"
  | "unknown";
export type HealthStatus = "unknown" | "healthy" | "unhealthy";
export type Phase =
  | "empty"
  | "restoring"
  | "pulling"
  | "ready"
  | "running"
  | "error"
  | "backup_stopping"
  | "backup_stopped"
  | "backing_up";

export interface ResticProgress {
  percent_done: number;
  total_bytes: number;
  bytes_done: number;
  total_files: number;
  files_done: number;
}

export interface NodeReport {
  node: string;
  node_online: boolean;
  observed_state: ObservedState;
  health: HealthStatus;
  phase: string;
  last_error: string;
  restart_count: number;
  quarantined: boolean;
  port: number;
  progress?: ResticProgress;
  prune_busy?: boolean;
  last_backup_at?: string;
  last_backup_error?: string;
  backup_status: string;
  last_prune_at?: string;
  last_prune_error?: string;
  prune_status: string;
  active_connections: boolean;
  last_active_at?: string;
  post_backup_phase?: string;
}

export interface AppStatus {
  app_id: string;
  desired_state: string;
  assigned_agent: string | null;
  is_global: boolean; // Runs on all connected nodes
  observed_state: ObservedState; // Consolidated
  health: HealthStatus; // Consolidated
  phase: string; // Consolidated
  backup_status: string; // Consolidated: "ok" | "needs_backup" | "never"
  prune_status: string; // Consolidated: "ok" | "needs_prune" | "never"
  backup_enabled: boolean; // Whether backup is enabled for this app
  backup_sensitive: boolean; // App is sensitive to backup interruptions
  backup_interval: string; // Human-readable backup interval (e.g. "1h0m0s")
  prune_interval: string; // Human-readable prune interval (e.g. "3h0m0s")
  restore_enabled: boolean; // Whether restore-on-start is enabled
  node_reports: NodeReport[]; // All node reports
}

export interface UpcomingBackup {
  agent_id: string;
  app_id: string;
  assigned_at: string;
  started: boolean;
}

export interface UpcomingPrune {
  agent_id: string;
  app_id: string;
  assigned_at: string;
  started: boolean;
}

export interface Agent {
  name: string;
  ip: string;
  last_seen: string;
  registered_at: string;
  online: boolean;
  is_gateway: boolean;
}

export interface Snapshot {
  id: string;
  short_id: string;
  time: string;
  hostname: string;
  tags: string[];
}

export interface SnapshotCacheResponse {
  snapshots: Snapshot[];
  cached_at: string;
  loading: boolean;
  error?: string;
}

export interface SnapshotEntry {
  name: string;
  type: "file" | "dir";
  path: string;
  size?: number;
  mtime?: string;
}

export interface ContainerInfo {
  id: string;
  name: string;
  image: string;
  status: string;
  state: string;
  ports: string;
  created: string;
}

export interface NodeContainers {
  node: string;
  online: boolean;
  containers: ContainerInfo[];
  error?: string;
}

export interface RepoOpStatus {
  busy: boolean;
  last_at?: string;
  last_error?: string;
  progress_percent?: number;
}

export interface RepoStatus {
  gateway_node: string;
  gateway_online: boolean;
  last_status_sync_at?: string;
  check: RepoOpStatus;
  unlock: RepoOpStatus;
}

export interface GatewayInfo {
  current_node: string;
  manual_node?: string;
  is_manual: boolean;
}
