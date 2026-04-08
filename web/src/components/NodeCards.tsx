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

function barColor(value?: number): string {
  if (value === undefined || Number.isNaN(value)) return 'bg-stone-200'
  if (value >= 90) return 'bg-red-400'
  if (value >= 60) return 'bg-amber-400'
  return 'bg-teal-500'
}

interface NodeCardsProps {
  agents: Agent[] | undefined
  gatewayInfo: GatewayInfo | undefined
}

export function NodeCards({ agents, gatewayInfo }: NodeCardsProps) {
  if (!agents || agents.length === 0) return null

  const sorted = [...agents].sort((a, b) => {
    if (a.is_gateway !== b.is_gateway) return a.is_gateway ? -1 : 1
    return a.name.localeCompare(b.name)
  })

  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3 mb-6">
      {sorted.map((agent) => (
        <NodeCard key={agent.name} agent={agent} gatewayInfo={gatewayInfo} />
      ))}
    </div>
  )
}

function NodeCard({ agent, gatewayInfo }: { agent: Agent; gatewayInfo: GatewayInfo | undefined }) {
  const setManualGateway = useSetManualGateway()
  const clearManualGateway = useClearManualGateway()

  const isManual = gatewayInfo?.is_manual ?? false
  const isThisManual = isManual && gatewayInfo?.manual_node === agent.name

  return (
    <div className="bg-white border border-[#e2ddd7] rounded-lg p-4">
      {/* Header row */}
      <div className="flex items-center justify-between mb-3">
        <div className="flex items-center gap-2">
          <span
            className={`w-2 h-2 rounded-full ${agent.online ? 'bg-emerald-500 health-glow' : 'bg-stone-300'}`}
          />
          <span className="text-sm font-semibold text-[#1c1917]">{agent.name}</span>
          {agent.is_gateway && (
            <span className="text-[10px] font-medium text-teal-700 bg-teal-50 border border-teal-200 px-1.5 py-0.5 rounded tracking-wide uppercase">
              gateway
            </span>
          )}
          {isThisManual && (
            <span className="text-[10px] font-medium text-blue-700 bg-blue-50 border border-blue-200 px-1.5 py-0.5 rounded tracking-wide uppercase">
              manual
            </span>
          )}
        </div>
        <span className={`text-[11px] font-medium ${agent.online ? 'text-emerald-600' : 'text-stone-400'}`}>
          {agent.online ? 'online' : 'offline'}
        </span>
      </div>

      {/* IP */}
      <div className="text-[11px] text-stone-400 mb-3 font-mono">{agent.ip}</div>

      {/* Metrics */}
      {agent.online && agent.metrics ? (
        <div className="space-y-2">
          <MetricRow
            label="CPU"
            text={pct(agent.metrics.cpu_percent)}
            value={agent.metrics.cpu_percent}
          />
          <MetricRow
            label="RAM"
            text={`${pct(agent.metrics.memory_percent)} · ${formatBytes(agent.metrics.memory_used)}/${formatBytes(agent.metrics.memory_total)}`}
            value={agent.metrics.memory_percent}
          />
          <MetricRow
            label="Disk"
            text={`${pct(agent.metrics.disk_percent)} · ${formatBytes(agent.metrics.disk_used)}/${formatBytes(agent.metrics.disk_total)}`}
            value={agent.metrics.disk_percent}
          />
        </div>
      ) : (
        <div className="text-[11px] text-stone-400">no metrics available</div>
      )}

      {/* Actions */}
      <div className="mt-3 pt-3 border-t border-[#e2ddd7] flex gap-2">
        {!isThisManual ? (
          <button
            onClick={() => setManualGateway.mutate(agent.name)}
            disabled={!agent.online || setManualGateway.isPending}
            className="flex-1 text-[11px] px-2 py-1 rounded border border-[#e2ddd7] text-stone-500 hover:border-teal-300 hover:text-teal-700 hover:bg-teal-50 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            Set gateway
          </button>
        ) : (
          <button
            onClick={() => clearManualGateway.mutate()}
            disabled={clearManualGateway.isPending}
            className="flex-1 text-[11px] px-2 py-1 rounded border border-[#e2ddd7] text-stone-500 hover:border-stone-400 hover:text-stone-700 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
          >
            Revert to auto
          </button>
        )}
      </div>
    </div>
  )
}

function MetricRow({ label, text, value }: { label: string; text: string; value?: number }) {
  return (
    <div>
      <div className="flex justify-between items-baseline mb-1">
        <span className="text-[10px] text-stone-400 uppercase tracking-wider">{label}</span>
        <span className="text-[11px] text-stone-600">{text}</span>
      </div>
      <div className="h-1 w-full bg-stone-100 rounded-full overflow-hidden">
        <div
          className={`h-full rounded-full transition-all ${barColor(value)}`}
          style={{ width: `${Math.min(100, Math.max(0, value ?? 0))}%` }}
        />
      </div>
    </div>
  )
}
