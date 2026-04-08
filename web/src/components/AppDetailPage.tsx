import { useEffect, useRef, useState, useCallback, useMemo } from 'react'
import { useApp, useContainers, useUpdateAppDesiredState, useRestartApp, useTriggerBackup, useSnapshots, useGatewayInfo } from '../api/hooks'
import { HealthBadge, PhaseBadge, DesiredToggle, ContainerStateBadge } from './StatusBadge'
import { useLiveState } from '../api/live'
import type { NodeReport } from '../api/types'

function timeAgo(dateStr?: string): string {
  if (!dateStr) return '—'
  const d = new Date(dateStr)
  const s = Math.floor((Date.now() - d.getTime()) / 1000)
  if (s < 60) return `${s}s ago`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}

// ── Log panel ──────────────────────────────────────────────────────────────

interface LogLine { id: number; text: string }
let logIdSeq = 0

function useAppLogs(appId: string, node: string | null) {
  const [lines, setLines] = useState<LogLine[]>([])
  const seenRef = useRef<Set<string>>(new Set())
  const { subscribe, unsubscribe } = useLiveState()

  const addLines = useCallback((raw: string) => {
    const incoming = raw.split('\n').filter(Boolean)
    setLines(prev => {
      const next = [...prev]
      for (const text of incoming) {
        if (seenRef.current.has(text)) continue
        seenRef.current.add(text)
        next.push({ id: ++logIdSeq, text })
      }
      // Cap at 2000 lines
      return next.length > 2000 ? next.slice(-2000) : next
    })
  }, [])

  useEffect(() => {
    if (!node) return
    seenRef.current.clear()
    setLines([])

    const id = subscribe('app-logs', { appId, node }, (line, _source, done) => {
      if (done || !line) return
      addLines(line)
    })

    return () => unsubscribe(id)
  }, [appId, node, addLines, subscribe, unsubscribe])

  return lines
}


