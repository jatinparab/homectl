import { useEffect, useState, useRef, useCallback, useMemo } from 'react';
import styled from '@emotion/styled';
import { useSystemLogSources } from '../../api/hooks';
import { useLiveState } from '../../api/live';
import { Button } from '../../components/ui';

interface ParsedLog { time?: string; level?: string; msg?: string; component?: string; area?: string; [key: string]: string | undefined; }
interface LogEntry { id: number; source: string; raw: string; parsed: ParsedLog; count: number; }
let entryIdCounter = 0;

function extractJSON(raw: string) { const idx = raw.indexOf('{'); return idx >= 0 ? raw.slice(idx) : raw; }
function parseLogLine(line: string): ParsedLog {
  try { const obj = JSON.parse(extractJSON(line)) as Record<string, unknown>; const r: ParsedLog = {}; for (const [k,v] of Object.entries(obj)) { if (v !== null && v !== undefined) r[k] = String(v); } if (!r.msg && r.message) { r.msg = r.message; delete r.message; } return r; } catch { return { msg: line }; }
}
function fingerprint(source: string, parsed: ParsedLog, raw: string) { if (parsed.time && parsed.msg) return `${source}|${parsed.time}|${parsed.msg}|${parsed.area??''}|${parsed.component??''}`; return `${source}|${extractJSON(raw)}`; }
function formatLogTime(timeStr?: string): string {
  if (!timeStr) return ''; try { const d = new Date(timeStr); if (Number.isNaN(d.getTime())) return timeStr.slice(11,23); return `${d.getHours().toString().padStart(2,'0')}:${d.getMinutes().toString().padStart(2,'0')}:${d.getSeconds().toString().padStart(2,'0')}.${d.getMilliseconds().toString().padStart(3,'0')}`; } catch { return timeStr.slice(11,23); }
}

const levelColor: Record<string, string> = { DBG: '#a8a29e', INF: '#2563eb', WRN: '#d97706', ERR: '#dc2626' };
const levelBg: Record<string, string> = { WRN: '#fffbeb', ERR: '#fef2f2' };
const LEVELS = ['DBG','INF','WRN','ERR'];
const HIDDEN_PROPS = new Set(['time','level','msg','message','component','node']);

// ── Styled ─────────────────────────────────
const Wrapper = styled.div<{ $expanded: boolean; $padBottom: number }>`
  position: fixed; bottom: 0; left: 208px; right: 0; z-index: 50;
  background: #fff; border-top: 1px solid #e7e5e4;
  height: ${(p) => (p.$expanded ? `${p.$padBottom + 40}px` : '40px')};
  transition: height 200ms ease;
`;
const TitleBar = styled.div<{ $expanded: boolean }>`height: 40px; display: flex; align-items: center; padding: 0 16px; gap: 12px; user-select: none; border-bottom: ${(p) => (p.$expanded ? '1px solid #f0ede8' : 'none')};`;


const BadgeChip = styled.span`font-size: 10px; padding: 2px 6px; border-radius: 3px;`;
const CountChip = styled(BadgeChip)`background: #f5f5f4; color: #a8a29e;`;
const ErrChip = styled(BadgeChip)`background: #fef2f2; color: #dc2626; font-weight: 500;`;
const WarnChip = styled(BadgeChip)`background: #fffbeb; color: #d97706; font-weight: 500;`;
const Search = styled.input`padding: 2px 8px; font-size: 11px; background: #f7f5f0; border: 1px solid #e2ddd7; border-radius: 3px; color: #57534e; width: 9rem; &:focus { outline: none; border-color: #2dd4bf; } &::placeholder { color: #a8a29e; }`;


const ExpandedBody = styled.div<{ $h: number }>`display: flex; flex-direction: column; height: ${(p) => p.$h}px;`;
const FilterStrip = styled.div`border-bottom: 1px solid #f0ede8; padding: 8px 16px; display: flex; flex-wrap: wrap; gap: 6px 16px; background: #faf9f7; flex-shrink: 0;`;
const FilterGroup = styled.div`display: flex; align-items: center; gap: 6px;`;
const FilterLabel = styled.span`font-size: 10px; color: #a8a29e; text-transform: uppercase; letter-spacing: 0.05em;`;


