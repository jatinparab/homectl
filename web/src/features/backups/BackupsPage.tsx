import { useMemo, useState, useCallback, useEffect, useRef } from "react";
import styled from "@emotion/styled";
import type { AppStatus, ResticProgress, Snapshot } from "../../api/types";
import {
  useSnapshots,
  useSnapshotFiles,
  useRefreshSnapshots,
  useRepoStatus,
  useTriggerRepoCheck,
  useTriggerRepoUnlock,
  useForceRepoUnlock,
  useTriggerPrune,
} from "../../api/hooks";
import { useStreamSubscription } from "../../api/live";

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const value = bytes / Math.pow(1024, i);
  return `${value.toFixed(i > 0 ? 1 : 0)} ${units[i]}`;
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

function formatFullDate(dateStr: string): string {
  const date = new Date(dateStr);
  const day = date.getDate();
  const suffix = day % 10 === 1 && day !== 11 ? "st" : day % 10 === 2 && day !== 12 ? "nd" : day % 10 === 3 && day !== 13 ? "rd" : "th";
  const month = date.toLocaleString(undefined, { month: "short" });
  const year = date.getFullYear();
  const hours = date.getHours().toString().padStart(2, "0");
  const minutes = date.getMinutes().toString().padStart(2, "0");
  return `${day}${suffix} ${month} ${year}, ${hours}:${minutes}`;
}

// ══════════════════════════════════════════════
// Styled components
// ══════════════════════════════════════════════

const Page = styled.div`
  min-height: 100vh;
  background: #faf9f7;
`;

const HeaderBar = styled.div`
  position: sticky;
  top: 0;
  z-index: 30;
  border-bottom: 1px solid ${(p) => p.theme.colors.border};
  background: ${(p) => p.theme.colors.neutralSurface};
  padding: 16px;
`;

const HeaderInner = styled.div`
  max-width: 72rem;
  margin: 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
`;

const Title = styled.h1`
  font-size: ${(p) => p.theme.fontSizes.sm};
  font-weight: 600;
  color: ${(p) => p.theme.colors.textPrimary};
`;

const Subtitle = styled.p`
  font-size: 11px;
  color: #a8a29e;
  margin: 2px 0 0;
`;

const Main = styled.main`
  max-width: 72rem;
  margin: 0 auto;
  padding: 24px 16px;
  display: flex;
  flex-direction: column;
  gap: 16px;
`;

const Muted = styled.p`
  font-size: ${(p) => p.theme.fontSizes.sm};
  color: #a8a29e;
`;

// ══ Repo panel ═══════════════════════════════

const RepoWrap = styled.div`
  background: ${(p) => p.theme.colors.neutralSurface};
  border: 1px solid ${(p) => p.theme.colors.border};
  border-radius: ${(p) => p.theme.radii.lg};
  margin-bottom: 16px;
`;

const RepoHeader = styled.div`
  padding: 12px 16px;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  border-bottom: 1px solid ${(p) => p.theme.colors.borderLight};
`;

const RepoTitle = styled.div`
  font-size: ${(p) => p.theme.fontSizes.sm};
  font-weight: 600;
  color: ${(p) => p.theme.colors.textPrimary};
`;

const RepoSub = styled.div`
  font-size: 11px;
  color: #a8a29e;
  margin-top: 2px;
`;

const RepoError = styled.div`
  font-size: 11px;
  color: #ef4444;
  margin-top: 4px;
`;

const RepoActions = styled.div`
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
`;

const RepoBody = styled.div`
  padding: 12px 16px;
  display: grid;
  grid-template-columns: 1fr;
  gap: 12px;
  @media (min-width: 768px) {
    grid-template-columns: 1fr 1fr;
  }
`;

const RepoCard = styled.div`
  border: 1px solid ${(p) => p.theme.colors.border};
  border-radius: ${(p) => p.theme.radii.md};
  padding: 12px;
`;

const RepoCardHeader = styled.div`
  display: flex;
  align-items: center;
  justify-content: space-between;
`;

const RepoCardLabel = styled.div`
  font-size: 11px;
  font-weight: 600;
  color: #78716c;
  text-transform: uppercase;
  letter-spacing: 0.05em;
`;

