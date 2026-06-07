import { useEffect, useMemo, useState } from 'react'
import type { FormEvent } from 'react'
import {
  useAddCrowdSecDecision,
  useCrowdSecDecisions,
  useCrowdSecHub,
  useCrowdSecLogs,
  useCrowdSecMetrics,
  useCrowdSecStatus,
  useCrowdSecVersion,
  useDeleteCrowdSecDecision,
  useInstallCrowdSecHubItem,
  useRemoveCrowdSecHubItem,
  useUpdateCrowdSec,
} from '../api/hooks'
import type { CrowdSecDecision, CrowdSecHubItem, CrowdSecLogEntry, CrowdSecMetrics, CrowdSecStatus } from '../api/types'

function formatDateTime(value?: string): string {
  if (!value) return '--'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function formatDuration(value?: string): string {
  if (!value) return '--'
  return value.replace(/m\d+s$/, 'm').replace(/h\d+m$/, 'h')
}

function countryFlag(code?: string): string {
  if (!code || code.length !== 2) return ''
  return String.fromCodePoint(...[...code.toUpperCase()].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65))
}

function countryName(code?: string): string {
  if (!code) return ''
  try {
    const names = new Intl.DisplayNames(['en'], { type: 'region' })
    return names.of(code.toUpperCase()) ?? code.toUpperCase()
  } catch {
    return code.toUpperCase()
  }
}

function levelClass(level?: string): string {
  switch ((level ?? '').toLowerCase()) {
    case 'error':
    case 'fatal':
      return 'text-red-600'
    case 'warning':
    case 'warn':
      return 'text-amber-600'
    case 'info':
      return 'text-teal-700'
    default:
      return 'text-stone-500'
  }
}

function decisionKey(decision: CrowdSecDecision): string {
  return `${decision.id ?? ''}-${decision.scope}-${decision.value}-${decision.type}`
}

function logKey(entry: CrowdSecLogEntry, index: number): string {
  return `${entry.timestamp ?? ''}-${entry.source}-${entry.level ?? ''}-${entry.message}-${index}`
}

function statusDot(ready: boolean, active: boolean, error: boolean): string {
  if (error) return 'bg-red-500'
  if (ready) return 'bg-emerald-500'
  if (active) return 'bg-amber-400'
  return 'bg-stone-300'
}

function originBadge(origin: string): { text: string; className: string } {
  switch (origin.toLowerCase()) {
    case 'cscli':
      return { text: 'Manual', className: 'bg-violet-100 text-violet-700' }
    case 'crowdsec':
      return { text: 'Engine', className: 'bg-teal-100 text-teal-700' }
    case 'capi':
      return { text: 'Community', className: 'bg-blue-100 text-blue-700' }
    default:
      return { text: origin, className: 'bg-stone-100 text-stone-600' }
  }
}

function StatusStep({
  label,
  ready,
  active,
  error,
}: {
  label: string
  ready: boolean
  active: boolean
  error: boolean
}) {
  return (
    <div className="flex items-center gap-2 min-w-0">
      <span className={`inline-block h-2 w-2 rounded-full ${statusDot(ready, active, error)}`} />
      <span className={ready ? 'text-stone-700' : active ? 'text-amber-700' : error ? 'text-red-600' : 'text-stone-400'}>
        {label}
      </span>
    </div>
  )
}

