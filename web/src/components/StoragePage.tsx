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

function usageBarClass(value?: number): string {
  if (value === undefined || Number.isNaN(value)) return 'bg-gray-600'
  if (value >= 90) return 'bg-red-500'
  if (value >= 60) return 'bg-yellow-500'
  return 'bg-green-500'
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
      if (b.disk_free !== a.disk_free) {
        return b.disk_free - a.disk_free
      }
      return a.node.localeCompare(b.node)
    })
  }, [data?.nodes])

  function triggerPrune(node: string) {
    const scope = node === 'all' ? 'all online nodes' : node
    if (!window.confirm(`Prune unassigned appdata on ${scope}? This deletes data directories and backup metadata for apps not assigned to those nodes.`)) {
      return
    }
    pruneMutation.mutate({ node })
  }

  if (isLoading) {
    return <div className="text-sm text-gray-400">Loading storage status...</div>
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-white">Storage</h2>
        <button
          onClick={() => triggerPrune('all')}
          disabled={pruneMutation.isPending || pruningAny}
          className="px-3 py-1.5 text-xs bg-red-900 hover:bg-red-800 disabled:opacity-50 rounded text-red-200"
        >
          {pruningAny ? 'Pruning...' : 'Prune All Online Nodes'}
        </button>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
        <div className="bg-gray-800 border border-gray-700 rounded p-3">
          <div className="text-xs text-gray-400">Total</div>
          <div className="text-white font-semibold">{formatBytes(data?.aggregate.disk_total)}</div>
        </div>
        <div className="bg-gray-800 border border-gray-700 rounded p-3">
          <div className="text-xs text-gray-400">Used</div>
          <div className="text-white font-semibold">{formatBytes(data?.aggregate.disk_used)} ({formatPercent(data?.aggregate.disk_percent)})</div>
        </div>
        <div className="bg-gray-800 border border-gray-700 rounded p-3">
          <div className="text-xs text-gray-400">Free</div>
          <div className="text-white font-semibold">{formatBytes(data?.aggregate.disk_free)}</div>
        </div>
        <div className="bg-gray-800 border border-gray-700 rounded p-3 sm:col-span-3">
          <div className="text-xs text-gray-400">Reclaimable by prune</div>
          <div className="text-white font-semibold">{formatBytes(data?.aggregate.reclaimable)} ({formatPercent(data?.aggregate.reclaim_pct)})</div>
        </div>
      </div>

      <div className="bg-gray-800 border border-gray-700 rounded overflow-x-auto">
        <table className="w-full text-sm">
          <thead className="bg-gray-800 border-b border-gray-700">
            <tr className="text-left text-gray-300">
              <th className="px-3 py-2">Node</th>
              <th className="px-3 py-2">Status</th>
              <th className="px-3 py-2">Appdata Path</th>
              <th className="px-3 py-2">Usage</th>
              <th className="px-3 py-2">Reclaimable</th>
              <th className="px-3 py-2">Prune</th>
              <th className="px-3 py-2">Action</th>
            </tr>
          </thead>
          <tbody>
            {sortedNodes.map((node) => (
              <tr key={node.node} className="border-b border-gray-700/60 text-gray-200">
                <td className="px-3 py-2 font-medium">{node.node}</td>
                <td className="px-3 py-2">{node.online ? 'Online' : 'Offline'}</td>
                <td className="px-3 py-2 text-gray-400">{node.appdata_path || '--'}</td>
                <td className="px-3 py-2 min-w-56">
                  <div>{formatBytes(node.disk_used)} / {formatBytes(node.disk_total)} ({formatPercent(node.disk_percent)})</div>
                  <div className="mt-1 h-1.5 w-full rounded bg-gray-700 overflow-hidden">
                    <div
                      className={`h-full ${usageBarClass(node.disk_percent)}`}
                      style={{ width: `${Math.min(100, Math.max(0, node.disk_percent ?? 0))}%` }}
                    />
                  </div>
                </td>
                <td className="px-3 py-2">{formatPercent(node.reclaim_pct)} ({formatBytes(node.reclaimable)})</td>
                <td className="px-3 py-2 text-xs">
                  {node.prune.busy ? (
                    <span className="text-yellow-300">Pruning...</span>
                  ) : node.prune.last_error ? (
                    <span className="text-red-300">{node.prune.last_error}</span>
                  ) : (
                    <span className="text-gray-400">Last: {formatTime(node.prune.finished_at)}</span>
                  )}
                </td>
                <td className="px-3 py-2">
                  <button
                    onClick={() => triggerPrune(node.node)}
                    disabled={!node.online || node.prune.busy || pruneMutation.isPending}
                    className="px-2 py-1 text-xs rounded bg-gray-700 hover:bg-gray-600 disabled:opacity-40 disabled:cursor-not-allowed"
                  >
                    {node.prune.busy ? 'Pruning...' : 'Prune'}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
