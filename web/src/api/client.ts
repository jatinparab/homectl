import type { Agent, AppStatus, NodeContainers } from './types'

const API_BASE = '/api/v1'

async function fetchJSON<T>(url: string): Promise<T> {
  const response = await fetch(url)
  if (!response.ok) {
    throw new Error(`HTTP error! status: ${response.status}`)
  }
  return response.json()
}

export async function fetchApps(): Promise<AppStatus[]> {
  return fetchJSON<AppStatus[]>(`${API_BASE}/apps`)
}

export async function fetchAgents(): Promise<Agent[]> {
  return fetchJSON<Agent[]>(`${API_BASE}/agents`)
}

export async function fetchSystemLogSources(): Promise<string[]> {
  return fetchJSON<string[]>(`${API_BASE}/system-logs`)
}

export async function fetchContainers(): Promise<NodeContainers[]> {
  return fetchJSON<NodeContainers[]>(`${API_BASE}/containers`)
}
