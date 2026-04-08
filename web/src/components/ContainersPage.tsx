import { useMemo } from 'react'
import { useContainers } from '../api/hooks'
import { ContainerStateBadge } from './StatusBadge'
import type { NodeContainers } from '../api/types'

interface ContainersPageProps {
  onShowLogs: (node: string, containerId: string, containerName: string) => void
}

export function ContainersPage({ onShowLogs }: ContainersPageProps) {
  const { data: nodeContainers, isLoading } = useContainers()

  const sortedNodes = useMemo(() => {
    if (!nodeContainers) return []
    return [...nodeContainers].sort((a, b) => a.node.localeCompare(b.node))
  }, [nodeContainers])

  const totalContainers = sortedNodes.reduce((n, nd) => n + nd.containers.length, 0)

  return (
    <div className="min-h-screen bg-[#faf9f7]">
      <div className="border-b border-[#e2ddd7] bg-white px-4 py-4">
        <div className="max-w-6xl mx-auto flex items-center justify-between">
          <div>
            <h1 className="text-sm font-semibold text-[#1c1917]">Containers</h1>
            <p className="text-[11px] text-stone-400 mt-0.5">
              {isLoading ? 'Loading...' : `${totalContainers} container${totalContainers !== 1 ? 's' : ''} across ${sortedNodes.length} node${sortedNodes.length !== 1 ? 's' : ''}`}
            </p>
          </div>
        </div>
      </div>

      <main className="max-w-6xl mx-auto px-4 py-6">
        {isLoading ? (
          <div className="text-sm text-stone-400 py-8 text-center">Loading containers...</div>
        ) : sortedNodes.length === 0 ? (
          <div className="bg-white border border-[#e2ddd7] rounded-lg p-8 text-center text-stone-400 text-sm">
            No nodes found
          </div>
        ) : (
          <div className="space-y-3">
            {sortedNodes.map((nc) => (
              <NodeContainersCard key={nc.node} nodeData={nc} onShowLogs={onShowLogs} />
            ))}
          </div>
        )}
      </main>
    </div>
  )
}

function NodeContainersCard({
  nodeData,
  onShowLogs,
}: {
  nodeData: NodeContainers
  onShowLogs: (node: string, containerId: string, containerName: string) => void
}) {
  const sorted = [...nodeData.containers].sort((a, b) => a.name.localeCompare(b.name))

  return (
    <div className="bg-white border border-[#e2ddd7] rounded-lg overflow-hidden">
      {/* Node header */}
      <div className="px-4 py-2.5 border-b border-[#f0ede8] flex items-center gap-2">
        <span
          className={`w-2 h-2 rounded-full ${nodeData.online ? 'bg-emerald-500 health-glow' : 'bg-stone-300'}`}
        />
        <span className="text-sm font-semibold text-[#1c1917]">{nodeData.node}</span>
        <span className="text-[11px] text-stone-400">
          {sorted.length} container{sorted.length !== 1 ? 's' : ''}
        </span>
        {nodeData.error && (
          <span className="text-[11px] text-red-500 ml-2">{nodeData.error}</span>
        )}
      </div>

      {sorted.length === 0 && !nodeData.error ? (
        <div className="px-4 py-3 text-[11px] text-stone-400">No containers</div>
      ) : (
        <table className="w-full text-xs">
          <thead>
            <tr className="border-b border-[#f0ede8]">
              <th className="px-4 py-2 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Name</th>
              <th className="px-3 py-2 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Image</th>
              <th className="px-3 py-2 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">State</th>
              <th className="px-3 py-2 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Status</th>
              <th className="px-3 py-2"></th>
            </tr>
          </thead>
          <tbody>
            {sorted.map((c) => (
              <tr key={c.id} className="border-b border-[#f7f5f0] hover:bg-[#faf9f7]">
                <td className="px-4 py-2 font-mono text-[#1c1917] font-medium">{c.name}</td>
                <td className="px-3 py-2 text-stone-400 max-w-48 truncate">{c.image}</td>
                <td className="px-3 py-2">
                  <ContainerStateBadge state={c.state} />
                </td>
                <td className="px-3 py-2 text-stone-400">{c.status}</td>
                <td className="px-3 py-2">
                  <button
                    onClick={() => onShowLogs(nodeData.node, c.id, c.name)}
                    className="text-[11px] text-teal-600 hover:text-teal-800 hover:underline"
                  >
                    logs
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
