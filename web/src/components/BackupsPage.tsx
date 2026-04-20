import { useMemo, useState, useCallback, useEffect, useRef } from "react"
import type { AppStatus, ResticProgress, Snapshot, SnapshotEntry } from "../api/types"
import { useSnapshots, useSnapshotFiles, useRefreshSnapshots, useRepoStatus, useTriggerRepoCheck, useTriggerRepoUnlock, useForceRepoUnlock, useTriggerPrune } from "../api/hooks"
import { useStreamSubscription } from "../api/live"

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
    <div className="min-h-screen bg-[#faf9f7]">
      <div className="border-b border-[#e2ddd7] bg-white px-4 py-4">
        <div className="max-w-6xl mx-auto flex items-center justify-between">
          <div>
            <h1 className="text-sm font-semibold text-[#1c1917]">Backups</h1>
            <p className="text-[11px] text-stone-400 mt-0.5">
              {backupApps.length} backup-enabled app{backupApps.length !== 1 ? 's' : ''}
            </p>
          </div>
        </div>
      </div>

      <main className="max-w-6xl mx-auto px-4 py-6 space-y-4">
        <RepoMaintenancePanel />

        {backupApps.length === 0 ? (
          <p className="text-stone-400 text-sm">No backup-enabled apps found.</p>
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
      </main>
    </div>
  )
}

