import { useEffect, useMemo, useState } from 'react'
import type { FormEvent } from 'react'
import {
  useCreatePublicRoute,
  useDeletePublicRoute,
  usePublicRoutes,
  useUpdatePublicRoute,
} from '../api/hooks'
import type { PublicRoute, PublicRouteMode } from '../api/types'
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

const emptyForm = {
  id: '',
  host: '',
  mode: 'homelab' as PublicRouteMode,
  custom_target: '',
  enabled: true,
}

export function RoutingPage() {
  const { data: routes, isLoading } = usePublicRoutes()
  const createRoute = useCreatePublicRoute()
  const updateRoute = useUpdatePublicRoute()
  const deleteRoute = useDeletePublicRoute()
  const [form, setForm] = useState(emptyForm)
  const [error, setError] = useState<string | null>(null)

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
      </main>
    </div>
  )
}
