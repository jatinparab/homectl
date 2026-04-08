import { useEffect, useState, useRef, useCallback } from 'react'
import { useSystemLogSources } from '../api/hooks'
import { decodeStreamMessage, openStreamSocket } from '../api/ws'

interface LogEntry {
  source: string
  line: string
}

export function SystemLogViewer() {
  const [expanded, setExpanded] = useState(false)
  const [activeTab, setActiveTab] = useState<string>('All')
  const [logs, setLogs] = useState<LogEntry[]>([])
  const [filter, setFilter] = useState('')
  const [isAtBottom, setIsAtBottom] = useState(true)
  const scrollRef = useRef<HTMLDivElement>(null)
  const socketsRef = useRef<Map<string, WebSocket>>(new Map())
  const { data: sources } = useSystemLogSources()

  const addLog = useCallback((source: string, line: string) => {
    setLogs((prev) => [...prev.slice(-499), { source, line }])
  }, [])

  // Manage websocket connections
  useEffect(() => {
    if (!expanded || !sources) {
      // Close all connections when collapsed
      socketsRef.current.forEach((ws) => ws.close())
      socketsRef.current.clear()
      return
    }

    const activeSources = activeTab === 'All' ? sources : [activeTab]

    // Close connections for sources no longer active
    socketsRef.current.forEach((ws, src) => {
      if (!activeSources.includes(src)) {
        ws.close()
        socketsRef.current.delete(src)
      }
    })

    // Open connections for new active sources
    for (const src of activeSources) {
      if (socketsRef.current.has(src)) continue

      const ws = openStreamSocket(`/api/v1/system-logs/${encodeURIComponent(src)}`)
      ws.onmessage = (e) => {
        const line = decodeStreamMessage(e.data)
        if (line == null) return
        addLog(src, line)
      }
      socketsRef.current.set(src, ws)
    }

    return () => {
      socketsRef.current.forEach((ws) => ws.close())
      socketsRef.current.clear()
    }
  }, [expanded, sources, activeTab, addLog])

  // Clear logs when switching tabs
  useEffect(() => {
    setLogs([])
    setIsAtBottom(true)
  }, [activeTab])

  const handleScroll = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40
    setIsAtBottom(atBottom)
  }, [])

  // Auto-scroll only when at bottom
  useEffect(() => {
    if (isAtBottom && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [logs, isAtBottom])

  const scrollToBottom = () => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
      setIsAtBottom(true)
    }
  }

  const filteredLogs = filter
    ? logs.filter((l) => l.line.toLowerCase().includes(filter.toLowerCase()))
    : logs

  const tabs = ['All', ...(sources || [])]

  return (
    <div className="mb-6">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-2 text-lg font-semibold text-white mb-4 hover:text-gray-300"
      >
        <span className={`transform transition-transform ${expanded ? 'rotate-90' : ''}`}>
          &#9654;
        </span>
        System Logs
      </button>

      {expanded && (
        <div className="bg-gray-800 rounded-lg border border-gray-700">
          {/* Tab bar */}
          <div className="flex items-center gap-1 p-2 border-b border-gray-700 overflow-x-auto">
            {tabs.map((tab) => (
              <button
                key={tab}
                onClick={() => setActiveTab(tab)}
                className={`px-3 py-1 rounded text-sm whitespace-nowrap ${
                  activeTab === tab
                    ? 'bg-blue-600 text-white'
                    : 'text-gray-400 hover:text-white hover:bg-gray-700'
                }`}
              >
                {tab}
              </button>
            ))}
            <div className="flex-1" />
            <input
              type="text"
              placeholder="Filter..."
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              className="px-2 py-1 text-sm bg-gray-900 border border-gray-600 rounded text-gray-300 placeholder-gray-500 w-48"
            />
          </div>

          {/* Log output */}
          <div className="h-80 overflow-auto p-3 relative" ref={scrollRef} onScroll={handleScroll}>
            <pre className="text-xs font-mono text-gray-300 whitespace-pre-wrap">
              {filteredLogs.length === 0 ? (
                <span className="text-gray-500">Waiting for logs...</span>
              ) : (
                filteredLogs.map((entry, i) => (
                  <div key={i}>
                    {activeTab === 'All' && (
                      <span className="text-blue-400">[{entry.source}] </span>
                    )}
                    {entry.line}
                  </div>
                ))
              )}
            </pre>
          </div>

          {/* Scroll to bottom button */}
          {!isAtBottom && (
            <div className="relative">
              <div className="absolute bottom-2 right-4 z-10">
                <button
                  onClick={scrollToBottom}
                  className="bg-gray-700 hover:bg-gray-600 text-white rounded-full w-8 h-8 flex items-center justify-center shadow-lg border border-gray-600"
                  title="Scroll to bottom"
                >
                  &#8595;
                </button>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
