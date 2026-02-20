import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  fetchApps,
  fetchAgents,
  fetchSystemLogSources,
  fetchContainers,
  fetchUpcomingBackup,
  fetchUpcomingPrune,
  fetchSnapshots,
  fetchSnapshotFiles,
  refreshSnapshots,
  restartApp,
  triggerBackup,
  triggerPrune,
  triggerRepoCheck,
  triggerRepoUnlock,
  triggerRebalance,
  triggerSync,
  updateAppRestoreEnabled,
  updateAppDesiredState,
  fetchRepoStatus,
  fetchGatewayInfo,
  setManualGateway,
  clearManualGateway,
  forceRepoUnlock,
} from "./client";

export function useApps() {
  return useQuery({
    queryKey: ["apps"],
    queryFn: fetchApps,
    refetchInterval: 2000,
  });
}

export function useAgents() {
  return useQuery({
    queryKey: ["agents"],
    queryFn: fetchAgents,
    refetchInterval: 2000,
  });
}

export function useSystemLogSources() {
  return useQuery({
    queryKey: ["system-log-sources"],
    queryFn: fetchSystemLogSources,
    refetchInterval: 10000,
  });
}

export function useContainers() {
  return useQuery({
    queryKey: ["containers"],
    queryFn: fetchContainers,
    refetchInterval: 5000,
  });
}

export function useUpcomingBackup() {
  return useQuery({
    queryKey: ["upcoming-backup"],
    queryFn: fetchUpcomingBackup,
    refetchInterval: 2000,
  });
}

export function useUpcomingPrune() {
  return useQuery({
    queryKey: ["upcoming-prune"],
    queryFn: fetchUpcomingPrune,
    refetchInterval: 2000,
  });
}

export function useRestartApp() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ appId, node }: { appId: string; node: string }) =>
      restartApp(appId, node),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["apps"] });
    },
  });
}

export function useTriggerBackup() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ appId, node }: { appId: string; node: string }) =>
      triggerBackup(appId, node),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["apps"] });
      queryClient.invalidateQueries({ queryKey: ["upcoming-backup"] });
    },
  });
}

export function useTriggerPrune() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ appId, node }: { appId: string; node: string }) =>
      triggerPrune(appId, node),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["apps"] });
      queryClient.invalidateQueries({ queryKey: ["upcoming-prune"] });
    },
  });
}

export function useSync() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: triggerSync,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["apps"] });
      queryClient.invalidateQueries({ queryKey: ["agents"] });
    },
  });
}

export function useRebalance() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: triggerRebalance,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["apps"] });
    },
  });
}

export function useUpdateAppRestoreEnabled() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ appId, enabled }: { appId: string; enabled: boolean }) =>
      updateAppRestoreEnabled(appId, enabled),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["apps"] });
    },
  });
}

export function useSnapshots(appId: string | null, node: string | null) {
  return useQuery({
    queryKey: ["snapshots", appId, node],
    queryFn: () => fetchSnapshots(appId!, node!),
    enabled: !!appId && !!node,
    // staleTime: Infinity prevents refetch on mount — expanding SnapshotList just reads cache.
    // Background polling keeps the data in sync with the agent's state.
    staleTime: Infinity,
    refetchInterval: (query) => query.state.data?.loading ? 2000 : 5 * 60 * 1000,
  });
}

export function useRefreshSnapshots() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ appId, node }: { appId: string; node: string }) =>
      refreshSnapshots(appId, node),
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
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ appId, state }: { appId: string; state: "running" | "stopped" }) =>
      updateAppDesiredState(appId, state),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["apps"] });
    },
  });
}

export function useRepoStatus() {
  return useQuery({
    queryKey: ["repo-status"],
    queryFn: fetchRepoStatus,
    refetchInterval: (query) =>
      query.state.data?.check.busy || query.state.data?.unlock.busy ? 2000 : 10000,
  });
}

export function useTriggerRepoCheck() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: triggerRepoCheck,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["repo-status"] });
    },
  });
}

export function useTriggerRepoUnlock() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: triggerRepoUnlock,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["repo-status"] });
    },
  });
}

export function useGatewayInfo() {
  return useQuery({
    queryKey: ["gateway"],
    queryFn: fetchGatewayInfo,
    refetchInterval: 5000,
  });
}

export function useSetManualGateway() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (node: string) => setManualGateway(node),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["gateway"] });
      queryClient.invalidateQueries({ queryKey: ["agents"] });
    },
  });
}

export function useClearManualGateway() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: clearManualGateway,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["gateway"] });
      queryClient.invalidateQueries({ queryKey: ["agents"] });
    },
  });
}

export function useForceRepoUnlock() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: forceRepoUnlock,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["repo-status"] });
    },
  });
}
