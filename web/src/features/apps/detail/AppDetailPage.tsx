import { useEffect, useRef, useState, useCallback, useMemo } from 'react';
import styled from '@emotion/styled';
import { useApp, useContainers, useUpdateAppDesiredState, useRestartApp, useTriggerBackup, useSnapshots, useGatewayInfo, useRefreshSnapshots, useSetRestoreSnapshotOverride, useClearRestoreSnapshotOverride } from '../../../api/hooks';
import { HealthBadge, PhaseBadge, DesiredToggle, ContainerStateBadge } from '../../../features/shared/components/StatusBadge';
import { useStreamSubscription } from '../../../api/live';
import type { NodeReport, RestoreSnapshotOverride, Snapshot } from '../../../api/types';
import { Button, Dialog } from '../../../components/ui';

function timeAgo(dateStr?: string): string {
  if (!dateStr) return '—';
  const d = new Date(dateStr);
  const s = Math.floor((Date.now() - d.getTime()) / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}

function shortSnapshotID(override: RestoreSnapshotOverride): string {
  return override.short_id || override.snapshot_id.slice(0, 8);
}

function restoreOverrideText(override: RestoreSnapshotOverride): string {
  const parts = [`snapshot ${shortSnapshotID(override)}`];
  if (override.time) parts.push(`from ${timeAgo(override.time)}`);
  if (override.node) parts.push(`on ${override.node}`);
  return parts.join(' ');
}

// ── Shared styled components ──────────────────

const Page = styled.div`
  min-height: 100vh;
  background: #faf9f7;
`;

const HC = styled.div`
  display: flex;
  flex-direction: column;
  min-height: 100vh;
  background: #faf9f7;
`;

const HeaderBar = styled.div`
  position: sticky;
  top: 0;
  z-index: 30;
  border-bottom: 1px solid ${(p) => p.theme.colors.border};
  background: ${(p) => p.theme.colors.neutralSurface};
  padding: 12px 16px;
  flex-shrink: 0;
`;

const HeaderInner = styled.div`
  max-width: 80rem;
  margin: 0 auto;
  display: flex;
  align-items: center;
  gap: 12px;
`;

const Divider = styled.span`
  width: 1px;
  height: 16px;
  background: ${(p) => p.theme.colors.border};
`;

const AppTitle = styled.h1`
  font-size: ${(p) => p.theme.fontSizes.sm};
  font-weight: 600;
  color: ${(p) => p.theme.colors.textPrimary};
  font-family: ${(p) => p.theme.fonts.mono};
`;

const stateColors: Record<string, string> = {
  running: '#059669',
  error: '#dc2626',
  restarting: '#d97706',
};

const StateLabel = styled.span<{ $state: string }>`
  font-size: ${(p) => p.theme.fontSizes.xs};
  font-weight: 500;
  color: ${(p) => stateColors[p.$state] || '#a8a29e'};
`;

const QuarantineBadge = styled.span`
  font-size: 10px;
  background: #fef2f2;
  color: #b91c1c;
  padding: 2px 6px;
  border-radius: ${(p) => p.theme.radii.sm};
  font-weight: 500;
`;

const HeaderActions = styled.div`
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 8px;
`;

const Content = styled.div`
  flex: 1;
  max-width: 80rem;
  width: 100%;
  margin: 0 auto;
  padding: 16px;
  display: grid;
  grid-template-columns: 300px 1fr;
  gap: 16px;
  min-height: 0;
`;

const LeftCol = styled.div`
  display: flex;
  flex-direction: column;
  gap: 12px;
  overflow-y: auto;
  min-height: 0;
`;

const RightCol = styled.div`
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 0;
`;

const SectionWrap = styled.div`
  background: ${(p) => p.theme.colors.neutralSurface};
  border: 1px solid ${(p) => p.theme.colors.border};
  border-radius: ${(p) => p.theme.radii.lg};
  padding: 12px;
`;

const SectionTitle = styled.div`
  font-size: 10px;
  font-weight: 600;
  color: #a8a29e;
  text-transform: uppercase;
  letter-spacing: 0.1em;
  margin-bottom: 8px;
`;

const NoData = styled.p`
  font-size: 11px;
  color: #a8a29e;
`;

const DL = styled.dl`
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 6px 8px;
  font-size: 11px;
`;

const DT = styled.dt`
  color: #a8a29e;
`;

const DD = styled.dd`
  color: #57534e;
`;

const LogSelector = styled.div`
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
`;

const LogSelectorLabel = styled.span`
  font-size: 10px;
  font-weight: 600;
  color: #a8a29e;
  text-transform: uppercase;
  letter-spacing: 0.1em;
`;

const LogSelectorRow = styled.div`
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: #a8a29e;
`;

const OfflineHint = styled.span`
  color: #d6d3d1;
`;

const LogPanelWrap = styled.div`
  flex: 1;
  border: 1px solid ${(p) => p.theme.colors.border};
  border-radius: ${(p) => p.theme.radii.lg};
  overflow: hidden;
  min-height: 0;
`;

// ── NodeReportCard ───────────────────────────

const NRCHeader = styled.div`
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 6px;
`;

const NRCNodeGroup = styled.div`
  display: flex;
  align-items: center;
  gap: 6px;
`;

const NRCDot = styled.span<{ $online: boolean }>`
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
  background: ${(p) => (p.$online ? '#10b981' : '#d6d3d1')};
`;

const NRCNodeName = styled.span`
  font-size: 11px;
  font-weight: 600;
  color: ${(p) => p.theme.colors.textPrimary};
`;

const NRCGrid = styled.div`
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 2px 8px;
  font-size: 10px;
`;

const NRCKey = styled.span`
  color: #a8a29e;
`;

const NRCVal = styled.span<{ $color?: string }>`
  font-weight: 500;
  color: ${(p) => p.$color || '#78716c'};
`;

const NRCError = styled.div`
  grid-column: span 2;
  margin-top: 4px;
  color: #ef4444;
  word-break: break-word;
`;

// ── Container info ───────────────────────────

const ContainerItem = styled.div`
  font-size: 11px;
  margin-bottom: 6px;
  &:last-child { margin-bottom: 0; }
`;

const ContainerNameRow = styled.div`
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 4px;
`;

const ContainerName = styled.span`
  font-family: ${(p) => p.theme.fonts.mono};
  color: #57534e;
  overflow: hidden;
  text-overflow: ellipsis;
`;

const ContainerStatus = styled.div`
  color: #a8a29e;
  overflow: hidden;
  text-overflow: ellipsis;
`;

const ContainerImage = styled.div`
  color: #d6d3d1;
  overflow: hidden;
  text-overflow: ellipsis;
  font-size: 10px;
`;

// ── Backup status ────────────────────────────

const BackupRow = styled.div`
  font-size: 11px;
  margin-bottom: 6px;
  &:last-child { margin-bottom: 0; }
`;

const BackupNodeLabel = styled.span`
  color: #a8a29e;
`;

const BackupTime = styled.span<{ $error?: boolean }>`
  color: ${(p) => (p.$error ? '#dc2626' : '#57534e')};
`;

const RestoreWarning = styled.div`
  font-size: 11px;
  line-height: 1.4;
  color: #92400e;
  background: #fffbeb;
  border: 1px solid #fde68a;
  border-radius: ${(p) => p.theme.radii.md};
  padding: 8px 10px;
`;

const ModalBody = styled.div`
  display: flex;
  flex-direction: column;
  gap: 12px;
`;

const ModalToolbar = styled.div`
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  font-size: 11px;
  color: #a8a29e;
`;

const SnapshotList = styled.div`
  border: 1px solid ${(p) => p.theme.colors.border};
  border-radius: ${(p) => p.theme.radii.md};
  overflow: hidden;
`;

const SnapshotButton = styled.button<{ $selected: boolean }>`
  width: 100%;
  border: 0;
  border-bottom: 1px solid ${(p) => p.theme.colors.borderLight};
  background: ${(p) => (p.$selected ? '#f0fdfa' : '#fff')};
  padding: 8px 10px;
  text-align: left;
  cursor: pointer;

  &:last-child {
    border-bottom: 0;
  }

  &:hover {
    background: ${(p) => (p.$selected ? '#ccfbf1' : '#faf9f7')};
  }
`;

const SnapshotRowTop = styled.div`
  display: flex;
  align-items: center;
  gap: 8px;
`;

const RadioDot = styled.span<{ $selected: boolean }>`
  width: 10px;
  height: 10px;
  border-radius: 999px;
  border: 1px solid ${(p) => (p.$selected ? '#0d9488' : '#c9c4bc')};
  background: ${(p) => (p.$selected ? '#0d9488' : '#fff')};
  flex-shrink: 0;
`;

const SnapshotID = styled.span`
  font-size: 11px;
  font-weight: 600;
  color: #1c1917;
`;

const LatestTag = styled.span`
  font-size: 10px;
  color: #0d9488;
  background: #ccfbf1;
  border-radius: ${(p) => p.theme.radii.sm};
  padding: 1px 5px;
`;

const SnapshotMeta = styled.div`
  margin: 3px 0 0 18px;
  font-size: 10px;
  color: #78716c;
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
`;

const ModalError = styled.div`
  font-size: 11px;
  color: #dc2626;
`;

const ModalFooter = styled.div`
  display: flex;
  justify-content: flex-end;
  gap: 8px;
`;

// ── LogPanel component ───────────────────────

interface LogLine { id: number; text: string }
let logIdSeq = 0;

function useAppLogs(appId: string, node: string | null) {
  const [lines, setLines] = useState<LogLine[]>([]);
  const seenRef = useRef<Set<string>>(new Set());
  const pendingRef = useRef<string[]>([]);
  const flushTimerRef = useRef<number | null>(null);

  const flushPending = useCallback(() => {
    flushTimerRef.current = null;
    if (pendingRef.current.length === 0) return;
    const chunk = pendingRef.current;
    pendingRef.current = [];
    setLines((prev) => {
      const next = [...prev];
      for (const text of chunk) {
        if (seenRef.current.has(text)) continue;
        seenRef.current.add(text);
        next.push({ id: ++logIdSeq, text });
      }
      return next.length > 2000 ? next.slice(-2000) : next;
    });
  }, []);

  const addLines = useCallback(
    (raw: string) => {
      const incoming = raw.split('\n').filter(Boolean);
      pendingRef.current.push(...incoming);
      if (flushTimerRef.current != null) return;
      flushTimerRef.current = window.setTimeout(flushPending, 80);
    },
    [flushPending],
  );

  useEffect(() => {
    if (!node) return;
    if (flushTimerRef.current != null) {
      window.clearTimeout(flushTimerRef.current);
      flushTimerRef.current = null;
    }
    pendingRef.current = [];
    seenRef.current.clear();
    setLines([]);
  }, [appId, node]);

  const streamCallback = useCallback(
    (line: string, _source?: string, done?: boolean) => {
      if (line) addLines(line);
      if (done) { flushPending(); return; }
    },
    [addLines, flushPending],
  );

  useStreamSubscription(node ? 'app-logs' : null, node ? { appId, node } : null, node ? streamCallback : null);

  return lines;
}

const LPLabel = styled.div`
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 12px;
  border-bottom: 1px solid ${(p) => p.theme.colors.border};
  background: #f7f5f0;
  flex-shrink: 0;
`;

const LPLabelText = styled.span`
  font-size: 10px;
  font-weight: 600;
  color: #a8a29e;
  text-transform: uppercase;
  letter-spacing: 0.1em;
`;

const LPLinesCount = styled.span`
  font-size: 10px;
  color: #a8a29e;
`;

const LPContent = styled.div`
  flex: 1;
  overflow: auto;
  background: #1c1917;
  font-family: ${(p) => p.theme.fonts.mono};
  font-size: 11px;
  padding: 8px;
`;

const LPWaiting = styled.span`
  color: #78716c;
`;

const LPLogLine = styled.div`
  color: #d6d3d1;
  line-height: 1.25;
  white-space: pre-wrap;
  word-break: break-all;
`;

function LogPanel({ lines, label, autoScroll }: { lines: LogLine[]; label: string; autoScroll?: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  const [pinned, setPinned] = useState(true);

  useEffect(() => {
    if (pinned && autoScroll !== false && ref.current) ref.current.scrollTop = ref.current.scrollHeight;
  }, [lines, pinned, autoScroll]);

  function onScroll() {
    const el = ref.current;
    if (!el) return;
    setPinned(el.scrollHeight - el.scrollTop - el.clientHeight < 40);
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
      <LPLabel>
        <LPLabelText>{label}</LPLabelText>
        <LPLinesCount>{lines.length} lines</LPLinesCount>
      </LPLabel>
      <LPContent ref={ref} onScroll={onScroll}>
        {lines.length === 0 ? (
          <LPWaiting>Waiting for output...</LPWaiting>
        ) : (
          lines.map((l) => <LPLogLine key={l.id}>{l.text}</LPLogLine>)
        )}
        {!pinned && (
          <Button
            variant="secondary"
            size="sm"
            style={{ position: 'sticky', bottom: 8 }}
            onClick={() => {
              if (ref.current) ref.current.scrollTop = ref.current.scrollHeight;
              setPinned(true);
            }}
          >
            ↓ follow
          </Button>
        )}
      </LPContent>
    </div>
  );
}

// ── Main Page ────────────────────────────────

interface AppDetailPageProps {
  appId: string;
  onBack: () => void;
}

export function AppDetailPage({ appId, onBack }: AppDetailPageProps) {
  const { data: app } = useApp(appId);
  const { data: nodeContainers } = useContainers();
  const { data: gatewayInfo } = useGatewayInfo();
  const desiredMutation = useUpdateAppDesiredState();
  const restartMutation = useRestartApp();
  const backupMutation = useTriggerBackup();
  const [restoreModalOpen, setRestoreModalOpen] = useState(false);

  const allReports = useMemo(
    () => [...(app?.node_reports ?? [])].sort((a, b) => a.node.localeCompare(b.node)),
    [app?.app_id, app?.node_reports.map((r) => r.node).sort().join(',')],
  );

  const appContainers = useMemo(() => {
    const raw =
      nodeContainers?.flatMap((nc) =>
        nc.containers
          .filter(
            (c) => c.name.startsWith(appId + '-') || c.name.startsWith(appId + '_') || c.name === appId,
          )
          .map((c) => ({ ...c, node: nc.node, nodeOnline: nc.online })),
      ) ?? [];
    return raw.sort((a, b) => a.node.localeCompare(b.node) || a.name.localeCompare(b.name));
  }, [appId, nodeContainers]);

  const defaultNode = useMemo(() => {
    if (!app) return null;
    const nodes = allReports.map((r) => r.node);
    if (nodes.length === 0) return null;
    if (app.is_global && gatewayInfo?.current_node && nodes.includes(gatewayInfo.current_node)) return gatewayInfo.current_node;
    if (app.assigned_agent && nodes.includes(app.assigned_agent)) return app.assigned_agent;
    return allReports.find((r) => r.node_online)?.node ?? nodes[0];
  }, [app?.app_id, app?.is_global, app?.assigned_agent, gatewayInfo?.current_node, allReports]);

  const [logNode, setLogNode] = useState<string | null>(null);
  const initializedRef = useRef(false);
  useEffect(() => {
    if (initializedRef.current || !defaultNode) return;
    initializedRef.current = true;
    setLogNode(defaultNode);
  }, [defaultNode]);

  const appLogLines = useAppLogs(appId, logNode);
  const { data: snapshots } = useSnapshots(app?.backup_enabled ? appId : null, logNode ?? null);

  if (!app) {
    return (
      <Page style={{ display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <span style={{ color: '#a8a29e', fontSize: '13px' }}>Loading...</span>
      </Page>
    );
  }

  const onlineReport = allReports.find((nr) => nr.node_online);

  return (
    <HC>
      <HeaderBar>
        <HeaderInner>
          <Button variant="ghost" size="sm" onClick={onBack}>← back</Button>
          <Divider />
          <AppTitle>{appId}</AppTitle>
          <StateLabel $state={app.observed_state}>{app.observed_state}</StateLabel>
          {app.node_reports.some((nr) => nr.quarantined) && <QuarantineBadge>quarantined</QuarantineBadge>}

          <HeaderActions>
            <DesiredToggle
              state={app.desired_state}
              onToggle={() =>
                desiredMutation.mutate({
                  appId,
                  state: app.desired_state === 'running' ? 'stopped' : 'running',
                })
              }
              disabled={desiredMutation.isPending}
            />
            {onlineReport && (
              <>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => restartMutation.mutate({ appId, node: onlineReport.node })}
                  disabled={restartMutation.isPending}
                >
                  Restart
                </Button>
                {app.backup_enabled && (
                  <>
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => setRestoreModalOpen(true)}
                      disabled={!logNode}
                    >
                      Restore from previous backup
                    </Button>
                    <Button
                      variant="primary"
                      size="sm"
                      onClick={() => backupMutation.mutate({ appId, node: onlineReport.node })}
                      disabled={backupMutation.isPending}
                    >
                      Backup now
                    </Button>
                  </>
                )}
              </>
            )}
          </HeaderActions>
        </HeaderInner>
      </HeaderBar>

      <Content>
        <LeftCol>
          <SectionWrap>
            <SectionTitle>Nodes</SectionTitle>
            {allReports.length === 0 ? (
              <NoData>No node reports</NoData>
            ) : (
              <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                {allReports.map((nr) => (
                  <NodeReportCard
                    key={nr.node}
                    nr={nr}
                    selected={logNode === nr.node}
                    onSelect={() => setLogNode(nr.node)}
                  />
                ))}
              </div>
            )}
          </SectionWrap>

          <SectionWrap>
            <SectionTitle>Configuration</SectionTitle>
            <DL>
              <DT>Global</DT>
              <DD>{app.is_global ? 'yes' : 'no'}</DD>
              <DT>Assignment</DT>
              <DD style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>
                {app.manually_assigned ? `manual: ${app.assigned_agent}` : app.assigned_agent ?? '—'}
              </DD>
              <DT>Backup</DT>
              <DD>{app.backup_enabled ? `yes — every ${app.backup_interval}` : 'disabled'}</DD>
              {app.backup_enabled && (
                <>
                  <DT>Prune</DT>
                  <DD>every {app.prune_interval}</DD>
                  <DT>Restore on start</DT>
                  <DD>{app.restore_enabled ? 'yes' : 'no'}</DD>
                  {app.restore_snapshot_override && (
                    <>
                      <DT>Restore override</DT>
                      <DD title={app.restore_snapshot_override.snapshot_id}>
                        {shortSnapshotID(app.restore_snapshot_override)}
                      </DD>
                    </>
                  )}
                  <DT>Sensitive</DT>
                  <DD>{app.backup_sensitive ? 'yes' : 'no'}</DD>
                  {snapshots?.snapshots && (
                    <>
                      <DT>Snapshots</DT>
                      <DD>{snapshots.snapshots.length}</DD>
                    </>
                  )}
                </>
              )}
            </DL>
          </SectionWrap>

          {app.backup_enabled && app.restore_snapshot_override && (
            <SectionWrap>
              <SectionTitle>Restore override</SectionTitle>
              <RestoreWarning>
                Temporary restore override active: future restores use {restoreOverrideText(app.restore_snapshot_override)} instead of latest.
                {!app.restore_enabled ? ' Restore is disabled, so this is stored but inactive.' : ''}
              </RestoreWarning>
            </SectionWrap>
          )}

          {app.backup_enabled && allReports.some((nr) => nr.last_backup_at) && (
            <SectionWrap>
              <SectionTitle>Backup status</SectionTitle>
              {allReports
                .filter((nr) => nr.last_backup_at || nr.last_backup_error)
                .map((nr) => (
                  <BackupRow key={nr.node}>
                    <BackupNodeLabel>{nr.node}: </BackupNodeLabel>
                    <BackupTime $error={!!nr.last_backup_error}>
                      {nr.last_backup_error ?? timeAgo(nr.last_backup_at)}
                    </BackupTime>
                  </BackupRow>
                ))}
            </SectionWrap>
          )}

          {appContainers.length > 0 && (
            <SectionWrap>
              <SectionTitle>Containers ({appContainers.length})</SectionTitle>
              {appContainers.map((c) => (
                <ContainerItem key={`${c.node}-${c.id}`}>
                  <ContainerNameRow>
                    <ContainerName>{c.name}</ContainerName>
                    <ContainerStateBadge state={c.state} />
                  </ContainerNameRow>
                  <ContainerStatus>{c.status}</ContainerStatus>
                  <ContainerImage>{c.image}</ContainerImage>
                </ContainerItem>
              ))}
            </SectionWrap>
          )}
        </LeftCol>

        <RightCol style={{ height: 'calc(100vh - 140px)' }}>
          <LogSelector>
            <LogSelectorLabel>Container logs</LogSelectorLabel>
            {allReports.length > 1 && (
              <LogSelectorRow>
                <span>node:</span>
                {allReports.map((nr) => (
                  <Button
                    key={nr.node}
                    variant={logNode === nr.node ? 'primary' : 'ghost'}
                    size="sm"
                    onClick={() => setLogNode(nr.node)}
                  >
                    {nr.node}
                    {!nr.node_online && <OfflineHint> (offline)</OfflineHint>}
                  </Button>
                ))}
              </LogSelectorRow>
            )}
          </LogSelector>
          <LogPanelWrap>
            <LogPanel lines={appLogLines} label={`${appId} — ${logNode ?? '—'}`} />
          </LogPanelWrap>
        </RightCol>
      </Content>
      {app.backup_enabled && (
        <RestoreSnapshotOverrideModal
          appId={appId}
          node={logNode}
          open={restoreModalOpen}
          onOpenChange={setRestoreModalOpen}
          currentOverride={app.restore_snapshot_override}
        />
      )}
    </HC>
  );
}

function RestoreSnapshotOverrideModal({
  appId,
  node,
  open,
  onOpenChange,
  currentOverride,
}: {
  appId: string;
  node: string | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  currentOverride?: RestoreSnapshotOverride;
}) {
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const { data, isLoading, error } = useSnapshots(open ? appId : null, node);
  const refreshMutation = useRefreshSnapshots();
  const setMutation = useSetRestoreSnapshotOverride();
  const clearMutation = useClearRestoreSnapshotOverride();

  useEffect(() => {
    if (open) setSelectedId(currentOverride?.snapshot_id ?? null);
  }, [open, currentOverride?.snapshot_id]);

  const snapshots = data?.snapshots ?? [];
  const selectedSnapshot = snapshots.find((s) => s.id === selectedId);
  const mutationError = setMutation.error ?? clearMutation.error;

  function save() {
    if (!node || !selectedSnapshot) return;
    setMutation.mutate(
      {
        appId,
        override: {
          snapshot_id: selectedSnapshot.id,
          short_id: selectedSnapshot.short_id,
          time: selectedSnapshot.time,
          node,
        },
      },
      { onSuccess: () => onOpenChange(false) },
    );
  }

  function clear() {
    clearMutation.mutate(appId, { onSuccess: () => onOpenChange(false) });
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Restore from previous backup"
      description="Choose a snapshot to use for future restores instead of latest. This stays active until cleared."
    >
      <ModalBody>
        {currentOverride && (
          <RestoreWarning>
            Current override: {restoreOverrideText(currentOverride)}.
            {!currentOverride.time && !currentOverride.node ? '' : ' '}
            Future restores use this snapshot until cleared.
          </RestoreWarning>
        )}

        <ModalToolbar>
          <span>
            {!node
              ? 'Select an online node to view snapshots.'
              : data?.cached_at
                ? `Cached ${timeAgo(data.cached_at)}${data.loading ? ' · refreshing' : ''}`
                : isLoading
                  ? 'Loading snapshots...'
                  : `Node ${node}`}
          </span>
          {node && (
            <Button
              variant="secondary"
              size="sm"
              onClick={() => refreshMutation.mutate({ appId, node })}
              disabled={refreshMutation.isPending || data?.loading}
            >
              {refreshMutation.isPending || data?.loading ? 'Refreshing...' : 'Refresh'}
            </Button>
          )}
        </ModalToolbar>

        {error && <ModalError>{error.message}</ModalError>}
        {data?.error && <ModalError>Cache error: {data.error}</ModalError>}

        {node && !isLoading && snapshots.length === 0 && !data?.loading && (
          <NoData>No snapshots found for this app on this node.</NoData>
        )}

        {snapshots.length > 0 && (
          <SnapshotList>
            {snapshots.map((snapshot, index) => (
              <SnapshotOption
                key={snapshot.id}
                snapshot={snapshot}
                latest={index === 0}
                selected={selectedId === snapshot.id}
                onSelect={() => setSelectedId(snapshot.id)}
              />
            ))}
          </SnapshotList>
        )}

        {mutationError && <ModalError>{mutationError.message}</ModalError>}

        <ModalFooter>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          {currentOverride && (
            <Button
              variant="secondary"
              size="sm"
              color="amber"
              onClick={clear}
              disabled={clearMutation.isPending}
            >
              Clear override
            </Button>
          )}
          <Button
            variant="primary"
            size="sm"
            onClick={save}
            disabled={!selectedSnapshot || setMutation.isPending}
          >
            Use selected snapshot
          </Button>
        </ModalFooter>
      </ModalBody>
    </Dialog>
  );
}

function SnapshotOption({
  snapshot,
  latest,
  selected,
  onSelect,
}: {
  snapshot: Snapshot;
  latest: boolean;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <SnapshotButton type="button" $selected={selected} onClick={onSelect}>
      <SnapshotRowTop>
        <RadioDot $selected={selected} />
        <SnapshotID>{snapshot.short_id}</SnapshotID>
        {latest && <LatestTag>latest</LatestTag>}
      </SnapshotRowTop>
      <SnapshotMeta>
        <span title={new Date(snapshot.time).toLocaleString()}>{timeAgo(snapshot.time)}</span>
        <span>{snapshot.hostname}</span>
        {snapshot.tags?.length > 0 && <span>{snapshot.tags.join(', ')}</span>}
      </SnapshotMeta>
    </SnapshotButton>
  );
}

// ── NodeReportCard sub-component ──────────────

function NodeReportCard({ nr, selected, onSelect }: { nr: NodeReport; selected: boolean; onSelect: () => void }) {
  return (
    <button onClick={onSelect} style={{ width: '100%', textAlign: 'left', borderRadius: 'var(--radius-3)', padding: 8, border: `1px solid ${selected ? '#5eead4' : 'var(--gray-a5)'}`, background: selected ? '#f0fdfa' : 'transparent', cursor: 'pointer' }}>
      <NRCHeader>
        <NRCNodeGroup>
          <NRCDot $online={nr.node_online} />
          <NRCNodeName>{nr.node}</NRCNodeName>
        </NRCNodeGroup>
        <HealthBadge health={nr.health} />
      </NRCHeader>
      <NRCGrid>
        <NRCKey>State</NRCKey>
        <NRCVal
          $color={
            nr.observed_state === 'running' ? '#059669' : nr.observed_state === 'error' ? '#dc2626' : '#78716c'
          }
        >
          {nr.observed_state}
        </NRCVal>
        {nr.phase && nr.phase !== 'empty' && (
          <>
            <NRCKey>Phase</NRCKey>
            <span style={{ gridColumn: 'span 1' }}>
              <PhaseBadge phase={nr.phase} progress={nr.progress} />
            </span>
          </>
        )}
        {nr.restart_count > 0 && (
          <>
            <NRCKey>Restarts</NRCKey>
            <NRCVal $color={nr.restart_count >= 3 ? '#dc2626' : '#d97706'}>{nr.restart_count}</NRCVal>
          </>
        )}
        {nr.quarantined && (
          <>
            <NRCKey>Status</NRCKey>
            <NRCVal $color="#dc2626">quarantined</NRCVal>
          </>
        )}
        {nr.port > 0 && (
          <>
            <NRCKey>Port</NRCKey>
            <NRCVal>{nr.port}</NRCVal>
          </>
        )}
        {nr.last_error && <NRCError>{nr.last_error}</NRCError>}
      </NRCGrid>
    </button>
  );
}
