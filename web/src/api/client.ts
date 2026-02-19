import type { Agent, AppStatus, NodeContainers, RepoStatus, SnapshotCacheResponse, SnapshotEntry, UpcomingBackup } from "./types";

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
