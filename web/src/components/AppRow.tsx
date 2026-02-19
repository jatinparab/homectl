import { useState, useEffect, useRef, useCallback } from "react";
import type { AppStatus, ResticProgress, UpcomingBackup, UpcomingPrune } from "../api/types";
import { useRestartApp, useTriggerBackup, useTriggerPrune, useUpdateAppDesiredState, useUpdateAppRestoreEnabled } from "../api/hooks";
import {
  StatusBadge,
  HealthBadge,
  PhaseBadge,
  DesiredBadge,
  PostBackupPhaseBadge,
} from "./StatusBadge";
import { PhaseLogOverlay } from "./PullLogOverlay";

const BACKUP_PHASES = ["backup_stopping", "backup_stopped", "backing_up"];
const POST_BACKUP_PHASES = ["backup_pruning", "backup_checking"];

function formatInterval(interval: string): string {
  // Parse Go duration like "1h0m0s", "72h0m0s", "168h0m0s"
  const match = interval.match(/^(\d+)h/);
  if (!match) return interval;
  const hours = parseInt(match[1], 10);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  const rem = hours % 24;
  return rem > 0 ? `${days}d${rem}h` : `${days}d`;
}

function timeAgo(dateStr: string): string {
  const date = new Date(dateStr);
  const now = new Date();
  const seconds = Math.floor((now.getTime() - date.getTime()) / 1000);
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

const backupStatusColors: Record<string, string> = {
  ok: "bg-green-500",
  needs_backup: "bg-orange-500",
  never: "bg-gray-500",
};

const backupStatusLabels: Record<string, string> = {
  ok: "OK",
  needs_backup: "Stale",
  never: "Never",
};

const pruneStatusColors: Record<string, string> = {
  ok: "bg-green-500",
  needs_prune: "bg-orange-500",
  never: "bg-gray-500",
};

const pruneStatusLabels: Record<string, string> = {
  ok: "OK",
  needs_prune: "Stale",
  never: "Never",
};

interface AppRowProps {
  app: AppStatus;
  isUpcomingBackup?: boolean;
  upcomingBackup?: UpcomingBackup;
  isUpcomingPrune?: boolean;
  upcomingPrune?: UpcomingPrune;
  onShowLogs?: (node: string, appId: string) => void;
}

interface ProgressAccumulator {
  completedFiles: number;
  completedBytes: number;
  last: ResticProgress | null;
}

function newProgressAccumulator(): ProgressAccumulator {
  return { completedFiles: 0, completedBytes: 0, last: null };
}

function accumulateResticProgress(state: ProgressAccumulator, current: ResticProgress): ResticProgress {
  const previous = state.last;
  if (previous) {
    const filesReset =
      previous.total_files > 0 &&
      current.total_files > 0 &&
      (current.files_done < previous.files_done ||
        (current.percent_done < previous.percent_done && current.files_done <= previous.files_done));

    const bytesReset =
      previous.total_bytes > 0 &&
      current.total_bytes > 0 &&
      (current.bytes_done < previous.bytes_done ||
        (current.percent_done < previous.percent_done && current.bytes_done <= previous.bytes_done));

    if (filesReset || bytesReset) {
      state.completedFiles += Math.max(previous.files_done, previous.total_files);
      state.completedBytes += Math.max(previous.bytes_done, previous.total_bytes);
    }
  }

  state.last = current;

  const totalFiles = current.total_files > 0 ? state.completedFiles + current.total_files : current.total_files;
  const filesDone = current.total_files > 0 ? state.completedFiles + current.files_done : current.files_done;

  const totalBytes = current.total_bytes > 0 ? state.completedBytes + current.total_bytes : current.total_bytes;
  const bytesDone = current.total_bytes > 0 ? state.completedBytes + current.bytes_done : current.bytes_done;

  let percent = current.percent_done;
  if (totalFiles > 0) {
    percent = Math.min(1, Math.max(0, filesDone / totalFiles));
  } else if (totalBytes > 0) {
    percent = Math.min(1, Math.max(0, bytesDone / totalBytes));
  }

  return {
    ...current,
    percent_done: percent,
    total_files: totalFiles,
    files_done: filesDone,
    total_bytes: totalBytes,
    bytes_done: bytesDone,
  };
}

export function AppRow({ app, isUpcomingBackup, upcomingBackup, isUpcomingPrune, upcomingPrune, onShowLogs }: AppRowProps) {
  const restartMutation = useRestartApp();
  const backupMutation = useTriggerBackup();
  const pruneMutation = useTriggerPrune();
  const desiredStateMutation = useUpdateAppDesiredState();
  const restoreMutation = useUpdateAppRestoreEnabled();

  const STREAMABLE_PHASES = ["pulling", "restoring", "backing_up"];

  const [phaseLogOpen, setPhaseLogOpen] = useState(false);
  const [postBackupLogOpen, setPostBackupLogOpen] = useState(false);
  const [realtimeProgress, setRealtimeProgress] = useState<ResticProgress | null>(null);
  const [realtimePostBackupProgress, setRealtimePostBackupProgress] = useState<ResticProgress | null>(null);
  const prevStreamable = useRef(false);
  const prevPostBackupStreamable = useRef(false);
  const streamProgressRef = useRef<ProgressAccumulator>(newProgressAccumulator());
  const postBackupProgressRef = useRef<ProgressAccumulator>(newProgressAccumulator());

  const nodeReports = [...app.node_reports].sort((a, b) => a.node.localeCompare(b.node));

  const streamableNode = nodeReports.find((nr) => STREAMABLE_PHASES.includes(nr.phase));
  const isStreamable = !!streamableNode;

  const postBackupStreamableNode = nodeReports.find((nr) => nr.post_backup_phase && POST_BACKUP_PHASES.includes(nr.post_backup_phase));
  const isPostBackupStreamable = !!postBackupStreamableNode;

  // Auto-close when streamable phase ends
  useEffect(() => {
    if (!isStreamable && prevStreamable.current) {
      setPhaseLogOpen(false);
      setRealtimeProgress(null);
      streamProgressRef.current = newProgressAccumulator();
    }
    prevStreamable.current = isStreamable;
  }, [isStreamable]);

  // Background progress stream for backup/restore (keeps progress realtime even if logs overlay is closed)
  useEffect(() => {
    if (!streamableNode) return;
    if (!(streamableNode.phase === "backing_up" || streamableNode.phase === "restoring")) {
      setRealtimeProgress(null);
      streamProgressRef.current = newProgressAccumulator();
      return;
    }
    streamProgressRef.current = newProgressAccumulator();
    const url = `/api/v1/apps/${encodeURIComponent(app.app_id)}/phase-logs?node=${encodeURIComponent(streamableNode.node)}`;
    const es = new EventSource(url);
    es.onmessage = (e) => {
      const line: string = e.data;
      if (!line.startsWith("__PROGRESS__:")) return;
      try {
        const p = JSON.parse(line.slice("__PROGRESS__:".length)) as ResticProgress;
        setRealtimeProgress(accumulateResticProgress(streamProgressRef.current, p));
      } catch {
        // ignore parse errors
      }
    };
    es.onerror = () => {
      es.close();
    };
    return () => {
      es.close();
    };
  }, [app.app_id, streamableNode?.node, streamableNode?.phase]);

  // Auto-close post-backup log when post-processing ends
  useEffect(() => {
    if (!isPostBackupStreamable && prevPostBackupStreamable.current) {
      setPostBackupLogOpen(false);
      setRealtimePostBackupProgress(null);
      postBackupProgressRef.current = newProgressAccumulator();
    }
    prevPostBackupStreamable.current = isPostBackupStreamable;
  }, [isPostBackupStreamable]);

  // Background progress stream for post-backup steps (e.g. prune)
  useEffect(() => {
    if (!postBackupStreamableNode) return;
    const phase = postBackupStreamableNode.post_backup_phase;
    if (!phase) {
      setRealtimePostBackupProgress(null);
      postBackupProgressRef.current = newProgressAccumulator();
      return;
    }
    postBackupProgressRef.current = newProgressAccumulator();
    const url = `/api/v1/apps/${encodeURIComponent(app.app_id)}/phase-logs?node=${encodeURIComponent(postBackupStreamableNode.node)}`;
    const es = new EventSource(url);
    es.onmessage = (e) => {
      const line: string = e.data;
      if (!line.startsWith("__PROGRESS__:")) return;
      try {
        const p = JSON.parse(line.slice("__PROGRESS__:".length)) as ResticProgress;
        setRealtimePostBackupProgress(accumulateResticProgress(postBackupProgressRef.current, p));
      } catch {
        // ignore parse errors
      }
    };
    es.onerror = () => {
      es.close();
    };
    return () => {
      es.close();
    };
  }, [app.app_id, postBackupStreamableNode?.node, postBackupStreamableNode?.post_backup_phase]);

  const handleProgress = useCallback((p: ResticProgress) => {
    setRealtimeProgress(accumulateResticProgress(streamProgressRef.current, p));
  }, []);
  const rowCount = Math.max(nodeReports.length, 1);

  // No node reports — single row with dashes
  if (nodeReports.length === 0) {
    return (
      <tr className="border-b border-gray-700">
        <td className="px-4 py-2">
          <div className="flex items-center gap-2 truncate">
            <span className="font-medium text-white truncate">{app.app_id}</span>
            {app.is_global && <GlobalIcon />}
            {app.backup_enabled && isUpcomingBackup && <UpcomingTag started={upcomingBackup?.started} kind="backup" />}
            {app.backup_enabled && isUpcomingPrune && <UpcomingTag started={upcomingPrune?.started} kind="prune" />}
          </div>
        </td>
        <td className="px-3 py-2 whitespace-nowrap">
          <DesiredToggle app={app} mutation={desiredStateMutation} />
        </td>
        <td className="px-3 py-2 text-gray-500 text-sm">-</td>
        <td className="px-3 py-2 text-gray-500 text-sm">-</td>
        <td className="px-3 py-2 text-gray-500 text-sm">-</td>
        <td className="px-3 py-2 text-gray-500 text-sm">-</td>
        <td className="px-3 py-2 text-gray-500 text-sm">-</td>
        <td className="px-3 py-2 text-gray-500 text-sm">-</td>
        <td className="px-3 py-2"></td>
      </tr>
    );
  }

  return (
    <>
      {nodeReports.map((nr, i) => {
        const isFirst = i === 0;
        const isLast = i === rowCount - 1;
        const backupColor = backupStatusColors[nr.backup_status] || "bg-gray-500";
        const backupLabel = backupStatusLabels[nr.backup_status] || "-";

        return (
          <tr
            key={`${app.app_id}-${nr.node}`}
            className={`${isLast ? "border-b border-gray-700" : ""} hover:bg-gray-750`}
          >
            {/* App name — only first row */}
            <td className={`px-4 py-2 align-top ${isFirst ? "" : "border-t border-gray-800"}`}>
              {isFirst ? (
                <div>
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
                    <span className="font-medium text-white truncate">{app.app_id}</span>
                    {app.is_global && <GlobalIcon />}
                    {app.backup_enabled && isUpcomingBackup && <UpcomingTag started={upcomingBackup?.started} kind="backup" />}
                    {app.backup_enabled && isUpcomingPrune && <UpcomingTag started={upcomingPrune?.started} kind="prune" />}
                    {app.backup_enabled && !app.restore_enabled && (
                      <span
                        className="shrink-0 inline-flex items-center px-1 py-0.5 rounded text-[10px] font-medium bg-yellow-700 text-yellow-200 cursor-pointer hover:bg-yellow-600"
                        title="Restore disabled — using local data. Click to re-enable."
                        onClick={() => restoreMutation.mutate({ appId: app.app_id, enabled: true })}
                      >
                        LOCAL
                      </span>
                    )}
                    {app.backup_enabled && (
                      <span className="shrink-0 text-[10px] text-gray-500" title={`Backup every ${formatInterval(app.backup_interval)}`}>
                        every {formatInterval(app.backup_interval)}
                      </span>
                    )}
                    {app.backup_sensitive && (
                      <span
                        className="shrink-0 inline-flex items-center px-1 py-0.5 rounded text-[10px] font-medium bg-purple-800/60 text-purple-300"
                        title="Backup-sensitive: waits for no active connections (or 24h)"
                      >
                        sensitive
                      </span>
                    )}
                  </div>
                </div>
              ) : null}
            </td>

            {/* Desired — only first row */}
            <td className={`px-3 py-2 whitespace-nowrap align-top ${isFirst ? "" : "border-t border-gray-800"}`}>
              {isFirst && <DesiredToggle app={app} mutation={desiredStateMutation} />}
            </td>

            {/* Node name */}
            <td className="px-3 py-2 align-top">
              <span className={`text-sm font-medium ${nr.node_online ? "text-green-400" : "text-gray-500"}`}>
                {nr.node}
              </span>
              {!app.is_global && nodeReports.length > 1 && app.assigned_agent === nr.node && (
                <span className="ml-1 text-[10px] text-blue-400" title="Assigned node">*</span>
              )}
              {nr.quarantined && <span className="ml-1 text-[10px] text-yellow-400">Q</span>}
              {nr.restart_count > 0 && (
                <span className="ml-1 text-[10px] text-gray-500">R:{nr.restart_count}</span>
              )}
            </td>

            {/* Status */}
            <td className="px-3 py-2 whitespace-nowrap align-top">
              <StatusBadge status={nr.observed_state} />
            </td>

            {/* Health */}
            <td className="px-3 py-2 whitespace-nowrap align-top">
              <HealthBadge health={nr.health} />
            </td>

            {/* Phase — allow wrap for progress bar */}
            <td className="px-3 py-2 align-top">
              <div className="flex flex-wrap items-center gap-1">
                {nr.phase ? (
                  STREAMABLE_PHASES.includes(nr.phase) ? (
                    <button
                      onClick={() => setPhaseLogOpen((v) => !v)}
                      className="relative cursor-pointer hover:opacity-80"
                      title="Toggle phase logs"
                    >
                      <PhaseBadge
                        phase={nr.phase}
                        progress={realtimeProgress ?? nr.progress}
                      />
                      <span className={`absolute -top-0.5 -right-1.5 w-1.5 h-1.5 rounded-full ${
                        nr.phase === "pulling" ? "bg-blue-400"
                          : nr.phase === "restoring" ? "bg-yellow-400"
                          : "bg-amber-400"
                      }`} />
                    </button>
                  ) : (
                    <PhaseBadge phase={nr.phase} progress={nr.progress} />
                  )
                ) : (
                  <span className="text-gray-500 text-sm">-</span>
                )}
                {nr.post_backup_phase && (
                  <PostBackupPhaseBadge
                    phase={nr.post_backup_phase}
                    progress={realtimePostBackupProgress ?? nr.progress}
                    onClick={isPostBackupStreamable && postBackupStreamableNode?.node === nr.node ? () => setPostBackupLogOpen((v) => !v) : undefined}
                  />
                )}
              </div>
            </td>

            {/* Backup — allow wrap for badge + time */}
            <td className="px-3 py-2 align-top">
              {app.backup_enabled ? (
                <div className="flex flex-col gap-1">
                  <div>
                    <span className="text-[10px] text-gray-500 mr-1">B</span>
                    {nr.last_backup_error ? (
                      <span
                        className="inline-flex items-center px-1.5 py-0.5 rounded text-[11px] font-medium text-white bg-red-600"
                        title={nr.last_backup_error}
                      >
                        Failed
                      </span>
                    ) : (
                      <span
                        className={`inline-flex items-center px-1.5 py-0.5 rounded text-[11px] font-medium text-white ${backupColor}`}
                        title={nr.last_backup_at ? `Last: ${new Date(nr.last_backup_at).toLocaleString()}` : "Never backed up"}
                      >
                        {backupLabel}
                      </span>
                    )}
                    {nr.last_backup_at && (
                      <span
                        className={`ml-1 text-[11px] ${nr.last_backup_error ? "text-red-400" : nr.backup_status === "needs_backup" ? "text-orange-400" : "text-gray-500"}`}
                      >
                        {timeAgo(nr.last_backup_at)}
                      </span>
                    )}
                  </div>
                  <div>
                    <span className="text-[10px] text-gray-500 mr-1">P</span>
                    {nr.last_prune_error ? (
                      <span
                        className="inline-flex items-center px-1.5 py-0.5 rounded text-[11px] font-medium text-white bg-red-600"
                        title={nr.last_prune_error}
                      >
                        Failed
                      </span>
                    ) : (
                      <span
                        className={`inline-flex items-center px-1.5 py-0.5 rounded text-[11px] font-medium text-white ${pruneStatusColors[nr.prune_status] || "bg-gray-500"}`}
                        title={nr.last_prune_at ? `Last: ${new Date(nr.last_prune_at).toLocaleString()}` : "Never pruned"}
                      >
                        {pruneStatusLabels[nr.prune_status] || "-"}
                      </span>
                    )}
                    {nr.last_prune_at && (
                      <span
                        className={`ml-1 text-[11px] ${nr.last_prune_error ? "text-red-400" : nr.prune_status === "needs_prune" ? "text-orange-400" : "text-gray-500"}`}
                      >
                        {timeAgo(nr.last_prune_at)}
                      </span>
                    )}
                  </div>
                </div>
              ) : (
                <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[11px] font-medium text-gray-400 bg-gray-700">
                  Off
                </span>
              )}
            </td>

            {/* Connections */}
            <td className="px-3 py-2 whitespace-nowrap align-top">
              {nr.active_connections ? (
                <span className="inline-flex items-center gap-1 text-xs text-green-400" title="Active TCP connections">
                  <span className="w-2 h-2 rounded-full bg-green-400" />
                  <span>now</span>
                </span>
              ) : nr.port > 0 ? (
                <span className="inline-flex items-center gap-1 text-xs text-gray-500" title={nr.last_active_at ? `Last active: ${new Date(nr.last_active_at).toLocaleString()}` : "No activity recorded"}>
                  <span className="w-2 h-2 rounded-full bg-gray-600" />
                  {nr.last_active_at ? (
                    <span>{timeAgo(nr.last_active_at)}</span>
                  ) : (
                    <span className="text-gray-600">never</span>
                  )}
                </span>
              ) : (
                <span className="text-gray-600 text-xs">-</span>
              )}
            </td>

            {/* Actions — allow wrap */}
            <td className="px-3 py-2 align-top">
              <div className="flex flex-wrap items-center gap-1">
                {nr.last_error && (
                  <span className="text-red-400 text-[11px] break-all" title={nr.last_error}>
                    {nr.last_error.length > 30 ? nr.last_error.slice(0, 30) + "…" : nr.last_error}
                  </span>
                )}
                <button
                  onClick={() => restartMutation.mutate({ appId: app.app_id, node: nr.node })}
                  disabled={restartMutation.isPending}
                  className="px-1.5 py-0.5 text-[11px] bg-gray-700 hover:bg-gray-600 disabled:opacity-50 rounded text-gray-300"
                  title="Clear status and restart app"
                >
                  Restart
                </button>
                {onShowLogs && (
                  <button
                    onClick={() => onShowLogs(nr.node, app.app_id)}
                    className="px-1.5 py-0.5 text-[11px] bg-gray-700 hover:bg-gray-600 rounded text-gray-300"
                  >
                    Logs
                  </button>
                )}
                {app.backup_enabled && !BACKUP_PHASES.includes(nr.phase) && !nr.post_backup_phase && nr.observed_state === "running" && (
                  <button
                    onClick={() => backupMutation.mutate({ appId: app.app_id, node: nr.node })}
                    disabled={backupMutation.isPending}
                    className="px-1.5 py-0.5 text-[11px] bg-amber-700 hover:bg-amber-600 disabled:opacity-50 rounded text-white"
                  >
                    Backup
                  </button>
                )}
                {app.backup_enabled && !BACKUP_PHASES.includes(nr.phase) && !nr.post_backup_phase && nr.observed_state === "running" && (
                  <button
                    onClick={() => pruneMutation.mutate({ appId: app.app_id, node: nr.node })}
                    disabled={pruneMutation.isPending}
                    className="px-1.5 py-0.5 text-[11px] bg-purple-700 hover:bg-purple-600 disabled:opacity-50 rounded text-white"
                  >
                    Prune
                  </button>
                )}
                {isFirst && app.backup_enabled && (
                  <button
                    onClick={() => restoreMutation.mutate({ appId: app.app_id, enabled: !app.restore_enabled })}
                    disabled={restoreMutation.isPending}
                    className={`px-1.5 py-0.5 text-[11px] rounded disabled:opacity-50 ${
                      app.restore_enabled
                        ? "bg-gray-700 hover:bg-gray-600 text-gray-300"
                        : "bg-yellow-700 hover:bg-yellow-600 text-yellow-200"
                    }`}
                    title={app.restore_enabled ? "Disable restore (force local data)" : "Re-enable restore from backup"}
                  >
                    {app.restore_enabled ? "No Restore" : "Restore On"}
                  </button>
                )}
              </div>
            </td>
          </tr>
        );
      })}
      {phaseLogOpen && streamableNode && (
        <tr>
          <td colSpan={9} className="p-0">
            <PhaseLogOverlay
              appId={app.app_id}
              node={streamableNode.node}
              phase={streamableNode.phase}
              onClose={() => { setPhaseLogOpen(false); setRealtimeProgress(null); streamProgressRef.current = newProgressAccumulator(); }}
              onProgress={handleProgress}
            />
          </td>
        </tr>
      )}
      {postBackupLogOpen && postBackupStreamableNode && postBackupStreamableNode.post_backup_phase && (
        <tr>
          <td colSpan={9} className="p-0">
            <PhaseLogOverlay
              appId={app.app_id}
              node={postBackupStreamableNode.node}
              phase={postBackupStreamableNode.post_backup_phase}
              onClose={() => { setPostBackupLogOpen(false); postBackupProgressRef.current = newProgressAccumulator(); }}
            />
          </td>
        </tr>
      )}
    </>
  );
}

function GlobalIcon() {
  return (
    <span title="Global app (runs on all nodes)" className="shrink-0 text-blue-400">
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" className="w-3.5 h-3.5">
        <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM4.332 8.027a6.012 6.012 0 011.912-2.706C6.512 5.73 6.974 6 7.5 6A1.5 1.5 0 019 7.5V8a2 2 0 004 0 2 2 0 011.523-1.943A5.977 5.977 0 0116 10c0 .34-.028.675-.083 1H15a2 2 0 00-2 2v2.197A5.973 5.973 0 0110 16v-2a2 2 0 00-2-2 2 2 0 01-2-2 2 2 0 00-1.668-1.973z" clipRule="evenodd" />
      </svg>
    </span>
  );
}

function UpcomingTag({ started, kind }: { started?: boolean; kind: "backup" | "prune" }) {
	const label = kind === "backup" ? (started ? "BACKING UP" : "BACKUP NEXT") : (started ? "PRUNING" : "PRUNE NEXT");
  return (
    <span
      className={`shrink-0 inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium text-white ${kind === "backup" ? "bg-amber-600" : "bg-purple-600"}`}
      title={kind === "backup" ? (started ? "Backup in progress" : "Backup scheduled by manager") : (started ? "Prune in progress" : "Prune scheduled by manager")}
    >
      {label}
    </span>
  );
}

function DesiredToggle({
  app,
  mutation,
}: {
  app: AppStatus;
  mutation: ReturnType<typeof useUpdateAppDesiredState>;
}) {
  return (
    <button
      onClick={(e) => {
        e.stopPropagation();
        const newState = app.desired_state === "running" ? "stopped" : "running";
        mutation.mutate({ appId: app.app_id, state: newState });
      }}
      disabled={mutation.isPending}
      className="cursor-pointer hover:opacity-80 disabled:opacity-50 disabled:cursor-not-allowed transition-opacity"
      title={`Toggle to ${app.desired_state === "running" ? "stopped" : "running"}`}
    >
      <DesiredBadge state={app.desired_state} />
    </button>
  );
}