const LogScrollDiv = styled.div`flex: 1; overflow: auto; position: relative;`;
const Empty = styled.div`display: flex; align-items: center; justify-content: center; height: 100%; font-size: 11px; color: #a8a29e;`;
const LogTable = styled.table`width: 100%; font-size: 11px; table-layout: fixed;`;
const LogThead = styled.thead`position: sticky; top: 0; background: #f7f5f0; border-bottom: 1px solid #e2ddd7; z-index: 10;`;
const LogTh = styled.th`padding: 6px 12px; text-align: left; font-size: 10px; font-weight: 600; color: #a8a29e; text-transform: uppercase; letter-spacing: 0.1em;`;
const LogTr = styled.tr<{ $bg?: string }>`border-bottom: 1px solid #f7f5f0; font-family: ${(p) => p.theme.fonts.mono}; background: ${(p) => p.$bg || 'transparent'}; &:hover { background: #faf9f7; }`;
const LogTd = styled.td`padding: 4px 12px;`;
const LogTdTime = styled(LogTd)`color: #a8a29e; white-space: nowrap; font-variant-numeric: tabular-nums;`;
const LogTdSource = styled(LogTd)`color: #0d9488; overflow: hidden; text-overflow: ellipsis;`;
const LogTdLevel = styled(LogTd)<{ $lvl: string }>`white-space: nowrap; font-weight: 600; color: ${(p) => levelColor[p.$lvl] || '#78716c'};`;
const LogTdArea = styled(LogTd)`color: #a8a29e; overflow: hidden; text-overflow: ellipsis;`;
const LogTdMsg = styled(LogTd)`color: #57534e;`;
const MsgRow = styled.div`display: flex; align-items: baseline; gap: 6px; min-width: 0;`;
const MsgText = styled.span`overflow: hidden; text-overflow: ellipsis; white-space: nowrap;`;
const DupBadge = styled.span`flex-shrink: 0; font-size: 10px; color: #a8a29e; background: #f5f5f4; padding: 0 4px; border-radius: 2px;`;
const PropsTd = styled(LogTd)``;
const PropsWrap = styled.div`display: flex; flex-wrap: wrap; gap: 4px 8px;`;
const PropItem = styled.span`display: inline-flex; align-items: center; gap: 2px; font-size: 10px;`;
const PropKey = styled.span`color: #a8a29e;`;
const PropVal = styled.span`color: #57534e; font-weight: 500;`;


interface StickyLogPanelProps { expanded: boolean; onToggle: () => void; bottomPadding?: number; }

