import { useCallback, useEffect, useRef, useState } from 'react';
import styled from '@emotion/styled';
import type { ResticProgress } from '../../../api/types';
import { useStreamSubscription } from '../../../api/live';

interface PhaseLogOverlayProps {
  appId: string;
  node: string;
  phase: string;
  onClose: () => void;
  onProgress?: (progress: ResticProgress) => void;
}

const phaseLabels: Record<string, string> = {
  pulling: 'Pull logs',
  restoring: 'Restore logs',
  backing_up: 'Backup logs',
  backup_pruning: 'Prune logs',
  backup_checking: 'Check logs',
};

// ── Styled ─────────────────────────────────

const Wrapper = styled.div`
  position: relative;
  background: #faf9f7;
  border: 1px solid ${(p) => p.theme.colors.border};
  border-radius: ${(p) => p.theme.radii.md};
  margin: 4px 16px;
`;

const Header = styled.div`
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 12px;
  border-bottom: 1px solid ${(p) => p.theme.colors.border};
`;

const Label = styled.span`
  font-size: 11px;
  color: #78716c;
`;

const CloseBtn = styled.button`
  color: #a8a29e;
  font-size: ${(p) => p.theme.fontSizes.xs};
  background: none;
  border: none;
  cursor: pointer;
  padding: 0 4px;

  &:hover {
    color: #44403c;
  }
`;

const LogPre = styled.pre`
  font-size: 11px;
  color: #57534e;
  font-family: ${(p) => p.theme.fonts.mono};
  padding: 8px;
  margin: 0;
  overflow: auto;
  max-height: 160px;
  white-space: pre-wrap;
`;

const Waiting = styled.span`
  color: #a8a29e;
`;

export function PhaseLogOverlay({ appId, node, phase, onClose, onProgress }: PhaseLogOverlayProps) {
  const [lines, setLines] = useState<string[]>([]);
  const preRef = useRef<HTMLPreElement>(null);
  const maxLines = 200;

  const streamCallback = useCallback(
    (line: string, _source?: string, done?: boolean) => {
      if (line?.startsWith('__PROGRESS__:') && onProgress) {
        try {
          const progress = JSON.parse(line.slice('__PROGRESS__:'.length)) as ResticProgress;
          onProgress(progress);
        } catch {
          // ignore parse errors
        }
        return;
      }

      if (line) {
        setLines((prev) => {
          const next = [...prev, line];
          return next.length > maxLines ? next.slice(-maxLines) : next;
        });
      }

      if (done) return;
    },
    [maxLines, onProgress],
  );

  useStreamSubscription('phase-logs', { appId, node }, streamCallback);

  useEffect(() => {
    if (preRef.current) {
      preRef.current.scrollTop = preRef.current.scrollHeight;
    }
  }, [lines]);

  const label = phaseLabels[phase] || 'Phase logs';

  return (
    <Wrapper>
      <Header>
        <Label>
          {label} — {appId} on {node}
        </Label>
        <CloseBtn onClick={onClose}>✕</CloseBtn>
      </Header>
      <LogPre ref={preRef}>
        {lines.length === 0 ? <Waiting>Waiting for output...</Waiting> : lines.join('\n')}
      </LogPre>
    </Wrapper>
  );
}