function CrowdSecStatusPanel({ status }: { status?: CrowdSecStatus }) {
  const state = status?.state ?? 'idle'
  const activeStep = status?.step ?? ''
  const error = state === 'error'
  const steps = [
    { key: 'config', label: 'Config', ready: !!status?.config_ready },
    { key: 'hub', label: 'Collections', ready: !!status?.hub_ready },
    { key: 'engine', label: 'Engine', ready: !!status?.engine_running },
    { key: 'bouncer', label: 'Bouncer', ready: !!status?.bouncer_ready },
    { key: 'caddy', label: 'Caddy', ready: !!status?.caddy_integrated },
  ]
  return (
    <section className="bg-white border border-[#e2ddd7] rounded-lg overflow-hidden">
      <div className="px-4 py-3 border-b border-[#e2ddd7] flex flex-col gap-1 sm:flex-row sm:items-center sm:justify-between">
        <h2 className="text-xs font-semibold text-[#1c1917] uppercase tracking-widest">Configuration</h2>
        <span className={`text-[11px] font-semibold uppercase tracking-widest ${error ? 'text-red-600' : state === 'ready' ? 'text-emerald-700' : 'text-stone-400'}`}>
          {state}
        </span>
      </div>
      <div className="px-4 py-3">
        <div className="grid grid-cols-2 sm:grid-cols-5 gap-3 text-[11px]">
          {steps.map((step) => (
            <StatusStep
              key={step.key}
              label={step.label}
              ready={step.ready}
              active={activeStep === step.key}
              error={error && activeStep === step.key}
            />
          ))}
        </div>
        <div className={`mt-3 text-[11px] ${error ? 'text-red-600' : 'text-stone-500'}`}>
          {status?.error || status?.message || 'Waiting for CrowdSec bootstrap status.'}
        </div>
      </div>
    </section>
  )
}

function CrowdSecVersionSection({
  version,
  updateCrowdSec,
}: {
  version?: { current: string; latest: string; update_available: boolean }
  updateCrowdSec: ReturnType<typeof useUpdateCrowdSec>
}) {
  const [updateError, setUpdateError] = useState<string | null>(null)

  if (!version?.current) return null

  function handleUpdate() {
    if (!window.confirm(`Update CrowdSec from v${version!.current} to v${version!.latest}? Engine will restart.`)) return
    setUpdateError(null)
    updateCrowdSec.mutateAsync().catch((err) =>
      setUpdateError(err instanceof Error ? err.message.trim() : 'Update failed'),
    )
  }

  return (
    <section className="bg-white border border-[#e2ddd7] rounded-lg overflow-hidden">
      <div className="px-4 py-3 border-b border-[#e2ddd7] flex items-center justify-between">
        <h2 className="text-xs font-semibold text-[#1c1917] uppercase tracking-widest">Version</h2>
        <span
          className={`text-[10px] font-semibold uppercase tracking-widest px-1.5 py-0.5 rounded ${
            version.update_available ? 'bg-amber-100 text-amber-700' : 'bg-emerald-100 text-emerald-700'
          }`}
        >
          {version.update_available ? 'Update available' : 'Up to date'}
        </span>
      </div>
      <div className="px-4 py-3">
        <div className="flex flex-wrap items-center gap-4">
          <div>
            <div className="text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Installed</div>
            <div className="mt-0.5 text-sm font-mono text-stone-800">v{version.current}</div>
          </div>
          {version.latest && (
            <div>
              <div className="text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Latest</div>
              <div className="mt-0.5 text-sm font-mono text-stone-500">v{version.latest}</div>
            </div>
          )}
          {version.update_available && (
            <button
              onClick={handleUpdate}
              disabled={updateCrowdSec.isPending}
              className="ml-auto px-3 py-1.5 text-[11px] bg-[#0d9488] text-white rounded hover:bg-[#0f766e] disabled:opacity-50"
            >
              {updateCrowdSec.isPending ? 'Updating...' : 'Update CrowdSec'}
            </button>
          )}
        </div>
        {updateError && <div className="mt-2 text-[11px] text-red-600">{updateError}</div>}
      </div>
    </section>
  )
}