export function SystemLogViewer({ expanded, onToggle, bottomPadding = 320 }: StickyLogPanelProps) {
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [selectedSources, setSelectedSources] = useState<Set<string>>(new Set());
  const [selectedLevels, setSelectedLevels] = useState<Set<string>>(new Set());
  const [selectedAreas, setSelectedAreas] = useState<Set<string>>(new Set());
  const [textFilter, setTextFilter] = useState('');
  const [showFilters, setShowFilters] = useState(false);
  const [isAtBottom, setIsAtBottom] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);
  const { data: sources } = useSystemLogSources();

  const addLog = useCallback((source: string, line: string) => {
    const parsed = parseLogLine(line);
    const fp = fingerprint(source, parsed, line);
    setLogs((prev) => {
      if (prev.length > 0) {
        const last = prev[prev.length - 1];
        if (last.source === source && fingerprint(source, last.parsed, last.raw) === fp) {
          const updated = [...prev];
          updated[prev.length - 1] = { ...last, count: last.count + 1 };
          return updated;
        }
      }
      const id = ++entryIdCounter;
      return [...prev.slice(-1499), { id, source, raw: line, parsed, count: 1 }];
    });
  }, []);

  const { subscribe, unsubscribe } = useLiveState();
  useEffect(() => { if (!expanded) return; const id = subscribe('system-logs', {}, (line, source) => { if (line && source) addLog(source, line); }); return () => unsubscribe(id); }, [expanded, addLog, subscribe, unsubscribe]);

  const handleScroll = useCallback(() => { const el = scrollRef.current; if (!el) return; setIsAtBottom(el.scrollHeight - el.scrollTop - el.clientHeight < 40); }, []);
  useEffect(() => { if (isAtBottom && scrollRef.current && expanded) scrollRef.current.scrollTop = scrollRef.current.scrollHeight; }, [logs, isAtBottom, expanded]);

  const allAreas = useMemo(() => { const a = new Set<string>(); for (const e of logs) if (e.parsed.area) a.add(e.parsed.area); return Array.from(a).sort(); }, [logs]);

  const filteredLogs = useMemo(() => logs.filter((e) => {
    if (selectedSources.size > 0 && !selectedSources.has(e.source)) return false;
    if (selectedLevels.size > 0 && !selectedLevels.has(e.parsed.level ?? '')) return false;
    if (selectedAreas.size > 0 && !selectedAreas.has(e.parsed.area ?? '')) return false;
    if (textFilter && !e.raw.toLowerCase().includes(textFilter.toLowerCase())) return false;
    return true;
  }), [logs, selectedSources, selectedLevels, selectedAreas, textFilter]);

  const errorCount = logs.filter(e => e.parsed.level === 'ERR').length;
  const warnCount = logs.filter(e => e.parsed.level === 'WRN').length;
  const activeFilterCount = selectedSources.size + selectedLevels.size + selectedAreas.size + (textFilter ? 1 : 0);

  function toggleSource(src: string) { setSelectedSources(p => { const s = new Set(p); s.has(src) ? s.delete(src) : s.add(src); return s; }); }
  function toggleLevel(lvl: string) { setSelectedLevels(p => { const s = new Set(p); s.has(lvl) ? s.delete(lvl) : s.add(lvl); return s; }); }
  function toggleArea(area: string) { setSelectedAreas(p => { const s = new Set(p); s.has(area) ? s.delete(area) : s.add(area); return s; }); }

  return (
    <Wrapper $expanded={expanded} $padBottom={bottomPadding}>
      <TitleBar $expanded={expanded}>
        <Button variant="ghost" size="sm" onClick={onToggle}><span style={{ display: 'inline-block', transition: 'transform 200ms', transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)' }}>▶</span> System Logs</Button>
        {logs.length > 0 && <CountChip>{filteredLogs.length}/{logs.length}</CountChip>}
        {errorCount > 0 && <ErrChip>{errorCount} error{errorCount !== 1 ? 's' : ''}</ErrChip>}
        {warnCount > 0 && <WarnChip>{warnCount} warn{warnCount !== 1 ? 's' : ''}</WarnChip>}
        <div style={{ flex: 1 }} />
        {expanded && (
          <>
            <Search type="text" placeholder="search..." value={textFilter} onChange={(e) => setTextFilter(e.target.value)} />
            <Button variant={showFilters || activeFilterCount > 0 ? 'primary' : 'secondary'} size="sm" onClick={() => setShowFilters(v => !v)}>Filters{activeFilterCount > 0 ? ` ·${activeFilterCount}` : ''}</Button>
            <Button variant="ghost" size="sm" onClick={() => setLogs([])}>Clear</Button>
          </>
        )}
      </TitleBar>

      {expanded && (
        <ExpandedBody $h={bottomPadding}>
          {showFilters && (
            <FilterStrip>
              {sources && sources.length > 0 && <FilterGroup><FilterLabel>Source</FilterLabel>{sources.map(src => <Button variant={selectedSources.has(src) ? 'primary' : 'secondary'} size="sm" key={src} onClick={() => toggleSource(src)}>{src}</Button>)}</FilterGroup>}
              <FilterGroup><FilterLabel>Level</FilterLabel>{LEVELS.map(lvl => <Button variant={selectedLevels.has(lvl) ? lvl === 'ERR' ? 'destructive' : 'primary' : 'secondary'} size="sm" key={lvl} onClick={() => toggleLevel(lvl)}>{lvl}</Button>)}</FilterGroup>
              {allAreas.length > 0 && <FilterGroup><FilterLabel>Area</FilterLabel>{allAreas.map(area => <Button variant={selectedAreas.has(area) ? 'primary' : 'secondary'} size="sm" key={area} onClick={() => toggleArea(area)}>{area}</Button>)}</FilterGroup>}
              {activeFilterCount > 0 && <Button variant="ghost" size="sm" onClick={() => { setSelectedSources(new Set()); setSelectedLevels(new Set()); setSelectedAreas(new Set()); setTextFilter(''); }}>Clear filters</Button>}
            </FilterStrip>
          )}
          <LogScrollDiv ref={scrollRef} onScroll={handleScroll}>
            {filteredLogs.length === 0 ? (
              <Empty>{logs.length === 0 ? 'Waiting for logs...' : 'No logs match current filters'}</Empty>
            ) : (
              <LogTable>
                <colgroup><col style={{ width: 88 }} /><col style={{ width: 72 }} /><col style={{ width: 36 }} /><col style={{ width: 56 }} /><col /><col style={{ width: '35%' }} /></colgroup>
                <LogThead><tr><LogTh>Time</LogTh><LogTh>Source</LogTh><LogTh>Lvl</LogTh><LogTh>Area</LogTh><LogTh>Message</LogTh><LogTh>Properties</LogTh></tr></LogThead>
                <tbody>
                  {filteredLogs.map(entry => {
                    const { parsed, source, count } = entry;
                    const level = parsed.level ?? '';
                    const extraProps: [string, string][] = [];
                    for (const [k, v] of Object.entries(parsed)) { if (!HIDDEN_PROPS.has(k) && k !== 'area' && v !== undefined) extraProps.push([k, v]); }
                    return (
                      <LogTr key={entry.id} $bg={levelBg[level]}>
                        <LogTdTime>{formatLogTime(parsed.time)}</LogTdTime>
                        <LogTdSource>{source}</LogTdSource>
                        <LogTdLevel $lvl={level}>{level}</LogTdLevel>
                        <LogTdArea>{parsed.area ?? ''}</LogTdArea>
                        <LogTdMsg><MsgRow><MsgText>{parsed.msg ?? entry.raw}</MsgText>{count > 1 && <DupBadge>×{count}</DupBadge>}</MsgRow></LogTdMsg>
                        <PropsTd><PropsWrap>{extraProps.map(([k, v]) => <PropItem key={k}><PropKey>{k}=</PropKey><PropVal>{v.length > 32 ? v.slice(0, 32) + '…' : v}</PropVal></PropItem>)}</PropsWrap></PropsTd>
                      </LogTr>
                    );
                  })}
                </tbody>
              </LogTable>
            )}
            {!isAtBottom && <Button variant="secondary" size="sm" style={{ position: 'absolute', right: 16, bottom: 12, borderRadius: '50%', width: 28, height: 28, padding: 0 }} onClick={() => { if (scrollRef.current) { scrollRef.current.scrollTop = scrollRef.current.scrollHeight; setIsAtBottom(true); } }}>↓</Button>}
          </LogScrollDiv>
        </ExpandedBody>
      )}
    </Wrapper>
  );
}
