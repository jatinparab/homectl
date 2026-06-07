import { useMutation } from "@tanstack/react-query";
import {
  triggerSync,
  triggerRebalance,
  triggerRepoCheck,
  triggerRepoUnlock,
  forceRepoUnlock,
  setManualGateway,
  clearManualGateway,
  triggerStoragePrune,
} from "../client";

export function useSync() {
  return useMutation({ mutationFn: triggerSync });
}

export function useRebalance() {
  return useMutation({ mutationFn: triggerRebalance });
}

export function useTriggerRepoCheck() {
  return useMutation({ mutationFn: triggerRepoCheck });
}

export function useTriggerRepoUnlock() {
  return useMutation({ mutationFn: triggerRepoUnlock });
}

export function useForceRepoUnlock() {
  return useMutation({ mutationFn: forceRepoUnlock });
}

export function useSetManualGateway() {
  return useMutation({ mutationFn: (node: string) => setManualGateway(node) });
}

export function useClearManualGateway() {
  return useMutation({ mutationFn: clearManualGateway });
}

export function useTriggerStoragePrune() {
  return useMutation({
    mutationFn: ({ node }: { node: string }) => triggerStoragePrune(node),
  });
}
