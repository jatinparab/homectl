import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  addCrowdSecDecision,
  deleteCrowdSecDecision,
  fetchCrowdSecDecisions,
  fetchCrowdSecHub,
  fetchCrowdSecLogs,
  fetchCrowdSecMetrics,
  fetchCrowdSecStatus,
  fetchCrowdSecVersion,
  updateCrowdSec,
  installCrowdSecHubItem,
  removeCrowdSecHubItem,
} from "../client";

export function useCrowdSecLogs() {
  return useQuery({
    queryKey: ["crowdsec-logs"],
    queryFn: () => fetchCrowdSecLogs(300),
    refetchInterval: 5000,
  });
}

export function useCrowdSecStatus() {
  return useQuery({
    queryKey: ["crowdsec-status"],
    queryFn: fetchCrowdSecStatus,
    refetchInterval: 2000,
  });
}

export function useCrowdSecMetrics() {
  return useQuery({
    queryKey: ["crowdsec-metrics"],
    queryFn: fetchCrowdSecMetrics,
    refetchInterval: 15000,
  });
}

export function useCrowdSecDecisions() {
  return useQuery({
    queryKey: ["crowdsec-decisions"],
    queryFn: fetchCrowdSecDecisions,
    refetchInterval: 30000,
  });
}

export function useAddCrowdSecDecision() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: addCrowdSecDecision,
    onSuccess: (data) => {
      queryClient.setQueryData(["crowdsec-decisions"], data);
    },
  });
}

export function useDeleteCrowdSecDecision() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: deleteCrowdSecDecision,
    onSuccess: (data) => {
      queryClient.setQueryData(["crowdsec-decisions"], data);
    },
  });
}

export function useCrowdSecHub(all?: boolean) {
  return useQuery({
    queryKey: ["crowdsec-hub", all ?? false],
    queryFn: () => fetchCrowdSecHub(all),
    refetchInterval: 15000,
  });
}

export function useInstallCrowdSecHubItem() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: installCrowdSecHubItem,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["crowdsec-hub"] });
    },
  });
}

export function useRemoveCrowdSecHubItem() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: removeCrowdSecHubItem,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["crowdsec-hub"] });
    },
  });
}

export function useCrowdSecVersion() {
  return useQuery({
    queryKey: ["crowdsec-version"],
    queryFn: fetchCrowdSecVersion,
    refetchInterval: 60000,
  });
}

export function useUpdateCrowdSec() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: updateCrowdSec,
    onSuccess: (data) => {
      queryClient.setQueryData(["crowdsec-version"], data);
      queryClient.invalidateQueries({ queryKey: ["crowdsec-status"] });
      queryClient.invalidateQueries({ queryKey: ["crowdsec-hub"] });
    },
  });
}
