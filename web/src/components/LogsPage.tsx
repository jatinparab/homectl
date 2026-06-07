import { useMemo, useState } from 'react'
import type { LogFilterParams, PublicRouteAccessLogEntry, SSHLogEntry } from '../api/types'
import { usePublicRouteLogs, useSSHLogs } from '../api/hooks'
import { LogFilterBar } from './LogFilterBar'
import { LogTable, type Column } from './LogTable'

// ─── helpers ──────────────────────────────────────────────────────────────────

function formatBytes(bytes?: number): string {
  if (!bytes || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
}

function formatDateTime(value?: string): string {
  if (!value) return '--'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return '--'
  return d.toLocaleString()
}

function formatShortDateTime(value?: string): string {
  if (!value) return '--'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return '--'
  return d.toLocaleDateString() + ' ' + d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function formatDuration(seconds?: number): string {
  if (!seconds || seconds <= 0) return '0 ms'
  if (seconds < 1) return `${Math.round(seconds * 1000)} ms`
  return `${seconds.toFixed(2)} s`
}

function statusClass(status: number): string {
  if (status >= 500) return 'text-red-600'
  if (status >= 400) return 'text-amber-600'
  if (status >= 300) return 'text-blue-600'
  if (status >= 200) return 'text-emerald-700'
  return 'text-stone-500'
}

function sshStatusBadge(status: string): string {
  switch (status) {
    case 'accepted': return 'bg-emerald-100 text-emerald-700'
    case 'failed': return 'bg-red-100 text-red-600'
    case 'error': return 'bg-amber-100 text-amber-700'
    case 'disconnected': return 'bg-stone-100 text-stone-500'
    default: return 'bg-stone-100 text-stone-400'
  }
}

function requestLogEntryKey(entry: PublicRouteAccessLogEntry): string {
  return [
    entry.timestamp,
    entry.host,
    entry.client_ip,
    entry.method,
    entry.uri,
    entry.status,
    entry.size,
  ].join('|')
}

function sshLogEntryKey(entry: SSHLogEntry, index: number): string {
  return `${entry.timestamp}-${entry.client_ip}-${entry.user}-${entry.status}-${index}`
}

function uniqueSorted(values: string[]): string[] {
  return Array.from(new Set(values.filter(Boolean))).sort((a, b) => a.localeCompare(b))
}

// ─── Intelligent exclude ─────────────────────────────────────────────────────

function isIntelligentlyExcluded(
  entry: PublicRouteAccessLogEntry,
  filters: { uptimePush: boolean; uptimeKumaAgent: boolean },
): boolean {
  if (filters.uptimePush && entry.host.startsWith('uptime.') && entry.uri.startsWith('/api/push')) {
    return true
  }
  if (filters.uptimeKumaAgent && entry.user_agent?.startsWith('Uptime-Kuma')) {
    return true
  }
  return false
}

// ─── Request Logs columns ────────────────────────────────────────────────────

const requestLogColumns: Column<PublicRouteAccessLogEntry>[] = [
  {
    header: 'Time',
    accessor: (e) => e.timestamp,
    render: (e) => <span className="whitespace-nowrap text-stone-500">{formatDateTime(e.timestamp)}</span>,
    width: 'auto',
  },
  {
    header: 'Host',
    accessor: (e) => e.host,
    render: (e) => <span className="font-mono text-[11px] text-stone-700 whitespace-nowrap">{e.host}</span>,
    width: 'auto',
  },
  {
    header: 'IP',
    accessor: (e) => e.client_ip,
    render: (e) => <span className="font-mono text-[11px] text-stone-600 whitespace-nowrap">{e.client_ip || '--'}</span>,
    width: 'auto',
  },
  {
    header: 'Request',
    accessor: (e) => `${e.method} ${e.uri}`,
    render: (e) => (
      <div>
        <div className="flex items-center gap-2">
          <span className="font-semibold text-stone-600">{e.method || '--'}</span>
          <span className="font-mono text-[11px] text-stone-600 truncate max-w-[12rem]" title={e.uri || ''}>{e.uri || '/'}</span>
        </div>
        <div className="text-[10px] text-stone-400 mt-0.5">{e.protocol || 'HTTP'} · {formatDuration(e.duration)}</div>
      </div>
    ),
    width: 'auto',
  },
  {
    header: 'Status',
    accessor: (e) => String(e.status),
    render: (e) => (
      <div>
        <div className={`font-semibold ${statusClass(e.status)}`}>{e.status || '--'}</div>
        <div className="text-[11px] text-stone-400">{formatBytes(e.size)} out</div>
      </div>
    ),
    width: 'auto',
  },
  {
    header: 'Agent',
    accessor: (e) => e.user_agent || '',
    render: (e) => (
      <div className="truncate text-stone-500 max-w-xs" title={e.user_agent || ''}>{e.user_agent || '--'}</div>
    ),
    width: 'auto',
  },
]

// ─── SSH Logs columns ────────────────────────────────────────────────────────

const sshLogColumns: Column<SSHLogEntry>[] = [
  {
    header: 'Time',
    accessor: (e) => e.timestamp,
    render: (e) => <span className="whitespace-nowrap text-stone-500">{formatShortDateTime(e.timestamp)}</span>,
    width: 'auto',
  },
  {
    header: 'IP',
    accessor: (e) => e.client_ip,
    render: (e) => <span className="font-mono text-[11px] text-stone-600 whitespace-nowrap">{e.client_ip || '--'}</span>,
    width: 'auto',
  },
  {
    header: 'User',
    accessor: (e) => e.user,
    render: (e) => <span className="font-mono text-[11px] text-stone-700">{e.user || '--'}</span>,
    width: 'auto',
  },
  {
    header: 'Status',
    accessor: (e) => e.status,
    render: (e) => (
      <span className={`inline-block px-1.5 py-0.5 rounded text-[10px] font-semibold ${sshStatusBadge(e.status)}`}>
        {e.status}
      </span>
    ),
    width: 'auto',
  },
  {
    header: 'Method',
    accessor: (e) => e.auth_method || '',
    render: (e) => <span className="text-stone-500">{e.auth_method || '--'}</span>,
    width: 'auto',
  },
  {
    header: 'Port',
    accessor: (e) => e.port,
    render: (e) => <span className="font-mono text-[11px] text-stone-500">{e.port || '--'}</span>,
    width: 'auto',
  },
  {
    header: 'Message',
    accessor: (e) => e.message,
    render: (e) => (
      <div className="truncate text-stone-600 max-w-xs" title={e.message}>{e.message}</div>
    ),
    width: 'auto',
  },
]

const emptyFilter: LogFilterParams = {
  host: undefined,
  ip: undefined,
  uri: undefined,
  status: undefined,
  search: undefined,
  limit: 500,
}

// ─── Page ────────────────────────────────────────────────────────────────────

export function LogsPage() {
  // Request logs state
  const [requestDraft, setRequestDraft] = useState<LogFilterParams>({ ...emptyFilter })
  const [requestFilter, setRequestFilter] = useState<LogFilterParams>({ ...emptyFilter })
  const [intelligentFiltering, setIntelligentFiltering] = useState(true)
  const [excludeFilters, setExcludeFilters] = useState({ uptimePush: true, uptimeKumaAgent: true })

  // SSH logs state
  const [sshDraft, setSSHDraft] = useState<LogFilterParams>({ ...emptyFilter })
  const [sshFilter, setSSHFilter] = useState<LogFilterParams>({ ...emptyFilter })

  // Fetch data
  const { data: rawRequestLogs, isLoading: requestLoading } = usePublicRouteLogs(requestFilter)
  const { data: rawSSHLogs, isLoading: sshLoading } = useSSHLogs(sshFilter)

  const allRequestLogs = useMemo(() => rawRequestLogs ?? [], [rawRequestLogs])
  const allSSHLogs = useMemo(() => rawSSHLogs ?? [], [rawSSHLogs])

  // Apply intelligent excludes to request logs
  const filteredRequestLogs = useMemo(() => {
    if (!intelligentFiltering) return allRequestLogs
    return allRequestLogs.filter((entry) => !isIntelligentlyExcluded(entry, excludeFilters))
  }, [allRequestLogs, intelligentFiltering, excludeFilters])

  // Derive filter options from filtered data
  const hostOptions = useMemo(
    () => uniqueSorted(filteredRequestLogs.map((e) => e.host)),
    [filteredRequestLogs],
  )
  const requestIPOptions = useMemo(
    () => uniqueSorted(filteredRequestLogs.map((e) => e.client_ip)),
    [filteredRequestLogs],
  )
  const sshIPOptions = useMemo(
    () => uniqueSorted(allSSHLogs.map((e) => e.client_ip)),
    [allSSHLogs],
  )
  const statusOptions = useMemo(
    () => uniqueSorted(filteredRequestLogs.map((e) => String(e.status))),
    [filteredRequestLogs],
  )
  const sshStatusOptions = useMemo(
    () => uniqueSorted(allSSHLogs.map((e) => e.status)),
    [allSSHLogs],
  )

  return (
    <div className="min-h-screen bg-[#faf9f7]">
      <div className="border-b border-[#e2ddd7] bg-white px-4 py-4">
        <div className="max-w-6xl mx-auto flex items-center justify-between">
          <div>
            <h1 className="text-sm font-semibold text-[#1c1917]">Logs</h1>
            <p className="text-[11px] text-stone-400 mt-0.5">
              Request logs and SSH authentication logs
            </p>
          </div>
        </div>
      </div>

      <main className="max-w-6xl mx-auto px-4 py-6 space-y-4">
        {/* ── Request Logs ──────────────────────────────────────────────── */}
        <section className="bg-white border border-[#e2ddd7] rounded-lg overflow-hidden">
          <div className="px-4 py-3 border-b border-[#e2ddd7] flex flex-col gap-2">
            <div className="flex items-center justify-between">
              <h2 className="text-xs font-semibold text-[#1c1917] uppercase tracking-widest">Request logs</h2>
              <span className="text-[11px] text-stone-400">
                {requestLoading ? 'Loading...' : `${filteredRequestLogs.length} entries`}
              </span>
            </div>
            <LogFilterBar
              filters={requestDraft}
              onChange={setRequestDraft}
              onApply={setRequestFilter}
              onClear={() => { setRequestDraft({ ...emptyFilter }); setRequestFilter({ ...emptyFilter }) }}
              hostOptions={hostOptions}
              ipOptions={requestIPOptions}
              statusOptions={statusOptions}
              showHost
              showURI
              showStatus
              showSearch
              extraControls={
                <>
                  <button
                    type="button"
                    onClick={() => setIntelligentFiltering((v) => !v)}
                    className={`h-7 px-2.5 text-[11px] border rounded flex items-center gap-1.5 ${
                      intelligentFiltering
                        ? 'border-teal-200 bg-teal-50 text-teal-700'
                        : 'border-[#e2ddd7] bg-white text-stone-500'
                    }`}
                  >
                    Intelligent filtering
                  </button>
                  <button
                    type="button"
                    onClick={() => setExcludeFilters((v) => ({ ...v, uptimePush: !v.uptimePush }))}
                    disabled={!intelligentFiltering}
                    className={`h-7 px-2.5 text-[11px] border rounded transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
                      intelligentFiltering && excludeFilters.uptimePush
                        ? 'border-[#e2ddd7] bg-[#f7f5f0] text-stone-700'
                        : 'border-[#e2ddd7] bg-white text-stone-400'
                    }`}
                  >
                    uptime. /api/push
                  </button>
                  <button
                    type="button"
                    onClick={() => setExcludeFilters((v) => ({ ...v, uptimeKumaAgent: !v.uptimeKumaAgent }))}
                    disabled={!intelligentFiltering}
                    className={`h-7 px-2.5 text-[11px] border rounded transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
                      intelligentFiltering && excludeFilters.uptimeKumaAgent
                        ? 'border-[#e2ddd7] bg-[#f7f5f0] text-stone-700'
                        : 'border-[#e2ddd7] bg-white text-stone-400'
                    }`}
                  >
                    Uptime-Kuma agent
                  </button>
                </>
              }
            />
          </div>
          <LogTable
            columns={requestLogColumns}
            entries={filteredRequestLogs}
            keyFn={requestLogEntryKey}
            isLoading={requestLoading}
            emptyMessage="No request log entries found."
            loadingMessage="Loading request logs..."
          />
        </section>

        {/* ── SSH Logs ──────────────────────────────────────────────────── */}
        <section className="bg-white border border-[#e2ddd7] rounded-lg overflow-hidden">
          <div className="px-4 py-3 border-b border-[#e2ddd7] flex flex-col gap-2">
            <div className="flex items-center justify-between">
              <h2 className="text-xs font-semibold text-[#1c1917] uppercase tracking-widest">SSH logs</h2>
              <span className="text-[11px] text-stone-400">
                {sshLoading ? 'Loading...' : `${allSSHLogs.length} entries`}
              </span>
            </div>
            <LogFilterBar
              filters={sshDraft}
              onChange={setSSHDraft}
              onApply={setSSHFilter}
              onClear={() => { setSSHDraft({ ...emptyFilter }); setSSHFilter({ ...emptyFilter }) }}
              ipOptions={sshIPOptions}
              statusOptions={sshStatusOptions}
              showHost={false}
              showURI={false}
              showStatus
              showSearch
            />
          </div>
          <LogTable
            columns={sshLogColumns}
            entries={allSSHLogs}
            keyFn={sshLogEntryKey}
            isLoading={sshLoading}
            emptyMessage="No SSH log entries found."
            loadingMessage="Loading SSH logs..."
          />
        </section>
      </main>
    </div>
  )
}
