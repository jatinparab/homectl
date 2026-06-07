import { useState, useEffect, useRef, useCallback } from 'react';
import styled from '@emotion/styled';
import type { AppStatus, ResticProgress, UpcomingBackup, UpcomingPrune, Agent } from '../../../api/types';
import {
  useRestartApp,
  useTriggerBackup,
  useTriggerPrune,
  useUpdateAppDesiredState,
  useUpdateAppRestoreEnabled,
  useReassignApp,
  useClearReassignment,
} from '../../../api/hooks';
import { useStreamSubscription } from '../../../api/live';
import { StatusBadge, HealthBadge, PhaseBadge, DesiredToggle, PostBackupPhaseBadge } from '../../shared/components/StatusBadge';
import { PhaseLogOverlay } from '../../shared/components/PullLogOverlay';
import { Button } from '../../../components/ui';
import { Tooltip } from '../../../components/ui/Tooltip';

const BACKUP_PHASES = ['backup_stopping', 'backup_stopped', 'backing_up'];
const POST_BACKUP_PHASES = ['backup_pruning', 'backup_checking'];
const STREAMABLE_PHASES = ['pulling', 'restoring', 'backing_up'];

// ── Utility ────────────────────────────────

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
  return {
    ...current,
    percent_done: percent,
    total_files: totalFiles,
    files_done: filesDone,
    total_bytes: totalBytes,
    bytes_done: bytesDone,
  };
}

// ── Styled ─────────────────────────────────

const Td = styled.td<{ $topBorder?: boolean }>`
  padding: 10px 12px;
  vertical-align: top;
  ${(p) => p.$topBorder && `border-top: 1px solid ${p.theme.colors.borderLight};`}
`;

const Row = styled.tr`
  border-bottom: 1px solid ${(p) => p.theme.colors.borderLight};
`;

const NodeName = styled.span<{ $online: boolean }>`
  font-size: ${(p) => p.theme.fontSizes.xs};
  font-weight: 500;
  color: ${(p) => (p.$online ? '#047857' : '#a8a29e')};
`;

const StarMark = styled.span`
  font-size: 10px;
  color: #3b82f6;
`;

const QuarantineMark = styled.span`
  font-size: 10px;
  color: #f59e0b;
  border: 1px solid #fcd34d;
  padding: 0 4px;
  border-radius: ${(p) => p.theme.radii.sm};
`;

const RestartCount = styled.span`
  font-size: 10px;
  color: ${(p) => p.theme.colors.textMuted};
`;

const NodeGroup = styled.div`
  display: flex;
  align-items: center;
  gap: 6px;
`;

const MoveMenu = styled.div`
  position: absolute;
  left: 0;
  bottom: 20px;
  z-index: 120;
  min-width: 140px;
  background: ${(p) => p.theme.colors.neutralSurface};
  border: 1px solid ${(p) => p.theme.colors.border};
  border-radius: ${(p) => p.theme.radii.md};
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
  padding: 4px 0;
`;

const MoveMenuWrapper = styled.div`
  position: relative;
`;

const PhaseRow = styled.div`
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
`;

const Dash = styled.span`
  font-size: ${(p) => p.theme.fontSizes.xs};
  color: #d6d3d1;
`;

const ActiveDot = styled.span`
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #10b981;
  box-shadow: 0 0 6px rgba(16, 185, 129, 0.5);
`;

const InactiveDot = styled.span`
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #d6d3d1;
`;

const ConnText = styled.span<{ $active?: boolean }>`
  font-size: ${(p) => p.theme.fontSizes.xs};
  color: ${(p) => (p.$active ? '#059669' : '#a8a29e')};
`;

const ConnGroup = styled.div`
  display: flex;
  align-items: center;
  gap: 4px;
  white-space: nowrap;
`;

const KebabMenu = styled.div`
  position: absolute;
  right: 0;
  top: 24px;
  z-index: 50;
  min-width: 130px;
  background: ${(p) => p.theme.colors.neutralSurface};
  border: 1px solid ${(p) => p.theme.colors.border};
  border-radius: ${(p) => p.theme.radii.md};
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
  padding: 4px 0;
`;

const ErrorDot = styled.div`
  position: relative;
  display: inline-flex;
  color: #ef4444;
  font-size: 10px;
`;

