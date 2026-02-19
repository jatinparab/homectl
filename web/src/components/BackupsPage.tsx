import { useMemo, useState, useCallback } from "react"
import type { AppStatus, Snapshot, SnapshotEntry } from "../api/types"
import { useSnapshots, useSnapshotFiles, useRefreshSnapshots } from "../api/hooks"

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  const value = bytes / Math.pow(1024, i)
  return `${value.toFixed(i > 0 ? 1 : 0)} ${units[i]}`
}

function timeAgo(dateStr: string): string {
  const date = new Date(dateStr)
  const now = new Date()
  const seconds = Math.floor((now.getTime() - date.getTime()) / 1000)
  if (seconds < 60) return `${seconds}s ago`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  return `${days}d ago`
}

function formatFullDate(dateStr: string): string {
  const date = new Date(dateStr)
  const day = date.getDate()
  const suffix =
    day % 10 === 1 && day !== 11
      ? "st"
      : day % 10 === 2 && day !== 12
        ? "nd"
        : day % 10 === 3 && day !== 13
          ? "rd"
          : "th"
  const month = date.toLocaleString(undefined, { month: "short" })
  const year = date.getFullYear()
  const hours = date.getHours().toString().padStart(2, "0")
  const minutes = date.getMinutes().toString().padStart(2, "0")
  return `${day}${suffix} ${month} ${year}, ${hours}:${minutes}`
}

interface BackupsPageProps {
  apps?: AppStatus[]
}

