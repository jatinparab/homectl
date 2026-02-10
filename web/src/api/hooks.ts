import { useQuery } from '@tanstack/react-query'
import { fetchApps, fetchAgents, fetchSystemLogSources, fetchContainers } from './client'

export function useApps() {
  return useQuery({
    queryKey: ['apps'],
    queryFn: fetchApps,
    refetchInterval: 2000,
  })
}

export function useAgents() {
  return useQuery({
    queryKey: ['agents'],
    queryFn: fetchAgents,
    refetchInterval: 2000,
  })
}

export function useSystemLogSources() {
  return useQuery({
    queryKey: ['system-log-sources'],
    queryFn: fetchSystemLogSources,
    refetchInterval: 10000,
  })
}

export function useContainers() {
  return useQuery({
    queryKey: ['containers'],
    queryFn: fetchContainers,
    refetchInterval: 5000,
  })
}