const ActionsWrap = styled.div`
  position: relative;
  display: flex;
  align-items: center;
  gap: 6px;
`;

const TooltipWrap = styled.div`
  display: inline-flex;
  align-items: center;
  gap: 4px;
`;

// Below this will be imported — defined above to use in AppRow

// ── App name sub-component ─────────────────

interface AppNameCellProps {
  app: AppStatus;
  isUpcomingBackup?: boolean;
  upcomingBackup?: UpcomingBackup;
  isUpcomingPrune?: boolean;
  upcomingPrune?: UpcomingPrune;
  restoreMutation?: ReturnType<typeof useUpdateAppRestoreEnabled>;
  onNavigate?: (path: string) => void;
}

const AppNameWrap = styled.div`
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px 4px;
`;

const AppLink = styled.a`
  font-weight: 600;
  color: ${(p) => p.theme.colors.textPrimary};
  font-size: ${(p) => p.theme.fontSizes.sm};
  text-decoration: none;

  &:hover {
    color: #0f766e;
    text-decoration: underline;
  }
`;

const SvgIcon = styled.svg`
  width: 14px;
  height: 14px;
  color: #60a5fa;
`;

const ManualTag = styled.span`
  font-size: 10px;
  color: #0d9488;
`;

const ScheduleBadge = styled.span<{ $variant: 'amber' | 'violet'; $strong?: boolean }>`
  font-size: 10px;
  font-weight: 500;
  padding: 2px 6px;
  border-radius: ${(p) => p.theme.radii.sm};
  ${(p) =>
    p.$variant === 'amber'
      ? `background: ${p.$strong ? '#fef3c7' : '#fffbeb'}; color: ${p.$strong ? '#b45309' : '#d97706'};`
      : `background: ${p.$strong ? '#ede9fe' : '#f5f3ff'}; color: ${p.$strong ? '#6d28d9' : '#7c3aed'};`}
`;

const SensitiveTag = styled.span`
  font-size: 10px;
  font-weight: 500;
  background: #f5f3ff;
  color: #7c3aed;
  border: 1px solid #ddd6fe;
  padding: 2px 6px;
  border-radius: ${(p) => p.theme.radii.sm};
`;

function AppNameCell({ app, isUpcomingBackup, upcomingBackup, isUpcomingPrune, upcomingPrune, restoreMutation, onNavigate }: AppNameCellProps) {
  return (
    <AppNameWrap>
      <AppLink
        href={`/apps/${app.app_id}`}
        onClick={(e) => {
          e.preventDefault();
          onNavigate?.(`/apps/${app.app_id}`);
        }}
      >
        {app.app_id}
      </AppLink>
      {app.is_global && (
        <Tooltip content="Global — runs on all nodes">
          <TooltipWrap>
            <SvgIcon xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor">
              <path
                fillRule="evenodd"
                d="M10 18a8 8 0 100-16 8 8 0 000 16zM4.332 8.027a6.012 6.012 0 011.912-2.706C6.512 5.73 6.974 6 7.5 6A1.5 1.5 0 019 7.5V8a2 2 0 004 0 2 2 0 011.523-1.943A5.977 5.977 0 0116 10c0 .34-.028.675-.083 1H15a2 2 0 00-2 2v2.197A5.973 5.973 0 0110 16v-2a2 2 0 00-2-2 2 2 0 01-2-2 2 2 0 00-1.668-1.973z"
                clipRule="evenodd"
              />
            </SvgIcon>
          </TooltipWrap>
        </Tooltip>
      )}
      {app.manually_assigned && (
        <Tooltip content="Manually assigned — reconciler will not rebalance">
          <TooltipWrap>
            <ManualTag>⊕</ManualTag>
          </TooltipWrap>
        </Tooltip>
      )}
      {app.backup_enabled && isUpcomingBackup && (
        <ScheduleBadge $variant="amber" $strong={upcomingBackup?.started}>
          {upcomingBackup?.started ? 'backing up' : 'backup next'}
        </ScheduleBadge>
      )}
      {app.backup_enabled && isUpcomingPrune && (
        <ScheduleBadge $variant="violet" $strong={upcomingPrune?.started}>
          {upcomingPrune?.started ? 'pruning' : 'prune next'}
        </ScheduleBadge>
      )}
      {app.backup_enabled && !app.restore_enabled && restoreMutation && (
        <Button
          variant="soft"
          size="sm"
          color="amber"
          title="Restore disabled — using local data. Click to re-enable."
          onClick={() => restoreMutation.mutate({ appId: app.app_id, enabled: true })}
        >
          local
        </Button>
      )}
      {app.backup_sensitive && (
        <Tooltip content="Backup-sensitive: waits for no active connections">
          <TooltipWrap>
            <SensitiveTag>sensitive</SensitiveTag>
          </TooltipWrap>
        </Tooltip>
      )}
    </AppNameWrap>
  );
}

