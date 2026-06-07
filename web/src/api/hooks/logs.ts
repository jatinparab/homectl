import { useQuery } from "@tanstack/react-query";
import { fetchPublicRouteLogs, fetchSSHLogs } from "../client";
import type { LogFilterParams } from "../types";

export function usePublicRouteLogs(filters: LogFilterParams = {}) {
  return useQuery({
    queryKey: ["public-route-logs", filters],
    queryFn: () => fetchPublicRouteLogs({ ...filters, limit: filters.limit ?? 500 }),
    refetchInterval: 5000,
  });
}

export function useSSHLogs(filters: LogFilterParams = {}) {
  return useQuery({
    queryKey: ["ssh-logs", filters],
    queryFn: () => fetchSSHLogs({ ...filters, limit: filters.limit ?? 500 }),
    refetchInterval: 5000,
  });
}