const RepoCardLast = styled.div`
  font-size: 11px;
  color: #a8a29e;
  margin-top: 4px;
`;

const RepoProgress = styled.div`
  margin-top: 8px;
  height: 4px;
  background: #f5f5f4;
  border-radius: 9999px;
  overflow: hidden;
`;

const RepoProgressFill = styled.div<{ $pct: number }>`
  height: 100%;
  width: ${(p) => `${Math.max(0, Math.min(100, p.$pct))}%`};
  background: #14b8a6;
`;

const RepoProgressLabel = styled.div`
  font-size: 10px;
  color: #a8a29e;
  margin-top: 4px;
`;

const LogPre = styled.pre`
  margin-top: 8px;
  font-size: 11px;
  color: #57534e;
  font-family: ${(p) => p.theme.fonts.mono};
  padding: 8px;
  background: #f7f5f0;
  border: 1px solid ${(p) => p.theme.colors.border};
  border-radius: ${(p) => p.theme.radii.md};
  max-height: 160px;
  overflow: auto;
  white-space: pre-wrap;
`;

// ══ App row ══════════════════════════════════

const AppCard = styled.div`
  background: ${(p) => p.theme.colors.neutralSurface};
  border: 1px solid ${(p) => p.theme.colors.border};
  border-radius: ${(p) => p.theme.radii.lg};
`;

const AppRowBtn = styled.button`
  width: 100%;
  padding: 12px 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  text-align: left;
  background: ${(p) => p.theme.colors.neutralSurface};
  border: none;
  cursor: pointer;
  border-radius: ${(p) => p.theme.radii.lg};

  &:hover {
    background: #faf9f7;
  }
`;

const AppRowLeft = styled.div`
  display: flex;
  align-items: center;
  gap: 12px;
`;

const AppRowRight = styled.div`
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 11px;
  color: #a8a29e;
`;

const Chevron = styled.span`
  font-size: 10px;
  color: #a8a29e;
`;

const AppName = styled.span`
  font-size: ${(p) => p.theme.fontSizes.sm};
  font-weight: 600;
  color: ${(p) => p.theme.colors.textPrimary};
`;

const StatusBadge = styled.span<{ $color: string }>`
  font-size: ${(p) => p.theme.fontSizes.xs};
  color: ${(p) => p.$color};
`;

const TimeSpan = styled.span`
  font-size: 11px;
  color: #a8a29e;
`;

const ErrorSpan = styled.span`
  font-size: 11px;
  color: #ef4444;
`;

const ExpandedBody = styled.div`
  border-top: 1px solid ${(p) => p.theme.colors.borderLight};
`;

const NoNode = styled.div`
  padding: 12px 16px;
  font-size: 11px;
  color: #a8a29e;
`;

// ══ Snapshot list ════════════════════════════

const SnapHeader = styled.div`
  padding: 8px 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid ${(p) => p.theme.colors.borderLight};
`;

const SnapInfo = styled.div`
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 11px;
  color: #a8a29e;
`;

const SnapActions = styled.div`
  display: flex;
  align-items: center;
  gap: 8px;
`;

const SnapTable = styled.table`
  width: 100%;
  font-size: ${(p) => p.theme.fontSizes.xs};
`;

const SnapTh = styled.th`
  padding: 8px 16px;
  text-align: left;
  font-size: 10px;
  font-weight: 600;
  color: #a8a29e;
  text-transform: uppercase;
  letter-spacing: 0.1em;
`;

const SnapTr = styled.tr`
  border-bottom: 1px solid #f7f5f0;
  &:hover {
    background: #faf9f7;
  }
  cursor: pointer;
`;

const SnapTd = styled.td`
  padding: 8px 16px;
`;

const SnapId = styled.span`
  color: #0d9488;
  font-family: ${(p) => p.theme.fonts.mono};
  font-size: 11px;
`;

// ══ File browser ════════════════════════════

const FBrowser = styled.div`
  background: #faf9f7;
  border-top: 1px solid ${(p) => p.theme.colors.border};
  padding: 12px 16px;
`;

