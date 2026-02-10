import { useMemo, useState } from 'react'
import { useApps, useAgents } from './api/hooks'
import { Header } from './components/Header'
import { AppsTable } from './components/AppsTable'
import { NodeCards } from './components/NodeCards'
import { LogPanel } from './components/LogPanel'
import { SystemLogViewer } from './components/SystemLogViewer'
import { ContainersSection } from './components/ContainersSection'
import { ContainerLogPanel } from './components/ContainerLogPanel'

type LogPanelState =
  | { type: 'app'; node: string; appId: string }
  | { type: 'container'; node: string; containerId: string; containerName: string }
  | null

function App() {
  const { data: apps, isLoading } = useApps()
  const { data: agents } = useAgents()
  const [logPanel, setLogPanel] = useState<LogPanelState>(null)

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

  const handleShowAppLogs = (node: string, appId: string) => {
    setLogPanel({ type: 'app', node, appId })
  }

  const handleShowContainerLogs = (node: string, containerId: string, containerName: string) => {
    setLogPanel({ type: 'container', node, containerId, containerName })
  }

  const handleClosePanel = () => {
    setLogPanel(null)
  }

  return (
    <div className="min-h-screen bg-gray-900">
      <Header />
      <main className="max-w-6xl mx-auto px-4 py-6">
        {/* Node Status Cards */}
        <NodeCards agents={agents} />

        {/* Main Apps Table */}
        <h2 className="text-lg font-semibold text-white mb-4">Applications</h2>
        <AppsTable apps={sortedApps} isLoading={isLoading} onShowLogs={handleShowAppLogs} />

        {/* System Logs */}
        <SystemLogViewer />

        {/* Docker Containers */}
        <ContainersSection onShowLogs={handleShowContainerLogs} />
      </main>

      {/* App Log Side Panel */}
      <LogPanel
        isOpen={logPanel?.type === 'app'}
        onClose={handleClosePanel}
        node={logPanel?.type === 'app' ? logPanel.node : ''}
        appId={logPanel?.type === 'app' ? logPanel.appId : ''}
      />

      {/* Container Log Side Panel */}
      <ContainerLogPanel
        isOpen={logPanel?.type === 'container'}
        onClose={handleClosePanel}
        node={logPanel?.type === 'container' ? logPanel.node : ''}
        containerId={logPanel?.type === 'container' ? logPanel.containerId : ''}
        containerName={logPanel?.type === 'container' ? logPanel.containerName : ''}
      />
    </div>
  )
}

export default App
