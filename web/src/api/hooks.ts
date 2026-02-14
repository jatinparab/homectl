import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  fetchApps,
  fetchAgents,
  fetchSystemLogSources,
  fetchContainers,
  triggerBackup,
  updateAppDesiredState,
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

export function useTriggerBackup() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ appId, node }: { appId: string; node: string }) =>
      triggerBackup(appId, node),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["apps"] });
    },
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
