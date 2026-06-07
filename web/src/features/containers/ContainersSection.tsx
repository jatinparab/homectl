import { useState, useMemo } from 'react'
import { useContainers } from '../../api/hooks'
import { ContainerStateBadge } from '../../features/shared/components/StatusBadge'
import type { NodeContainers } from '../../api/types'

interface ContainersSectionProps {
  onShowLogs: (node: string, containerId: string, containerName: string) => void
}

function NodeGroup({
  nodeData,
  onShowLogs,
}: {
  nodeData: NodeContainers
  onShowLogs: (node: string, containerId: string, containerName: string) => void
}) {
  const [expanded, setExpanded] = useState(true)

  return (
    <div className="mb-4">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-2 text-sm font-medium text-white mb-2 hover:text-gray-300"
      >
        <span className={`transform transition-transform ${expanded ? 'rotate-90' : ''}`}>
          &#9654;
        </span>
        {nodeData.node}
        <span className={`w-2 h-2 rounded-full ${nodeData.online ? 'bg-green-500' : 'bg-red-500'}`} />
        <span className="text-gray-400 text-xs">
          ({nodeData.containers.length} containers)
        </span>
      </button>

      {expanded && (
        <>
          {nodeData.error && (
            <div className="text-red-400 text-xs mb-2 ml-5">{nodeData.error}</div>
          )}
          {nodeData.containers.length === 0 && !nodeData.error && (
            <div className="text-gray-500 text-xs ml-5">No containers</div>
          )}
          {nodeData.containers.length > 0 && (
            <div className="overflow-x-auto ml-5">
              <table className="w-full text-sm text-left">
                <thead>
                  <tr className="text-gray-400 text-xs uppercase border-b border-gray-700">
                    <th className="py-2 px-3">Name</th>
                    <th className="py-2 px-3">Image</th>
                    <th className="py-2 px-3">State</th>
                    <th className="py-2 px-3">Status</th>
                    <th className="py-2 px-3"></th>
                  </tr>
                </thead>
                <tbody>
                  {[...nodeData.containers].sort((a, b) => a.name.localeCompare(b.name)).map((c) => (
                    <tr key={c.id} className="border-b border-gray-700/50 hover:bg-gray-700/30">
                      <td className="py-2 px-3 text-white font-mono text-xs">{c.name}</td>
                      <td className="py-2 px-3 text-gray-400 text-xs max-w-48 truncate">{c.image}</td>
                      <td className="py-2 px-3">
                        <ContainerStateBadge state={c.state} />
                      </td>
                      <td className="py-2 px-3 text-gray-400 text-xs">{c.status}</td>
                      <td className="py-2 px-3">
                        <button
                          onClick={() => onShowLogs(nodeData.node, c.id, c.name)}
                          className="text-blue-400 hover:text-blue-300 text-xs"
                        >
                          Logs
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}
    </div>
  )
}

export function ContainersSection({ onShowLogs }: ContainersSectionProps) {
  const { data: nodeContainers, isLoading } = useContainers()

  const sortedNodes = useMemo(() => {
    if (!nodeContainers) return []
    return [...nodeContainers].sort((a, b) => a.node.localeCompare(b.node))
  }, [nodeContainers])

  return (
    <div className="mb-6">
      <h2 className="text-lg font-semibold text-white mb-4">Docker Containers</h2>
      {isLoading ? (
        <div className="text-gray-500 text-sm">Loading containers...</div>
      ) : sortedNodes.length === 0 ? (
        <div className="text-gray-500 text-sm">No nodes found</div>
      ) : (
        <div className="bg-gray-800 rounded-lg border border-gray-700 p-4">
          {sortedNodes.map((nc) => (
            <NodeGroup key={nc.node} nodeData={nc} onShowLogs={onShowLogs} />
          ))}
        </div>
      )}
    </div>
  )
}
