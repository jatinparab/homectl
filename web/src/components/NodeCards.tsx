import type { Agent } from '../api/types'

interface NodeCardsProps {
  agents: Agent[] | undefined
}

export function NodeCards({ agents }: NodeCardsProps) {
  if (!agents || agents.length === 0) {
    return null
  }

  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 mb-6">
      {agents.map((agent) => (
        <NodeCard key={agent.name} agent={agent} />
      ))}
    </div>
  )
}

interface NodeCardProps {
  agent: Agent
}

function NodeCard({ agent }: NodeCardProps) {
  return (
    <div className="bg-gray-800 rounded-lg border border-gray-700 p-4">
      <div className="flex items-center justify-between mb-2">
        <h3 className="text-white font-medium">{agent.name}</h3>
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
      </div>
    </div>
  )
}