const BackupIcon = styled.span<{ $color: string }>`
  font-size: ${(p) => p.theme.fontSizes.sm};
  font-weight: 500;
  color: ${(p) => p.$color};
`;

const BackupTime = styled.span<{ $warn?: boolean }>`
  font-size: 11px;
  color: ${(p) => (p.$warn ? '#f59e0b' : '#a8a29e')};
`;

const BackupInterval = styled.span`
  font-size: 10px;
  color: #d6d3d1;
`;

function BackupCell({ app, nr }: { app: AppStatus; nr: { backup_enabled?: boolean; backup_status: string; last_backup_at?: string; last_backup_error?: string } }) {
  if (!app.backup_enabled) {
    return (
      <Tooltip content="backups disabled">
        <TooltipWrap>
          <Dash>—</Dash>
        </TooltipWrap>
      </Tooltip>
    );
  }

  const hasError = !!nr.last_backup_error;
  const status = nr.backup_status;

  let icon = '✓';
  let iconColor = '#10b981';
  let tooltipText = nr.last_backup_at ? `Last backup: ${new Date(nr.last_backup_at).toLocaleString()}` : 'Never backed up';

  if (hasError) {
    icon = '✕';
    iconColor = '#ef4444';
    tooltipText = nr.last_backup_error ?? 'Backup failed';
  } else if (status === 'needs_backup') {
    icon = '○';
    iconColor = '#f59e0b';
  } else if (status === 'never') {
    icon = '—';
    iconColor = '#a8a29e';
  }

  const interval = app.backup_interval ? formatInterval(app.backup_interval) : null;

  return (
    <Tooltip content={tooltipText}>
      <TooltipWrap>
        <BackupIcon $color={iconColor}>{icon}</BackupIcon>
        {nr.last_backup_at && !hasError && (
          <BackupTime $warn={status === 'needs_backup'}>{timeAgo(nr.last_backup_at)}</BackupTime>
        )}
        {interval && <BackupInterval>/{interval}</BackupInterval>}
      </TooltipWrap>
    </Tooltip>
  );
}

