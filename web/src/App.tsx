import { useEffect, useState } from 'react'
import styled from '@emotion/styled'
import type { Tab } from './app/navigation'
import { tabFromPath, pathFromTab } from './app/navigation'
import { AppShell } from './components/layout'
import { fetchAuthStatus, logout } from './api/auth'
import type { AuthStatus } from './api/auth'
import { AuthPage } from './features/auth/AuthPage'
import { DashboardPage } from './features/dashboard/DashboardPage'
import { SystemLogViewer } from './features/system-logs/SystemLogViewer'
import { ContainersPage } from './features/containers/ContainersPage'
import { ContainerLogPanel } from './features/logs/components/ContainerLogPanel'
import { BackupsPage } from './features/backups/BackupsPage'
import { StoragePage } from './features/storage/StoragePage'
import { RoutingPage } from './features/routing/RoutingPage'
import { CrowdSecPage } from './features/crowdsec/CrowdSecPage'
import { LogsPage } from './features/logs/LogsPage'
import { SettingsPage } from './features/settings/SettingsPage'
import { LogPanel } from './features/logs/components/LogPanel'
import { AppDetailPage } from './features/apps/detail/AppDetailPage'

type LogPanelState =
  | { type: 'app'; node: string; appId: string }
  | { type: 'container'; node: string; containerId: string; containerName: string }
  | null

const LoadingRoot = styled.div`
  min-height: 100vh;
  display: grid;
  place-items: center;
  background: ${({ theme }) => theme.colors.neutralBg};
  color: ${({ theme }) => theme.colors.textMuted};
  font-size: 13px;
`

function App() {
  const [logPanel, setLogPanel] = useState<LogPanelState>(null)
  const [path, setPath] = useState(() => window.location.pathname)
  const [logPanelExpanded, setLogPanelExpanded] = useState(false)
  const [auth, setAuth] = useState<AuthStatus | null>(null)
  const [authLoading, setAuthLoading] = useState(true)

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

  async function refreshAuth() {
    setAuthLoading(true)
    try {
      setAuth(await fetchAuthStatus())
    } finally {
      setAuthLoading(false)
    }
  }

  useEffect(() => {
    void refreshAuth()
  }, [])

  const handleShowAppLogs = (node: string, appId: string) => {
    setLogPanel({ type: 'app', node, appId })
  }

  const handleShowContainerLogs = (node: string, containerId: string, containerName: string) => {
    setLogPanel({ type: 'container', node, containerId, containerName })
  }

  const handleClosePanel = () => setLogPanel(null)
  const LOG_PANEL_HEIGHT = logPanelExpanded ? 360 : 40

  async function handleLogout() {
    await logout()
    setAuth(await fetchAuthStatus())
  }

  if (authLoading || !auth) {
    return <LoadingRoot>Loading</LoadingRoot>
  }

  if (auth.onboarding_required) {
    return <AuthPage mode="onboarding" onDone={refreshAuth} />
  }

  if (!auth.authenticated) {
    return <AuthPage mode="login" onDone={refreshAuth} />
  }

  return (
    <AppShell activeTab={activeTab} onTabChange={handleTabChange} onLogout={handleLogout} bottomPadding={LOG_PANEL_HEIGHT}>
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
      {!appDetailId && activeTab === 'settings' && <SettingsPage />}

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
