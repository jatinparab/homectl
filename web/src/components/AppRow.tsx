import { useState, useEffect, useRef, useCallback } from "react";
import type { AppStatus, ResticProgress, UpcomingBackup, UpcomingPrune, Agent } from "../api/types";
import { useRestartApp, useTriggerBackup, useTriggerPrune, useUpdateAppDesiredState, useUpdateAppRestoreEnabled, useReassignApp, useClearReassignment } from "../api/hooks";
import { decodeStreamMessage, openStreamSocket } from "../api/ws";
import {
  StatusBadge,
  HealthBadge,
  PhaseBadge,
  DesiredToggle,
  PostBackupPhaseBadge,
} from "./StatusBadge";
import { PhaseLogOverlay } from "./PullLogOverlay";

const BACKUP_PHASES = ["backup_stopping", "backup_stopped", "backing_up"];
const POST_BACKUP_PHASES = ["backup_pruning", "backup_checking"];

function formatInterval(interval: string): string {
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
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h`;
  const days = Math.floor(hours / 24);
  return `${days}d`;
}

interface AppRowProps {
  app: AppStatus;
  isUpcomingBackup?: boolean;
  upcomingBackup?: UpcomingBackup;
  isUpcomingPrune?: boolean;
  upcomingPrune?: UpcomingPrune;
  onShowLogs?: (node: string, appId: string) => void;
  agents?: Agent[];
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
  if (totalFiles > 0) percent = Math.min(1, Math.max(0, filesDone / totalFiles));
  else if (totalBytes > 0) percent = Math.min(1, Math.max(0, bytesDone / totalBytes));
  return { ...current, percent_done: percent, total_files: totalFiles, files_done: filesDone, total_bytes: totalBytes, bytes_done: bytesDone };
}

export function AppRow({ app, isUpcomingBackup, upcomingBackup, isUpcomingPrune, upcomingPrune, onShowLogs, agents }: AppRowProps) {
  const restartMutation = useRestartApp();
  const backupMutation = useTriggerBackup();
  const pruneMutation = useTriggerPrune();
  const desiredStateMutation = useUpdateAppDesiredState();
  const restoreMutation = useUpdateAppRestoreEnabled();
  const reassignMutation = useReassignApp();
  const clearReassignMutation = useClearReassignment();

  const onlineAgents = agents?.filter((a) => a.online) ?? [];
  const [openKebab, setOpenKebab] = useState<string | null>(null);
  const [openNodeMenu, setOpenNodeMenu] = useState<string | null>(null);

  useEffect(() => {
    if (!openKebab && !openNodeMenu) return;
    const handler = () => { setOpenKebab(null); setOpenNodeMenu(null); };
    document.addEventListener("click", handler);
    return () => document.removeEventListener("click", handler);
  }, [openKebab, openNodeMenu]);

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

  useEffect(() => {
    if (!isStreamable && prevStreamable.current) {
      setPhaseLogOpen(false);
      setRealtimeProgress(null);
      streamProgressRef.current = newProgressAccumulator();
    }
    prevStreamable.current = isStreamable;
  }, [isStreamable]);

  useEffect(() => {
    if (!streamableNode) return;
    if (!(streamableNode.phase === "backing_up" || streamableNode.phase === "restoring")) {
      setRealtimeProgress(null);
      streamProgressRef.current = newProgressAccumulator();
      return;
    }
    streamProgressRef.current = newProgressAccumulator();
    const path = `/api/v1/apps/${encodeURIComponent(app.app_id)}/phase-logs?node=${encodeURIComponent(streamableNode.node)}`;
    const ws = openStreamSocket(path);
    ws.onmessage = (e) => {
      const line = decodeStreamMessage(e.data);
      if (line == null) return;
      if (!line.startsWith("__PROGRESS__:")) return;
      try {
        const p = JSON.parse(line.slice("__PROGRESS__:".length)) as ResticProgress;
        setRealtimeProgress(accumulateResticProgress(streamProgressRef.current, p));
      } catch { /* ignore */ }
    };
    ws.onerror = () => ws.close();
    return () => ws.close();
  }, [app.app_id, streamableNode?.node, streamableNode?.phase]);

  useEffect(() => {
    if (!isPostBackupStreamable && prevPostBackupStreamable.current) {
      setPostBackupLogOpen(false);
      setRealtimePostBackupProgress(null);
      postBackupProgressRef.current = newProgressAccumulator();
    }
    prevPostBackupStreamable.current = isPostBackupStreamable;
  }, [isPostBackupStreamable]);

  useEffect(() => {
    if (!postBackupStreamableNode) return;
    const phase = postBackupStreamableNode.post_backup_phase;
    if (!phase) {
      setRealtimePostBackupProgress(null);
      postBackupProgressRef.current = newProgressAccumulator();
      return;
    }
    postBackupProgressRef.current = newProgressAccumulator();
    const path = `/api/v1/apps/${encodeURIComponent(app.app_id)}/phase-logs?node=${encodeURIComponent(postBackupStreamableNode.node)}`;
    const ws = openStreamSocket(path);
    ws.onmessage = (e) => {
      const line = decodeStreamMessage(e.data);
      if (line == null) return;
      if (!line.startsWith("__PROGRESS__:")) return;
      try {
        const p = JSON.parse(line.slice("__PROGRESS__:".length)) as ResticProgress;
        setRealtimePostBackupProgress(accumulateResticProgress(postBackupProgressRef.current, p));
      } catch { /* ignore */ }
    };
    ws.onerror = () => ws.close();
    return () => ws.close();
  }, [app.app_id, postBackupStreamableNode?.node, postBackupStreamableNode?.post_backup_phase]);

  const handleProgress = useCallback((p: ResticProgress) => {
    setRealtimeProgress(accumulateResticProgress(streamProgressRef.current, p));
  }, []);

  const rowCount = Math.max(nodeReports.length, 1);

  if (nodeReports.length === 0) {
    return (
      <tr className="app-row border-b border-[#f0ede8]">
        <td className="px-4 py-2.5">
          <AppNameCell app={app} isUpcomingBackup={isUpcomingBackup} upcomingBackup={upcomingBackup} isUpcomingPrune={isUpcomingPrune} upcomingPrune={upcomingPrune} />
        </td>
        <td className="px-3 py-2.5">
          <DesiredToggle
            state={app.desired_state}
            onToggle={() => desiredStateMutation.mutate({ appId: app.app_id, state: app.desired_state === "running" ? "stopped" : "running" })}
            disabled={desiredStateMutation.isPending}
          />
        </td>
        <td className="px-3 py-2.5 text-stone-300 text-xs">—</td>
        <td className="px-3 py-2.5 text-stone-300 text-xs">—</td>
        <td className="px-3 py-2.5 text-stone-300 text-xs">—</td>
        <td className="px-3 py-2.5 text-stone-300 text-xs">—</td>
        <td className="px-3 py-2.5 text-stone-300 text-xs">—</td>
        <td className="px-3 py-2.5 text-stone-300 text-xs">—</td>
        <td className="px-3 py-2.5"></td>
      </tr>
    );
  }

  return (
    <>
      {nodeReports.map((nr, i) => {
        const isFirst = i === 0;
        const isLast = i === rowCount - 1;
        const otherAgents = onlineAgents.filter((a) => a.name !== nr.node);

        return (
          <tr
            key={`${app.app_id}-${nr.node}`}
            className={`app-row ${isLast ? "border-b border-[#f0ede8]" : ""}`}
          >
            {/* App name — only first row */}
            <td className={`px-4 py-2.5 align-top ${!isFirst ? "border-t border-[#f7f5f0]" : ""}`}>
              {isFirst && (
                <AppNameCell
                  app={app}
                  isUpcomingBackup={isUpcomingBackup}
                  upcomingBackup={upcomingBackup}
                  isUpcomingPrune={isUpcomingPrune}
                  upcomingPrune={upcomingPrune}
                  restoreMutation={restoreMutation}
                />
              )}
            </td>

            {/* Desired toggle — only first row */}
            <td className={`px-3 py-2.5 align-top ${!isFirst ? "border-t border-[#f7f5f0]" : ""}`}>
              {isFirst && (
                <DesiredToggle
                  state={app.desired_state}
                  onToggle={() => desiredStateMutation.mutate({ appId: app.app_id, state: app.desired_state === "running" ? "stopped" : "running" })}
                  disabled={desiredStateMutation.isPending}
                />
              )}
            </td>

            {/* Node + Move inline */}
            <td className="px-3 py-2.5 align-top">
              <div className="flex items-center gap-1.5">
                <span className={`text-xs font-medium ${nr.node_online ? "text-emerald-700" : "text-stone-400"}`}>
                  {nr.node}
                </span>
                {!app.is_global && nodeReports.length > 1 && app.assigned_agent === nr.node && (
                  <span className="text-[10px] text-blue-500" title="Assigned node">★</span>
                )}
                {nr.quarantined && (
                  <span className="text-[10px] text-amber-500 border border-amber-300 px-1 rounded" title="Quarantined">Q</span>
                )}
                {nr.restart_count > 0 && (
                  <span className="text-[10px] text-stone-400" title={`Restarted ${nr.restart_count} times`}>×{nr.restart_count}</span>
                )}
                {/* Inline move dropdown */}
                {!app.is_global && otherAgents.length > 0 && (
                  <div className="relative" onClick={(e) => e.stopPropagation()}>
                    <button
                      onClick={() => setOpenNodeMenu(openNodeMenu === nr.node ? null : nr.node)}
                      className="text-[10px] text-stone-400 hover:text-teal-600 px-1 py-0.5 rounded hover:bg-teal-50 transition-colors"
                      title="Move to another node"
                    >
                      ⇄
                    </button>
                    {openNodeMenu === nr.node && (
                      <div className="absolute left-0 top-5 z-50 min-w-[120px] bg-white border border-[#e2ddd7] rounded shadow-lg py-1">
                        {otherAgents.map((a) => (
                          <button
                            key={a.name}
                            onClick={() => { reassignMutation.mutate({ appId: app.app_id, node: a.name }); setOpenNodeMenu(null); }}
                            disabled={reassignMutation.isPending}
                            className="w-full text-left px-3 py-1.5 text-xs text-stone-600 hover:bg-[#f0ede8] disabled:opacity-50"
                          >
                            move → {a.name}
                          </button>
                        ))}
                        {app.manually_assigned && (
                          <button
                            onClick={() => { clearReassignMutation.mutate(app.app_id); setOpenNodeMenu(null); }}
                            disabled={clearReassignMutation.isPending}
                            className="w-full text-left px-3 py-1.5 text-xs text-amber-600 hover:bg-[#f0ede8] disabled:opacity-50 border-t border-[#f0ede8] mt-0.5 pt-1.5"
                          >
                            clear assignment
                          </button>
                        )}
                      </div>
                    )}
                  </div>
                )}
              </div>
            </td>

            {/* Status */}
            <td className="px-3 py-2.5 align-top whitespace-nowrap">
              <StatusBadge status={nr.observed_state} />
            </td>

            {/* Health dot */}
            <td className="px-3 py-2.5 align-top">
              <HealthBadge health={nr.health} />
            </td>

            {/* Phase */}
            <td className="px-3 py-2.5 align-top">
              <div className="flex flex-wrap items-center gap-1.5">
                {nr.phase ? (
                  STREAMABLE_PHASES.includes(nr.phase) ? (
                    <button
                      onClick={() => setPhaseLogOpen((v) => !v)}
                      className="hover:opacity-70 cursor-pointer"
                      title="Toggle phase logs"
                    >
                      <PhaseBadge phase={nr.phase} progress={realtimeProgress ?? nr.progress} />
                    </button>
                  ) : (
                    <PhaseBadge phase={nr.phase} progress={nr.progress} />
                  )
                ) : (
                  <span className="text-stone-300 text-xs">—</span>
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

            {/* Backup — icon + info */}
            <td className="px-3 py-2.5 align-top">
              <BackupCell app={app} nr={nr} />
            </td>

            {/* Connections */}
            <td className="px-3 py-2.5 align-top whitespace-nowrap">
              {nr.active_connections ? (
                <div className="tooltip-container inline-flex items-center gap-1">
                  <span className="w-2 h-2 rounded-full bg-emerald-500 health-glow" />
                  <span className="text-xs text-emerald-600">active</span>
                </div>
              ) : nr.port > 0 ? (
                <div className="tooltip-container inline-flex items-center gap-1">
                  <span className="w-2 h-2 rounded-full bg-stone-300" />
                  <span className="text-xs text-stone-400">
                    {nr.last_active_at ? timeAgo(nr.last_active_at) : 'never'}
                  </span>
                  {nr.last_active_at && (
                    <span className="tooltip-content">{new Date(nr.last_active_at).toLocaleString()}</span>
                  )}
                </div>
              ) : (
                <span className="text-stone-300 text-xs">—</span>
              )}
            </td>

            {/* Actions kebab */}
            <td className="px-3 py-2.5 align-top">
              <div className="relative flex items-center gap-1.5">
                {nr.last_error && (
                  <div className="tooltip-container inline-flex">
                    <span className="text-red-500 text-[10px]">!</span>
                    <span className="tooltip-content">{nr.last_error}</span>
                  </div>
                )}
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    setOpenKebab(openKebab === nr.node ? null : nr.node);
                  }}
                  className="px-1.5 py-0.5 text-stone-400 hover:text-stone-700 hover:bg-[#f0ede8] rounded text-sm leading-none transition-colors"
                  title="Actions"
                >
                  ···
                </button>
                {openKebab === nr.node && (
                  <div
                    className="absolute right-0 top-6 z-50 min-w-[130px] bg-white border border-[#e2ddd7] rounded shadow-lg py-1"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <button
                      onClick={() => { restartMutation.mutate({ appId: app.app_id, node: nr.node }); setOpenKebab(null); }}
                      disabled={restartMutation.isPending}
                      className="w-full text-left px-3 py-1.5 text-xs text-stone-600 hover:bg-[#f0ede8] disabled:opacity-50"
                    >
                      Restart
                    </button>
                    {onShowLogs && (
                      <button
                        onClick={() => { onShowLogs(nr.node, app.app_id); setOpenKebab(null); }}
                        className="w-full text-left px-3 py-1.5 text-xs text-stone-600 hover:bg-[#f0ede8]"
                      >
                        App logs
                      </button>
                    )}
                    {app.backup_enabled && !BACKUP_PHASES.includes(nr.phase) && !nr.post_backup_phase && nr.observed_state === "running" && (
                      <button
                        onClick={() => { backupMutation.mutate({ appId: app.app_id, node: nr.node }); setOpenKebab(null); }}
                        disabled={backupMutation.isPending}
                        className="w-full text-left px-3 py-1.5 text-xs text-amber-600 hover:bg-[#f0ede8] disabled:opacity-50"
                      >
                        Backup now
                      </button>
                    )}
                    {app.backup_enabled && !BACKUP_PHASES.includes(nr.phase) && !nr.post_backup_phase && nr.observed_state === "running" && (
                      <button
                        onClick={() => { pruneMutation.mutate({ appId: app.app_id, node: nr.node }); setOpenKebab(null); }}
                        disabled={pruneMutation.isPending}
                        className="w-full text-left px-3 py-1.5 text-xs text-violet-600 hover:bg-[#f0ede8] disabled:opacity-50"
                      >
                        Prune now
                      </button>
                    )}
                    {isFirst && app.backup_enabled && (
                      <button
                        onClick={() => { restoreMutation.mutate({ appId: app.app_id, enabled: !app.restore_enabled }); setOpenKebab(null); }}
                        disabled={restoreMutation.isPending}
                        className={`w-full text-left px-3 py-1.5 text-xs hover:bg-[#f0ede8] disabled:opacity-50 ${app.restore_enabled ? "text-stone-500" : "text-amber-600"}`}
                      >
                        {app.restore_enabled ? "Disable restore" : "Enable restore"}
                      </button>
                    )}
                  </div>
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

