import styled from '@emotion/styled';
import type { ObservedState, HealthStatus, ResticProgress } from '../../../api/types';
import { Tooltip } from '../../../components/ui/Tooltip';

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const value = bytes / Math.pow(1024, i);
  return `${value.toFixed(i > 0 ? 1 : 0)} ${units[i]}`;
}

// ── StatusBadge ────────────────────────────

const statusColors: Record<ObservedState, string> = {
  running: '#047857',
  stopped: '#a8a29e',
  restarting: '#d97706',
  error: '#dc2626',
  unknown: '#a8a29e',
};

const StatusText = styled.span<{ $status: ObservedState }>`
  font-size: ${(p) => p.theme.fontSizes.xs};
  font-weight: 500;
  color: ${(p) => statusColors[p.$status]};
`;

export function StatusBadge({ status }: { status: ObservedState }) {
  return <StatusText $status={status}>{status}</StatusText>;
}

// ── HealthBadge ────────────────────────────

const dotColors: Record<HealthStatus, string> = {
  healthy: '#10b981',
  unhealthy: '#ef4444',
  unknown: '#fbbf24',
};

const HealthWrap = styled.div`
  position: relative;
  display: inline-flex;
  align-items: center;
`;

const Dot = styled.span<{ $health: HealthStatus }>`
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: ${(p) => dotColors[p.$health]};
  ${(p) => p.$health === 'healthy' && 'box-shadow: 0 0 6px rgba(16, 185, 129, 0.5);'}
`;

export function HealthBadge({ health }: { health: HealthStatus }) {
  return (
    <Tooltip content={health}>
      <HealthWrap>
        <Dot $health={health} />
      </HealthWrap>
    </Tooltip>
  );
}

// ── PhaseBadge ─────────────────────────────

const phaseColors: Record<string, string> = {
  running: '#047857',
  ready: '#2563eb',
  restoring: '#d97706',
  error: '#dc2626',
  empty: '#a8a29e',
  backup_stopping: '#d97706',
  backup_stopped: '#d97706',
  backing_up: '#d97706',
  backup_pruning: '#7c3aed',
  backup_checking: '#7c3aed',
  pulling: '#2563eb',
};

const PhaseText = styled.span<{ $phase: string }>`
  font-size: ${(p) => p.theme.fontSizes.xs};
  font-weight: 500;
  color: ${(p) => phaseColors[p.$phase] || '#78716c'};
`;

const PhaseCol = styled.div`
  display: flex;
  flex-direction: column;
  gap: 4px;
`;

const PhaseRow = styled.div`
  display: flex;
  align-items: center;
  gap: 6px;
`;

const PhasePct = styled.span`
  font-size: 10px;
  color: #78716c;
`;

const PhaseBar = styled.span<{ $pct: number }>`
  width: 96px;
  height: 4px;
  background: #e7e5e4;
  border-radius: 9999px;
  overflow: hidden;
  flex-shrink: 0;

  &::after {
    content: '';
    display: block;
    height: 100%;
    width: ${(p) => p.$pct}%;
    background: #fbbf24;
    border-radius: 9999px;
    transition: width 0.3s;
  }
`;

const PhaseBytes = styled.span`
  font-size: 10px;
  color: #a8a29e;
`;

const Dash = styled.span`
  font-size: ${(p) => p.theme.fontSizes.xs};
  color: #a8a29e;
`;

export function PhaseBadge({ phase, progress }: { phase?: string; progress?: ResticProgress }) {
  if (!phase) return <Dash>—</Dash>;

  if (progress && (phase === 'restoring' || phase === 'backing_up')) {
    const pct = Math.round(progress.percent_done * 100);
    return (
      <PhaseCol>
        <PhaseRow>
          <PhaseText $phase={phase}>{phase}</PhaseText>
          <PhasePct>{pct}%</PhasePct>
        </PhaseRow>
        <PhaseBar $pct={pct} />
        {progress.total_bytes > 0 && (
          <PhaseBytes>
            {formatBytes(progress.bytes_done)}/{formatBytes(progress.total_bytes)}
          </PhaseBytes>
        )}
      </PhaseCol>
    );
  }

  return <PhaseText $phase={phase}>{phase}</PhaseText>;
}