function MetricsBar({ metrics }: { metrics?: CrowdSecMetrics }) {
  if (!metrics) return null
  const stats = [
    { label: 'Lines Read', value: metrics.acquisition_lines_read.toLocaleString() },
    { label: 'Lines Parsed', value: metrics.acquisition_lines_parsed.toLocaleString() },
    { label: 'Poured', value: metrics.acquisition_lines_pour.toLocaleString() },
    { label: 'Alerts', value: metrics.alerts_count.toLocaleString() },
    { label: 'Active Bans', value: metrics.active_bans.toLocaleString() },
    { label: 'Scenarios', value: metrics.active_scenarios.toLocaleString() },
    { label: 'Parser Hits', value: metrics.parser_hits_total.toLocaleString() },
    { label: 'Parser Parsed', value: metrics.parser_parsed_total.toLocaleString() },
  ]
  return (
    <section className="bg-white border border-[#e2ddd7] rounded-lg overflow-hidden">
      <div className="px-4 py-3 border-b border-[#e2ddd7]">
        <h2 className="text-xs font-semibold text-[#1c1917] uppercase tracking-widest">Metrics</h2>
      </div>
      <div className="px-4 py-3">
        <div className="grid grid-cols-2 sm:grid-cols-4 lg:grid-cols-8 gap-3">
          {stats.map((stat) => (
            <div key={stat.label}>
              <div className="text-[10px] font-semibold text-stone-400 uppercase tracking-widest">{stat.label}</div>
              <div className="mt-0.5 text-sm font-mono text-stone-800">{stat.value}</div>
            </div>
          ))}
        </div>
      </div>
    </section>
  )
}

function CountryCell({ code, asName }: { code?: string; asName?: string }) {
  if (!code) return <span className="text-stone-400">--</span>
  const flag = countryFlag(code)
  const name = countryName(code)
  return (
    <div>
      <span title={name}>
        {flag} <span className="text-stone-600">{code}</span>
      </span>
      {asName && <div className="text-[10px] text-stone-400 mt-0.5 truncate max-w-[160px]" title={asName}>{asName}</div>}
    </div>
  )
}

