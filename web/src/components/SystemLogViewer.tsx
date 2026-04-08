import { useEffect, useState, useRef, useCallback, useMemo } from 'react'
import { useSystemLogSources } from '../api/hooks'
import { decodeStreamMessage, openStreamSocket } from '../api/ws'

interface ParsedLog {
  time?: string
  level?: string
  msg?: string
  props: Record<string, string>
}

interface LogEntry {
  id: number
  source: string
  raw: string
  parsed: ParsedLog
  count: number
}

let entryIdCounter = 0

function parseLogLine(line: string): ParsedLog {
  try {
    const obj = JSON.parse(line) as Record<string, unknown>
    const { time, ts, level, lvl, msg, message, ...rest } = obj
    const cleanProps: Record<string, string> = {}
    for (const [k, v] of Object.entries(rest)) {
      if (v !== null && v !== undefined && k !== 'caller' && k !== 'stacktrace') {
        cleanProps[k] = String(v)
      }
    }
    return {
      time: String(time ?? ts ?? ''),
      level: String(level ?? lvl ?? '').toUpperCase(),
      msg: String(msg ?? message ?? line),
      props: cleanProps,
    }
  } catch {
    return { msg: line, props: {} }
  }
}

function formatLogTime(timeStr?: string): string {
  if (!timeStr) return ''
  try {
    const d = new Date(timeStr)
    if (Number.isNaN(d.getTime())) return timeStr.slice(0, 19)
    const h = d.getHours().toString().padStart(2, '0')
    const m = d.getMinutes().toString().padStart(2, '0')
    const s = d.getSeconds().toString().padStart(2, '0')
    const ms = d.getMilliseconds().toString().padStart(3, '0')
    return `${h}:${m}:${s}.${ms}`
  } catch {
    return timeStr.slice(0, 19)
  }
}

const levelColors: Record<string, string> = {
  DEBUG: 'text-stone-400',
  INFO: 'text-blue-600',
  WARN: 'text-amber-600',
  WARNING: 'text-amber-600',
  ERROR: 'text-red-600',
  FATAL: 'text-red-700',
}

const levelBg: Record<string, string> = {
  DEBUG: '',
  INFO: '',
  WARN: 'bg-amber-50',
  WARNING: 'bg-amber-50',
  ERROR: 'bg-red-50',
  FATAL: 'bg-red-100',
}

function getLevelColor(level?: string): string {
  if (!level) return 'text-stone-500'
  return levelColors[level] ?? 'text-stone-500'
}

function getLevelBg(level?: string): string {
  if (!level) return ''
  return levelBg[level] ?? ''
}

const LEVELS = ['DEBUG', 'INFO', 'WARN', 'ERROR']

interface StickyLogPanelProps {
  expanded: boolean
  onToggle: () => void
}

