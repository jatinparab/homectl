import type { ObservedState, HealthStatus } from '../api/types'

const statusColors: Record<ObservedState, string> = {
  running: 'bg-green-500',
  stopped: 'bg-gray-500',
  restarting: 'bg-yellow-500',
  error: 'bg-red-500',
  unknown: 'bg-gray-500',
}

const healthColors: Record<HealthStatus, string> = {
  healthy: 'bg-green-500',
  unhealthy: 'bg-red-500',
  unknown: 'bg-yellow-500',
}

const phaseColors: Record<string, string> = {
  running: 'bg-green-500',
  ready: 'bg-blue-500',
  restoring: 'bg-yellow-500',
  error: 'bg-red-500',
  empty: 'bg-gray-500',
}

const desiredColors: Record<string, string> = {
  running: 'bg-green-600',
  stopped: 'bg-gray-600',
}

interface StatusBadgeProps {
  status: ObservedState
}

export function StatusBadge({ status }: StatusBadgeProps) {
  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium text-white ${statusColors[status]}`}
    >
      {status}
    </span>
  )
}

interface HealthBadgeProps {
  health: HealthStatus
}

export function HealthBadge({ health }: HealthBadgeProps) {
  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium text-white ${healthColors[health]}`}
    >
      {health}
    </span>
  )
}

interface PhaseBadgeProps {
  phase: string
}

export function PhaseBadge({ phase }: PhaseBadgeProps) {
  if (!phase) {
    return <span className="text-gray-500">-</span>
  }
  const color = phaseColors[phase] || 'bg-gray-500'
  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium text-white ${color}`}
    >
      {phase}
    </span>
  )
}

interface DesiredBadgeProps {
  state: string
}

export function DesiredBadge({ state }: DesiredBadgeProps) {
  const color = desiredColors[state] || 'bg-gray-600'
  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium text-white ${color}`}
    >
      {state}
    </span>
  )
}

const containerStateColors: Record<string, string> = {
  running: 'bg-green-500',
  exited: 'bg-red-500',
  restarting: 'bg-yellow-500',
  created: 'bg-gray-500',
  paused: 'bg-yellow-500',
  dead: 'bg-red-500',
}

interface ContainerStateBadgeProps {
  state: string
}

export function ContainerStateBadge({ state }: ContainerStateBadgeProps) {
  const color = containerStateColors[state] || 'bg-gray-500'
  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium text-white ${color}`}
    >
      {state}
    </span>
  )
}
