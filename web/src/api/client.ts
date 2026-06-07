import type { Agent, AppStatus, CrowdSecDecision, CrowdSecHubInstallInput, CrowdSecHubResponse, CrowdSecLogEntry, CrowdSecMetrics, CrowdSecStatus, CrowdSecVersionInfo, GatewayInfo, LogFilterParams, NodeConfig, NodeConfigInput, NodeContainers, PublicRoute, PublicRouteAccessLogEntry, PublicRouteMode, RepoStatus, SnapshotCacheResponse, SnapshotEntry, SSHLogEntry, StoragePruneResponse, StorageStatus, UpcomingBackup, UpcomingPrune } from "./types";

const API_BASE = "/api/v1";

async function fetchJSON<T>(url: string): Promise<T> {
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`HTTP error! status: ${response.status}`);
  }
  return response.json();
}

export async function fetchApps(): Promise<AppStatus[]> {
  return fetchJSON<AppStatus[]>(`${API_BASE}/apps`);
}

export async function fetchAgents(): Promise<Agent[]> {
  return fetchJSON<Agent[]>(`${API_BASE}/agents`);
}

export async function fetchStorageStatus(): Promise<StorageStatus> {
  return fetchJSON<StorageStatus>(`${API_BASE}/storage`);
}

export async function fetchNodeConfig(node: string): Promise<NodeConfig> {
  return fetchJSON<NodeConfig>(`${API_BASE}/agent-configs/${encodeURIComponent(node)}`);
}

