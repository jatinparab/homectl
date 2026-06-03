import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import {
  useCreatePublicRoute,
  useDeletePublicRoute,
  usePublicRouteLogs,
  usePublicRoutes,
  useUpdatePublicRoute,
} from '../api/hooks'
import type { PublicRoute, PublicRouteAccessLogEntry, PublicRouteMode } from '../api/types'
import { DesiredToggle } from './StatusBadge'

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

function formatTime(value?: string): string {
  if (!value) return '--'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return '--'
  return d.toLocaleTimeString()
}

function formatDateTime(value?: string): string {
  if (!value) return '--'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return '--'
  return d.toLocaleString()
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

function logEntryKey(entry: PublicRouteAccessLogEntry): string {
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

type LogFilters = {
  host: string
  ip: string
  uri: string
  status: string
}

type IntelligentExcludeFilters = {
  uptimePush: boolean
  uptimeKumaAgent: boolean
}

function applyLogFilters(entries: PublicRouteAccessLogEntry[], filters: LogFilters, omit?: keyof LogFilters) {
  return entries.filter((entry) => {
    if (omit !== 'host' && filters.host && entry.host !== filters.host) return false
    if (omit !== 'ip' && filters.ip && !entry.client_ip.includes(filters.ip)) return false
    if (omit !== 'uri' && filters.uri && !entry.uri.toLowerCase().includes(filters.uri.toLowerCase())) return false
    if (omit !== 'status' && filters.status && String(entry.status) !== filters.status) return false
    return true
  })
}

function uniqueSorted(values: string[]): string[] {
  return Array.from(new Set(values.filter(Boolean))).sort((a, b) => a.localeCompare(b))
}

function isIntelligentlyExcluded(entry: PublicRouteAccessLogEntry, filters: IntelligentExcludeFilters): boolean {
  if (filters.uptimePush && entry.host.startsWith('uptime.') && entry.uri.startsWith('/api/push')) {
    return true
  }
  if (filters.uptimeKumaAgent && entry.user_agent?.startsWith('Uptime-Kuma')) {
    return true
  }
  return false
}

const emptyForm = {
  id: '',
  host: '',
  mode: 'homelab' as PublicRouteMode,
  custom_target: '',
  enabled: true,
}

function FilterSelect({
  label,
  value,
  options,
  allLabel,
  onChange,
}: {
  label: string
  value: string
  options: string[]
  allLabel: string
  onChange: (value: string) => void
}) {
  const visibleOptions = value && !options.includes(value) ? [value, ...options] : options
  return (
    <label className="block">
      <span className="block text-[10px] font-semibold text-stone-400 uppercase tracking-widest">{label}</span>
      <select
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="mt-1 w-full px-1.5 py-1 text-[11px] normal-case tracking-normal font-normal border border-[#e2ddd7] rounded bg-white text-stone-600 focus:outline-none focus:border-teal-500"
      >
        <option value="">{allLabel}</option>
        {visibleOptions.map((option) => (
          <option key={option} value={option}>
            {option}
          </option>
        ))}
      </select>
    </label>
  )
}

function FilterTextInput({
  label,
  value,
  placeholder,
  onChange,
}: {
  label: string
  value: string
  placeholder: string
  onChange: (value: string) => void
}) {
  return (
    <label className="block">
      <span className="block text-[10px] font-semibold text-stone-400 uppercase tracking-widest">{label}</span>
      <input
        value={value}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
        className="mt-1 w-full px-1.5 py-1 text-[11px] normal-case tracking-normal font-normal border border-[#e2ddd7] rounded bg-white text-stone-600 placeholder:text-stone-300 focus:outline-none focus:border-teal-500"
      />
    </label>
  )
}

function FilterSearchSelect({
  label,
  value,
  options,
  placeholder,
  listId,
  onChange,
}: {
  label: string
  value: string
  options: string[]
  placeholder: string
  listId: string
  onChange: (value: string) => void
}) {
  return (
    <label className="block">
      <span className="block text-[10px] font-semibold text-stone-400 uppercase tracking-widest">{label}</span>
      <input
        list={listId}
        value={value}
        placeholder={placeholder}
        onChange={(event) => onChange(event.target.value)}
        className="mt-1 w-full px-1.5 py-1 text-[11px] normal-case tracking-normal font-normal border border-[#e2ddd7] rounded bg-white text-stone-600 placeholder:text-stone-300 focus:outline-none focus:border-teal-500"
      />
      <datalist id={listId}>
        {options.map((option) => (
          <option key={option} value={option} />
        ))}
      </datalist>
    </label>
  )
}

function ExcludeToggle({
  label,
  active,
  disabled,
  onToggle,
}: {
  label: string
  active: boolean
  disabled: boolean
  onToggle: () => void
}) {
  return (
    <button
      type="button"
      onClick={onToggle}
      disabled={disabled}
      className={`h-7 px-2.5 text-[11px] border rounded transition-colors disabled:opacity-40 disabled:cursor-not-allowed ${
        active
          ? 'border-[#e2ddd7] bg-[#f7f5f0] text-stone-700'
          : 'border-[#e2ddd7] bg-white text-stone-400'
      }`}
    >
      {label}
    </button>
  )
}

export function RoutingPage() {
  const { data: routes, isLoading } = usePublicRoutes()
  const createRoute = useCreatePublicRoute()
  const updateRoute = useUpdatePublicRoute()
  const deleteRoute = useDeletePublicRoute()
  const [form, setForm] = useState(emptyForm)
  const [error, setError] = useState<string | null>(null)
  const [logFilters, setLogFilters] = useState<LogFilters>({ host: '', ip: '', uri: '', status: '' })
  const [intelligentFiltering, setIntelligentFiltering] = useState(true)
  const [excludeFilters, setExcludeFilters] = useState<IntelligentExcludeFilters>({
    uptimePush: true,
    uptimeKumaAgent: true,
  })
  const { data: logs, isLoading: logsLoading } = usePublicRouteLogs()
  const logEntries = useMemo(() => logs ?? [], [logs])
  const intelligentLogEntries = useMemo(() => {
    if (!intelligentFiltering) return logEntries
    return logEntries.filter((entry) => !isIntelligentlyExcluded(entry, excludeFilters))
  }, [excludeFilters, intelligentFiltering, logEntries])
  const filteredLogEntries = useMemo(() => applyLogFilters(intelligentLogEntries, logFilters), [intelligentLogEntries, logFilters])
  const hostOptions = useMemo(
    () => uniqueSorted(applyLogFilters(intelligentLogEntries, logFilters, 'host').map((entry) => entry.host)),
    [intelligentLogEntries, logFilters],
  )
  const ipOptions = useMemo(
    () => uniqueSorted(applyLogFilters(intelligentLogEntries, logFilters, 'ip').map((entry) => entry.client_ip)),
    [intelligentLogEntries, logFilters],
  )
  const statusOptions = useMemo(
    () => uniqueSorted(applyLogFilters(intelligentLogEntries, logFilters, 'status').map((entry) => String(entry.status))),
    [intelligentLogEntries, logFilters],
  )
  const logScrollRef = useRef<HTMLDivElement | null>(null)
  const logAnchorRef = useRef<{ key: string; offset: number } | null>(null)
  const logFollowingRef = useRef(true)

  const sortedRoutes = useMemo(() => {
    return [...(routes ?? [])].sort((a, b) => a.host.localeCompare(b.host))
  }, [routes])

  useEffect(() => {
    if (!createRoute.error && !updateRoute.error && !deleteRoute.error) return
    const err = createRoute.error || updateRoute.error || deleteRoute.error
    setError(err instanceof Error ? err.message.trim() : 'Route update failed')
  }, [createRoute.error, updateRoute.error, deleteRoute.error])

  const saving = createRoute.isPending || updateRoute.isPending

  function resetForm() {
    setForm(emptyForm)
    setError(null)
  }

  function editRoute(route: PublicRoute) {
    setForm({
      id: route.id,
      host: route.host,
      mode: route.mode,
      custom_target: route.custom_target ?? '',
      enabled: route.enabled,
    })
    setError(null)
  }

  function submitRoute(event: FormEvent) {
    event.preventDefault()
    setError(null)
    const payload = {
      host: form.host.trim(),
      mode: form.mode,
      custom_target: form.mode === 'custom' ? form.custom_target.trim() : '',
      enabled: form.enabled,
    }
    const mutation = form.id
      ? updateRoute.mutateAsync({ id: form.id, route: payload })
      : createRoute.mutateAsync(payload)
    mutation.then(resetForm).catch((err) => {
      setError(err instanceof Error ? err.message.trim() : 'Route update failed')
    })
  }

  function toggleRoute(route: PublicRoute) {
    updateRoute.mutate({
      id: route.id,
      route: {
        host: route.host,
        mode: route.mode,
        custom_target: route.custom_target,
        enabled: !route.enabled,
      },
    })
  }

  function removeRoute(route: PublicRoute) {
    if (!window.confirm(`Delete public route ${route.host}?`)) return
    deleteRoute.mutate(route.id)
  }

  function captureLogScrollAnchor() {
    const container = logScrollRef.current
    if (!container) return
    logFollowingRef.current = container.scrollTop <= 4
    if (logFollowingRef.current) {
      logAnchorRef.current = null
      return
    }

    const rows = Array.from(container.querySelectorAll<HTMLElement>('[data-log-row="true"]'))
    const firstVisible = rows.find((row) => row.offsetTop + row.offsetHeight > container.scrollTop)
    if (!firstVisible) return
    logAnchorRef.current = {
      key: firstVisible.dataset.logKey ?? '',
      offset: firstVisible.offsetTop - container.scrollTop,
    }
  }

  useLayoutEffect(() => {
    const container = logScrollRef.current
    if (!container) return

    if (logFollowingRef.current) {
      container.scrollTop = 0
      return
    }

    const anchor = logAnchorRef.current
    if (!anchor?.key) return
    const row = container.querySelector<HTMLElement>(`[data-log-key="${CSS.escape(anchor.key)}"]`)
    if (!row) return
    container.scrollTop = row.offsetTop - anchor.offset
  }, [filteredLogEntries])

  return (
    <div className="min-h-screen bg-[#faf9f7]">
      <div className="border-b border-[#e2ddd7] bg-white px-4 py-4">
        <div className="max-w-6xl mx-auto flex items-center justify-between">
          <div>
            <h1 className="text-sm font-semibold text-[#1c1917]">Routing</h1>
            <p className="text-[11px] text-stone-400 mt-0.5">
              {isLoading ? 'Loading...' : `${sortedRoutes.length} public route${sortedRoutes.length !== 1 ? 's' : ''}`}
            </p>
          </div>
        </div>
      </div>

      <main className="max-w-6xl mx-auto px-4 py-6 space-y-4">
        <section className="bg-white border border-[#e2ddd7] rounded-lg overflow-hidden">
          <div className="px-4 py-3 border-b border-[#e2ddd7] flex items-center justify-between">
            <div>
              <h2 className="text-xs font-semibold text-[#1c1917] uppercase tracking-widest">Public routes</h2>
              <p className="text-[11px] text-stone-400 mt-1">Manager Caddy forwards homelab routes to the elected Caddy gateway.</p>
            </div>
          </div>

          <form onSubmit={submitRoute} className="px-4 py-3 border-b border-[#f0ede8] grid grid-cols-1 lg:grid-cols-[1.1fr_0.8fr_1.1fr_auto] gap-3 items-end">
            <label className="block">
              <span className="text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Host</span>
              <input
                value={form.host}
                onChange={(e) => setForm((v) => ({ ...v, host: e.target.value }))}
                placeholder="photos.your-domain.example"
                className="mt-1 w-full px-2 py-1.5 text-xs border border-[#e2ddd7] rounded bg-white text-[#1c1917] focus:outline-none focus:border-teal-500"
              />
            </label>
            <label className="block">
              <span className="text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Destination</span>
              <select
                value={form.mode}
                onChange={(e) => setForm((v) => ({ ...v, mode: e.target.value as PublicRouteMode }))}
                className="mt-1 w-full px-2 py-1.5 text-xs border border-[#e2ddd7] rounded bg-white text-[#1c1917] focus:outline-none focus:border-teal-500"
              >
                <option value="homelab">Homelab gateway</option>
                <option value="custom">Custom URL</option>
              </select>
            </label>
            <label className="block">
              <span className="text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Custom URL</span>
              <input
                value={form.custom_target}
                onChange={(e) => setForm((v) => ({ ...v, custom_target: e.target.value }))}
                placeholder="http://homectl-localhost:3001"
                disabled={form.mode !== 'custom'}
                className="mt-1 w-full px-2 py-1.5 text-xs border border-[#e2ddd7] rounded bg-white text-[#1c1917] disabled:bg-stone-50 disabled:text-stone-300 focus:outline-none focus:border-teal-500"
              />
              {form.mode === 'custom' && (
                <p className="mt-1 text-[10px] text-stone-400">
                  For VPS-local services, use <span className="font-mono">homectl-localhost</span> instead of <span className="font-mono">localhost</span>.
                </p>
              )}
            </label>
            <div className="flex items-center gap-2">
              <DesiredToggle
                state={form.enabled ? 'running' : 'stopped'}
                onToggle={() => setForm((v) => ({ ...v, enabled: !v.enabled }))}
                disabled={saving}
              />
              <button
                type="submit"
                disabled={saving || !form.host.trim()}
                className="px-3 py-1.5 text-[11px] bg-[#0d9488] text-white rounded hover:bg-[#0f766e] disabled:opacity-50"
              >
                {saving ? 'Saving...' : form.id ? 'Update' : 'Add'}
              </button>
              {form.id && (
                <button
                  type="button"
                  onClick={resetForm}
                  className="px-2 py-1.5 text-[11px] border border-[#e2ddd7] text-stone-500 hover:border-stone-400 rounded"
                >
                  Cancel
                </button>
              )}
            </div>
          </form>

          {error && <div className="px-4 py-2 text-[11px] text-red-600 border-b border-red-100 bg-red-50">{error}</div>}

          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-[#e2ddd7]">
                  <th className="px-4 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Route</th>
                  <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Status</th>
                  <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Target</th>
                  <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Traffic</th>
                  <th className="px-3 py-2.5"></th>
                </tr>
              </thead>
              <tbody>
                {sortedRoutes.map((route) => (
                  <tr key={route.id} className="border-b border-[#f7f5f0] hover:bg-[#faf9f7]">
                    <td className="px-4 py-2.5">
                      <div className="font-semibold text-[#1c1917]">{route.host}</div>
                      <div className="text-[11px] text-stone-400">{route.mode === 'homelab' ? 'homelab gateway' : 'custom route'}</div>
                    </td>
                    <td className="px-3 py-2.5">
                      <div className="flex items-center gap-2">
                        <span className={`inline-block w-2 h-2 rounded-full ${route.health.working ? 'bg-emerald-500 health-glow' : route.enabled ? 'bg-red-500' : 'bg-stone-300'}`} />
                        <span className={route.health.working ? 'text-emerald-700' : route.enabled ? 'text-red-600' : 'text-stone-400'}>
                          {route.enabled ? route.health.status || 'unknown' : 'disabled'}
                        </span>
                      </div>
                      <div className="text-[10px] text-stone-400 mt-0.5">
                        {route.health.http_status ? `HTTP ${route.health.http_status}` : route.health.last_error || 'Last check'} · {formatTime(route.health.checked_at)}
                      </div>
                    </td>
                    <td className="px-3 py-2.5 max-w-xs">
                      <div className="font-mono text-[11px] text-stone-600 truncate">
                        {route.health.resolved_to || route.custom_target || 'gateway pending'}
                      </div>
                    </td>
                    <td className="px-3 py-2.5">
                      <div className="text-stone-600">{route.health.request_count} req</div>
                      <div className="text-[11px] text-stone-400">{formatBytes(route.health.response_bytes)} out</div>
                    </td>
                    <td className="px-3 py-2.5">
                      <div className="flex items-center justify-end gap-2">
                        <DesiredToggle
                          state={route.enabled ? 'running' : 'stopped'}
                          onToggle={() => toggleRoute(route)}
                          disabled={updateRoute.isPending}
                        />
                        <button
                          onClick={() => editRoute(route)}
                          className="px-2 py-1 text-[11px] border border-[#e2ddd7] text-stone-500 hover:border-stone-400 rounded"
                        >
                          Edit
                        </button>
                        <button
                          onClick={() => removeRoute(route)}
                          disabled={deleteRoute.isPending}
                          className="px-2 py-1 text-[11px] border border-red-200 text-red-600 hover:border-red-400 rounded disabled:opacity-50"
                        >
                          Delete
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
                {!isLoading && sortedRoutes.length === 0 && (
                  <tr>
                    <td colSpan={5} className="px-4 py-8 text-center text-[11px] text-stone-400">
                      No public routes configured.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </section>

        <section className="bg-white border border-[#e2ddd7] rounded-lg overflow-hidden">
          <div className="px-4 py-3 border-b border-[#e2ddd7] flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
            <div>
              <h2 className="text-xs font-semibold text-[#1c1917] uppercase tracking-widest">Access logs</h2>
              <p className="text-[11px] text-stone-400 mt-1">Recent requests handled by the manager Caddy instance.</p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
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
                <span
                  title="homectl filters out healthchecks"
                  className="inline-flex h-4 w-4 items-center justify-center rounded-full border border-current text-[10px] leading-none"
                >
                  i
                </span>
              </button>
              <ExcludeToggle
                label="uptime. /api/push"
                active={intelligentFiltering && excludeFilters.uptimePush}
                disabled={!intelligentFiltering}
                onToggle={() => setExcludeFilters((v) => ({ ...v, uptimePush: !v.uptimePush }))}
              />
              <ExcludeToggle
                label="Uptime-Kuma agent"
                active={intelligentFiltering && excludeFilters.uptimeKumaAgent}
                disabled={!intelligentFiltering}
                onToggle={() => setExcludeFilters((v) => ({ ...v, uptimeKumaAgent: !v.uptimeKumaAgent }))}
              />
            </div>
          </div>

          <div ref={logScrollRef} onScroll={captureLogScrollAnchor} className="max-h-[28rem] overflow-auto log-scroll">
            <table className="w-full text-xs">
              <thead className="sticky top-0 z-10 bg-white">
                <tr className="border-b border-[#e2ddd7]">
                  <th className="px-4 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Time</th>
                  <th className="px-3 py-2.5 text-left w-44">
                    <FilterSelect
                      label="Host"
                      value={logFilters.host}
                      options={hostOptions}
                      allLabel="All hosts"
                      onChange={(host) => setLogFilters((v) => ({ ...v, host }))}
                    />
                  </th>
                  <th className="px-3 py-2.5 text-left w-36">
                    <FilterSearchSelect
                      label="IP"
                      value={logFilters.ip}
                      options={ipOptions}
                      placeholder="All IPs"
                      listId="public-route-log-ip-options"
                      onChange={(ip) => setLogFilters((v) => ({ ...v, ip }))}
                    />
                  </th>
                  <th className="px-3 py-2.5 text-left w-40">
                    <FilterTextInput
                      label="URL"
                      value={logFilters.uri}
                      placeholder="Filter URL"
                      onChange={(uri) => setLogFilters((v) => ({ ...v, uri }))}
                    />
                  </th>
                  <th className="px-3 py-2.5 text-left w-28">
                    <FilterSelect
                      label="Status"
                      value={logFilters.status}
                      options={statusOptions}
                      allLabel="All"
                      onChange={(status) => setLogFilters((v) => ({ ...v, status }))}
                    />
                  </th>
                  <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Agent</th>
                </tr>
              </thead>
              <tbody>
                {filteredLogEntries.map((entry: PublicRouteAccessLogEntry, index: number) => {
                  const key = logEntryKey(entry)
                  return (
                  <tr key={`${key}-${index}`} data-log-row="true" data-log-key={key} className="border-b border-[#f7f5f0] hover:bg-[#faf9f7]">
                    <td className="px-4 py-2.5 whitespace-nowrap text-stone-500">{formatDateTime(entry.timestamp)}</td>
                    <td className="px-3 py-2.5 font-mono text-[11px] text-stone-700 whitespace-nowrap">{entry.host}</td>
                    <td className="px-3 py-2.5 font-mono text-[11px] text-stone-600 whitespace-nowrap">{entry.client_ip || '--'}</td>
                    <td className="px-3 py-2.5 w-40 max-w-[10rem]">
                      <div className="flex items-center gap-2">
                        <span className="font-semibold text-stone-600">{entry.method || '--'}</span>
                        <span className="font-mono text-[11px] text-stone-600 truncate max-w-[7rem]" title={entry.uri || ''}>{entry.uri || '/'}</span>
                      </div>
                      <div className="text-[10px] text-stone-400 mt-0.5">{entry.protocol || 'HTTP'} · {formatDuration(entry.duration)}</div>
                    </td>
                    <td className="px-3 py-2.5 whitespace-nowrap">
                      <div className={`font-semibold ${statusClass(entry.status)}`}>{entry.status || '--'}</div>
                      <div className="text-[11px] text-stone-400">{formatBytes(entry.size)} out</div>
                    </td>
                    <td className="px-3 py-2.5 max-w-xs">
                      <div className="truncate text-stone-500" title={entry.user_agent || ''}>{entry.user_agent || '--'}</div>
                    </td>
                  </tr>
                  )
                })}
                {!logsLoading && filteredLogEntries.length === 0 && (
                  <tr>
                    <td colSpan={6} className="px-4 py-8 text-center text-[11px] text-stone-400">
                      No access log entries found.
                    </td>
                  </tr>
                )}
                {logsLoading && (
                  <tr>
                    <td colSpan={6} className="px-4 py-8 text-center text-[11px] text-stone-400">
                      Loading access logs...
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
