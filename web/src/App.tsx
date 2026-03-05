import { useMemo, useState } from 'react'
import { useApps, useAgents, useUpcomingBackup, useUpcomingPrune, useSync, useRebalance, useGatewayInfo } from './api/hooks'
import { Header, type Tab } from './components/Header'
import { AppsTable } from './components/AppsTable'
import { NodeCards } from './components/NodeCards'
import { LogPanel } from './components/LogPanel'
import { SystemLogViewer } from './components/SystemLogViewer'
import { ContainersSection } from './components/ContainersSection'
import { ContainerLogPanel } from './components/ContainerLogPanel'
import { BackupsPage } from './components/BackupsPage'
import { StoragePage } from './components/StoragePage'

type LogPanelState =
  | { type: 'app'; node: string; appId: string }
  | { type: 'container'; node: string; containerId: string; containerName: string }
  | null

function App() {
  const { data: apps, isLoading } = useApps()
  const { data: agents } = useAgents()
  const { data: gatewayInfo } = useGatewayInfo()
  const { data: upcomingBackup } = useUpcomingBackup()
  const { data: upcomingPrune } = useUpcomingPrune()
  const syncMutation = useSync()
  const rebalanceMutation = useRebalance()
  const [logPanel, setLogPanel] = useState<LogPanelState>(null)
  const [activeTab, setActiveTab] = useState<Tab>("dashboard")

  const sortedApps = useMemo(() => {
    if (!apps) return []

    return [...apps].sort((a, b) => {
      // 1. Desired running first, then stopped
      const desiredA = a.desired_state === 'running' ? 0 : 1
      const desiredB = b.desired_state === 'running' ? 0 : 1
      if (desiredA !== desiredB) return desiredA - desiredB

      // 2. Alphabetical within each group
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
      <Header activeTab={activeTab} onTabChange={setActiveTab} />

      {activeTab === "dashboard" && (
        <main className="max-w-6xl mx-auto px-4 py-6">
          {/* Node Status Cards */}
          <NodeCards agents={agents} gatewayInfo={gatewayInfo} />

          {/* Main Apps Table */}
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-lg font-semibold text-white">Applications</h2>
            <div className="flex items-center gap-2">
              <button
                onClick={() => syncMutation.mutate()}
                disabled={syncMutation.isPending}
                className="px-3 py-1.5 text-xs bg-gray-700 hover:bg-gray-600 disabled:opacity-50 rounded text-gray-300"
                title="Git pull and sync updated configs to all agents"
              >
                {syncMutation.isPending ? 'Syncing...' : 'Sync'}
              </button>
              <button
                onClick={() => rebalanceMutation.mutate()}
                disabled={rebalanceMutation.isPending}
                className="px-3 py-1.5 text-xs bg-gray-700 hover:bg-gray-600 disabled:opacity-50 rounded text-gray-300"
                title="Rebalance all apps across nodes (may restart running apps)"
              >
                {rebalanceMutation.isPending ? 'Rebalancing...' : 'Rebalance'}
              </button>
            </div>
          </div>
          <AppsTable apps={sortedApps} isLoading={isLoading} upcomingBackup={upcomingBackup} upcomingPrune={upcomingPrune} onShowLogs={handleShowAppLogs} />

          {/* System Logs */}
          <SystemLogViewer />

          {/* Docker Containers */}
          <ContainersSection onShowLogs={handleShowContainerLogs} />
        </main>
      )}

      {activeTab === "backups" && (
        <main className="max-w-6xl mx-auto px-4 py-6">
          <BackupsPage apps={apps} />
        </main>
      )}

      {activeTab === "storage" && (
        <main className="max-w-6xl mx-auto px-4 py-6">
          <StoragePage />
        </main>
      )}

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
