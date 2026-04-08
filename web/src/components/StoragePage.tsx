import { useMemo } from 'react'
import { useStorageStatus, useTriggerStoragePrune } from '../api/hooks'

function formatBytes(bytes?: number): string {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
}

function formatPercent(value?: number): string {
  if (value === undefined || Number.isNaN(value)) return '--'
  return `${value.toFixed(1)}%`
}

function barColor(value?: number): string {
  if (value === undefined || Number.isNaN(value)) return 'bg-stone-200'
  if (value >= 90) return 'bg-red-400'
  if (value >= 60) return 'bg-amber-400'
  return 'bg-teal-500'
}

function formatTime(value?: string): string {
  if (!value) return '--'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return '--'
  return d.toLocaleString()
}

export function StoragePage() {
  const { data, isLoading } = useStorageStatus()
  const pruneMutation = useTriggerStoragePrune()

  const pruningAny = data?.any_pruning ?? false
  const sortedNodes = useMemo(() => {
    return [...(data?.nodes ?? [])].sort((a, b) => {
      if (b.disk_free !== a.disk_free) return b.disk_free - a.disk_free
      return a.node.localeCompare(b.node)
    })
  }, [data?.nodes])

  function triggerPrune(node: string) {
    const scope = node === 'all' ? 'all online nodes' : node
    if (!window.confirm(`Prune unassigned appdata on ${scope}? This deletes data directories and backup metadata for apps not assigned to those nodes.`)) return
    pruneMutation.mutate({ node })
  }

  return (
    <div className="min-h-screen bg-[#faf9f7]">
      <div className="border-b border-[#e2ddd7] bg-white px-4 py-4">
        <div className="max-w-6xl mx-auto flex items-center justify-between">
          <div>
            <h1 className="text-sm font-semibold text-[#1c1917]">Storage</h1>
            <p className="text-[11px] text-stone-400 mt-0.5">
              {isLoading ? 'Loading...' : `${sortedNodes.length} node${sortedNodes.length !== 1 ? 's' : ''}`}
            </p>
          </div>
          <button
            onClick={() => triggerPrune('all')}
            disabled={pruneMutation.isPending || pruningAny}
            className="px-3 py-1.5 text-[11px] border border-red-200 text-red-600 hover:border-red-400 hover:bg-red-50 rounded disabled:opacity-50"
          >
            {pruningAny ? 'Pruning...' : 'Prune all nodes'}
          </button>
        </div>
      </div>

      <main className="max-w-6xl mx-auto px-4 py-6 space-y-4">
        {isLoading ? (
          <div className="text-[11px] text-stone-400 py-8 text-center">Loading storage status...</div>
        ) : (
          <>
            {/* Aggregate stats */}
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
              <StatCard label="Total" value={formatBytes(data?.aggregate.disk_total)} />
              <StatCard
                label="Used"
                value={formatBytes(data?.aggregate.disk_used)}
                sub={formatPercent(data?.aggregate.disk_percent)}
                bar={data?.aggregate.disk_percent}
              />
              <StatCard label="Free" value={formatBytes(data?.aggregate.disk_free)} />
              <StatCard
                label="Reclaimable"
                value={formatBytes(data?.aggregate.reclaimable)}
                sub={formatPercent(data?.aggregate.reclaim_pct)}
                bar={data?.aggregate.reclaim_pct}
              />
            </div>

            {/* Node table */}
            <div className="bg-white border border-[#e2ddd7] rounded-lg overflow-hidden">
              <table className="w-full text-xs">
                <thead>
                  <tr className="border-b border-[#e2ddd7]">
                    <th className="px-4 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Node</th>
                    <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Status</th>
                    <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Appdata Path</th>
                    <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest min-w-48">Usage</th>
                    <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest min-w-40">Reclaimable</th>
                    <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Prune</th>
                    <th className="px-3 py-2.5"></th>
                  </tr>
                </thead>
                <tbody>
                  {sortedNodes.map((node) => (
                    <tr key={node.node} className="border-b border-[#f7f5f0] hover:bg-[#faf9f7]">
                      <td className="px-4 py-2.5 font-semibold text-[#1c1917]">{node.node}</td>
                      <td className="px-3 py-2.5">
                        <span className={`text-xs font-medium ${node.online ? 'text-emerald-600' : 'text-stone-400'}`}>
                          {node.online ? 'online' : 'offline'}
                        </span>
                      </td>
                      <td className="px-3 py-2.5 text-stone-400 font-mono">{node.appdata_path || '—'}</td>
                      <td className="px-3 py-2.5 min-w-48">
                        <div className="text-stone-600">{formatBytes(node.disk_used)} / {formatBytes(node.disk_total)}</div>
                        <div className="text-stone-400">{formatPercent(node.disk_percent)}</div>
                        <div className="mt-1 h-1 w-full bg-stone-100 rounded-full overflow-hidden">
                          <div
                            className={`h-full rounded-full ${barColor(node.disk_percent)}`}
                            style={{ width: `${Math.min(100, Math.max(0, node.disk_percent ?? 0))}%` }}
                          />
                        </div>
                      </td>
                      <td className="px-3 py-2.5 min-w-40">
                        <div className="text-stone-600">{formatBytes(node.reclaimable)}</div>
                        <div className="text-stone-400">{formatPercent(node.reclaim_pct)}</div>
                        <div className="mt-1 h-1 w-full bg-stone-100 rounded-full overflow-hidden">
                          <div
                            className={`h-full rounded-full ${barColor(node.reclaim_pct)}`}
                            style={{ width: `${Math.min(100, Math.max(0, node.reclaim_pct ?? 0))}%` }}
                          />
                        </div>
                      </td>
                      <td className="px-3 py-2.5 text-[11px]">
                        {node.prune.busy ? (
                          <span className="text-amber-600">Pruning...</span>
                        ) : node.prune.last_error ? (
                          <span className="text-red-500">{node.prune.last_error}</span>
                        ) : (
                          <span className="text-stone-400">Last: {formatTime(node.prune.finished_at)}</span>
                        )}
                      </td>
                      <td className="px-3 py-2.5">
                        <button
                          onClick={() => triggerPrune(node.node)}
                          disabled={!node.online || node.prune.busy || pruneMutation.isPending}
                          className="px-2 py-1 text-[11px] border border-[#e2ddd7] text-stone-500 hover:border-stone-400 rounded disabled:opacity-40 disabled:cursor-not-allowed"
                        >
                          {node.prune.busy ? 'Pruning...' : 'Prune'}
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </>
        )}
      </main>
    </div>
  )
}

function StatCard({ label, value, sub, bar }: { label: string; value: string; sub?: string; bar?: number }) {
  return (
    <div className="bg-white border border-[#e2ddd7] rounded-lg p-3">
      <div className="text-[10px] text-stone-400 uppercase tracking-widest mb-1">{label}</div>
      <div className="text-sm font-semibold text-[#1c1917]">{value}</div>
      {sub && <div className="text-[11px] text-stone-400 mt-0.5">{sub}</div>}
      {bar !== undefined && (
        <div className="mt-2 h-1 w-full bg-stone-100 rounded-full overflow-hidden">
          <div
            className={`h-full rounded-full transition-all`}
            style={{
              width: `${Math.min(100, Math.max(0, bar))}%`,
              backgroundColor: bar >= 90 ? '#f87171' : bar >= 60 ? '#fbbf24' : '#14b8a6',
            }}
          />
        </div>
      )}
    </div>
  )
}