// ── Sub-components ─────────────────────────────────────────────────

interface AppNameCellProps {
  app: AppStatus;
  isUpcomingBackup?: boolean;
  upcomingBackup?: UpcomingBackup;
  isUpcomingPrune?: boolean;
  upcomingPrune?: UpcomingPrune;
  restoreMutation?: ReturnType<typeof useUpdateAppRestoreEnabled>;
}

function AppNameCell({ app, isUpcomingBackup, upcomingBackup, isUpcomingPrune, upcomingPrune, restoreMutation }: AppNameCellProps) {
  return (
    <div className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5">
      <span className="font-semibold text-[#1c1917] text-sm truncate">{app.app_id}</span>
      {app.is_global && (
        <span className="tooltip-container inline-flex">
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" className="w-3.5 h-3.5 text-blue-400">
            <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM4.332 8.027a6.012 6.012 0 011.912-2.706C6.512 5.73 6.974 6 7.5 6A1.5 1.5 0 019 7.5V8a2 2 0 004 0 2 2 0 011.523-1.943A5.977 5.977 0 0116 10c0 .34-.028.675-.083 1H15a2 2 0 00-2 2v2.197A5.973 5.973 0 0110 16v-2a2 2 0 00-2-2 2 2 0 01-2-2 2 2 0 00-1.668-1.973z" clipRule="evenodd" />
          </svg>
          <span className="tooltip-content">Global — runs on all nodes</span>
        </span>
      )}
      {app.manually_assigned && (
        <span className="tooltip-container inline-flex">
          <span className="text-[10px] text-teal-500">⊕</span>
          <span className="tooltip-content">Manually assigned — reconciler will not rebalance</span>
        </span>
      )}
      {app.backup_enabled && isUpcomingBackup && (
        <span className={`text-[10px] font-medium px-1.5 py-0.5 rounded ${upcomingBackup?.started ? "bg-amber-100 text-amber-700" : "bg-amber-50 text-amber-600"}`}>
          {upcomingBackup?.started ? "backing up" : "backup next"}
        </span>
      )}
      {app.backup_enabled && isUpcomingPrune && (
        <span className={`text-[10px] font-medium px-1.5 py-0.5 rounded ${upcomingPrune?.started ? "bg-violet-100 text-violet-700" : "bg-violet-50 text-violet-600"}`}>
          {upcomingPrune?.started ? "pruning" : "prune next"}
        </span>
      )}
      {app.backup_enabled && !app.restore_enabled && restoreMutation && (
        <button
          className="text-[10px] font-medium bg-amber-50 text-amber-600 border border-amber-200 px-1.5 py-0.5 rounded hover:bg-amber-100"
          title="Restore disabled — using local data. Click to re-enable."
          onClick={() => restoreMutation.mutate({ appId: app.app_id, enabled: true })}
        >
          local
        </button>
      )}
      {app.backup_sensitive && (
        <span
          className="text-[10px] font-medium bg-violet-50 text-violet-600 border border-violet-200 px-1.5 py-0.5 rounded"
          title="Backup-sensitive: waits for no active connections"
        >
          sensitive
        </span>
      )}
    </div>
  );
}

