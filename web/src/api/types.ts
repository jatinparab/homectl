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
  manually_assigned: boolean;
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
  version?: string;
  last_seen: string;
  registered_at: string;
  online: boolean;
  is_gateway: boolean;
  metrics?: NodeMetrics;
}

export interface NodeMetrics {
  cpu_percent: number;
  memory_total: number;
  memory_used: number;
  memory_percent: number;
  disk_total: number;
  disk_used: number;
  disk_free: number;
  disk_percent: number;
}

export interface NodeConfig {
  agent_name: string;
  version: number;
  shared: Record<string, string>;
  agent: Record<string, string>;
  updated_at?: string;
}

export interface NodeConfigInput {
  shared: Record<string, string>;
  agent: Record<string, string>;
}

export interface StoragePruneState {
  busy: boolean;
  started_at?: string;
  finished_at?: string;
  last_error?: string;
  deleted_apps?: string[];
  deleted_dirs?: string[];
  deleted_bytes?: number;
}

export interface StorageNode {
  node: string;
  online: boolean;
  appdata_path?: string;
  disk_total: number;
  disk_used: number;
  disk_free: number;
  disk_percent: number;
  reclaimable: number;
  reclaim_pct: number;
  prune: StoragePruneState;
  error?: string;
}

export interface StorageAggregate {
  disk_total: number;
  disk_used: number;
  disk_free: number;
  disk_percent: number;
  reclaimable: number;
  reclaim_pct: number;
}

export interface StorageStatus {
  nodes: StorageNode[];
  aggregate: StorageAggregate;
  any_pruning: boolean;
}

export interface StoragePruneResult {
  node: string;
  started: boolean;
  error?: string;
}

export interface StoragePruneResponse {
  results: StoragePruneResult[];
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

export type PublicRouteMode = "homelab" | "custom";

export interface PublicRouteHealth {
  working: boolean;
  status: string;
  checked_at?: string;
  last_error?: string;
  resolved_to?: string;
  http_status?: number;
  response_ms?: number;
  request_count: number;
  response_bytes: number;
}

export interface PublicRoute {
  id: string;
  host: string;
  mode: PublicRouteMode;
  custom_target?: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
  health: PublicRouteHealth;
}

export interface LogFilterParams {
  host?: string;
  ip?: string;
  uri?: string;
  status?: string;
  search?: string;
  limit?: number;
  before?: string;
  after?: string;
}

export interface SSHLogEntry {
  timestamp: string;
  host: string;
  client_ip: string;
  port: string;
  user: string;
  auth_method?: string;
  status: string;
  message: string;
  raw: string;
}

export interface PublicRouteAccessLogEntry {
  timestamp: string;
  host: string;
  client_ip: string;
  method: string;
  uri: string;
  protocol: string;
  status: number;
  size: number;
  duration: number;
  user_agent?: string;
}

export interface CrowdSecDecision {
  id?: number;
  scope: string;
  value: string;
  type: string;
  origin?: string;
  scenario?: string;
  reason?: string;
  duration?: string;
  until?: string;
  as_name?: string;
  as_number?: string;
  country?: string;
  source_range?: string;
  event_count: number;
  start_at?: string;
  stop_at?: string;
}

export interface CrowdSecMetrics {
  acquisition_lines_read: number;
  acquisition_lines_parsed: number;
  acquisition_lines_pour: number;
  alerts_count: number;
  active_bans: number;
  active_scenarios: number;
  parser_hits_total: number;
  parser_parsed_total: number;
}

export interface CrowdSecHubItem {
  name: string;
  type: string;
  description: string;
  version: string;
  status?: string;
  installed: boolean;
  update_available: boolean;
}

export interface CrowdSecHubResponse {
  items: CrowdSecHubItem[];
}

export interface CrowdSecHubInstallInput {
  type: string;
  name: string;
}

export interface CrowdSecLogEntry {
  timestamp?: string;
  source: string;
  level?: string;
  message: string;
  raw: string;
}

export interface CrowdSecStatus {
  state: string;
  step: string;
  message?: string;
  error?: string;
  config_ready: boolean;
  hub_ready: boolean;
  engine_running: boolean;
  bouncer_ready: boolean;
  caddy_integrated: boolean;
  updated_at: string;
}

export interface CrowdSecVersionInfo {
  current: string;
  latest: string;
  update_available: boolean;
  error?: string;
}
