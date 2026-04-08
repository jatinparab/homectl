import { useEffect, useState, useRef, useCallback, useMemo } from 'react'
import { useSystemLogSources } from '../api/hooks'

// ── Types ──────────────────────────────────────────────────────────

interface ParsedLog {
  time?: string       // RFC3339 string from slog JSON
  level?: string      // DBG | INF | WRN | ERR
  msg?: string
  component?: string
  area?: string
  [key: string]: string | undefined
}

interface LogEntry {
  id: number
  source: string
  raw: string
  parsed: ParsedLog
  count: number
}

let entryIdCounter = 0

// ── Parsing ────────────────────────────────────────────────────────

/**
 * Logs are emitted as pure JSON by slog.NewJSONHandler and captured in-process.
 * We still scan for '{' as a safety net against any non-JSON noise.
 */
function extractJSON(raw: string): string {
  const idx = raw.indexOf('{')
  return idx >= 0 ? raw.slice(idx) : raw
}

function parseLogLine(line: string): ParsedLog {
  const jsonStr = extractJSON(line)
  try {
    const obj = JSON.parse(jsonStr) as Record<string, unknown>
    const result: ParsedLog = {}
    for (const [k, v] of Object.entries(obj)) {
      if (v !== null && v !== undefined) {
        result[k] = String(v)
      }
    }
    // Normalise common field aliases
    if (!result.msg && result.message) {
      result.msg = result.message
      delete result.message
    }
    return result
  } catch {
    return { msg: line }
  }
}

/**
 * Fingerprint for dedup — derived from parsed content so journalctl's
 * own timestamp prefix doesn't create false uniqueness on reconnect.
 */
function fingerprint(source: string, parsed: ParsedLog, raw: string): string {
  if (parsed.time && parsed.msg) {
    return `${source}|${parsed.time}|${parsed.msg}|${parsed.area ?? ''}|${parsed.component ?? ''}`
  }
  // Fallback for non-JSON lines: use raw content (strips journalctl timestamp via extractJSON)
  return `${source}|${extractJSON(raw)}`
}

function formatLogTime(timeStr?: string): string {
  if (!timeStr) return ''
  try {
    const d = new Date(timeStr)
    if (Number.isNaN(d.getTime())) return timeStr.slice(11, 23)
    const h = d.getHours().toString().padStart(2, '0')
    const m = d.getMinutes().toString().padStart(2, '0')
    const s = d.getSeconds().toString().padStart(2, '0')
    const ms = d.getMilliseconds().toString().padStart(3, '0')
    return `${h}:${m}:${s}.${ms}`
  } catch {
    return timeStr.slice(11, 23)
  }
}

// ── Colours ────────────────────────────────────────────────────────

const levelTextColor: Record<string, string> = {
  DBG: 'text-stone-400',
  INF: 'text-blue-600',
  WRN: 'text-amber-600',
  ERR: 'text-red-600',
}

const levelRowBg: Record<string, string> = {
  WRN: 'bg-amber-50',
  ERR: 'bg-red-50',
}

const LEVELS = ['DBG', 'INF', 'WRN', 'ERR']

// Known prop keys to exclude from the "properties" column (shown elsewhere or noise)
const HIDDEN_PROPS = new Set(['time', 'level', 'msg', 'message', 'component', 'node'])

// ── Component ──────────────────────────────────────────────────────

interface StickyLogPanelProps {
  expanded: boolean
  onToggle: () => void
}