export function AppRow({ app, isUpcomingBackup, upcomingBackup, isUpcomingPrune, upcomingPrune, onShowLogs, onNavigate, agents }: AppRowProps) {
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
    const handler = () => {
      setOpenKebab(null);
      setOpenNodeMenu(null);
    };
    document.addEventListener('click', handler);
    return () => document.removeEventListener('click', handler);
  }, [openKebab, openNodeMenu]);

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
  const postBackupStreamableNode = nodeReports.find(
    (nr) => nr.post_backup_phase && POST_BACKUP_PHASES.includes(nr.post_backup_phase),
  );
  const isPostBackupStreamable = !!postBackupStreamableNode;

  useEffect(() => {
    if (!isStreamable && prevStreamable.current) {
      setPhaseLogOpen(false);
      setRealtimeProgress(null);
      streamProgressRef.current = newProgressAccumulator();
    }
    prevStreamable.current = isStreamable;
  }, [isStreamable]);

  const shouldStreamPhaseLogs = !!(streamableNode && (streamableNode.phase === 'backing_up' || streamableNode.phase === 'restoring'));

  useEffect(() => {
    if (!shouldStreamPhaseLogs) {
      setRealtimeProgress(null);
      streamProgressRef.current = newProgressAccumulator();
      return;
    }
    streamProgressRef.current = newProgressAccumulator();
  }, [shouldStreamPhaseLogs, streamableNode?.node]);

  const handlePhaseStream = useCallback(
    (line: string, _source?: string, done?: boolean) => {
      if (line?.startsWith('__PROGRESS__:')) {
        try {
          const p = JSON.parse(line.slice('__PROGRESS__:'.length)) as ResticProgress;
          setRealtimeProgress(accumulateResticProgress(streamProgressRef.current, p));
        } catch {
          /* ignore */
        }
      }
      if (done) return;
    },
    [],
  );

  useStreamSubscription(
    shouldStreamPhaseLogs ? 'phase-logs' : null,
    shouldStreamPhaseLogs && streamableNode ? { appId: app.app_id, node: streamableNode.node } : null,
    shouldStreamPhaseLogs ? handlePhaseStream : null,
  );

  useEffect(() => {
    if (!isPostBackupStreamable && prevPostBackupStreamable.current) {
      setPostBackupLogOpen(false);
      setRealtimePostBackupProgress(null);
      postBackupProgressRef.current = newProgressAccumulator();
    }
    prevPostBackupStreamable.current = isPostBackupStreamable;
  }, [isPostBackupStreamable]);

  const postBackupPhase = postBackupStreamableNode?.post_backup_phase;
  const shouldStreamPostBackup = !!(postBackupStreamableNode && postBackupPhase);

  useEffect(() => {
    if (!shouldStreamPostBackup) {
      setRealtimePostBackupProgress(null);
      postBackupProgressRef.current = newProgressAccumulator();
      return;
    }
    postBackupProgressRef.current = newProgressAccumulator();
  }, [shouldStreamPostBackup, postBackupStreamableNode?.node]);

  const handlePostBackupStream = useCallback(
    (line: string, _source?: string, done?: boolean) => {
      if (line?.startsWith('__PROGRESS__:')) {
        try {
          const p = JSON.parse(line.slice('__PROGRESS__:'.length)) as ResticProgress;
          setRealtimePostBackupProgress(accumulateResticProgress(postBackupProgressRef.current, p));
        } catch {
          /* ignore */
        }
      }
      if (done) return;
    },
    [],
  );

  useStreamSubscription(
    shouldStreamPostBackup ? 'phase-logs' : null,
    shouldStreamPostBackup && postBackupStreamableNode ? { appId: app.app_id, node: postBackupStreamableNode.node } : null,
    shouldStreamPostBackup ? handlePostBackupStream : null,
  );

  const handleProgress = useCallback((p: ResticProgress) => {
    setRealtimeProgress(accumulateResticProgress(streamProgressRef.current, p));
  }, []);

  const rowCount = Math.max(nodeReports.length, 1);

  if (nodeReports.length === 0) {
    return (
      <Row>
        <Td>
          <AppNameCell app={app} isUpcomingBackup={isUpcomingBackup} upcomingBackup={upcomingBackup} isUpcomingPrune={isUpcomingPrune} upcomingPrune={upcomingPrune} onNavigate={onNavigate} />
        </Td>
        <Td>
          <DesiredToggle
            state={app.desired_state}
            onToggle={() =>
              desiredStateMutation.mutate({
                appId: app.app_id,
                state: app.desired_state === 'running' ? 'stopped' : 'running',
              })
            }
            disabled={desiredStateMutation.isPending}
          />
        </Td>
        <Td><Dash>—</Dash></Td>
        <Td><Dash>—</Dash></Td>
        <Td><Dash>—</Dash></Td>
        <Td><Dash>—</Dash></Td>
        <Td><Dash>—</Dash></Td>
        <Td><Dash>—</Dash></Td>
        <Td></Td>
      </Row>
    );
  }

  return (
    <>
      {nodeReports.map((nr, i) => {
        const isFirst = i === 0;
        const isLast = i === rowCount - 1;
        const otherAgents = onlineAgents.filter((a) => a.name !== nr.node);

        return (
          <Row key={`${app.app_id}-${nr.node}`} style={!isLast ? { borderBottom: 'none' } : undefined}>
            <Td $topBorder={!isFirst}>
              {isFirst && (
                <AppNameCell
                  app={app}
                  isUpcomingBackup={isUpcomingBackup}
                  upcomingBackup={upcomingBackup}
                  isUpcomingPrune={isUpcomingPrune}
                  upcomingPrune={upcomingPrune}
                  restoreMutation={restoreMutation}
                  onNavigate={onNavigate}
                />
              )}
            </Td>

            <Td $topBorder={!isFirst}>
              {isFirst && (
                <DesiredToggle
                  state={app.desired_state}
                  onToggle={() =>
                    desiredStateMutation.mutate({
                      appId: app.app_id,
                      state: app.desired_state === 'running' ? 'stopped' : 'running',
                    })
                  }
                  disabled={desiredStateMutation.isPending}
                />
              )}
            </Td>

            <Td>
              <NodeGroup>
                <NodeName $online={nr.node_online}>{nr.node}</NodeName>
                {!app.is_global && nodeReports.length > 1 && app.assigned_agent === nr.node && (
                  <Tooltip content="Assigned node">
                    <TooltipWrap>
                      <StarMark>★</StarMark>
                    </TooltipWrap>
                  </Tooltip>
                )}
                {nr.quarantined && <QuarantineMark>Q</QuarantineMark>}
                {nr.restart_count > 0 && <RestartCount>×{nr.restart_count}</RestartCount>}
                {!app.is_global && otherAgents.length > 0 && (
                  <MoveMenuWrapper onClick={(e) => e.stopPropagation()}>
                    <Button variant="ghost" size="sm" onClick={() => setOpenNodeMenu(openNodeMenu === nr.node ? null : nr.node)} title="Move to another node">
                      ⇄
                    </Button>
                    {openNodeMenu === nr.node && (
                      <MoveMenu>
                        {otherAgents.map((a) => (
                          <Button
                            variant="ghost"
                            size="sm"
                            key={a.name}
                            onClick={() => {
                              reassignMutation.mutate({ appId: app.app_id, node: a.name });
                              setOpenNodeMenu(null);
                            }}
                            disabled={reassignMutation.isPending}
                            style={{ width: '100%', textAlign: 'left', justifyContent: 'flex-start' }}
                          >
                            move → {a.name}
                          </Button>
                        ))}
                        {app.manually_assigned && (
                          <Button
                            variant="ghost"
                            size="sm"
                            color="amber"
                            onClick={() => {
                              clearReassignMutation.mutate(app.app_id);
                              setOpenNodeMenu(null);
                            }}
                            disabled={clearReassignMutation.isPending}
                            style={{ width: '100%', textAlign: 'left', justifyContent: 'flex-start' }}
                          >
                            clear assignment
                          </Button>
                        )}
                      </MoveMenu>
                    )}
                  </MoveMenuWrapper>
                )}
              </NodeGroup>
            </Td>

            <Td>
              <StatusBadge status={nr.observed_state} />
            </Td>

            <Td>
              <HealthBadge health={nr.health} />
            </Td>

            <Td>
              <PhaseRow>
                {nr.phase ? (
                  STREAMABLE_PHASES.includes(nr.phase) ? (
                    <button onClick={() => setPhaseLogOpen((v) => !v)} style={{ cursor: 'pointer', border: 'none', background: 'none', padding: 0 }}>
                      <PhaseBadge phase={nr.phase} progress={realtimeProgress ?? nr.progress} />
                    </button>
                  ) : (
                    <PhaseBadge phase={nr.phase} progress={nr.progress} />
                  )
                ) : (
                  <Dash>—</Dash>
                )}
                {nr.post_backup_phase && (
                  <PostBackupPhaseBadge
                    phase={nr.post_backup_phase}
                    progress={realtimePostBackupProgress ?? nr.progress}
                    onClick={
                      isPostBackupStreamable && postBackupStreamableNode?.node === nr.node
                        ? () => setPostBackupLogOpen((v) => !v)
                        : undefined
                    }
                  />
                )}
              </PhaseRow>
            </Td>

            <Td>
              <BackupCell app={app} nr={nr} />
            </Td>

            <Td>
              <ConnGroup>
                {nr.active_connections ? (
                  <>
                    <ActiveDot />
                    <ConnText $active>active</ConnText>
                  </>
                ) : nr.port > 0 ? (
                  nr.last_active_at ? (
                    <Tooltip content={new Date(nr.last_active_at).toLocaleString()}>
                      <TooltipWrap>
                        <InactiveDot />
                        <ConnText>{timeAgo(nr.last_active_at)}</ConnText>
                      </TooltipWrap>
                    </Tooltip>
                  ) : (
                    <>
                      <InactiveDot />
                      <ConnText>never</ConnText>
                    </>
                  )
                ) : (
                  <Dash>—</Dash>
                )}
              </ConnGroup>
            </Td>

            <Td>
              <ActionsWrap>
                {nr.last_error && (
                  <Tooltip content={nr.last_error}>
                    <TooltipWrap>
                      <ErrorDot>!</ErrorDot>
                    </TooltipWrap>
                  </Tooltip>
                )}
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={(e) => {
                    e.stopPropagation();
                    setOpenKebab(openKebab === nr.node ? null : nr.node);
                  }}
                  title="Actions"
                >
                  ···
                </Button>
                {openKebab === nr.node && (
                  <KebabMenu onClick={(e) => e.stopPropagation()}>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        restartMutation.mutate({ appId: app.app_id, node: nr.node });
                        setOpenKebab(null);
                      }}
                      disabled={restartMutation.isPending}
                      style={{ width: '100%', textAlign: 'left', justifyContent: 'flex-start' }}
                    >
                      Restart
                    </Button>
                    {onShowLogs && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => {
                          onShowLogs(nr.node, app.app_id);
                          setOpenKebab(null);
                        }}
                        style={{ width: '100%', textAlign: 'left', justifyContent: 'flex-start' }}
                      >
                        App logs
                      </Button>
                    )}
                    {app.backup_enabled &&
                      !BACKUP_PHASES.includes(nr.phase) &&
                      !nr.post_backup_phase &&
                      nr.observed_state === 'running' && (
                        <>
                          <Button
                            variant="ghost"
                            size="sm"
                            color="amber"
                            onClick={() => {
                              backupMutation.mutate({ appId: app.app_id, node: nr.node });
                              setOpenKebab(null);
                            }}
                            disabled={backupMutation.isPending}
                            style={{ width: '100%', textAlign: 'left', justifyContent: 'flex-start' }}
                          >
                            Backup now
                          </Button>
                          <Button
                            variant="ghost"
                            size="sm"
                            color="violet"
                            onClick={() => {
                              pruneMutation.mutate({ appId: app.app_id, node: nr.node });
                              setOpenKebab(null);
                            }}
                            disabled={pruneMutation.isPending}
                            style={{ width: '100%', textAlign: 'left', justifyContent: 'flex-start' }}
                          >
                            Prune now
                          </Button>
                        </>
                      )}
                    {isFirst && app.backup_enabled && (
                      <Button
                        variant="ghost"
                        size="sm"
                        color={app.restore_enabled ? 'gray' : 'amber'}
                        onClick={() => {
                          restoreMutation.mutate({ appId: app.app_id, enabled: !app.restore_enabled });
                          setOpenKebab(null);
                        }}
                        disabled={restoreMutation.isPending}
                        style={{ width: '100%', textAlign: 'left', justifyContent: 'flex-start' }}
                      >
                        {app.restore_enabled ? 'Disable restore' : 'Enable restore'}
                      </Button>
                    )}
                  </KebabMenu>
                )}
              </ActionsWrap>
            </Td>
          </Row>
        );
      })}
      {phaseLogOpen && streamableNode && (
        <tr>
          <td colSpan={9} style={{ padding: 0 }}>
            <PhaseLogOverlay
              appId={app.app_id}
              node={streamableNode.node}
              phase={streamableNode.phase}
              onClose={() => {
                setPhaseLogOpen(false);
                setRealtimeProgress(null);
                streamProgressRef.current = newProgressAccumulator();
              }}
              onProgress={handleProgress}
            />
          </td>
        </tr>
      )}
      {postBackupLogOpen && postBackupStreamableNode && postBackupStreamableNode.post_backup_phase && (
        <tr>
          <td colSpan={9} style={{ padding: 0 }}>
            <PhaseLogOverlay
              appId={app.app_id}
              node={postBackupStreamableNode.node}
              phase={postBackupStreamableNode.post_backup_phase}
              onClose={() => {
                setPostBackupLogOpen(false);
                postBackupProgressRef.current = newProgressAccumulator();
              }}
            />
          </td>
        </tr>
      )}
    </>
  );
}

interface AppRowProps {
  app: AppStatus;
  isUpcomingBackup?: boolean;
  upcomingBackup?: UpcomingBackup;
  isUpcomingPrune?: boolean;
  upcomingPrune?: UpcomingPrune;
  onShowLogs?: (node: string, appId: string) => void;
  onNavigate?: (path: string) => void;
  agents?: Agent[];
}