export function CrowdSecPage() {
  const { data: status } = useCrowdSecStatus()
  const { data: metrics } = useCrowdSecMetrics()
  const { data: decisions, isLoading: decisionsLoading, error: decisionsError } = useCrowdSecDecisions()
  const { data: logs, isLoading: logsLoading, error: logsError } = useCrowdSecLogs()
  const addDecision = useAddCrowdSecDecision()
  const deleteDecision = useDeleteCrowdSecDecision()
  const { data: hubData, isLoading: hubLoading } = useCrowdSecHub()
  const { data: version } = useCrowdSecVersion()
  const updateCrowdSec = useUpdateCrowdSec()
  const installHubItem = useInstallCrowdSecHubItem()
  const removeHubItem = useRemoveCrowdSecHubItem()
  const [ip, setIP] = useState('')
  const [duration, setDuration] = useState('4h')
  const [reason, setReason] = useState('manual ban from homectl')
  const [error, setError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [hubType, setHubType] = useState('collection')
  const [hubInstallName, setHubInstallName] = useState('')
  const [hubError, setHubError] = useState<string | null>(null)

  const hubItems = useMemo(() => hubData?.items ?? [], [hubData])
  const installedItems = useMemo(() => hubItems.filter((h) => h.installed), [hubItems])
  const installedByType = useMemo(() => {
    const groups: Record<string, CrowdSecHubItem[]> = {}
    for (const item of installedItems) {
      if (!groups[item.type]) groups[item.type] = []
      groups[item.type].push(item)
    }
    return groups
  }, [installedItems])

  const sortedDecisions = useMemo(() => [...(decisions ?? [])], [decisions])
  const logEntries = useMemo(() => logs ?? [], [logs])
  const activeBans = sortedDecisions.filter((decision) => decision.type === 'ban').length

  useEffect(() => {
    const err = decisionsError || logsError || addDecision.error || deleteDecision.error
    if (!err) return
    setError(err instanceof Error ? err.message.trim() : 'CrowdSec request failed')
  }, [addDecision.error, decisionsError, deleteDecision.error, logsError])

  function submitHubInstall(event: FormEvent) {
    event.preventDefault()
    if (!hubInstallName.trim()) return
    setHubError(null)
    installHubItem.mutateAsync({ type: hubType, name: hubInstallName.trim() })
      .then(() => setHubInstallName(''))
      .catch((err) => setHubError(err instanceof Error ? err.message.trim() : 'Install failed'))
  }

  function submitHubRemove(item: CrowdSecHubItem) {
    if (!window.confirm(`Remove ${item.name}?`)) return
    removeHubItem.mutate({ type: item.type, name: item.name })
  }

  function submitBan(event: FormEvent) {
    event.preventDefault()
    setError(null)
    addDecision.mutateAsync({ ip: ip.trim(), duration: duration.trim(), reason: reason.trim() })
      .then(() => setIP(''))
      .catch((err) => setError(err instanceof Error ? err.message.trim() : 'CrowdSec request failed'))
  }

  function removeBan(decision: CrowdSecDecision) {
    if (!decision.id || !window.confirm(`Delete CrowdSec ban for ${decision.value}?`)) return
    deleteDecision.mutate(decision.id)
  }

  function toggleExpand(decision: CrowdSecDecision) {
    const key = decisionKey(decision)
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  return (
    <div className="min-h-screen bg-[#faf9f7]">
      <div className="border-b border-[#e2ddd7] bg-white px-4 py-4">
        <div className="max-w-6xl mx-auto flex items-center justify-between">
          <div>
            <h1 className="text-sm font-semibold text-[#1c1917]">CrowdSec</h1>
            <p className="text-[11px] text-stone-400 mt-0.5">
              {decisionsLoading ? 'Loading...' : `${activeBans} active ban${activeBans === 1 ? '' : 's'}`}
            </p>
          </div>
        </div>
      </div>

      <main className="max-w-6xl mx-auto px-4 py-6 space-y-4">
        <CrowdSecStatusPanel status={status} />

        <CrowdSecVersionSection version={version} updateCrowdSec={updateCrowdSec} />

        <section className="bg-white border border-[#e2ddd7] rounded-lg overflow-hidden">
          <div className="px-4 py-3 border-b border-[#e2ddd7]">
            <h2 className="text-xs font-semibold text-[#1c1917] uppercase tracking-widest">Manual ban</h2>
          </div>
          <form onSubmit={submitBan} className="px-4 py-3 grid grid-cols-1 lg:grid-cols-[0.9fr_0.45fr_1.1fr_auto] gap-3 items-end">
            <label className="block">
              <span className="text-[10px] font-semibold text-stone-400 uppercase tracking-widest">IP address</span>
              <input
                value={ip}
                onChange={(event) => setIP(event.target.value)}
                placeholder="203.0.113.10"
                className="mt-1 w-full px-2 py-1.5 text-xs border border-[#e2ddd7] rounded bg-white text-[#1c1917] focus:outline-none focus:border-teal-500"
              />
            </label>
            <label className="block">
              <span className="text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Duration</span>
              <input
                value={duration}
                onChange={(event) => setDuration(event.target.value)}
                placeholder="4h"
                className="mt-1 w-full px-2 py-1.5 text-xs border border-[#e2ddd7] rounded bg-white text-[#1c1917] focus:outline-none focus:border-teal-500"
              />
            </label>
            <label className="block">
              <span className="text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Reason</span>
              <input
                value={reason}
                onChange={(event) => setReason(event.target.value)}
                className="mt-1 w-full px-2 py-1.5 text-xs border border-[#e2ddd7] rounded bg-white text-[#1c1917] focus:outline-none focus:border-teal-500"
              />
            </label>
            <button
              type="submit"
              disabled={addDecision.isPending || !ip.trim()}
              className="px-3 py-1.5 text-[11px] bg-[#0d9488] text-white rounded hover:bg-[#0f766e] disabled:opacity-50"
            >
              {addDecision.isPending ? 'Adding...' : 'Add ban'}
            </button>
          </form>
          {error && <div className="px-4 py-2 text-[11px] text-red-600 border-t border-red-100 bg-red-50">{error}</div>}
        </section>

        <section className="bg-white border border-[#e2ddd7] rounded-lg overflow-hidden">
          <div className="px-4 py-3 border-b border-[#e2ddd7]">
            <h2 className="text-xs font-semibold text-[#1c1917] uppercase tracking-widest">Current bans</h2>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-[#e2ddd7]">
                  <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest w-8"></th>
                  <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">IP</th>
                  <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Country</th>
                  <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Reason</th>
                  <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Origin</th>
                  <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Events</th>
                  <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Until</th>
                  <th className="px-3 py-2.5"></th>
                </tr>
              </thead>
              <tbody>
                {sortedDecisions.map((decision) => {
                  const isExpanded = expanded.has(decisionKey(decision))
                  const badge = originBadge(decision.origin || '')
                  return (
                    <>
                      <tr key={decisionKey(decision)} className="border-b border-[#f7f5f0] hover:bg-[#faf9f7]">
                        <td className="px-3 py-2.5">
                          <button
                            onClick={() => toggleExpand(decision)}
                            className={`text-[11px] transition-transform ${isExpanded ? 'rotate-90' : ''}`}
                            title={isExpanded ? 'Collapse' : 'Expand'}
                          >
                            &#9654;
                          </button>
                        </td>
                        <td className="px-3 py-2.5 font-mono text-[11px] text-stone-700">{decision.value}</td>
                        <td className="px-3 py-2.5">
                          <CountryCell code={decision.country} asName={decision.as_name} />
                        </td>
                        <td className="px-3 py-2.5">
                          <div className="text-stone-600 max-w-[200px] truncate" title={decision.reason || decision.scenario || ''}>
                            {decision.reason || decision.scenario || '--'}
                          </div>
                        </td>
                        <td className="px-3 py-2.5">
                          <span className={`inline-block px-1.5 py-0.5 rounded text-[10px] font-semibold ${badge.className}`}>
                            {badge.text}
                          </span>
                        </td>
                        <td className="px-3 py-2.5 text-stone-500">
                          {decision.event_count > 0 ? decision.event_count : '--'}
                        </td>
                        <td className="px-3 py-2.5 text-stone-500">
                          {formatDuration(decision.duration)}
                          {decision.until && (
                            <div className="text-[10px] text-stone-400">{formatDateTime(decision.until)}</div>
                          )}
                        </td>
                        <td className="px-3 py-2.5 text-right">
                          <button
                            onClick={() => removeBan(decision)}
                            disabled={deleteDecision.isPending || decision.scope.toLowerCase() !== 'ip'}
                            className="px-2 py-1 text-[11px] border border-red-200 text-red-600 hover:border-red-400 rounded disabled:opacity-50"
                          >
                            Delete
                          </button>
                        </td>
                      </tr>
                      {isExpanded && (
                        <tr key={`${decisionKey(decision)}-expanded`} className="bg-[#faf9f7] border-b border-[#e2ddd7]">
                          <td colSpan={8} className="px-6 py-3">
                            <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 text-[11px]">
                              <div>
                                <div className="font-semibold text-stone-400 uppercase text-[10px]">Scope</div>
                                <div className="text-stone-700">{decision.scope || '--'}</div>
                              </div>
                              <div>
                                <div className="font-semibold text-stone-400 uppercase text-[10px]">Type</div>
                                <div className="text-stone-700">{decision.type || '--'}</div>
                              </div>
                              <div>
                                <div className="font-semibold text-stone-400 uppercase text-[10px]">Source Range</div>
                                <div className="text-stone-700">{decision.source_range || '--'}</div>
                              </div>
                              <div>
                                <div className="font-semibold text-stone-400 uppercase text-[10px]">ASN</div>
                                <div className="text-stone-700">
                                  {decision.as_number ? `AS${decision.as_number}` : '--'}
                                  {decision.as_name && <span className="text-stone-400"> {decision.as_name}</span>}
                                </div>
                              </div>
                              <div>
                                <div className="font-semibold text-stone-400 uppercase text-[10px]">Start</div>
                                <div className="text-stone-700">{formatDateTime(decision.start_at)}</div>
                              </div>
                              <div>
                                <div className="font-semibold text-stone-400 uppercase text-[10px]">Stop</div>
                                <div className="text-stone-700">{formatDateTime(decision.stop_at)}</div>
                              </div>
                              <div>
                                <div className="font-semibold text-stone-400 uppercase text-[10px]">Scenario</div>
                                <div className="text-stone-700 max-w-[300px] truncate" title={decision.scenario}>
                                  {decision.scenario || '--'}
                                </div>
                              </div>
                              <div>
                                <div className="font-semibold text-stone-400 uppercase text-[10px]">Duration</div>
                                <div className="text-stone-700">{decision.duration || '--'}</div>
                              </div>
                            </div>
                          </td>
                        </tr>
                      )}
                    </>
                  )
                })}
                {!decisionsLoading && sortedDecisions.length === 0 && (
                  <tr>
                    <td colSpan={8} className="px-4 py-8 text-center text-[11px] text-stone-400">
                      No active CrowdSec bans.
                    </td>
                  </tr>
                )}
                {decisionsLoading && (
                  <tr>
                    <td colSpan={8} className="px-4 py-8 text-center text-[11px] text-stone-400">
                      Loading bans...
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </section>

        <MetricsBar metrics={metrics} />

        <section className="bg-white border border-[#e2ddd7] rounded-lg overflow-hidden">
          <div className="px-4 py-3 border-b border-[#e2ddd7] flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <h2 className="text-xs font-semibold text-[#1c1917] uppercase tracking-widest">Hub</h2>
            <div className="flex items-center gap-2">
              {(['collection', 'scenario', 'parser'] as const).map((t) => (
                <button
                  key={t}
                  onClick={() => setHubType(t)}
                  className={`px-2 py-1 text-[10px] font-semibold uppercase tracking-widest rounded ${hubType === t ? 'bg-teal-100 text-teal-700' : 'text-stone-400 hover:text-stone-600'}`}
                >
                  {t === 'collection' ? 'Collections' : t === 'scenario' ? 'Scenarios' : 'Parsers'}
                </button>
              ))}
            </div>
          </div>
          <div className="px-4 py-3">
            <div className="grid grid-cols-1 sm:grid-cols-[1fr_1fr_auto] gap-3 items-end text-xs text-stone-500">
              <div>
                {(installedByType[hubType] ?? []).length} installed
                {hubType === 'collection' ? ' / 6 installed' : ''}
              </div>
              <div></div>
              <div className="text-right">
                {hubLoading && <span className="text-stone-400">Loading...</span>}
              </div>
            </div>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-t border-[#e2ddd7]">
                  <th className="px-4 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Name</th>
                  <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Version</th>
                  <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Status</th>
                  <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Description</th>
                  <th className="px-3 py-2.5"></th>
                </tr>
              </thead>
              <tbody>
                {(installedByType[hubType] ?? []).map((item) => (
                  <tr key={`${item.type}-${item.name}`} className="border-b border-[#f7f5f0] hover:bg-[#faf9f7]">
                    <td className="px-4 py-2.5 font-mono text-[11px] text-stone-700">{item.name}</td>
                    <td className="px-3 py-2.5 text-stone-500">{item.version || '--'}</td>
                    <td className="px-3 py-2.5">
                      <span className={`inline-block px-1.5 py-0.5 rounded text-[10px] font-semibold ${item.update_available ? 'bg-amber-100 text-amber-700' : 'bg-emerald-100 text-emerald-700'}`}>
                        {item.update_available ? 'Update available' : 'Installed'}
                      </span>
                    </td>
                    <td className="px-3 py-2.5 text-stone-500 max-w-[300px] truncate" title={item.description}>
                      {item.description || '--'}
                    </td>
                    <td className="px-3 py-2.5 text-right">
                      <button
                        onClick={() => submitHubRemove(item)}
                        disabled={removeHubItem.isPending}
                        className="px-2 py-1 text-[11px] border border-red-200 text-red-600 hover:border-red-400 rounded disabled:opacity-50"
                      >
                        Remove
                      </button>
                    </td>
                  </tr>
                ))}
                {(installedByType[hubType] ?? []).length === 0 && !hubLoading && (
                  <tr>
                    <td colSpan={5} className="px-4 py-4 text-center text-[11px] text-stone-400">
                      No installed {hubType}s.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          <div className="px-4 py-3 border-t border-[#e2ddd7]">
            <form onSubmit={submitHubInstall} className="flex items-end gap-3">
              <label className="block flex-1">
                <span className="text-[10px] font-semibold text-stone-400 uppercase tracking-widest">
                  Install {hubType === 'collection' ? 'collection' : hubType === 'scenario' ? 'scenario' : 'parser'}
                </span>
                <input
                  value={hubInstallName}
                  onChange={(event) => setHubInstallName(event.target.value)}
                  placeholder={hubType === 'collection' ? 'crowdsecurity/nginx' : hubType === 'scenario' ? 'crowdsecurity/ssh-bf' : 'crowdsecurity/nginx-logs'}
                  className="mt-1 w-full px-2 py-1.5 text-xs border border-[#e2ddd7] rounded bg-white text-[#1c1917] focus:outline-none focus:border-teal-500 font-mono"
                />
              </label>
              <button
                type="submit"
                disabled={installHubItem.isPending || !hubInstallName.trim()}
                className="px-3 py-1.5 text-[11px] bg-[#0d9488] text-white rounded hover:bg-[#0f766e] disabled:opacity-50 whitespace-nowrap"
              >
                {installHubItem.isPending ? 'Installing...' : 'Install'}
              </button>
            </form>
            {hubError && <div className="mt-2 text-[11px] text-red-600">{hubError}</div>}
          </div>
        </section>

        <section className="bg-white border border-[#e2ddd7] rounded-lg overflow-hidden">
          <div className="px-4 py-3 border-b border-[#e2ddd7]">
            <h2 className="text-xs font-semibold text-[#1c1917] uppercase tracking-widest">CrowdSec logs</h2>
          </div>
          <div className="max-h-[28rem] overflow-auto log-scroll">
            <table className="w-full text-xs">
              <thead className="sticky top-0 z-10 bg-white">
                <tr className="border-b border-[#e2ddd7]">
                  <th className="px-4 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Time</th>
                  <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Source</th>
                  <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Level</th>
                  <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Message</th>
                </tr>
              </thead>
              <tbody>
                {logEntries.map((entry, index) => (
                  <tr key={logKey(entry, index)} className="border-b border-[#f7f5f0] hover:bg-[#faf9f7]">
                    <td className="px-4 py-2.5 whitespace-nowrap text-stone-500">{formatDateTime(entry.timestamp)}</td>
                    <td className="px-3 py-2.5 font-mono text-[11px] text-stone-600">{entry.source}</td>
                    <td className={`px-3 py-2.5 font-semibold ${levelClass(entry.level)}`}>{entry.level || '--'}</td>
                    <td className="px-3 py-2.5 text-stone-600">
                      <div className="max-w-4xl truncate" title={entry.raw}>{entry.message || entry.raw}</div>
                    </td>
                  </tr>
                ))}
                {!logsLoading && logEntries.length === 0 && (
                  <tr>
                    <td colSpan={4} className="px-4 py-8 text-center text-[11px] text-stone-400">
                      No CrowdSec logs found.
                    </td>
                  </tr>
                )}
                {logsLoading && (
                  <tr>
                    <td colSpan={4} className="px-4 py-8 text-center text-[11px] text-stone-400">
                      Loading CrowdSec logs...
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </section>
      </main>
    </div>
  )
}