export function SystemLogViewer({ expanded, onToggle }: StickyLogPanelProps) {
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [selectedSources, setSelectedSources] = useState<Set<string>>(new Set())
  const [selectedLevels, setSelectedLevels] = useState<Set<string>>(new Set())
  const [propFilters, setPropFilters] = useState<Record<string, Set<string>>>({})
  const [textFilter, setTextFilter] = useState('')
  const [isAtBottom, setIsAtBottom] = useState(true)
  const [showFilters, setShowFilters] = useState(false)
  const scrollRef = useRef<HTMLDivElement>(null)
  const socketsRef = useRef<Map<string, WebSocket>>(new Map())
  const seenFingerprints = useRef<Map<string, number>>(new Map())
  const { data: sources } = useSystemLogSources()

  const addLog = useCallback((source: string, line: string) => {
    const parsed = parseLogLine(line)
    const fingerprint = `${source}|${line}`

    setLogs((prev) => {
      // Check if last entry from this source is identical → increment count
      const lastIdx = prev.length - 1
      if (lastIdx >= 0 && prev[lastIdx].source === source && prev[lastIdx].raw === line) {
        const updated = [...prev]
        updated[lastIdx] = { ...updated[lastIdx], count: updated[lastIdx].count + 1 }
        return updated
      }

      // Global dedup: skip if we've seen this fingerprint in the last 200 entries
      const lastSeen = seenFingerprints.current.get(fingerprint)
      if (lastSeen !== undefined && entryIdCounter - lastSeen < 200) {
        // Find existing entry and bump count
        const existingIdx = prev.findIndex((e) => e.raw === line && e.source === source)
        if (existingIdx >= 0) {
          const updated = [...prev]
          updated[existingIdx] = { ...updated[existingIdx], count: updated[existingIdx].count + 1 }
          return updated
        }
      }

      const id = ++entryIdCounter
      seenFingerprints.current.set(fingerprint, id)

      // Keep fingerprint map from growing too large
      if (seenFingerprints.current.size > 500) {
        const toDelete = Array.from(seenFingerprints.current.entries())
          .sort((a, b) => a[1] - b[1])
          .slice(0, 200)
          .map((e) => e[0])
        toDelete.forEach((k) => seenFingerprints.current.delete(k))
      }

      return [...prev.slice(-999), { id, source, raw: line, parsed, count: 1 }]
    })
  }, [])

  // Manage websocket connections
  useEffect(() => {
    if (!expanded || !sources) {
      socketsRef.current.forEach((ws) => ws.close())
      socketsRef.current.clear()
      return
    }

    const activeSources = sources

    // Close stale connections
    socketsRef.current.forEach((ws, src) => {
      if (!activeSources.includes(src)) {
        ws.close()
        socketsRef.current.delete(src)
      }
    })

    // Open new connections
    for (const src of activeSources) {
      if (socketsRef.current.has(src)) continue
      const ws = openStreamSocket(`/api/v1/system-logs/${encodeURIComponent(src)}`)
      ws.onmessage = (e) => {
        const line = decodeStreamMessage(e.data)
        if (line == null) return
        addLog(src, line)
      }
      ws.onerror = () => ws.close()
      socketsRef.current.set(src, ws)
    }

    return () => {
      socketsRef.current.forEach((ws) => ws.close())
      socketsRef.current.clear()
    }
  }, [expanded, sources, addLog])

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

  // Collect all unique prop keys and values from logs
  const propOptions = useMemo(() => {
    const map: Record<string, Set<string>> = {}
    for (const entry of logs) {
      for (const [k, v] of Object.entries(entry.parsed.props)) {
        if (!map[k]) map[k] = new Set()
        map[k].add(v)
      }
    }
    return map
  }, [logs])

  const filteredLogs = useMemo(() => {
    return logs.filter((entry) => {
      if (selectedSources.size > 0 && !selectedSources.has(entry.source)) return false
      if (selectedLevels.size > 0) {
        const level = entry.parsed.level
        const normalized = level === 'WARNING' ? 'WARN' : level ?? ''
        if (!selectedLevels.has(normalized)) return false
      }
      for (const [k, vals] of Object.entries(propFilters)) {
        if (vals.size === 0) continue
        const propVal = entry.parsed.props[k]
        if (propVal === undefined || !vals.has(propVal)) return false
      }
      if (textFilter) {
        const q = textFilter.toLowerCase()
        if (!entry.raw.toLowerCase().includes(q)) return false
      }
      return true
    })
  }, [logs, selectedSources, selectedLevels, propFilters, textFilter])

  const errorCount = logs.filter((e) => e.parsed.level === 'ERROR' || e.parsed.level === 'FATAL').length
  const warnCount = logs.filter((e) => e.parsed.level === 'WARN' || e.parsed.level === 'WARNING').length

  const activeFilterCount =
    selectedSources.size + selectedLevels.size + Object.values(propFilters).filter((s) => s.size > 0).length + (textFilter ? 1 : 0)

  function toggleSource(src: string) {
    setSelectedSources((prev) => {
      const next = new Set(prev)
      next.has(src) ? next.delete(src) : next.add(src)
      return next
    })
  }

  function toggleLevel(level: string) {
    setSelectedLevels((prev) => {
      const next = new Set(prev)
      next.has(level) ? next.delete(level) : next.add(level)
      return next
    })
  }

  function togglePropFilter(key: string, value: string) {
    setPropFilters((prev) => {
      const set = new Set(prev[key] ?? [])
      set.has(value) ? set.delete(value) : set.add(value)
      return { ...prev, [key]: set }
    })
  }

  function clearAllFilters() {
    setSelectedSources(new Set())
    setSelectedLevels(new Set())
    setPropFilters({})
    setTextFilter('')
  }

  function clearLogs() {
    setLogs([])
    seenFingerprints.current.clear()
  }

  const PANEL_HEIGHT = expanded ? 320 : 0

  return (
    <div
      className="fixed bottom-0 left-0 right-0 z-50 bg-white border-t border-[#e2ddd7]"
      style={{ height: expanded ? `${PANEL_HEIGHT + 40}px` : '40px', transition: 'height 200ms ease' }}
    >
      {/* Title bar */}
      <div className="h-10 flex items-center px-4 gap-3 border-b border-[#f0ede8] select-none">
        <button
          onClick={onToggle}
          className="flex items-center gap-2 text-[11px] font-semibold text-stone-500 hover:text-stone-800 tracking-widest uppercase transition-colors"
        >
          <span
            className="text-stone-400"
            style={{ transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)', display: 'inline-block', transition: 'transform 200ms ease' }}
          >
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
            {/* Text search */}
            <input
              type="text"
              placeholder="search..."
              value={textFilter}
              onChange={(e) => setTextFilter(e.target.value)}
              className="px-2 py-0.5 text-[11px] bg-[#f7f5f0] border border-[#e2ddd7] rounded text-stone-600 placeholder-stone-400 w-36 focus:outline-none focus:border-teal-400"
            />

            {/* Filter toggle */}
            <button
              onClick={() => setShowFilters((v) => !v)}
              className={`text-[11px] px-2 py-0.5 rounded border transition-colors ${showFilters || activeFilterCount > 0 ? 'border-teal-300 text-teal-700 bg-teal-50' : 'border-[#e2ddd7] text-stone-500 hover:border-stone-400'}`}
            >
              Filters{activeFilterCount > 0 ? ` ·${activeFilterCount}` : ''}
            </button>

            {/* Clear logs */}
            <button
              onClick={clearLogs}
              className="text-[11px] text-stone-400 hover:text-stone-600 px-2 py-0.5 rounded border border-transparent hover:border-[#e2ddd7] transition-colors"
              title="Clear log history"
            >
              Clear
            </button>
          </>
        )}
      </div>

      {/* Expanded content */}
      {expanded && (
        <div className="flex flex-col" style={{ height: `${PANEL_HEIGHT}px` }}>
          {/* Filter panel */}
          {showFilters && (
            <div className="border-b border-[#f0ede8] px-4 py-2 flex flex-wrap gap-3 bg-[#faf9f7]">
              {/* Sources */}
              {sources && sources.length > 0 && (
                <div className="flex items-center gap-1.5">
                  <span className="text-[10px] text-stone-400 uppercase tracking-wider mr-1">Source</span>
                  {sources.map((src) => (
                    <FilterChip
                      key={src}
                      label={src}
                      active={selectedSources.has(src)}
                      onClick={() => toggleSource(src)}
                    />
                  ))}
                </div>
              )}

              {/* Levels */}
              <div className="flex items-center gap-1.5">
                <span className="text-[10px] text-stone-400 uppercase tracking-wider mr-1">Level</span>
                {LEVELS.map((lvl) => (
                  <FilterChip
                    key={lvl}
                    label={lvl}
                    active={selectedLevels.has(lvl)}
                    onClick={() => toggleLevel(lvl)}
                    color={lvl === 'ERROR' ? 'red' : lvl === 'WARN' ? 'amber' : undefined}
                  />
                ))}
              </div>

              {/* Dynamic prop filters */}
              {Object.entries(propOptions).slice(0, 4).map(([key, vals]) => {
                if (vals.size < 2 || vals.size > 12) return null
                return (
                  <div key={key} className="flex items-center gap-1.5">
                    <span className="text-[10px] text-stone-400 uppercase tracking-wider mr-1">{key}</span>
                    {Array.from(vals).slice(0, 8).map((val) => (
                      <FilterChip
                        key={val}
                        label={val}
                        active={!!(propFilters[key]?.has(val))}
                        onClick={() => togglePropFilter(key, val)}
                      />
                    ))}
                  </div>
                )
              })}

              {activeFilterCount > 0 && (
                <button
                  onClick={clearAllFilters}
                  className="text-[10px] text-stone-400 hover:text-stone-600 ml-auto"
                >
                  Clear filters
                </button>
              )}
            </div>
          )}

          {/* Log table */}
          <div
            className="flex-1 overflow-auto log-scroll"
            ref={scrollRef}
            onScroll={handleScroll}
          >
            {filteredLogs.length === 0 ? (
              <div className="flex items-center justify-center h-full text-[11px] text-stone-400">
                {logs.length === 0 ? 'Waiting for logs...' : 'No logs match current filters'}
              </div>
            ) : (
              <table className="w-full text-[11px]">
                <thead className="sticky top-0 bg-[#f7f5f0] border-b border-[#e2ddd7] z-10">
                  <tr>
                    <th className="px-3 py-1.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest w-28">Time</th>
                    <th className="px-2 py-1.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest w-20">Source</th>
                    <th className="px-2 py-1.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest w-14">Level</th>
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

          {/* Scroll to bottom */}
          {!isAtBottom && (
            <button
              onClick={() => {
                if (scrollRef.current) {
                  scrollRef.current.scrollTop = scrollRef.current.scrollHeight
                  setIsAtBottom(true)
                }
              }}
              className="absolute right-4 bottom-3 bg-white border border-[#e2ddd7] text-stone-500 hover:text-stone-800 rounded-full w-7 h-7 flex items-center justify-center shadow-sm text-xs"
              title="Scroll to bottom"
            >
              ↓
            </button>
          )}
        </div>
      )}
    </div>
  )
}

function LogRow({ entry }: { entry: LogEntry }) {
  const { parsed, source, count } = entry
  const level = parsed.level
  const bg = getLevelBg(level)

  return (
    <tr className={`border-b border-[#f7f5f0] hover:bg-[#faf9f7] font-mono ${bg}`}>
      <td className="px-3 py-1 text-stone-400 whitespace-nowrap">
        {formatLogTime(parsed.time)}
      </td>
      <td className="px-2 py-1 text-stone-500 truncate max-w-0">
        <span className="text-teal-600">{source}</span>
      </td>
      <td className="px-2 py-1 whitespace-nowrap">
        <span className={`font-semibold ${getLevelColor(level)}`}>
          {level ? level.slice(0, 4) : ''}
        </span>
      </td>
      <td className="px-2 py-1 text-stone-700 max-w-xs">
        <span className="truncate block">{parsed.msg}</span>
        {count > 1 && (
          <span className="ml-1.5 text-[10px] text-stone-400 bg-stone-100 px-1 py-0.5 rounded">×{count}</span>
        )}
      </td>
      <td className="px-2 py-1">
        <PropsList props={parsed.props} />
      </td>
    </tr>
  )
}

function PropsList({ props }: { props: Record<string, string> }) {
  const entries = Object.entries(props)
  if (entries.length === 0) return null

  return (
    <div className="flex flex-wrap gap-1">
      {entries.map(([k, v]) => (
        <span key={k} className="inline-flex items-center gap-0.5 text-[10px]">
          <span className="text-stone-400">{k}=</span>
          <span className="text-stone-600 font-medium">{String(v).length > 30 ? String(v).slice(0, 30) + '…' : String(v)}</span>
        </span>
      ))}
    </div>
  )
}

function FilterChip({ label, active, onClick, color }: { label: string; active: boolean; onClick: () => void; color?: 'red' | 'amber' }) {
  let activeClass = 'bg-teal-100 text-teal-700 border-teal-300'
  if (color === 'red') activeClass = 'bg-red-100 text-red-700 border-red-300'
  if (color === 'amber') activeClass = 'bg-amber-100 text-amber-700 border-amber-300'

  return (
    <button
      onClick={onClick}
      className={`text-[10px] px-1.5 py-0.5 rounded border transition-colors ${
        active ? activeClass : 'bg-white text-stone-500 border-[#e2ddd7] hover:border-stone-400'
      }`}
    >
      {label}
    </button>
  )
}
