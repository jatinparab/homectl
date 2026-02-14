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
}

export interface AppStatus {
  app_id: string;
  desired_state: string;
  observed_state: ObservedState; // Consolidated
  health: HealthStatus; // Consolidated
  phase: string; // Consolidated
  node_reports: NodeReport[]; // All node reports
}

export interface Agent {
  name: string;
  ip: string;
  last_seen: string;
  registered_at: string;
  online: boolean;
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
