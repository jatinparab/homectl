import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  fetchSnapshotFiles,
  fetchSnapshots,
  refreshSnapshots,
  restartApp,
  triggerBackup,
  triggerPrune,
  triggerRebalance,
  triggerRepoCheck,
  triggerRepoUnlock,
  triggerStoragePrune,
  triggerSync,
  updateAppDesiredState,
  updateAppRestoreEnabled,
  setManualGateway,
  clearManualGateway,
  createPublicRoute,
  deletePublicRoute,
  forceRepoUnlock,
  reassignApp,
  updatePublicRoute,
  clearReassignment,
} from "./client";
import { useLiveState } from "./live";

type LiveResult<T> = {
  data: T | undefined;
  isLoading: boolean;
  error: Error | null;
};

function useLiveSlice<T>(selector: (payload: NonNullable<ReturnType<typeof useLiveState>["payload"]>) => T): LiveResult<T> {
  const { payload, initialized, error } = useLiveState();
  return {
    data: payload ? selector(payload) : undefined,
    isLoading: !initialized,
    error,
  };
}

export function useApps() {
  return useLiveSlice((payload) => payload.apps);
}

export function useApp(appId: string) {
  return useLiveSlice((payload) => payload.apps.find((a) => a.app_id === appId));
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

export function useRestartApp() {
  return useMutation({
    mutationFn: ({ appId, node }: { appId: string; node: string }) => restartApp(appId, node),
  });
}

export function useTriggerBackup() {
  return useMutation({
    mutationFn: ({ appId, node }: { appId: string; node: string }) => triggerBackup(appId, node),
  });
}

export function useTriggerPrune() {
  return useMutation({
    mutationFn: ({ appId, node }: { appId: string; node: string }) => triggerPrune(appId, node),
  });
}

export function useSync() {
  return useMutation({
    mutationFn: triggerSync,
  });
}

export function useRebalance() {
  return useMutation({
    mutationFn: triggerRebalance,
  });
}

export function useUpdateAppRestoreEnabled() {
  return useMutation({
    mutationFn: ({ appId, enabled }: { appId: string; enabled: boolean }) =>
      updateAppRestoreEnabled(appId, enabled),
  });
}

export function useSnapshots(appId: string | null, node: string | null) {
  return useQuery({
    queryKey: ["snapshots", appId, node],
    queryFn: () => fetchSnapshots(appId!, node!),
    enabled: !!appId && !!node,
    staleTime: Infinity,
    refetchInterval: false,
  });
}

export function useRefreshSnapshots() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ appId, node }: { appId: string; node: string }) => refreshSnapshots(appId, node),
    onSuccess: (_data, variables) => {
      queryClient.invalidateQueries({ queryKey: ["snapshots", variables.appId, variables.node] });
    },
  });
}

export function useSnapshotFiles(snapshotId: string | null, node: string | null, path: string) {
  return useQuery({
    queryKey: ["snapshot-files", snapshotId, node, path],
    queryFn: () => fetchSnapshotFiles(snapshotId!, node!, path),
    enabled: !!snapshotId && !!node,
    refetchInterval: false,
  });
}

export function useUpdateAppDesiredState() {
  return useMutation({
    mutationFn: ({ appId, state }: { appId: string; state: "running" | "stopped" }) =>
      updateAppDesiredState(appId, state),
  });
}

export function useTriggerRepoCheck() {
  return useMutation({ mutationFn: triggerRepoCheck });
}

export function useTriggerRepoUnlock() {
  return useMutation({ mutationFn: triggerRepoUnlock });
}

export function useSetManualGateway() {
  return useMutation({ mutationFn: (node: string) => setManualGateway(node) });
}

export function useClearManualGateway() {
  return useMutation({ mutationFn: clearManualGateway });
}

export function useForceRepoUnlock() {
  return useMutation({ mutationFn: forceRepoUnlock });
}

export function useCreatePublicRoute() {
  return useMutation({ mutationFn: createPublicRoute });
}

export function useUpdatePublicRoute() {
  return useMutation({
    mutationFn: ({ id, route }: { id: string; route: Parameters<typeof updatePublicRoute>[1] }) =>
      updatePublicRoute(id, route),
  });
}

export function useDeletePublicRoute() {
  return useMutation({ mutationFn: deletePublicRoute });
}

export function useTriggerStoragePrune() {
  return useMutation({
    mutationFn: ({ node }: { node: string }) => triggerStoragePrune(node),
  });
}

export function useReassignApp() {
  return useMutation({
    mutationFn: ({ appId, node }: { appId: string; node: string }) => reassignApp(appId, node),
  });
}

export function useClearReassignment() {
  return useMutation({
    mutationFn: (appId: string) => clearReassignment(appId),
  });
}
