import { useQuery } from '@tanstack/react-query'
import { fetchApps, fetchAgents } from './client'

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