function LogPanel({ lines, label, autoScroll }: { lines: LogLine[]; label: string; autoScroll?: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  const [pinned, setPinned] = useState(true)

  useEffect(() => {
    if (pinned && autoScroll !== false && ref.current) {
      ref.current.scrollTop = ref.current.scrollHeight
    }
  }, [lines, pinned, autoScroll])

  function onScroll() {
    const el = ref.current
    if (!el) return
    setPinned(el.scrollHeight - el.scrollTop - el.clientHeight < 40)
  }

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center justify-between px-3 py-1.5 border-b border-[#e2ddd7] bg-[#f7f5f0] flex-shrink-0">
        <span className="text-[10px] font-semibold text-stone-400 uppercase tracking-widest">{label}</span>
        <span className="text-[10px] text-stone-400">{lines.length} lines</span>
      </div>
      <div
        ref={ref}
        onScroll={onScroll}
        className="flex-1 overflow-auto bg-[#1c1917] font-mono text-[11px] p-2 log-scroll"
      >
        {lines.length === 0 ? (
          <span className="text-stone-500">Waiting for output...</span>
        ) : (
          lines.map(l => (
            <div key={l.id} className="text-stone-300 leading-5 whitespace-pre-wrap break-all">{l.text}</div>
          ))
        )}
        {!pinned && (
          <button
            onClick={() => { if (ref.current) ref.current.scrollTop = ref.current.scrollHeight; setPinned(true) }}
            className="sticky bottom-2 left-full mr-2 bg-stone-700 text-stone-200 text-[10px] px-2 py-0.5 rounded"
          >↓ follow</button>
        )}
      </div>
    </div>
  )
}

// ── Main page ──────────────────────────────────────────────────────────────

interface AppDetailPageProps {
  appId: string
  onBack: () => void
}

export function AppDetailPage({ appId, onBack }: AppDetailPageProps) {
  const { data: app } = useApp(appId)
  const { data: nodeContainers } = useContainers()
  const { data: gatewayInfo } = useGatewayInfo()
  const desiredMutation = useUpdateAppDesiredState()
  const restartMutation = useRestartApp()
  const backupMutation = useTriggerBackup()

  // Sort reports alphabetically — stable order regardless of server response ordering
  const allReports = useMemo(
    () => [...(app?.node_reports ?? [])].sort((a, b) => a.node.localeCompare(b.node)),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [app?.app_id, app?.node_reports.map(r => r.node).sort().join(',')]
  )

  // Containers for this app, sorted deterministically
  const appContainers = useMemo(() => {
    const raw = nodeContainers?.flatMap(nc =>
      nc.containers
        .filter(c => c.name.startsWith(appId + '-') || c.name.startsWith(appId + '_') || c.name === appId)
        .map(c => ({ ...c, node: nc.node, nodeOnline: nc.online }))
    ) ?? []
    return raw.sort((a, b) => a.node.localeCompare(b.node) || a.name.localeCompare(b.name))
  }, [appId, nodeContainers])

  // Compute the ideal default node once — gateway for global apps, assigned_agent otherwise
  const defaultNode = useMemo(() => {
    if (!app) return null
    const nodes = allReports.map(r => r.node)
    if (nodes.length === 0) return null
    if (app.is_global && gatewayInfo?.current_node && nodes.includes(gatewayInfo.current_node)) {
      return gatewayInfo.current_node
    }
    if (app.assigned_agent && nodes.includes(app.assigned_agent)) {
      return app.assigned_agent
    }
    // Fall back to first online node, then alphabetically first
    return allReports.find(r => r.node_online)?.node ?? nodes[0]
  }, [app?.app_id, app?.is_global, app?.assigned_agent, gatewayInfo?.current_node, allReports]) // eslint-disable-line react-hooks/exhaustive-deps

  // Initialize logNode once; never reset it from live data — only the user can change it
  const [logNode, setLogNode] = useState<string | null>(null)
  const initializedRef = useRef(false)
  useEffect(() => {
    if (initializedRef.current || !defaultNode) return
    initializedRef.current = true
    setLogNode(defaultNode)
  }, [defaultNode])

  const appLogLines = useAppLogs(appId, logNode)

  // Snapshot count for selected log node
  const { data: snapshots } = useSnapshots(
    app?.backup_enabled ? appId : null,
    logNode ?? null
  )

  if (!app) {
    return (
      <div className="min-h-screen bg-[#faf9f7] flex items-center justify-center">
        <span className="text-stone-400 text-sm">Loading...</span>
      </div>
    )
  }

  const onlineReport = allReports.find(nr => nr.node_online)

  return (
    <div className="min-h-screen bg-[#faf9f7] flex flex-col">
      {/* ── Page header ── */}
      <div className="border-b border-[#e2ddd7] bg-white px-4 py-3 flex-shrink-0">
        <div className="max-w-7xl mx-auto flex items-center gap-3">
          <button
            onClick={onBack}
            className="text-stone-400 hover:text-stone-700 text-[11px] flex items-center gap-1 transition-colors"
          >
            ← back
          </button>
          <span className="w-px h-4 bg-[#e2ddd7]" />
          <h1 className="text-sm font-semibold text-[#1c1917] font-mono">{appId}</h1>
          <span className={`text-xs font-medium ${
            app.observed_state === 'running' ? 'text-emerald-600' :
            app.observed_state === 'error' ? 'text-red-600' :
            app.observed_state === 'restarting' ? 'text-amber-600' : 'text-stone-400'
          }`}>{app.observed_state}</span>
          {app.node_reports.some(nr => nr.quarantined) && (
            <span className="text-[10px] bg-red-100 text-red-700 px-1.5 py-0.5 rounded font-medium">quarantined</span>
          )}

          <div className="ml-auto flex items-center gap-2">
            <DesiredToggle
              state={app.desired_state}
              onToggle={() => desiredMutation.mutate({ appId, state: app.desired_state === 'running' ? 'stopped' : 'running' })}
              disabled={desiredMutation.isPending}
            />
            {onlineReport && (
              <>
                <button
                  onClick={() => restartMutation.mutate({ appId, node: onlineReport.node })}
                  disabled={restartMutation.isPending}
                  className="px-2.5 py-1 text-[11px] border border-[#e2ddd7] text-stone-500 hover:border-stone-400 hover:text-stone-700 rounded disabled:opacity-50 transition-colors"
                >
                  Restart
                </button>
                {app.backup_enabled && (
                  <button
                    onClick={() => backupMutation.mutate({ appId, node: onlineReport.node })}
                    disabled={backupMutation.isPending}
                    className="px-2.5 py-1 text-[11px] border border-teal-200 text-teal-700 hover:border-teal-400 rounded disabled:opacity-50 transition-colors"
                  >
                    Backup now
                  </button>
                )}
              </>
            )}
          </div>
        </div>
      </div>

      {/* ── Content ── */}
      <div className="flex-1 max-w-7xl w-full mx-auto px-4 py-4 grid grid-cols-[300px_1fr] gap-4 min-h-0">

        {/* ── Left sidebar ── */}
        <div className="flex flex-col gap-3 overflow-y-auto min-h-0">

          {/* Node reports */}
          <Section title="Nodes">
            {allReports.length === 0 ? (
              <p className="text-[11px] text-stone-400">No node reports</p>
            ) : (
              <div className="space-y-2">
                {allReports.map(nr => (
                  <NodeReportCard
                    key={nr.node}
                    nr={nr}
                    selected={logNode === nr.node}
                    onSelect={() => setLogNode(nr.node)}
                  />
                ))}
              </div>
            )}
          </Section>

          {/* Config */}
          <Section title="Configuration">
            <dl className="grid grid-cols-2 gap-x-2 gap-y-1.5 text-[11px]">
              <dt className="text-stone-400">Global</dt>
              <dd className="text-stone-700">{app.is_global ? 'yes' : 'no'}</dd>
              <dt className="text-stone-400">Assignment</dt>
              <dd className="text-stone-700 truncate">{app.manually_assigned ? `manual: ${app.assigned_agent}` : (app.assigned_agent ?? '—')}</dd>
              <dt className="text-stone-400">Backup</dt>
              <dd className="text-stone-700">{app.backup_enabled ? `yes — every ${app.backup_interval}` : 'disabled'}</dd>
              {app.backup_enabled && (
                <>
                  <dt className="text-stone-400">Prune</dt>
                  <dd className="text-stone-700">every {app.prune_interval}</dd>
                  <dt className="text-stone-400">Restore on start</dt>
                  <dd className="text-stone-700">{app.restore_enabled ? 'yes' : 'no'}</dd>
                  <dt className="text-stone-400">Sensitive</dt>
                  <dd className="text-stone-700">{app.backup_sensitive ? 'yes' : 'no'}</dd>
                  {snapshots?.snapshots && (
                    <>
                      <dt className="text-stone-400">Snapshots</dt>
                      <dd className="text-stone-700">{snapshots.snapshots.length}</dd>
                    </>
                  )}
                </>
              )}
            </dl>
          </Section>

          {/* Backup status */}
          {app.backup_enabled && allReports.some(nr => nr.last_backup_at) && (
            <Section title="Backup status">
              <div className="space-y-1.5">
                {allReports.filter(nr => nr.last_backup_at || nr.last_backup_error).map(nr => (
                  <div key={nr.node} className="text-[11px]">
                    <span className="text-stone-400">{nr.node}: </span>
                    <span className={nr.last_backup_error ? 'text-red-600' : 'text-stone-600'}>
                      {nr.last_backup_error ?? timeAgo(nr.last_backup_at)}
                    </span>
                  </div>
                ))}
              </div>
            </Section>
          )}

          {/* Containers for this app */}
          {appContainers.length > 0 && (
            <Section title={`Containers (${appContainers.length})`}>
              <div className="space-y-1.5">
                {appContainers.map(c => (
                  <div key={`${c.node}-${c.id}`} className="text-[11px]">
                    <div className="flex items-center justify-between gap-1">
                      <span className="font-mono text-stone-700 truncate">{c.name}</span>
                      <ContainerStateBadge state={c.state} />
                    </div>
                    <div className="text-stone-400 truncate">{c.status}</div>
                    <div className="text-stone-300 truncate text-[10px]">{c.image}</div>
                  </div>
                ))}
              </div>
            </Section>
          )}
        </div>

        {/* ── Right: logs ── */}
        <div className="flex flex-col gap-3 min-h-0" style={{ height: 'calc(100vh - 140px)' }}>
          {/* Log node selector */}
          <div className="flex items-center justify-between flex-shrink-0">
            <span className="text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Container logs</span>
            {allReports.length > 1 && (
              <div className="flex items-center gap-1.5 text-[11px] text-stone-400">
                <span>node:</span>
                {allReports.map(nr => (
                  <button
                    key={nr.node}
                    onClick={() => setLogNode(nr.node)}
                    className={`px-2 py-0.5 rounded transition-colors ${logNode === nr.node ? 'bg-teal-100 text-teal-700' : 'text-stone-400 hover:text-stone-700 hover:bg-[#f0ede8]'}`}
                  >
                    {nr.node}
                    {!nr.node_online && <span className="text-stone-300"> (offline)</span>}
                  </button>
                ))}
              </div>
            )}
          </div>

          {/* Log panel */}
          <div className="flex-1 border border-[#e2ddd7] rounded-lg overflow-hidden min-h-0">
            <LogPanel lines={appLogLines} label={`${appId} — ${logNode ?? '—'}`} />
          </div>
        </div>
      </div>
    </div>
  )
}

// ── Sub-components ─────────────────────────────────────────────────────────

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="bg-white border border-[#e2ddd7] rounded-lg p-3">
      <div className="text-[10px] font-semibold text-stone-400 uppercase tracking-widest mb-2">{title}</div>
      {children}
    </div>
  )
}