// ── DesiredToggle ──────────────────────────

const ToggleWrap = styled.div`
  position: relative;
  display: inline-flex;
  align-items: center;
`;

const ToggleBtn = styled.button<{ $on: boolean }>`
  position: relative;
  display: inline-flex;
  height: 16px;
  width: 28px;
  flex-shrink: 0;
  cursor: pointer;
  align-items: center;
  border-radius: 9999px;
  transition: background-color 0.15s;
  background: ${(p) => (p.$on ? '#0d9488' : '#d6d3d1')};
  border: none;
  padding: 0;

  &:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  &:focus {
    outline: none;
  }
`;

const ToggleThumb = styled.span<{ $on: boolean }>`
  display: inline-block;
  height: 10px;
  width: 10px;
  border-radius: 50%;
  background: white;
  transition: transform 0.15s;
  transform: ${(p) => (p.$on ? 'translateX(14px)' : 'translateX(3px)')};
`;

export function DesiredToggle({ state, onToggle, disabled }: { state: string; onToggle: () => void; disabled?: boolean }) {
  const isRunning = state === 'running';
  return (
    <Tooltip content={isRunning ? 'should be running' : 'should be stopped'}>
      <ToggleWrap>
        <ToggleBtn role="switch" aria-checked={isRunning} onClick={onToggle} disabled={disabled} $on={isRunning}>
          <ToggleThumb $on={isRunning} />
        </ToggleBtn>
      </ToggleWrap>
    </Tooltip>
  );
}

// ── PostBackupPhaseBadge ───────────────────

const postBackupColors: Record<string, string> = {
  backup_pruning: '#7c3aed',
  backup_checking: '#7c3aed',
};

const PostBackupRow = styled.div`
  display: flex;
  align-items: center;
  gap: 6px;
`;

const PostBackupText = styled.span<{ $phase: string }>`
  font-size: ${(p) => p.theme.fontSizes.xs};
  font-weight: 500;
  color: ${(p) => postBackupColors[p.$phase] || '#7c3aed'};
`;

const MiniBar = styled.span<{ $pct: number }>`
  width: 48px;
  height: 4px;
  background: #e7e5e4;
  border-radius: 9999px;
  overflow: hidden;

  &::after {
    content: '';
    display: block;
    height: 100%;
    width: ${(p) => p.$pct}%;
    background: #c4b5fd;
    border-radius: 9999px;
  }
`;

const MiniPct = styled.span`
  font-size: 10px;
`;

const PostBackupBtn = styled.button`
  position: relative;
  background: none;
  border: none;
  cursor: pointer;
  padding: 0;

  &:hover {
    opacity: 0.7;
  }
`;

const PingDot = styled.span`
  position: absolute;
  top: -2px;
  right: -6px;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #c4b5fd;
`;

export function PostBackupPhaseBadge({
  phase,
  onClick,
  progress,
}: {
  phase: string;
  onClick?: () => void;
  progress?: ResticProgress;
}) {
  const pct = progress ? Math.round(progress.percent_done * 100) : 0;

  const content = (
    <PostBackupRow>
      <PostBackupText $phase={phase}>{phase}</PostBackupText>
      {progress && (
        <>
          <MiniBar $pct={pct} />
          <MiniPct>{pct}%</MiniPct>
        </>
      )}
    </PostBackupRow>
  );

  if (onClick) {
    return (
      <PostBackupBtn onClick={onClick}>
        {content}
        <PingDot />
      </PostBackupBtn>
    );
  }

  return content;
}

// ── ContainerStateBadge ─────────────────────

const containerStateColors: Record<string, string> = {
  running: '#047857',
  exited: '#dc2626',
  restarting: '#d97706',
  created: '#78716c',
  paused: '#d97706',
  dead: '#dc2626',
};

const ContainerState = styled.span<{ $state: string }>`
  font-size: ${(p) => p.theme.fontSizes.xs};
  font-weight: 500;
  color: ${(p) => containerStateColors[p.$state] || '#78716c'};
`;

export function ContainerStateBadge({ state }: { state: string }) {
  return <ContainerState $state={state}>{state}</ContainerState>;
}