function BackupCell({ app, nr }: { app: AppStatus; nr: { backup_enabled?: boolean; backup_status: string; last_backup_at?: string; last_backup_error?: string } }) {
  if (!app.backup_enabled) {
    return (
      <div className="tooltip-container inline-flex items-center">
        <span className="text-stone-300 text-xs">—</span>
        <span className="tooltip-content">backups disabled</span>
      </div>
    );
  }

  const hasError = !!nr.last_backup_error;
  const status = nr.backup_status;

  let icon = "✓";
  let iconColor = "text-emerald-500";
  let tooltipText = nr.last_backup_at
    ? `Last backup: ${new Date(nr.last_backup_at).toLocaleString()}`
    : "Never backed up";

  if (hasError) {
    icon = "✕";
    iconColor = "text-red-500";
    tooltipText = nr.last_backup_error ?? "Backup failed";
  } else if (status === "needs_backup") {
    icon = "○";
    iconColor = "text-amber-500";
  } else if (status === "never") {
    icon = "—";
    iconColor = "text-stone-400";
  }

  const interval = app.backup_interval ? formatInterval(app.backup_interval) : null;

  return (
    <div className="tooltip-container inline-flex items-center gap-1.5">
      <span className={`text-sm font-medium ${iconColor}`}>{icon}</span>
      {nr.last_backup_at && !hasError && (
        <span className={`text-[11px] ${status === "needs_backup" ? "text-amber-500" : "text-stone-400"}`}>
          {timeAgo(nr.last_backup_at)}
        </span>
      )}
      {interval && (
        <span className="text-[10px] text-stone-300">/{interval}</span>
      )}
      <span className="tooltip-content">{tooltipText}</span>
    </div>
  );
}
