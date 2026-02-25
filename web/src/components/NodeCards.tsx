import type { Agent, GatewayInfo } from '../api/types'
import { useSetManualGateway, useClearManualGateway } from '../api/hooks'

function formatBytes(bytes?: number): string {
  if (!bytes || bytes <= 0) return '--'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
}

function pct(n?: number): string {
  if (n === undefined || Number.isNaN(n)) return '--'
  return `${n.toFixed(1)}%`
}

interface NodeCardsProps {
  agents: Agent[] | undefined
  gatewayInfo: GatewayInfo | undefined
}

export function NodeCards({ agents, gatewayInfo }: NodeCardsProps) {
  if (!agents || agents.length === 0) {
    return null
  }

  const sorted = [...agents].sort((a, b) => {
    if (a.is_gateway !== b.is_gateway) return a.is_gateway ? -1 : 1
    return a.name.localeCompare(b.name)
  })

  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 mb-6">
      {sorted.map((agent) => (
        <NodeCard key={agent.name} agent={agent} gatewayInfo={gatewayInfo} />
      ))}
    </div>
  )
}

interface NodeCardProps {
  agent: Agent
  gatewayInfo: GatewayInfo | undefined
}

function NodeCard({ agent, gatewayInfo }: NodeCardProps) {
  const setManualGateway = useSetManualGateway()
  const clearManualGateway = useClearManualGateway()

  const isManual = gatewayInfo?.is_manual ?? false
  const isThisManual = isManual && gatewayInfo?.manual_node === agent.name

  function handleSetGateway() {
    setManualGateway.mutate(agent.name)
  }

  function handleClearGateway() {
    clearManualGateway.mutate()
  }

  return (
    <div className="bg-gray-800 rounded-lg border border-gray-700 p-4">
      <div className="flex items-center justify-between mb-2">
        <div className="flex items-center gap-2">
          <h3 className="text-white font-medium">{agent.name}</h3>
          {agent.is_gateway && (
            <span className="px-1.5 py-0.5 text-xs font-medium rounded bg-green-900 text-green-300 border border-green-700">
              Gateway
            </span>
          )}
          {isThisManual && (
            <span className="px-1.5 py-0.5 text-xs font-medium rounded bg-blue-900 text-blue-300 border border-blue-700">
              Manual
            </span>
          )}
        </div>
        <span
          className={`w-3 h-3 rounded-full ${agent.online ? 'bg-green-500' : 'bg-red-500'}`}
        />
      </div>
      <div className="text-sm text-gray-400 space-y-1">
        <div className="flex justify-between">
          <span>IP:</span>
          <span className="text-gray-300">{agent.ip}</span>
        </div>
        <div className="flex justify-between">
          <span>Status:</span>
          <span className={agent.online ? 'text-green-400' : 'text-red-400'}>
            {agent.online ? 'Online' : 'Offline'}
          </span>
        </div>
        <div className="pt-1 border-t border-gray-700 mt-2 space-y-1">
          <div className="flex justify-between">
            <span>CPU:</span>
            <span className="text-gray-300">{agent.online ? pct(agent.metrics?.cpu_percent) : '--'}</span>
          </div>
          <div className="flex justify-between">
            <span>RAM:</span>
            <span className="text-gray-300">
              {agent.online ? `${pct(agent.metrics?.memory_percent)} (${formatBytes(agent.metrics?.memory_used)} / ${formatBytes(agent.metrics?.memory_total)})` : '--'}
            </span>
          </div>
          <div className="flex justify-between">
            <span>Disk:</span>
            <span className="text-gray-300">
              {agent.online ? `${pct(agent.metrics?.disk_percent)} (${formatBytes(agent.metrics?.disk_used)} / ${formatBytes(agent.metrics?.disk_total)})` : '--'}
            </span>
          </div>
        </div>
      </div>
      <div className="mt-3 flex gap-2">
        {!isThisManual && (
          <button
            onClick={handleSetGateway}
            disabled={!agent.online || setManualGateway.isPending}
            className="flex-1 text-xs px-2 py-1 rounded bg-blue-800 hover:bg-blue-700 text-blue-200 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            Set as Gateway
          </button>
        )}
        {isThisManual && (
          <button
            onClick={handleClearGateway}
            disabled={clearManualGateway.isPending}
            className="flex-1 text-xs px-2 py-1 rounded bg-gray-700 hover:bg-gray-600 text-gray-300 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            Revert to Auto
          </button>
        )}
      </div>
    </div>
  )
}