export function SystemLogViewer({ expanded, onToggle }: StickyLogPanelProps) {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [selectedSources, setSelectedSources] = useState<Set<string>>(new Set())
  const [selectedLevels, setSelectedLevels] = useState<Set<string>>(new Set())
  const [selectedAreas, setSelectedAreas] = useState<Set<string>>(new Set())
  const [textFilter, setTextFilter] = useState('')
  const [showFilters, setShowFilters] = useState(false)
  const [isAtBottom, setIsAtBottom] = useState(true)
  const scrollRef = useRef<HTMLDivElement>(null)
  // Fingerprint set persists across reconnects — only cleared on explicit "Clear"
  const seenFingerprints = useRef<Set<string>>(new Set())
  const { data: sources } = useSystemLogSources()

  const addLog = useCallback((source: string, line: string) => {
    const parsed = parseLogLine(line)
    const fp = fingerprint(source, parsed, line)

    setLogs((prev) => {
      // 1. Consecutive identical from same source → increment count
      if (prev.length > 0) {
        const last = prev[prev.length - 1]
        if (last.source === source && fingerprint(source, last.parsed, last.raw) === fp) {
          const updated = [...prev]
          updated[prev.length - 1] = { ...last, count: last.count + 1 }
          return updated
        }
      }

      // 2. Global dedup — skip if we've seen this fingerprint before
      if (seenFingerprints.current.has(fp)) {
        return prev
      }

      seenFingerprints.current.add(fp)
      // Keep the fingerprint set from growing unbounded
      if (seenFingerprints.current.size > 2000) {
        const iter = seenFingerprints.current.values()
        for (let i = 0; i < 500; i++) seenFingerprints.current.delete(iter.next().value as string)
      }

      const id = ++entryIdCounter
      return [...prev.slice(-1499), { id, source, raw: line, parsed, count: 1 }]
    })
  }, [])

  // Single WebSocket for all log sources
  const logSocketRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    if (!expanded) {
      logSocketRef.current?.close()
      logSocketRef.current = null
      return
    }
    if (logSocketRef.current) return // already open

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(`${protocol}//${window.location.host}/api/v1/ws/logs`)
    logSocketRef.current = ws

    ws.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data as string) as { type?: string; source?: string; line?: string }
        if (msg.type === 'log' && msg.source && msg.line) {
          addLog(msg.source, msg.line)
        }
      } catch { /* ignore */ }
    }
    ws.onerror = () => { ws.close(); logSocketRef.current = null }
    ws.onclose = () => { logSocketRef.current = null }

    return () => {
      ws.close()
      logSocketRef.current = null
    }
  }, [expanded, addLog])

  const handleScroll = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    setIsAtBottom(el.scrollHeight - el.scrollTop - el.clientHeight < 40)
  }, [])

  useEffect(() => {
    if (isAtBottom && scrollRef.current && expanded) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [logs, isAtBottom, expanded])

  // Collect unique areas across all logs
  const allAreas = useMemo(() => {
    const areas = new Set<string>()
    for (const entry of logs) {
      if (entry.parsed.area) areas.add(entry.parsed.area)
    }
    return Array.from(areas).sort()
  }, [logs])

  const filteredLogs = useMemo(() => {
    return logs.filter((entry) => {
      if (selectedSources.size > 0 && !selectedSources.has(entry.source)) return false
      if (selectedLevels.size > 0 && !selectedLevels.has(entry.parsed.level ?? '')) return false
      if (selectedAreas.size > 0 && !selectedAreas.has(entry.parsed.area ?? '')) return false
      if (textFilter) {
        const q = textFilter.toLowerCase()
        if (!entry.raw.toLowerCase().includes(q)) return false
      }
      return true
    })
  }, [logs, selectedSources, selectedLevels, selectedAreas, textFilter])

  const errorCount = logs.filter((e) => e.parsed.level === 'ERR').length
  const warnCount = logs.filter((e) => e.parsed.level === 'WRN').length
  const activeFilterCount = selectedSources.size + selectedLevels.size + selectedAreas.size + (textFilter ? 1 : 0)

  function toggleSource(src: string) {
    setSelectedSources((prev) => { const s = new Set(prev); s.has(src) ? s.delete(src) : s.add(src); return s })
  }
  function toggleLevel(lvl: string) {
    setSelectedLevels((prev) => { const s = new Set(prev); s.has(lvl) ? s.delete(lvl) : s.add(lvl); return s })
  }
  function toggleArea(area: string) {
    setSelectedAreas((prev) => { const s = new Set(prev); s.has(area) ? s.delete(area) : s.add(area); return s })
  }
  function clearAllFilters() {
    setSelectedSources(new Set()); setSelectedLevels(new Set()); setSelectedAreas(new Set()); setTextFilter('')
  }
  function clearLogs() {
    setLogs([]); seenFingerprints.current.clear()
  }

  const PANEL_HEIGHT = 320

  return (
    <div
      className="fixed bottom-0 left-0 right-0 z-50 bg-white border-t border-[#e2ddd7]"
      style={{ height: expanded ? `${PANEL_HEIGHT + 40}px` : '40px', transition: 'height 200ms ease' }}
    >
      {/* ── Title bar ── */}
      <div className="h-10 flex items-center px-4 gap-3 select-none" style={{ borderBottom: expanded ? '1px solid #f0ede8' : 'none' }}>
        <button
          onClick={onToggle}
          className="flex items-center gap-2 text-[11px] font-semibold text-stone-500 hover:text-stone-800 tracking-widest uppercase transition-colors"
        >
          <span style={{ display: 'inline-block', transition: 'transform 200ms ease', transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)' }} className="text-stone-400">
            ▶
          </span>
          System Logs
        </button>

        {logs.length > 0 && (
          <span className="text-[10px] text-stone-400 bg-stone-100 px-1.5 py-0.5 rounded">
            {filteredLogs.length}/{logs.length}
          </span>
        )}
        {errorCount > 0 && (
          <span className="text-[10px] font-medium text-red-600 bg-red-50 px-1.5 py-0.5 rounded">
            {errorCount} error{errorCount !== 1 ? 's' : ''}
          </span>
        )}
        {warnCount > 0 && (
          <span className="text-[10px] font-medium text-amber-600 bg-amber-50 px-1.5 py-0.5 rounded">
            {warnCount} warn{warnCount !== 1 ? 's' : ''}
          </span>
        )}

        <div className="flex-1" />

        {expanded && (
          <>
            <input
              type="text"
              placeholder="search..."
              value={textFilter}
              onChange={(e) => setTextFilter(e.target.value)}
              className="px-2 py-0.5 text-[11px] bg-[#f7f5f0] border border-[#e2ddd7] rounded text-stone-600 placeholder-stone-400 w-36 focus:outline-none focus:border-teal-400"
            />
            <button
              onClick={() => setShowFilters((v) => !v)}
              className={`text-[11px] px-2 py-0.5 rounded border transition-colors ${showFilters || activeFilterCount > 0 ? 'border-teal-300 text-teal-700 bg-teal-50' : 'border-[#e2ddd7] text-stone-500 hover:border-stone-400'}`}
            >
              Filters{activeFilterCount > 0 ? ` ·${activeFilterCount}` : ''}
            </button>
            <button
              onClick={clearLogs}
              className="text-[11px] text-stone-400 hover:text-stone-600 px-2 py-0.5 rounded border border-transparent hover:border-[#e2ddd7] transition-colors"
            >
              Clear
            </button>
          </>
        )}
      </div>

      {/* ── Expanded content ── */}
      {expanded && (
        <div className="flex flex-col" style={{ height: `${PANEL_HEIGHT}px` }}>
          {/* Filter strip */}
          {showFilters && (
            <div className="border-b border-[#f0ede8] px-4 py-2 flex flex-wrap gap-x-4 gap-y-1.5 bg-[#faf9f7] flex-shrink-0">
              {/* Sources */}
              {sources && sources.length > 0 && (
                <FilterGroup label="Source">
                  {sources.map((src) => (
                    <FilterChip key={src} label={src} active={selectedSources.has(src)} onClick={() => toggleSource(src)} />
                  ))}
                </FilterGroup>
              )}
              {/* Levels */}
              <FilterGroup label="Level">
                {LEVELS.map((lvl) => (
                  <FilterChip
                    key={lvl} label={lvl} active={selectedLevels.has(lvl)} onClick={() => toggleLevel(lvl)}
                    color={lvl === 'ERR' ? 'red' : lvl === 'WRN' ? 'amber' : undefined}
                  />
                ))}
              </FilterGroup>
              {/* Areas */}
              {allAreas.length > 0 && (
                <FilterGroup label="Area">
                  {allAreas.map((area) => (
                    <FilterChip key={area} label={area} active={selectedAreas.has(area)} onClick={() => toggleArea(area)} />
                  ))}
                </FilterGroup>
              )}
              {activeFilterCount > 0 && (
                <button onClick={clearAllFilters} className="text-[10px] text-stone-400 hover:text-stone-600 self-center ml-auto">
                  Clear filters
                </button>
              )}
            </div>
          )}

          {/* Log table */}
          <div className="flex-1 overflow-auto log-scroll relative" ref={scrollRef} onScroll={handleScroll}>
            {filteredLogs.length === 0 ? (
              <div className="flex items-center justify-center h-full text-[11px] text-stone-400">
                {logs.length === 0 ? 'Waiting for logs...' : 'No logs match current filters'}
              </div>
            ) : (
              <table className="w-full text-[11px]" style={{ tableLayout: 'fixed' }}>
                <colgroup>
                  <col style={{ width: '88px' }} />   {/* Time */}
                  <col style={{ width: '72px' }} />   {/* Source */}
                  <col style={{ width: '36px' }} />   {/* Level */}
                  <col style={{ width: '56px' }} />   {/* Area */}
                  <col />                              {/* Message */}
                  <col style={{ width: '35%' }} />    {/* Props */}
                </colgroup>
                <thead className="sticky top-0 bg-[#f7f5f0] border-b border-[#e2ddd7] z-10">
                  <tr>
                    <th className="px-3 py-1.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Time</th>
                    <th className="px-2 py-1.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Source</th>
                    <th className="px-2 py-1.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Lvl</th>
                    <th className="px-2 py-1.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Area</th>
                    <th className="px-2 py-1.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Message</th>
                    <th className="px-2 py-1.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Properties</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredLogs.map((entry) => (
                    <LogRow key={entry.id} entry={entry} />
                  ))}
                </tbody>
              </table>
            )}
          </div>

          {!isAtBottom && (
            <button
              onClick={() => { if (scrollRef.current) { scrollRef.current.scrollTop = scrollRef.current.scrollHeight; setIsAtBottom(true) } }}
              className="absolute right-4 bottom-3 bg-white border border-[#e2ddd7] text-stone-500 hover:text-stone-800 rounded-full w-7 h-7 flex items-center justify-center shadow-sm text-xs"
            >
              ↓
            </button>
          )}
        </div>
      )}
    </div>
  )
}