function RepoMaintenancePanel() {
	const { data, error } = useRepoStatus()
	const triggerCheck = useTriggerRepoCheck()
	const triggerUnlock = useTriggerRepoUnlock()
	const forceUnlock = useForceRepoUnlock()

	const [showCheckLogs, setShowCheckLogs] = useState(false)
	const [showUnlockLogs, setShowUnlockLogs] = useState(false)
	const [checkLines, setCheckLines] = useState<string[]>([])
	const [unlockLines, setUnlockLines] = useState<string[]>([])
	const checkPreRef = useRef<HTMLPreElement>(null)
	const unlockPreRef = useRef<HTMLPreElement>(null)
	const maxLines = 200

	const checkBusy = data?.check.busy === true
	const unlockBusy = data?.unlock.busy === true
	const gatewayOnline = data?.gateway_online === true

	const checkStreamEnabled = gatewayOnline && (showCheckLogs || checkBusy)
	const handleCheckLine = useCallback((line: string, _source?: string, done?: boolean) => {
		if (line) {
			setCheckLines((prev) => {
				const next = [...prev, line]
				return next.length > maxLines ? next.slice(-maxLines) : next
			})
		}
		if (done) {
			return
		}
	}, [maxLines])

	useStreamSubscription(
		checkStreamEnabled ? 'repo-check-logs' : null,
		checkStreamEnabled ? {} : null,
		checkStreamEnabled ? handleCheckLine : null,
	)

	useEffect(() => {
		if (checkPreRef.current) {
			checkPreRef.current.scrollTop = checkPreRef.current.scrollHeight
		}
	}, [checkLines])

	const unlockStreamEnabled = gatewayOnline && (showUnlockLogs || unlockBusy)
	const handleUnlockLine = useCallback((line: string, _source?: string, done?: boolean) => {
		if (line) {
			setUnlockLines((prev) => {
				const next = [...prev, line]
				return next.length > maxLines ? next.slice(-maxLines) : next
			})
		}
		if (done) {
			return
		}
	}, [maxLines])

	useStreamSubscription(
		unlockStreamEnabled ? 'repo-unlock-logs' : null,
		unlockStreamEnabled ? {} : null,
		unlockStreamEnabled ? handleUnlockLine : null,
	)

	useEffect(() => {
		if (unlockPreRef.current) {
			unlockPreRef.current.scrollTop = unlockPreRef.current.scrollHeight
		}
	}, [unlockLines])

	const lastCheckAt = data?.check.last_at
	const lastUnlockAt = data?.unlock.last_at
	const checkProgress = data?.check.progress_percent

	return (
		<div className="bg-white border border-[#e2ddd7] rounded-lg mb-4">
			<div className="px-4 py-3 flex items-start justify-between gap-4 border-b border-[#f0ede8]">
				<div className="min-w-0">
					<div className="text-sm font-semibold text-[#1c1917]">Repository maintenance</div>
					<div className="text-[11px] text-stone-400 mt-0.5">
						Gateway: <span className="text-stone-600">{data?.gateway_node ? data.gateway_node : "(none)"}</span>
						{!gatewayOnline && <span className="text-red-500"> — offline</span>}
					</div>
					{error && (
						<div className="text-[11px] text-red-500 mt-1">{(error as Error).message}</div>
					)}
				</div>
				<div className="flex items-center gap-2 flex-shrink-0">
					<button
						onClick={() => triggerCheck.mutate()}
						disabled={!gatewayOnline || checkBusy || unlockBusy || triggerCheck.isPending}
						className="px-2 py-1 text-[11px] border border-[#e2ddd7] text-stone-500 hover:border-stone-400 hover:text-stone-700 rounded disabled:opacity-50 disabled:cursor-not-allowed"
					>
						{checkBusy ? "Checking..." : "Run check"}
					</button>
					<button
						onClick={() => triggerUnlock.mutate()}
						disabled={!gatewayOnline || checkBusy || unlockBusy || triggerUnlock.isPending}
						className="px-2 py-1 text-[11px] border border-[#e2ddd7] text-stone-500 hover:border-stone-400 hover:text-stone-700 rounded disabled:opacity-50 disabled:cursor-not-allowed"
					>
						{unlockBusy ? "Unlocking..." : "Unlock repo"}
					</button>
					<button
						onClick={() => forceUnlock.mutate()}
						disabled={!gatewayOnline || unlockBusy || forceUnlock.isPending}
						className="px-2 py-1 text-[11px] border border-red-200 text-red-600 hover:border-red-400 hover:bg-red-50 rounded disabled:opacity-50 disabled:cursor-not-allowed"
						title="Force unlock — runs restic unlock --remove-all"
					>
						Force unlock
					</button>
				</div>
			</div>

			<div className="px-4 py-3 grid grid-cols-1 md:grid-cols-2 gap-3">
				<div className="border border-[#e2ddd7] rounded p-3">
					<div className="flex items-center justify-between">
						<div className="text-[11px] font-semibold text-stone-500 uppercase tracking-wider">Check</div>
						<button
							onClick={() => setShowCheckLogs((v) => !v)}
							className="text-[11px] text-teal-600 hover:underline"
							disabled={!gatewayOnline}
						>
							{showCheckLogs ? "Hide logs" : "Show logs"}
						</button>
					</div>
					<div className="text-[11px] text-stone-400 mt-1">
						Last: {lastCheckAt ? <span title={formatFullDate(lastCheckAt)}>{timeAgo(lastCheckAt)}</span> : "never"}
						{data?.check.last_error ? <span className="text-red-500"> — {data.check.last_error}</span> : null}
					</div>
					{checkBusy && (
						<div className="mt-2">
							<div className="h-1 bg-stone-100 rounded overflow-hidden">
								<div
									className="h-full bg-teal-500"
									style={{ width: `${Math.max(0, Math.min(100, checkProgress ?? 0))}%` }}
								/>
							</div>
							<div className="text-[10px] text-stone-400 mt-1">
								{checkProgress != null ? `${checkProgress.toFixed(2)}%` : "Running..."}
							</div>
						</div>
					)}
					{showCheckLogs && (
						<pre
							ref={checkPreRef}
							className="mt-2 text-[11px] text-stone-600 font-mono p-2 bg-[#f7f5f0] border border-[#e2ddd7] rounded max-h-40 overflow-auto whitespace-pre-wrap"
						>
							{checkLines.length === 0 ? "Waiting for output..." : checkLines.join("\n")}
						</pre>
					)}
				</div>

				<div className="border border-[#e2ddd7] rounded p-3">
					<div className="flex items-center justify-between">
						<div className="text-[11px] font-semibold text-stone-500 uppercase tracking-wider">Unlock</div>
						<button
							onClick={() => setShowUnlockLogs((v) => !v)}
							className="text-[11px] text-teal-600 hover:underline"
							disabled={!gatewayOnline}
						>
							{showUnlockLogs ? "Hide logs" : "Show logs"}
						</button>
					</div>
					<div className="text-[11px] text-stone-400 mt-1">
						Last: {lastUnlockAt ? <span title={formatFullDate(lastUnlockAt)}>{timeAgo(lastUnlockAt)}</span> : "never"}
						{data?.unlock.last_error ? <span className="text-red-500"> — {data.unlock.last_error}</span> : null}
					</div>
					{showUnlockLogs && (
						<pre
							ref={unlockPreRef}
							className="mt-2 text-[11px] text-stone-600 font-mono p-2 bg-[#f7f5f0] border border-[#e2ddd7] rounded max-h-40 overflow-auto whitespace-pre-wrap"
						>
							{unlockLines.length === 0 ? "Waiting for output..." : unlockLines.join("\n")}
						</pre>
					)}
				</div>
			</div>
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

  // Register background polling for this app's snapshots regardless of expanded state
  useSnapshots(app.app_id, onlineNode)

  const sortedReports = [...app.node_reports].sort((a, b) => a.node.localeCompare(b.node))
  const reportForPrune = sortedReports.find((nr) => nr.node === onlineNode) ?? sortedReports[0]
  const backupReport = reportForPrune
  const postBackupReport = sortedReports.find((nr) => !!nr.post_backup_phase)
  const activeBackupReport = sortedReports.find(
    (nr) => nr.phase === "backing_up" || nr.phase === "restoring"
  )
  const streamReport = postBackupReport ?? activeBackupReport

  const backupStatusColor: Record<string, string> = {
    ok: "text-emerald-600",
    needs_backup: "text-amber-600",
    never: "text-stone-400",
  }

  const pruneStatusColor: Record<string, string> = {
    ok: "text-emerald-600",
    needs_prune: "text-amber-600",
    never: "text-stone-400",
  }

  return (
    <div className="bg-white border border-[#e2ddd7] rounded-lg">
      <button
        onClick={onToggle}
        className="w-full px-4 py-3 flex items-center justify-between text-left hover:bg-[#faf9f7] sticky top-0 z-10 bg-white rounded-t"
      >
        <div className="flex items-center gap-3">
          <span className="text-[10px] text-stone-400">{expanded ? "▼" : "▶"}</span>
          <span className="text-sm font-semibold text-[#1c1917]">{app.app_id}</span>
          <span className="text-[10px] text-stone-400">B</span>
          <span className={`text-xs ${backupStatusColor[app.backup_status] ?? "text-stone-400"}`}>
            {app.backup_status}
          </span>
          {backupReport?.last_backup_at && (
            <span className="text-[11px] text-stone-400" title={formatFullDate(backupReport.last_backup_at)}>
              {timeAgo(backupReport.last_backup_at)}
            </span>
          )}
          {backupReport?.last_backup_error && (
            <span className="text-[11px] text-red-500" title={backupReport.last_backup_error}>err</span>
          )}
          <span className="text-stone-200">·</span>
          <span className="text-[10px] text-stone-400">P</span>
          <span className={`text-xs ${pruneStatusColor[reportForPrune?.prune_status ?? app.prune_status] ?? "text-stone-400"}`}>
            {reportForPrune?.last_prune_error ? "failed" : (reportForPrune?.prune_status ?? app.prune_status)}
          </span>
          {reportForPrune?.last_prune_at && (
            <span className="text-[11px] text-stone-400" title={formatFullDate(reportForPrune.last_prune_at)}>
              {timeAgo(reportForPrune.last_prune_at)}
            </span>
          )}
          {reportForPrune?.last_prune_error && (
            <span className="text-[11px] text-red-500" title={reportForPrune.last_prune_error}>
              {reportForPrune.last_prune_error.length > 40
                ? reportForPrune.last_prune_error.slice(0, 40) + "..."
                : reportForPrune.last_prune_error}
            </span>
          )}
          {streamReport?.progress && <ProgressMini progress={streamReport.progress} />}
          <SnapshotCount appId={app.app_id} node={onlineNode} />
        </div>
        <div className="flex items-center gap-3 text-[11px] text-stone-400">
          <span>every {app.backup_interval}</span>
          {!onlineNode && <span className="text-red-500">no online node</span>}
        </div>
      </button>

      {expanded && onlineNode && (
        <div className="border-t border-[#f0ede8]">
          <SnapshotList appId={app.app_id} node={onlineNode} app={app} />
        </div>
      )}
      {expanded && !onlineNode && (
        <div className="px-4 pb-3 text-[11px] text-stone-400 border-t border-[#f0ede8] pt-3">
          No online node available to query snapshots.
        </div>
      )}
    </div>
  )
}

function ProgressMini({ progress }: { progress: ResticProgress }) {
  const pct = Math.max(0, Math.min(100, Math.round(progress.percent_done * 100)))
  return (
    <span className="inline-flex items-center gap-1.5 text-[11px] text-stone-500">
      <span className="w-14 h-1 bg-stone-200 rounded-full overflow-hidden">
        <span className="block h-full bg-violet-400 rounded-full transition-all" style={{ width: `${pct}%` }} />
      </span>
      <span>{pct}%</span>
      {progress.total_files > 0 ? (
        <span className="text-stone-400">{progress.files_done}/{progress.total_files}</span>
      ) : (
        <span className="text-stone-400">{formatBytes(progress.bytes_done)}/{formatBytes(progress.total_bytes)}</span>
      )}
    </span>
  )
}



function SnapshotCount({ appId, node }: { appId: string; node: string | null }) {
  const { data } = useSnapshots(appId, node)
  const count = data?.snapshots?.length
  if (count == null) return null
  return (
    <span className="text-[11px] text-stone-400">
      ({count} snapshot{count !== 1 ? "s" : ""})
    </span>
  )
}

function SnapshotList({ appId, node, app }: { appId: string; node: string; app: AppStatus }) {
  const { data, isLoading, error } = useSnapshots(appId, node)
  const refreshMutation = useRefreshSnapshots()
  const pruneMutation = useTriggerPrune()
  const [expandedSnapshot, setExpandedSnapshot] = useState<string | null>(null)

  const snapshots = data?.snapshots
  const cacheLoading = data?.loading === true

  const nodeReport = app.node_reports.find((nr) => nr.node === node)
  const isBusy = !!nodeReport?.post_backup_phase || nodeReport?.phase === "backing_up"
  const canPrune = nodeReport?.observed_state === "running" && !isBusy

  return (
    <div>
      <div className="px-4 py-2 flex items-center justify-between border-b border-[#f0ede8]">
        <div className="flex items-center gap-2 text-[11px] text-stone-400">
          {data?.cached_at && (
            <span title={formatFullDate(data.cached_at)}>Cached {timeAgo(data.cached_at)}</span>
          )}
          {cacheLoading && (
            <span className="text-amber-500">Refreshing...</span>
          )}
        </div>
        <div className="flex items-center gap-2">
          {canPrune && (
            <button
              onClick={() => pruneMutation.mutate({ appId, node })}
              disabled={pruneMutation.isPending}
              className="px-2 py-1 text-[11px] border border-violet-200 text-violet-600 hover:border-violet-400 hover:bg-violet-50 rounded disabled:opacity-50 disabled:cursor-not-allowed"
            >
              Prune
            </button>
          )}
          <button
            onClick={() => refreshMutation.mutate({ appId, node })}
            disabled={cacheLoading || refreshMutation.isPending}
            className="px-2 py-1 text-[11px] border border-[#e2ddd7] text-stone-500 hover:border-stone-400 rounded disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {cacheLoading ? "Refreshing..." : "Refresh"}
          </button>
        </div>
      </div>

      {isLoading && !data && (
        <div className="px-4 py-3 text-[11px] text-stone-400">Loading snapshots...</div>
      )}

      {error && !data && (
        <div className="px-4 py-3 text-[11px] text-red-500">
          Error: {(error as Error).message}
        </div>
      )}

      {data?.error && (
        <div className="px-4 py-3 text-[11px] text-red-500">
          Cache error: {data.error}
        </div>
      )}

      {!isLoading && !error && snapshots && snapshots.length === 0 && !cacheLoading && (
        <div className="px-4 py-3 text-[11px] text-stone-400">No snapshots found.</div>
      )}

      {snapshots && snapshots.length > 0 && (
        <table className="w-full text-xs">
          <thead>
            <tr className="border-b border-[#f0ede8]">
              <th className="px-4 py-2 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest w-8"></th>
              <th className="px-4 py-2 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest w-10">#</th>
              <th className="px-4 py-2 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">ID</th>
              <th className="px-4 py-2 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Time</th>
              <th className="px-4 py-2 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Host</th>
              <th className="px-4 py-2 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Tags</th>
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
        className="border-b border-[#f7f5f0] hover:bg-[#faf9f7] cursor-pointer"
      >
        <td className="px-4 py-2 text-stone-400 text-[10px]">{expanded ? "▼" : "▶"}</td>
        <td className="px-4 py-2 text-stone-400 text-[11px]">{index}</td>
        <td className="px-4 py-2 text-teal-600 font-mono text-[11px]">{snapshot.short_id}</td>
        <td className="px-4 py-2 text-stone-600 text-[11px]" title={formatFullDate(snapshot.time)}>
          {timeAgo(snapshot.time)}
        </td>
        <td className="px-4 py-2 text-stone-600 text-[11px]">{snapshot.hostname}</td>
        <td className="px-4 py-2 text-stone-400 text-[11px]">
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
    <div className="bg-[#faf9f7] border-t border-[#e2ddd7] px-4 py-3">
      {/* Breadcrumb */}
      <div className="flex items-center gap-1 text-[11px] mb-2">
        {breadcrumbs.map((bc, i) => (
          <span key={bc.path} className="flex items-center gap-1">
            {i > 0 && <span className="text-stone-300">/</span>}
            <button
              onClick={() => setCurrentPath(bc.path)}
              className="text-teal-600 hover:underline"
            >
              {bc.label}
            </button>
          </span>
        ))}
      </div>

      {isLoading && <div className="text-[11px] text-stone-400">Loading...</div>}
      {error && (
        <div className="text-[11px] text-red-500">
          Error: {(error as Error).message}
        </div>
      )}

      {entries && entries.length === 0 && (
        <div className="text-[11px] text-stone-400">Empty directory.</div>
      )}

      {entries && entries.length > 0 && (
        <table className="w-full text-xs">
          <tbody>
            {entries.map((entry: SnapshotEntry) => (
              <tr
                key={entry.path}
                className="border-t border-[#f0ede8] hover:bg-white"
              >
                <td className="py-1.5 pr-2 text-stone-400 w-6">
                  {entry.type === "dir" ? "📁" : "📄"}
                </td>
                <td className="py-1.5">
                  {entry.type === "dir" ? (
                    <button
                      onClick={() => setCurrentPath(entry.path + "/")}
                      className="text-teal-600 hover:underline"
                    >
                      {entry.name}/
                    </button>
                  ) : (
                    <span className="text-stone-600">{entry.name}</span>
                  )}
                </td>
                <td className="py-1.5 text-right text-stone-400 w-20">
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