function NodeReportCard({ nr, selected, onSelect }: { nr: NodeReport; selected: boolean; onSelect: () => void }) {
  return (
    <button
      onClick={onSelect}
      className={`w-full text-left rounded p-2 border transition-colors ${selected ? 'border-teal-300 bg-teal-50' : 'border-[#f0ede8] hover:border-[#e2ddd7] hover:bg-[#faf9f7]'}`}
    >
      <div className="flex items-center justify-between mb-1.5">
        <div className="flex items-center gap-1.5">
          <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${nr.node_online ? 'bg-emerald-500' : 'bg-stone-300'}`} />
          <span className="text-[11px] font-semibold text-[#1c1917]">{nr.node}</span>
        </div>
        <HealthBadge health={nr.health} />
      </div>
      <div className="grid grid-cols-2 gap-x-2 gap-y-0.5 text-[10px]">
        <span className="text-stone-400">State</span>
        <span className={`font-medium ${
          nr.observed_state === 'running' ? 'text-emerald-600' :
          nr.observed_state === 'error' ? 'text-red-600' : 'text-stone-500'
        }`}>{nr.observed_state}</span>
        {nr.phase && nr.phase !== 'empty' && (
          <>
            <span className="text-stone-400">Phase</span>
            <span className="col-span-1"><PhaseBadge phase={nr.phase} progress={nr.progress} /></span>
          </>
        )}
        {nr.restart_count > 0 && (
          <>
            <span className="text-stone-400">Restarts</span>
            <span className={`font-medium ${nr.restart_count >= 3 ? 'text-red-600' : 'text-amber-600'}`}>{nr.restart_count}</span>
          </>
        )}
        {nr.quarantined && (
          <>
            <span className="text-stone-400">Status</span>
            <span className="text-red-600 font-medium">quarantined</span>
          </>
        )}
        {nr.port > 0 && (
          <>
            <span className="text-stone-400">Port</span>
            <span className="text-stone-600">{nr.port}</span>
          </>
        )}
        {nr.last_error && (
          <div className="col-span-2 mt-1 text-red-500 break-words">{nr.last_error}</div>
        )}
      </div>
    </button>
  )
}