// ── Row ────────────────────────────────────────────────────────────

function LogRow({ entry }: { entry: LogEntry }) {
  const { parsed, source, count } = entry
  const level = parsed.level ?? ''
  const bg = levelRowBg[level] ?? ''

  // Collect extra props (exclude well-known columns)
  const extraProps: [string, string][] = []
  for (const [k, v] of Object.entries(parsed)) {
    if (!HIDDEN_PROPS.has(k) && k !== 'area' && v !== undefined) {
      extraProps.push([k, v])
    }
  }

  return (
    <tr className={`border-b border-[#f7f5f0] hover:bg-[#faf9f7] font-mono ${bg}`}>
      <td className="px-3 py-1 text-stone-400 whitespace-nowrap tabular-nums">
        {formatLogTime(parsed.time)}
      </td>
      <td className="px-2 py-1 text-teal-600 truncate">
        {source}
      </td>
      <td className="px-2 py-1 whitespace-nowrap">
        <span className={`font-semibold ${levelTextColor[level] ?? 'text-stone-500'}`}>
          {level}
        </span>
      </td>
      <td className="px-2 py-1 text-stone-400 truncate">
        {parsed.area ?? ''}
      </td>
      <td className="px-2 py-1 text-stone-700">
        <div className="flex items-baseline gap-1.5 min-w-0">
          <span className="truncate">{parsed.msg ?? entry.raw}</span>
          {count > 1 && (
            <span className="flex-shrink-0 text-[10px] text-stone-400 bg-stone-100 px-1 py-0.5 rounded">×{count}</span>
          )}
        </div>
      </td>
      <td className="px-2 py-1">
        <div className="flex flex-wrap gap-x-2 gap-y-0.5">
          {extraProps.map(([k, v]) => (
            <span key={k} className="inline-flex items-center gap-0.5 text-[10px]">
              <span className="text-stone-400">{k}=</span>
              <span className="text-stone-600 font-medium">{v.length > 32 ? v.slice(0, 32) + '…' : v}</span>
            </span>
          ))}
        </div>
      </td>
    </tr>
  )
}

// ── Filter helpers ─────────────────────────────────────────────────

function FilterGroup({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-1.5">
      <span className="text-[10px] text-stone-400 uppercase tracking-wider">{label}</span>
      {children}
    </div>
  )
}

function FilterChip({ label, active, onClick, color }: { label: string; active: boolean; onClick: () => void; color?: 'red' | 'amber' }) {
  const activeClass = color === 'red' ? 'bg-red-100 text-red-700 border-red-300'
    : color === 'amber' ? 'bg-amber-100 text-amber-700 border-amber-300'
    : 'bg-teal-100 text-teal-700 border-teal-300'

  return (
    <button
      onClick={onClick}
      className={`text-[10px] px-1.5 py-0.5 rounded border transition-colors ${active ? activeClass : 'bg-white text-stone-500 border-[#e2ddd7] hover:border-stone-400'}`}
    >
      {label}
    </button>
  )
}
