import { useEffect, useState, useRef, useCallback, useMemo } from 'react';
import styled from '@emotion/styled';
import { useStreamSubscription } from '../../../api/live';
import { IconButton } from '../../../components/ui';

interface ContainerLogPanelProps { isOpen: boolean; onClose: () => void; node: string; containerId: string; containerName: string; }

const Panel = styled.div<{ $open: boolean }>`
  position: fixed; right: 0; top: 0; height: 100%; width: 50%;
  background: #fff; border-left: 1px solid ${(p) => p.theme.colors.border};
  transform: ${(p) => (p.$open ? 'translateX(0)' : 'translateX(100%)')};
  transition: transform 300ms; z-index: 50; display: flex; flex-direction: column;
`;
const Header = styled.div`display: flex; align-items: center; justify-content: space-between; padding: 0 16px; height: 48px; border-bottom: 1px solid ${(p) => p.theme.colors.border}; flex-shrink: 0;`;
const Title = styled.span`font-size: ${(p) => p.theme.fontSizes.sm}; font-weight: 600; color: ${(p) => p.theme.colors.textPrimary};`;
const Sub = styled.span`color: #a8a29e; font-size: ${(p) => p.theme.fontSizes.xs};`;
const Dot = styled.span<{ $on: boolean }>`width: 8px; height: 8px; border-radius: 50%; background: ${(p) => (p.$on ? '#10b981' : '#d6d3d1')};`;
const Controls = styled.div`display: flex; align-items: center; gap: 12px; padding: 8px 16px; border-bottom: 1px solid ${(p) => p.theme.colors.borderLight}; flex-shrink: 0;`;
const CheckLabel = styled.label`display: flex; align-items: center; gap: 8px; font-size: 11px; color: #78716c; cursor: pointer;`;
const ErrorMsg = styled.div`padding: 8px 16px; background: #fef2f2; color: #ef4444; font-size: 11px; border-bottom: 1px solid #fecaca;`;
const LogContent = styled.div`flex: 1; overflow: auto; padding: 16px; position: relative;`;
const LogPre = styled.pre`font-size: 11px; font-family: ${(p) => p.theme.fonts.mono}; color: #57534e; white-space: pre-wrap; line-height: 1.6;`;
const Footer = styled.div`padding: 8px 16px; border-top: 1px solid ${(p) => p.theme.colors.borderLight}; font-size: 10px; color: #a8a29e; text-align: center;`;

export function ContainerLogPanel({ isOpen, onClose, node, containerId, containerName }: ContainerLogPanelProps) {
  const [logs, setLogs] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [connected, setConnected] = useState(false);
  const [showAll, setShowAll] = useState(false);
  const [isAtBottom, setIsAtBottom] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);
  const logsRef = useRef<string[]>([]);
  const pendingRef = useRef<string[]>([]);
  const flushTimerRef = useRef<number | null>(null);

  useEffect(() => { logsRef.current = logs; }, [logs]);
  const flushPending = useCallback(() => { flushTimerRef.current = null; if (pendingRef.current.length === 0) return; const chunk = pendingRef.current; pendingRef.current = []; setLogs((prev) => { const n = [...prev, ...chunk]; return n.length > 500 ? n.slice(-500) : n; }); }, []);
  const enqueueLine = useCallback((line: string) => { pendingRef.current.push(line); if (flushTimerRef.current != null) return; flushTimerRef.current = window.setTimeout(flushPending, 80); }, [flushPending]);
  const streamEnabled = isOpen && !!node && !!containerId;
  const streamParams = useMemo(() => (streamEnabled ? { containerId, node, all: showAll ? 'true' : 'false' } : null), [streamEnabled, containerId, node, showAll]);

  useEffect(() => { if (!streamEnabled) { if (flushTimerRef.current != null) { window.clearTimeout(flushTimerRef.current); flushTimerRef.current = null; } pendingRef.current = []; setLogs([]); setError(null); setConnected(false); return; } setLogs([]); setConnected(true); setError(null); }, [streamEnabled, showAll, containerId, node]);

  const handleStream = useCallback((line: string, _source?: string, done?: boolean) => {
    if (line) enqueueLine(line);
    if (done) { flushPending(); setConnected(false); if (line && logsRef.current.length === 0) setError(line); if (!line && logsRef.current.length === 0) setError('log stream ended with no output'); return; }
  }, [enqueueLine, flushPending]);

  useStreamSubscription(streamEnabled ? 'container-logs' : null, streamParams, streamEnabled ? handleStream : null);

  const handleScroll = useCallback(() => { const el = scrollRef.current; if (!el) return; setIsAtBottom(el.scrollHeight - el.scrollTop - el.clientHeight < 40); }, []);
  useEffect(() => { if (isAtBottom && scrollRef.current) scrollRef.current.scrollTop = scrollRef.current.scrollHeight; }, [logs, isAtBottom]);

  return (
    <Panel $open={isOpen}>
      <Header>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}><Title>{containerName}</Title><Sub>({node})</Sub><Dot $on={connected} /></div>
        <IconButton label="Close" variant="ghost" color="gray" size="1" onClick={onClose}><span>×</span></IconButton>
      </Header>
      <Controls>
        <CheckLabel><input type="checkbox" checked={showAll} onChange={(e) => setShowAll(e.target.checked)} style={{ borderRadius: 3 }} />Show all logs (including restarts)</CheckLabel>
      </Controls>
      {error && <ErrorMsg>{error}</ErrorMsg>}
      <LogContent ref={scrollRef} onScroll={handleScroll}>
        <LogPre>{logs.length === 0 ? <span style={{ color: '#a8a29e' }}>Waiting for logs...</span> : logs.join('\n')}</LogPre>
      </LogContent>
      {!isAtBottom && <IconButton label="Scroll to bottom" variant="soft" color="gray" size="1" onClick={() => { if (scrollRef.current) { scrollRef.current.scrollTop = scrollRef.current.scrollHeight; setIsAtBottom(true); } }} style={{ position: 'absolute', right: 16, bottom: 40, boxShadow: '0 1px 3px rgba(0,0,0,0.1)' }}><span>↓</span></IconButton>}
      <Footer>last 500 lines</Footer>
    </Panel>
  );
}
