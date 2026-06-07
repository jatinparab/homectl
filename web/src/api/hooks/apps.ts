import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  restartApp,
  triggerBackup,
  triggerPrune,
  updateAppDesiredState,
  updateAppRestoreEnabled,
  reassignApp,
  clearReassignment,
  fetchSnapshots,
  refreshSnapshots,
  fetchSnapshotFiles,
} from "../client";

// ── App actions ──────────────────────────────

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

export function useUpdateAppDesiredState() {
  return useMutation({
    mutationFn: ({ appId, state }: { appId: string; state: "running" | "stopped" }) =>
      updateAppDesiredState(appId, state),
  });
}

export function useUpdateAppRestoreEnabled() {
  return useMutation({
    mutationFn: ({ appId, enabled }: { appId: string; enabled: boolean }) =>
      updateAppRestoreEnabled(appId, enabled),
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

// ── Snapshots ──────────────────────────────

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