const Breadcrumbs = styled.div`
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  margin-bottom: 8px;
`;

const BreadcrumbBtn = styled.button`
  color: #0d9488;
  background: none;
  border: none;
  cursor: pointer;
  font-size: 11px;

  &:hover {
    text-decoration: underline;
  }
`;

const FbTable = styled.table`
  width: 100%;
  font-size: ${(p) => p.theme.fontSizes.xs};
`;

const FbTr = styled.tr`
  border-top: 1px solid ${(p) => p.theme.colors.borderLight};
  &:hover {
    background: ${(p) => p.theme.colors.neutralSurface};
  }
`;

const FbTd = styled.td`
  padding: 6px 0;
`;

// ══ Shared buttons ══════════════════════════

const Btn = styled.button<{ $danger?: boolean; $violet?: boolean; $link?: boolean }>`
  padding: 4px 8px;
  font-size: 11px;
  border: 1px solid ${(p) => (p.$danger ? "#fecaca" : p.$violet ? "#ddd6fe" : p.theme.colors.border)};
  color: ${(p) => (p.$danger ? "#dc2626" : p.$violet ? "#7c3aed" : "#78716c")};
  background: none;
  border-radius: ${(p) => p.theme.radii.sm};
  cursor: pointer;

  &:hover:not(:disabled) {
    ${(p) => (p.$danger ? "border-color: #f87171; background: #fef2f2;" : p.$violet ? "border-color: #c4b5fd; background: #f5f3ff;" : "border-color: #a8a29e;")}
  }

  &:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }

  ${(p) =>
    p.$link &&
    `
    border: none;
    padding: 0;
    color: #0d9488;
    &:hover { text-decoration: underline; }
  `}
`;

// ══ Progress mini ═══════════════════════════

const ProMini = styled.span`
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: #78716c;
`;

const ProBar = styled.span<{ $pct: number }>`
  width: 56px;
  height: 4px;
  background: #e7e5e4;
  border-radius: 9999px;
  overflow: hidden;

  &::after {
    content: "";
    display: block;
    height: 100%;
    width: ${(p) => p.$pct}%;
    background: #c4b5fd;
    border-radius: 9999px;
  }
`;

const ProMuted = styled.span`
  color: #a8a29e;
`;

// ══════════════════════════════════════════════
// Components
// ══════════════════════════════════════════════

interface BackupsPageProps {
  apps?: AppStatus[];
}

