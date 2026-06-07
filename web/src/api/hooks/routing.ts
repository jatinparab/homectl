import { useMutation } from "@tanstack/react-query";
import {
  createPublicRoute,
  deletePublicRoute,
  updatePublicRoute,
} from "../client";

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