export async function updateNodeConfig(node: string, input: NodeConfigInput): Promise<NodeConfig> {
  const response = await fetch(`${API_BASE}/agent-configs/${encodeURIComponent(node)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
  return response.json();
}

export async function triggerStoragePrune(node: string): Promise<StoragePruneResponse> {
  const response = await fetch(`${API_BASE}/storage/prune?node=${encodeURIComponent(node)}`, {
    method: "POST",
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
  return response.json();
}

export async function fetchSystemLogSources(): Promise<string[]> {
  return fetchJSON<string[]>(`${API_BASE}/system-logs`);
}

export async function fetchContainers(): Promise<NodeContainers[]> {
  return fetchJSON<NodeContainers[]>(`${API_BASE}/containers`);
}

export async function fetchUpcomingBackup(): Promise<UpcomingBackup | null> {
  const response = await fetch(`${API_BASE}/backup/upcoming`);
  if (!response.ok) {
    throw new Error(`HTTP error! status: ${response.status}`);
  }
  const data = await response.json();
  return data as UpcomingBackup | null;
}

export async function fetchUpcomingPrune(): Promise<UpcomingPrune | null> {
  const response = await fetch(`${API_BASE}/prune/upcoming`);
  if (!response.ok) {
    throw new Error(`HTTP error! status: ${response.status}`);
  }
  const data = await response.json();
  return data as UpcomingPrune | null;
}

export async function restartApp(
  appId: string,
  node: string,
): Promise<void> {
  const response = await fetch(
    `${API_BASE}/apps/${appId}/restart?node=${node}`,
    {
      method: "POST",
    },
  );
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
}

export async function triggerBackup(
  appId: string,
  node: string,
): Promise<void> {
  const response = await fetch(
    `${API_BASE}/apps/${appId}/backup?node=${node}`,
    {
      method: "POST",
    },
  );
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
}

export async function triggerPrune(
  appId: string,
  node: string,
): Promise<void> {
  const response = await fetch(
    `${API_BASE}/apps/${appId}/prune?node=${node}`,
    {
      method: "POST",
    },
  );
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
}

export async function triggerSync(): Promise<void> {
  const response = await fetch(`${API_BASE}/sync`, {
    method: "POST",
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
}

export async function triggerRebalance(): Promise<void> {
  const response = await fetch(`${API_BASE}/rebalance`, {
    method: "POST",
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
}

export async function updateAppRestoreEnabled(
  appId: string,
  enabled: boolean,
): Promise<void> {
  const response = await fetch(
    `${API_BASE}/apps-restore/${appId}?enabled=${enabled}`,
    {
      method: "PUT",
    },
  );
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
}

export async function fetchSnapshots(appId: string, node: string): Promise<SnapshotCacheResponse> {
  return fetchJSON<SnapshotCacheResponse>(`${API_BASE}/apps/${appId}/snapshots?node=${node}`);
}

export async function refreshSnapshots(appId: string, node: string): Promise<void> {
  const response = await fetch(`${API_BASE}/apps/${appId}/snapshot-refresh?node=${node}`, {
    method: "POST",
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
}

export async function fetchSnapshotFiles(snapshotId: string, node: string, path: string): Promise<SnapshotEntry[]> {
  return fetchJSON<SnapshotEntry[]>(`${API_BASE}/snapshots/${snapshotId}/ls?node=${node}&path=${encodeURIComponent(path)}`);
}

export async function updateAppDesiredState(
  appId: string,
  state: "running" | "stopped",
): Promise<void> {
  const response = await fetch(
    `/api/v1/apps-desired-state/${appId}?state=${state}`,
    {
      method: "PUT",
    },
  );
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
}

export async function fetchRepoStatus(): Promise<RepoStatus> {
  return fetchJSON<RepoStatus>(`${API_BASE}/repo/status`);
}

export async function triggerRepoCheck(): Promise<void> {
  const response = await fetch(`${API_BASE}/repo/check`, { method: "POST" });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
}

export async function triggerRepoUnlock(): Promise<void> {
  const response = await fetch(`${API_BASE}/repo/unlock`, { method: "POST" });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
}

export async function fetchGatewayInfo(): Promise<GatewayInfo> {
  return fetchJSON<GatewayInfo>(`${API_BASE}/gateway`);
}

export async function fetchPublicRoutes(): Promise<PublicRoute[]> {
  return fetchJSON<PublicRoute[]>(`${API_BASE}/public-routes`);
}

function buildLogQueryParams(filters: LogFilterParams): string {
  const params = new URLSearchParams();
  if (filters.host?.trim()) params.set("host", filters.host.trim());
  if (filters.ip?.trim()) params.set("ip", filters.ip.trim());
  if (filters.uri?.trim()) params.set("uri", filters.uri.trim());
  if (filters.status?.trim()) params.set("status", filters.status.trim());
  if (filters.search?.trim()) params.set("search", filters.search.trim());
  if (filters.limit) params.set("limit", String(filters.limit));
  if (filters.before?.trim()) params.set("before", filters.before.trim());
  if (filters.after?.trim()) params.set("after", filters.after.trim());
  const query = params.toString();
  return query ? `?${query}` : "";
}

export async function fetchPublicRouteLogs(filters: LogFilterParams = {}): Promise<PublicRouteAccessLogEntry[]> {
  return fetchJSON<PublicRouteAccessLogEntry[]>(`${API_BASE}/public-route-logs${buildLogQueryParams(filters)}`);
}

export async function fetchSSHLogs(filters: LogFilterParams = {}): Promise<SSHLogEntry[]> {
  return fetchJSON<SSHLogEntry[]>(`${API_BASE}/ssh-logs${buildLogQueryParams(filters)}`);
}

export async function fetchCrowdSecLogs(limit = 300): Promise<CrowdSecLogEntry[]> {
  return fetchJSON<CrowdSecLogEntry[]>(`${API_BASE}/crowdsec/logs?limit=${limit}`);
}

export async function fetchCrowdSecStatus(): Promise<CrowdSecStatus> {
  return fetchJSON<CrowdSecStatus>(`${API_BASE}/crowdsec/status`);
}

export async function fetchCrowdSecMetrics(): Promise<CrowdSecMetrics> {
  return fetchJSON<CrowdSecMetrics>(`${API_BASE}/crowdsec/metrics`);
}

export async function fetchCrowdSecDecisions(): Promise<CrowdSecDecision[]> {
  return fetchJSON<CrowdSecDecision[]>(`${API_BASE}/crowdsec/bans`);
}

export async function addCrowdSecDecision(input: {
  ip: string;
  duration: string;
  reason: string;
}): Promise<CrowdSecDecision[]> {
  const response = await fetch(`${API_BASE}/crowdsec/bans`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
  return response.json();
}

export async function deleteCrowdSecDecision(id: number): Promise<CrowdSecDecision[]> {
  const response = await fetch(`${API_BASE}/crowdsec/bans/${id}`, {
    method: "DELETE",
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
  return response.json();
}

export async function fetchCrowdSecHub(all?: boolean): Promise<CrowdSecHubResponse> {
  const url = all ? `${API_BASE}/crowdsec/hub?all=true` : `${API_BASE}/crowdsec/hub`;
  return fetchJSON<CrowdSecHubResponse>(url);
}

export async function installCrowdSecHubItem(input: CrowdSecHubInstallInput): Promise<void> {
  const response = await fetch(`${API_BASE}/crowdsec/hub`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
}

export async function removeCrowdSecHubItem(input: CrowdSecHubInstallInput): Promise<void> {
  const response = await fetch(`${API_BASE}/crowdsec/hub`, {
    method: "DELETE",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
}

export async function fetchCrowdSecVersion(): Promise<CrowdSecVersionInfo> {
  return fetchJSON<CrowdSecVersionInfo>(`${API_BASE}/crowdsec/version`);
}

export async function updateCrowdSec(): Promise<CrowdSecVersionInfo> {
  const response = await fetch(`${API_BASE}/crowdsec/update`, { method: "POST" });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
  return response.json();
}

export interface PublicRouteInput {
  host: string;
  mode: PublicRouteMode;
  custom_target?: string;
  enabled: boolean;
}

export async function createPublicRoute(route: PublicRouteInput): Promise<PublicRoute[]> {
  const response = await fetch(`${API_BASE}/public-routes`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(route),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
  return response.json();
}

export async function updatePublicRoute(id: string, route: PublicRouteInput): Promise<PublicRoute[]> {
  const response = await fetch(`${API_BASE}/public-routes/${encodeURIComponent(id)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(route),
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
  return response.json();
}

export async function deletePublicRoute(id: string): Promise<PublicRoute[]> {
  const response = await fetch(`${API_BASE}/public-routes/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
  return response.json();
}

export async function setManualGateway(node: string): Promise<GatewayInfo> {
  const response = await fetch(`${API_BASE}/gateway?node=${encodeURIComponent(node)}`, {
    method: "POST",
  });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
  return response.json();
}

export async function forceRepoUnlock(): Promise<void> {
  const response = await fetch(`${API_BASE}/repo/force-unlock`, { method: "POST" });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
}

export async function reassignApp(appId: string, node: string): Promise<void> {
  const response = await fetch(
    `${API_BASE}/apps/${appId}/reassign?node=${encodeURIComponent(node)}`,
    { method: "POST" },
  );
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
}

export async function clearReassignment(appId: string): Promise<void> {
  const response = await fetch(`${API_BASE}/apps/${appId}/reassign`, { method: "DELETE" });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
}

export async function clearManualGateway(): Promise<GatewayInfo> {
  const response = await fetch(`${API_BASE}/gateway`, { method: "DELETE" });
  if (!response.ok) {
    const text = await response.text();
    throw new Error(text || `HTTP error! status: ${response.status}`);
  }
  return response.json();
}