export function BackupsPage({ apps }: BackupsPageProps) {
  const [expandedApps, setExpandedApps] = useState<Set<string>>(new Set());

  const toggleApp = useCallback((appId: string) => {
    setExpandedApps((prev) => {
      const next = new Set(prev);
      if (next.has(appId)) next.delete(appId);
      else next.add(appId);
      return next;
    });
  }, []);

  const backupApps = useMemo(
    () => (apps ?? []).filter((a) => a.backup_enabled).sort((a, b) => a.app_id.localeCompare(b.app_id)),
    [apps],
  );

  return (
    <Page>
      <HeaderBar>
        <HeaderInner>
          <div>
            <Title>Backups</Title>
            <Subtitle>
              {backupApps.length} backup-enabled app{backupApps.length !== 1 ? "s" : ""}
            </Subtitle>
          </div>
        </HeaderInner>
      </HeaderBar>

      <Main>
        <RepoMaintenancePanel />

        {backupApps.length === 0 ? (
          <Muted>No backup-enabled apps found.</Muted>
        ) : (
          <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
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
      </Main>
    </Page>
  );
}

function RepoMaintenancePanel() {
  const { data, error } = useRepoStatus();
  const triggerCheck = useTriggerRepoCheck();
  const triggerUnlock = useTriggerRepoUnlock();
  const forceUnlock = useForceRepoUnlock();

  const [showCheckLogs, setShowCheckLogs] = useState(false);
  const [showUnlockLogs, setShowUnlockLogs] = useState(false);
  const [checkLines, setCheckLines] = useState<string[]>([]);
  const [unlockLines, setUnlockLines] = useState<string[]>([]);
  const checkPreRef = useRef<HTMLPreElement>(null);
  const unlockPreRef = useRef<HTMLPreElement>(null);
  const maxLines = 200;

  const checkBusy = data?.check.busy === true;
  const unlockBusy = data?.unlock.busy === true;
  const gatewayOnline = data?.gateway_online === true;

  const checkStreamEnabled = gatewayOnline && (showCheckLogs || checkBusy);
  const handleCheckLine = useCallback(
    (line: string, _source?: string, done?: boolean) => {
      if (line) setCheckLines((prev) => { const n = [...prev, line]; return n.length > maxLines ? n.slice(-maxLines) : n; });
      if (done) return;
    },
    [maxLines],
  );
  useStreamSubscription(checkStreamEnabled ? "repo-check-logs" : null, checkStreamEnabled ? {} : null, checkStreamEnabled ? handleCheckLine : null);

  useEffect(() => { if (checkPreRef.current) checkPreRef.current.scrollTop = checkPreRef.current.scrollHeight; }, [checkLines]);

  const unlockStreamEnabled = gatewayOnline && (showUnlockLogs || unlockBusy);
  const handleUnlockLine = useCallback(
    (line: string, _source?: string, done?: boolean) => {
      if (line) setUnlockLines((prev) => { const n = [...prev, line]; return n.length > maxLines ? n.slice(-maxLines) : n; });
      if (done) return;
    },
    [maxLines],
  );
  useStreamSubscription(unlockStreamEnabled ? "repo-unlock-logs" : null, unlockStreamEnabled ? {} : null, unlockStreamEnabled ? handleUnlockLine : null);

  useEffect(() => { if (unlockPreRef.current) unlockPreRef.current.scrollTop = unlockPreRef.current.scrollHeight; }, [unlockLines]);

  const lastCheckAt = data?.check.last_at;
  const lastUnlockAt = data?.unlock.last_at;
  const checkProgress = data?.check.progress_percent;

  return (
    <RepoWrap>
      <RepoHeader>
        <div style={{ minWidth: 0 }}>
          <RepoTitle>Repository maintenance</RepoTitle>
          <RepoSub>
            Gateway: <span style={{ color: "#57534e" }}>{data?.gateway_node ? data.gateway_node : "(none)"}</span>
            {!gatewayOnline && <span style={{ color: "#ef4444" }}> — offline</span>}
          </RepoSub>
          {error && <RepoError>{(error as Error).message}</RepoError>}
        </div>
        <RepoActions>
          <Btn onClick={() => triggerCheck.mutate()} disabled={!gatewayOnline || checkBusy || unlockBusy || triggerCheck.isPending}>
            {checkBusy ? "Checking..." : "Run check"}
          </Btn>
          <Btn onClick={() => triggerUnlock.mutate()} disabled={!gatewayOnline || checkBusy || unlockBusy || triggerUnlock.isPending}>
            {unlockBusy ? "Unlocking..." : "Unlock repo"}
          </Btn>
          <Btn $danger onClick={() => forceUnlock.mutate()} disabled={!gatewayOnline || unlockBusy || forceUnlock.isPending} title="Force unlock — runs restic unlock --remove-all">
            Force unlock
          </Btn>
        </RepoActions>
      </RepoHeader>

      <RepoBody>
        <RepoCard>
          <RepoCardHeader>
            <RepoCardLabel>Check</RepoCardLabel>
            <Btn $link onClick={() => setShowCheckLogs((v) => !v)} disabled={!gatewayOnline}>
              {showCheckLogs ? "Hide logs" : "Show logs"}
            </Btn>
          </RepoCardHeader>
          <RepoCardLast>
            Last: {lastCheckAt ? <span title={formatFullDate(lastCheckAt)}>{timeAgo(lastCheckAt)}</span> : "never"}
            {data?.check.last_error ? <span style={{ color: "#ef4444" }}> — {data.check.last_error}</span> : null}
          </RepoCardLast>
          {checkBusy && (
            <>
              <RepoProgress>
                <RepoProgressFill $pct={checkProgress ?? 0} />
              </RepoProgress>
              <RepoProgressLabel>{checkProgress != null ? `${checkProgress.toFixed(2)}%` : "Running..."}</RepoProgressLabel>
            </>
          )}
          {showCheckLogs && <LogPre ref={checkPreRef}>{checkLines.length === 0 ? "Waiting for output..." : checkLines.join("\n")}</LogPre>}
        </RepoCard>

        <RepoCard>
          <RepoCardHeader>
            <RepoCardLabel>Unlock</RepoCardLabel>
            <Btn $link onClick={() => setShowUnlockLogs((v) => !v)} disabled={!gatewayOnline}>
              {showUnlockLogs ? "Hide logs" : "Show logs"}
            </Btn>
          </RepoCardHeader>
          <RepoCardLast>
            Last: {lastUnlockAt ? <span title={formatFullDate(lastUnlockAt)}>{timeAgo(lastUnlockAt)}</span> : "never"}
            {data?.unlock.last_error ? <span style={{ color: "#ef4444" }}> — {data.unlock.last_error}</span> : null}
          </RepoCardLast>
          {showUnlockLogs && <LogPre ref={unlockPreRef}>{unlockLines.length === 0 ? "Waiting for output..." : unlockLines.join("\n")}</LogPre>}
        </RepoCard>
      </RepoBody>
    </RepoWrap>
  );
}

const backupStatusColor: Record<string, string> = { ok: "#059669", needs_backup: "#d97706", never: "#a8a29e" };
const pruneStatusColor: Record<string, string> = { ok: "#059669", needs_prune: "#d97706", never: "#a8a29e" };

function AppBackupRow({ app, expanded, onToggle }: { app: AppStatus; expanded: boolean; onToggle: () => void }) {
  const onlineNode = app.node_reports.find((nr) => nr.node_online)?.node ?? null;
  useSnapshots(app.app_id, onlineNode);

  const sortedReports = [...app.node_reports].sort((a, b) => a.node.localeCompare(b.node));
  const reportForPrune = sortedReports.find((nr) => nr.node === onlineNode) ?? sortedReports[0];
  const backupReport = reportForPrune;
  const postBackupReport = sortedReports.find((nr) => !!nr.post_backup_phase);
  const activeBackupReport = sortedReports.find((nr) => nr.phase === "backing_up" || nr.phase === "restoring");
  const streamReport = postBackupReport ?? activeBackupReport;

  return (
    <AppCard>
      <AppRowBtn onClick={onToggle} style={expanded ? { borderBottomLeftRadius: 0, borderBottomRightRadius: 0 } : undefined}>
        <AppRowLeft>
          <Chevron>{expanded ? "▼" : "▶"}</Chevron>
          <AppName>{app.app_id}</AppName>
          <span style={{ fontSize: 10, color: "#a8a29e" }}>B</span>
          <StatusBadge $color={backupStatusColor[app.backup_status] ?? "#a8a29e"}>{app.backup_status}</StatusBadge>
          {backupReport?.last_backup_at && (
            <TimeSpan title={formatFullDate(backupReport.last_backup_at)}>{timeAgo(backupReport.last_backup_at)}</TimeSpan>
          )}
          {backupReport?.last_backup_error && <ErrorSpan title={backupReport.last_backup_error}>err</ErrorSpan>}
          <span style={{ color: "#e7e5e4" }}>·</span>
          <span style={{ fontSize: 10, color: "#a8a29e" }}>P</span>
          <StatusBadge $color={pruneStatusColor[reportForPrune?.prune_status ?? app.prune_status] ?? "#a8a29e"}>
            {reportForPrune?.last_prune_error ? "failed" : (reportForPrune?.prune_status ?? app.prune_status)}
          </StatusBadge>
          {reportForPrune?.last_prune_at && (
            <TimeSpan title={formatFullDate(reportForPrune.last_prune_at)}>{timeAgo(reportForPrune.last_prune_at)}</TimeSpan>
          )}
          {reportForPrune?.last_prune_error && (
            <ErrorSpan title={reportForPrune.last_prune_error}>
              {reportForPrune.last_prune_error.length > 40 ? reportForPrune.last_prune_error.slice(0, 40) + "..." : reportForPrune.last_prune_error}
            </ErrorSpan>
          )}
          {streamReport?.progress && <ProgressMini progress={streamReport.progress} />}
          <SnapshotCount appId={app.app_id} node={onlineNode} />
        </AppRowLeft>
        <AppRowRight>
          <span>every {app.backup_interval}</span>
          {!onlineNode && <span style={{ color: "#ef4444" }}>no online node</span>}
        </AppRowRight>
      </AppRowBtn>

      {expanded && onlineNode && (
        <ExpandedBody>
          <SnapshotList appId={app.app_id} node={onlineNode} app={app} />
        </ExpandedBody>
      )}
      {expanded && !onlineNode && <NoNode>No online node available to query snapshots.</NoNode>}
    </AppCard>
  );
}

function ProgressMini({ progress }: { progress: ResticProgress }) {
  const pct = Math.max(0, Math.min(100, Math.round(progress.percent_done * 100)));
  return (
    <ProMini>
      <ProBar $pct={pct} />
      <span>{pct}%</span>
      {progress.total_files > 0 ? (
        <ProMuted>{progress.files_done}/{progress.total_files}</ProMuted>
      ) : (
        <ProMuted>
          {formatBytes(progress.bytes_done)}/{formatBytes(progress.total_bytes)}
        </ProMuted>
      )}
    </ProMini>
  );
}

function SnapshotCount({ appId, node }: { appId: string; node: string | null }) {
  const { data } = useSnapshots(appId, node);
  const count = data?.snapshots?.length;
  if (count == null) return null;
  return <span style={{ fontSize: 11, color: "#a8a29e" }}>({count} snapshot{count !== 1 ? "s" : ""})</span>;
}

function SnapshotList({ appId, node, app }: { appId: string; node: string; app: AppStatus }) {
  const { data, isLoading, error } = useSnapshots(appId, node);
  const refreshMutation = useRefreshSnapshots();
  const pruneMutation = useTriggerPrune();
  const [expandedSnapshot, setExpandedSnapshot] = useState<string | null>(null);

  const snapshots = data?.snapshots;
  const cacheLoading = data?.loading === true;
  const nodeReport = app.node_reports.find((nr) => nr.node === node);
  const isBusy = !!nodeReport?.post_backup_phase || nodeReport?.phase === "backing_up";
  const canPrune = nodeReport?.observed_state === "running" && !isBusy;

  return (
    <div>
      <SnapHeader>
        <SnapInfo>
          {data?.cached_at && <span title={formatFullDate(data.cached_at)}>Cached {timeAgo(data.cached_at)}</span>}
          {cacheLoading && <span style={{ color: "#f59e0b" }}>Refreshing...</span>}
        </SnapInfo>
        <SnapActions>
          {canPrune && (
            <Btn $violet onClick={() => pruneMutation.mutate({ appId, node })} disabled={pruneMutation.isPending}>
              Prune
            </Btn>
          )}
          <Btn onClick={() => refreshMutation.mutate({ appId, node })} disabled={cacheLoading || refreshMutation.isPending}>
            {cacheLoading ? "Refreshing..." : "Refresh"}
          </Btn>
        </SnapActions>
      </SnapHeader>

      {isLoading && !data && <div style={{ padding: "12px 16px", fontSize: 11, color: "#a8a29e" }}>Loading snapshots...</div>}
      {error && !data && <div style={{ padding: "12px 16px", fontSize: 11, color: "#ef4444" }}>Error: {(error as Error).message}</div>}
      {data?.error && <div style={{ padding: "12px 16px", fontSize: 11, color: "#ef4444" }}>Cache error: {data.error}</div>}
      {!isLoading && !error && snapshots && snapshots.length === 0 && !cacheLoading && (
        <div style={{ padding: "12px 16px", fontSize: 11, color: "#a8a29e" }}>No snapshots found.</div>
      )}

      {snapshots && snapshots.length > 0 && (
        <SnapTable>
          <thead>
            <tr>
              <SnapTh style={{ width: 32 }}></SnapTh>
              <SnapTh style={{ width: 40 }}>#</SnapTh>
              <SnapTh>ID</SnapTh>
              <SnapTh>Time</SnapTh>
              <SnapTh>Host</SnapTh>
              <SnapTh>Tags</SnapTh>
            </tr>
          </thead>
          <tbody>
            {snapshots.map((snap, index) => (
              <SnapshotRow
                key={snap.id}
                index={index + 1}
                snapshot={snap}
                node={node}
                expanded={expandedSnapshot === snap.id}
                onToggle={() => setExpandedSnapshot(expandedSnapshot === snap.id ? null : snap.id)}
              />
            ))}
          </tbody>
        </SnapTable>
      )}
    </div>
  );
}

function SnapshotRow({ index, snapshot, node, expanded, onToggle }: { index: number; snapshot: Snapshot; node: string; expanded: boolean; onToggle: () => void }) {
  return (
    <>
      <SnapTr onClick={onToggle}>
        <SnapTd style={{ color: "#a8a29e", fontSize: 10 }}>{expanded ? "▼" : "▶"}</SnapTd>
        <SnapTd style={{ color: "#a8a29e", fontSize: 11 }}>{index}</SnapTd>
        <SnapTd>
          <SnapId>{snapshot.short_id}</SnapId>
        </SnapTd>
        <SnapTd style={{ color: "#57534e", fontSize: 11 }} title={formatFullDate(snapshot.time)}>
          {timeAgo(snapshot.time)}
        </SnapTd>
        <SnapTd style={{ color: "#57534e", fontSize: 11 }}>{snapshot.hostname}</SnapTd>
        <SnapTd style={{ color: "#a8a29e", fontSize: 11 }}>{snapshot.tags?.join(", ") ?? ""}</SnapTd>
      </SnapTr>
      {expanded && (
        <tr>
          <td colSpan={6} style={{ padding: 0 }}>
            <FileBrowser snapshotId={snapshot.id} node={node} />
          </td>
        </tr>
      )}
    </>
  );
}

function FileBrowser({ snapshotId, node }: { snapshotId: string; node: string }) {
  const [currentPath, setCurrentPath] = useState("/");
  const { data: entries, isLoading, error } = useSnapshotFiles(snapshotId, node, currentPath);

  const pathParts = currentPath.split("/").filter(Boolean);
  const breadcrumbs = [
    { label: "/", path: "/" },
    ...pathParts.map((part, i) => ({ label: part, path: "/" + pathParts.slice(0, i + 1).join("/") + "/" })),
  ];

  return (
    <FBrowser>
      <Breadcrumbs>
        {breadcrumbs.map((bc, i) => (
          <span key={bc.path} style={{ display: "flex", alignItems: "center", gap: 4 }}>
            {i > 0 && <span style={{ color: "#d6d3d1" }}>/</span>}
            <BreadcrumbBtn onClick={() => setCurrentPath(bc.path)}>{bc.label}</BreadcrumbBtn>
          </span>
        ))}
      </Breadcrumbs>

      {isLoading && <div style={{ fontSize: 11, color: "#a8a29e" }}>Loading...</div>}
      {error && <div style={{ fontSize: 11, color: "#ef4444" }}>Error: {(error as Error).message}</div>}
      {entries && entries.length === 0 && <div style={{ fontSize: 11, color: "#a8a29e" }}>Empty directory.</div>}

      {entries && entries.length > 0 && (
        <FbTable>
          <tbody>
            {entries.map((entry) => (
              <FbTr key={entry.path}>
                <FbTd style={{ color: "#a8a29e", width: 24 }}>{entry.type === "dir" ? "📁" : "📄"}</FbTd>
                <FbTd>
                  {entry.type === "dir" ? (
                    <BreadcrumbBtn onClick={() => setCurrentPath(entry.path + "/")}>{entry.name}/</BreadcrumbBtn>
                  ) : (
                    <span style={{ color: "#57534e" }}>{entry.name}</span>
                  )}
                </FbTd>
                <FbTd style={{ textAlign: "right", color: "#a8a29e", width: 80 }}>
                  {entry.type === "file" && entry.size != null ? formatBytes(entry.size) : ""}
                </FbTd>
              </FbTr>
            ))}
          </tbody>
        </FbTable>
      )}
    </FBrowser>
  );
}
