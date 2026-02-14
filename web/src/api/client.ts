import type { Agent, AppStatus, NodeContainers } from "./types";

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
