import { useMemo, useState } from 'react'
import { useApps, useAgents, useUpcomingBackup, useUpcomingPrune, useSync, useRebalance, useGatewayInfo } from './api/hooks'
import { Header, type Tab } from './components/Header'
import { AppsTable } from './components/AppsTable'
import { NodeCards } from './components/NodeCards'
import { SystemLogViewer } from './components/SystemLogViewer'
import { ContainersPage } from './components/ContainersPage'
import { ContainerLogPanel } from './components/ContainerLogPanel'
import { BackupsPage } from './components/BackupsPage'
import { StoragePage } from './components/StoragePage'
import { LogPanel } from './components/LogPanel'

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
  const [logPanelExpanded, setLogPanelExpanded] = useState(false)

  const sortedApps = useMemo(() => {
    if (!apps) return []
    return [...apps].sort((a, b) => {
      const desiredA = a.desired_state === 'running' ? 0 : 1
      const desiredB = b.desired_state === 'running' ? 0 : 1
      if (desiredA !== desiredB) return desiredA - desiredB
      return a.app_id.localeCompare(b.app_id)
    })
  }, [apps])

  const handleShowAppLogs = (node: string, appId: string) => {
    setLogPanel({ type: 'app', node, appId })
  }

  const handleShowContainerLogs = (node: string, containerId: string, containerName: string) => {
    setLogPanel({ type: 'container', node, containerId, containerName })
  }

  const handleClosePanel = () => setLogPanel(null)

  const LOG_PANEL_HEIGHT = logPanelExpanded ? 360 : 40

  return (
    <div className="min-h-screen" style={{ paddingBottom: `${LOG_PANEL_HEIGHT}px` }}>
      <Header activeTab={activeTab} onTabChange={setActiveTab} />

      {activeTab === "dashboard" && (
        <main className="max-w-6xl mx-auto px-4 py-6">
          <NodeCards agents={agents} gatewayInfo={gatewayInfo} />

          <div className="flex items-center justify-between mb-3">
            <h2 className="text-xs font-semibold text-stone-400 uppercase tracking-widest">Applications</h2>
            <div className="flex items-center gap-2">
              <button
                onClick={() => syncMutation.mutate()}
                disabled={syncMutation.isPending}
                className="px-3 py-1 text-[11px] border border-[#e2ddd7] text-stone-500 hover:border-stone-400 hover:text-stone-700 rounded disabled:opacity-50 transition-colors"
                title="Git pull and sync updated configs to all agents"
              >
                {syncMutation.isPending ? 'Syncing...' : 'Sync'}
              </button>
              <button
                onClick={() => rebalanceMutation.mutate()}
                disabled={rebalanceMutation.isPending}
                className="px-3 py-1 text-[11px] border border-[#e2ddd7] text-stone-500 hover:border-stone-400 hover:text-stone-700 rounded disabled:opacity-50 transition-colors"
                title="Rebalance all apps across nodes (may restart running apps)"
              >
                {rebalanceMutation.isPending ? 'Rebalancing...' : 'Rebalance'}
              </button>
            </div>
          </div>
          <AppsTable
            apps={sortedApps}
            isLoading={isLoading}
            upcomingBackup={upcomingBackup}
            upcomingPrune={upcomingPrune}
            onShowLogs={handleShowAppLogs}
            agents={agents}
          />
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

      {activeTab === "containers" && (
        <main className="max-w-6xl mx-auto px-4 py-6">
          <ContainersPage onShowLogs={handleShowContainerLogs} />
        </main>
      )}

      {/* Sticky system log panel */}
      <SystemLogViewer
        expanded={logPanelExpanded}
        onToggle={() => setLogPanelExpanded((v) => !v)}
      />

      {/* App log side panel */}
      <LogPanel
        isOpen={logPanel?.type === 'app'}
        onClose={handleClosePanel}
        node={logPanel?.type === 'app' ? logPanel.node : ''}
        appId={logPanel?.type === 'app' ? logPanel.appId : ''}
      />

      {/* Container log side panel */}
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
