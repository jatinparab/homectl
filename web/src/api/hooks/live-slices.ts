import { useLiveState } from "../live";

type LiveResult<T> = {
  data: T | undefined;
  isLoading: boolean;
  error: Error | null;
};

export function useLiveSlice<T>(selector: (payload: NonNullable<ReturnType<typeof useLiveState>["payload"]>) => T): LiveResult<T> {
  const { payload, initialized, error } = useLiveState();
  return {
    data: payload ? selector(payload) : undefined,
    isLoading: !initialized,
    error,
  };
}

import type { AppStatus } from "../types";

export function useApps() {
  return useLiveSlice((payload) => payload.apps);
}

export function useApp(appId: string) {
  return useLiveSlice((payload) => payload.apps.find((a: AppStatus) => a.app_id === appId));
}

export function useAgents() {
  return useLiveSlice((payload) => payload.agents);
}

export function useSystemLogSources() {
  return useLiveSlice((payload) => payload.system_log_sources);
}

export function useContainers() {
  return useLiveSlice((payload) => payload.containers);
}

export function useStorageStatus() {
  return useLiveSlice((payload) => payload.storage);
}

export function useUpcomingBackup() {
  return useLiveSlice((payload) => payload.upcoming_backup);
}

export function useUpcomingPrune() {
  return useLiveSlice((payload) => payload.upcoming_prune);
}

export function useRepoStatus() {
  return useLiveSlice((payload) => payload.repo_status);
}

export function useGatewayInfo() {
  return useLiveSlice((payload) => payload.gateway);
}

export function usePublicRoutes() {
  return useLiveSlice((payload) => payload.public_routes);
}
