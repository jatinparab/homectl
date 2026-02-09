import { useMemo, useState } from 'react'
import { useApps, useAgents } from './api/hooks'
import { Header } from './components/Header'
import { AppsTable } from './components/AppsTable'
import { NodeCards } from './components/NodeCards'
import { LogPanel } from './components/LogPanel'

function App() {
  const { data: apps, isLoading } = useApps()
  const { data: agents } = useAgents()
  const [logPanel, setLogPanel] = useState<{ node: string; appId: string } | null>(null)

  const sortedApps = useMemo(() => {
    if (!apps) return []

    const statusPriority = (state: string, hasQuarantined: boolean): number => {
      if (state === 'error') return 0
      if (hasQuarantined) return 1
      if (state === 'restarting') return 2
      if (state === 'stopped') return 3
      if (state === 'unknown') return 4
      return 5 // running
    }

    return [...apps].sort((a, b) => {
      const hasQuarantinedA = a.node_reports.some(nr => nr.quarantined)
      const hasQuarantinedB = b.node_reports.some(nr => nr.quarantined)
      const priorityA = statusPriority(a.observed_state, hasQuarantinedA)
      const priorityB = statusPriority(b.observed_state, hasQuarantinedB)
      if (priorityA !== priorityB) return priorityA - priorityB
      return a.app_id.localeCompare(b.app_id)
    })
  }, [apps])

  const handleShowLogs = (node: string, appId: string) => {
    setLogPanel({ node, appId })
  }

  return (
    <div className="min-h-screen bg-gray-900">
      <Header />
      <main className="max-w-6xl mx-auto px-4 py-6">
        {/* Node Status Cards */}
        <NodeCards agents={agents} />

        {/* Main Apps Table */}
        <h2 className="text-lg font-semibold text-white mb-4">Applications</h2>
        <AppsTable apps={sortedApps} isLoading={isLoading} onShowLogs={handleShowLogs} />
      </main>

      {/* Log Side Panel */}
      <LogPanel
        isOpen={!!logPanel}
        onClose={() => setLogPanel(null)}
        node={logPanel?.node ?? ''}
        appId={logPanel?.appId ?? ''}
      />
    </div>
  )
}

export default App