export function BackupsPage({ apps }: BackupsPageProps) {
  const [expandedApps, setExpandedApps] = useState<Set<string>>(new Set())

  const toggleApp = useCallback((appId: string) => {
    setExpandedApps((prev) => {
      const next = new Set(prev)
      if (next.has(appId)) {
        next.delete(appId)
      } else {
        next.add(appId)
      }
      return next
    })
  }, [])

  const backupApps = useMemo(
    () =>
      (apps ?? [])
        .filter((a) => a.backup_enabled)
        .sort((a, b) => a.app_id.localeCompare(b.app_id)),
    [apps]
  )

  return (
    <div>
      <h2 className="text-lg font-semibold text-white mb-4">Backups</h2>
      {backupApps.length === 0 ? (
        <p className="text-gray-400">No backup-enabled apps found.</p>
      ) : (
        <div className="space-y-2">
          {backupApps.map((app) => (
            <AppBackupRow
              key={app.app_id}
              app={app}
              expanded={expandedApps.has(app.app_id)}
              onToggle={() => toggleApp(app.app_id)}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function AppBackupRow({
  app,
  expanded,
  onToggle,
}: {
  app: AppStatus
  expanded: boolean
  onToggle: () => void
}) {
  const onlineNode = app.node_reports.find((nr) => nr.node_online)?.node ?? null

  const backupStatusColor: Record<string, string> = {
    ok: "text-green-400",
    needs_backup: "text-yellow-400",
    never: "text-gray-500",
  }

  return (
    <div className="bg-gray-800 rounded border border-gray-700">
      <button
        onClick={onToggle}
        className="w-full px-4 py-3 flex items-center justify-between text-left hover:bg-gray-750 sticky top-0 z-10 bg-gray-800 rounded-t"
      >
        <div className="flex items-center gap-4">
          <span className="text-sm text-gray-400">{expanded ? "\u25BC" : "\u25B6"}</span>
          <span className="text-white font-medium">{app.app_id}</span>
          <span
            className={`text-xs ${backupStatusColor[app.backup_status] ?? "text-gray-500"}`}
          >
            {app.backup_status}
          </span>
          <SnapshotCount appId={app.app_id} node={onlineNode} />
        </div>
        <div className="flex items-center gap-4 text-xs text-gray-400">
          <span>Interval: {app.backup_interval}</span>
          {!onlineNode && <span className="text-red-400">No online node</span>}
        </div>
      </button>

      {expanded && onlineNode && (
        <SnapshotList appId={app.app_id} node={onlineNode} />
      )}
      {expanded && !onlineNode && (
        <div className="px-4 pb-3 text-sm text-gray-500">
          No online node available to query snapshots.
        </div>
      )}
    </div>
  )
}

function SnapshotCount({ appId, node }: { appId: string; node: string | null }) {
  // Uses cached data if previously fetched; won't trigger a new fetch if cache is empty
  const { data } = useSnapshots(appId, node)
  const count = data?.snapshots?.length
  if (count == null) return null
  return (
    <span className="text-xs text-gray-500">
      ({count} snapshot{count !== 1 ? "s" : ""})
    </span>
  )
}

function SnapshotList({ appId, node }: { appId: string; node: string }) {
  const { data, isLoading, error } = useSnapshots(appId, node)
  const refreshMutation = useRefreshSnapshots()
  const [expandedSnapshot, setExpandedSnapshot] = useState<string | null>(null)

  const snapshots = data?.snapshots
  const cacheLoading = data?.loading === true

  return (
    <div className="border-t border-gray-700">
      <div className="px-4 py-2 flex items-center justify-between">
        <div className="flex items-center gap-2 text-xs text-gray-400">
          {data?.cached_at && (
            <span title={formatFullDate(data.cached_at)}>Cached {timeAgo(data.cached_at)}</span>
          )}
          {cacheLoading && (
            <span className="text-yellow-400">Refreshing...</span>
          )}
        </div>
        <button
          onClick={() => refreshMutation.mutate({ appId, node })}
          disabled={cacheLoading || refreshMutation.isPending}
          className="px-2 py-1 text-xs bg-gray-700 hover:bg-gray-600 text-gray-300 rounded disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {cacheLoading ? "Refreshing..." : "Refresh"}
        </button>
      </div>

      {isLoading && !data && (
        <div className="px-4 pb-3 text-sm text-gray-400">Loading snapshots...</div>
      )}

      {error && !data && (
        <div className="px-4 pb-3 text-sm text-red-400">
          Error: {(error as Error).message}
        </div>
      )}

      {data?.error && (
        <div className="px-4 pb-3 text-sm text-red-400">
          Cache error: {data.error}
        </div>
      )}

      {!isLoading && !error && snapshots && snapshots.length === 0 && !cacheLoading && (
        <div className="px-4 pb-3 text-sm text-gray-500">No snapshots found.</div>
      )}

      {snapshots && snapshots.length > 0 && (
        <table className="w-full text-sm">
          <thead>
            <tr className="text-gray-400 text-xs">
              <th className="px-4 py-2 text-left w-8"></th>
              <th className="px-4 py-2 text-left w-10">#</th>
              <th className="px-4 py-2 text-left">ID</th>
              <th className="px-4 py-2 text-left">Time</th>
              <th className="px-4 py-2 text-left">Host</th>
              <th className="px-4 py-2 text-left">Tags</th>
            </tr>
          </thead>
          <tbody>
            {snapshots.map((snap: Snapshot, index: number) => (
              <SnapshotRow
                key={snap.id}
                index={index + 1}
                snapshot={snap}
                node={node}
                expanded={expandedSnapshot === snap.id}
                onToggle={() =>
                  setExpandedSnapshot(
                    expandedSnapshot === snap.id ? null : snap.id
                  )
                }
              />
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}

function SnapshotRow({
  index,
  snapshot,
  node,
  expanded,
  onToggle,
}: {
  index: number
  snapshot: Snapshot
  node: string
  expanded: boolean
  onToggle: () => void
}) {
  return (
    <>
      <tr
        onClick={onToggle}
        className="border-t border-gray-700/50 hover:bg-gray-750 cursor-pointer"
      >
        <td className="px-4 py-2 text-gray-400">{expanded ? "\u25BC" : "\u25B6"}</td>
        <td className="px-4 py-2 text-gray-500 text-xs">{index}</td>
        <td className="px-4 py-2 text-blue-400 font-mono">{snapshot.short_id}</td>
        <td className="px-4 py-2 text-gray-300" title={formatFullDate(snapshot.time)}>
          {timeAgo(snapshot.time)}
        </td>
        <td className="px-4 py-2 text-gray-300">{snapshot.hostname}</td>
        <td className="px-4 py-2 text-gray-400">
          {snapshot.tags?.join(", ") ?? ""}
        </td>
      </tr>
      {expanded && (
        <tr>
          <td colSpan={6} className="p-0">
            <FileBrowser snapshotId={snapshot.id} node={node} />
          </td>
        </tr>
      )}
    </>
  )
}

function FileBrowser({
  snapshotId,
  node,
}: {
  snapshotId: string
  node: string
}) {
  const [currentPath, setCurrentPath] = useState("/")
  const { data: entries, isLoading, error } = useSnapshotFiles(
    snapshotId,
    node,
    currentPath
  )

  const pathParts = currentPath.split("/").filter(Boolean)
  const breadcrumbs = [
    { label: "/", path: "/" },
    ...pathParts.map((part, i) => ({
      label: part,
      path: "/" + pathParts.slice(0, i + 1).join("/") + "/",
    })),
  ]

  return (
    <div className="bg-gray-900 border-t border-gray-700 px-4 py-3">
      {/* Breadcrumb */}
      <div className="flex items-center gap-1 text-xs mb-2">
        {breadcrumbs.map((bc, i) => (
          <span key={bc.path} className="flex items-center gap-1">
            {i > 0 && <span className="text-gray-600">/</span>}
            <button
              onClick={() => setCurrentPath(bc.path)}
              className="text-blue-400 hover:underline"
            >
              {bc.label}
            </button>
          </span>
        ))}
      </div>

      {isLoading && <div className="text-sm text-gray-400">Loading...</div>}
      {error && (
        <div className="text-sm text-red-400">
          Error: {(error as Error).message}
        </div>
      )}

      {entries && entries.length === 0 && (
        <div className="text-sm text-gray-500">Empty directory.</div>
      )}

      {entries && entries.length > 0 && (
        <table className="w-full text-xs">
          <tbody>
            {entries.map((entry: SnapshotEntry) => (
              <tr
                key={entry.path}
                className="border-t border-gray-800 hover:bg-gray-800/50"
              >
                <td className="py-1.5 pr-2 text-gray-500 w-6">
                  {entry.type === "dir" ? "\uD83D\uDCC1" : "\uD83D\uDCC4"}
                </td>
                <td className="py-1.5">
                  {entry.type === "dir" ? (
                    <button
                      onClick={() => setCurrentPath(entry.path + "/")}
                      className="text-blue-400 hover:underline"
                    >
                      {entry.name}/
                    </button>
                  ) : (
                    <span className="text-gray-300">{entry.name}</span>
                  )}
                </td>
                <td className="py-1.5 text-right text-gray-500 w-20">
                  {entry.type === "file" && entry.size != null
                    ? formatBytes(entry.size)
                    : ""}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
