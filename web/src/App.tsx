import { useEffect, useState } from 'react'
import type { Tab } from './app/navigation'
import { tabFromPath, pathFromTab } from './app/navigation'
import { AppShell } from './components/layout'
import { DashboardPage } from './features/dashboard/DashboardPage'
import { SystemLogViewer } from './features/system-logs/SystemLogViewer'
import { ContainersPage } from './features/containers/ContainersPage'
import { ContainerLogPanel } from './features/logs/components/ContainerLogPanel'
import { BackupsPage } from './features/backups/BackupsPage'
import { StoragePage } from './features/storage/StoragePage'
import { RoutingPage } from './features/routing/RoutingPage'
import { CrowdSecPage } from './features/crowdsec/CrowdSecPage'
import { LogsPage } from './features/logs/LogsPage'
import { LogPanel } from './features/logs/components/LogPanel'
import { AppDetailPage } from './features/apps/detail/AppDetailPage'

type LogPanelState =
  | { type: 'app'; node: string; appId: string }
  | { type: 'container'; node: string; containerId: string; containerName: string }
  | null

function App() {
  const [logPanel, setLogPanel] = useState<LogPanelState>(null)
  const [path, setPath] = useState(() => window.location.pathname)
  const [logPanelExpanded, setLogPanelExpanded] = useState(false)

  const activeTab = tabFromPath(path)
  const appDetailId = path.startsWith('/apps/') ? path.slice(6) : null

  function navigate(to: string) {
    if (window.location.pathname !== to) {
      window.history.pushState(null, '', to)
    }
    setPath(to)
  }

  function handleTabChange(tab: Tab) {
    navigate(pathFromTab(tab))
  }

  useEffect(() => {
    const onPop = () => setPath(window.location.pathname)
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  }, [])

  const handleShowAppLogs = (node: string, appId: string) => {
    setLogPanel({ type: 'app', node, appId })
  }

  const handleShowContainerLogs = (node: string, containerId: string, containerName: string) => {
    setLogPanel({ type: 'container', node, containerId, containerName })
  }

  const handleClosePanel = () => setLogPanel(null)
  const LOG_PANEL_HEIGHT = logPanelExpanded ? 360 : 40

  return (
    <AppShell activeTab={activeTab} onTabChange={handleTabChange} bottomPadding={LOG_PANEL_HEIGHT}>
      {!appDetailId && activeTab === 'dashboard' && (
        <DashboardPage onShowLogs={handleShowAppLogs} onNavigate={navigate} />
      )}

      {appDetailId && <AppDetailPage appId={appDetailId} onBack={() => navigate('/')} />}
      {!appDetailId && activeTab === 'backups' && <BackupsPage />}
      {!appDetailId && activeTab === 'storage' && <StoragePage />}
      {!appDetailId && activeTab === 'containers' && <ContainersPage onShowLogs={handleShowContainerLogs} />}
      {!appDetailId && activeTab === 'routing' && <RoutingPage />}
      {!appDetailId && activeTab === 'crowdsec' && <CrowdSecPage />}
      {!appDetailId && activeTab === 'logs' && <LogsPage />}

      <SystemLogViewer expanded={logPanelExpanded} onToggle={() => setLogPanelExpanded((v) => !v)} />

      <LogPanel
        isOpen={logPanel?.type === 'app'}
        onClose={handleClosePanel}
        node={logPanel?.type === 'app' ? logPanel.node : ''}
        appId={logPanel?.type === 'app' ? logPanel.appId : ''}
      />

      <ContainerLogPanel
        isOpen={logPanel?.type === 'container'}
        onClose={handleClosePanel}
        node={logPanel?.type === 'container' ? logPanel.node : ''}
        containerId={logPanel?.type === 'container' ? logPanel.containerId : ''}
        containerName={logPanel?.type === 'container' ? logPanel.containerName : ''}
      />
    </AppShell>
  )
}

export default App
