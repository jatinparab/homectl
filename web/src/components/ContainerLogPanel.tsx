import { useEffect, useState, useRef, useCallback } from 'react'
import { decodeStreamMessage, openStreamSocket } from '../api/ws'

interface ContainerLogPanelProps {
  isOpen: boolean
  onClose: () => void
  node: string
  containerId: string
  containerName: string
}

export function ContainerLogPanel({
  isOpen,
  onClose,
  node,
  containerId,
  containerName,
}: ContainerLogPanelProps) {
  const [logs, setLogs] = useState<string[]>([])
  const [error, setError] = useState<string | null>(null)
  const [connected, setConnected] = useState(false)
  const [showAll, setShowAll] = useState(false)
  const [isAtBottom, setIsAtBottom] = useState(true)
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!isOpen || !node || !containerId) {
      setLogs([])
      setError(null)
      setConnected(false)
      return
    }

    const ws = openStreamSocket(
      `/api/v1/containers/${encodeURIComponent(containerId)}/logs?node=${encodeURIComponent(node)}&all=${showAll}`
    )

    ws.onopen = () => {
      setConnected(true)
      setError(null)
    }

    ws.onmessage = (e) => {
      const line = decodeStreamMessage(e.data)
      if (line == null) return
      setLogs((prev) => [...prev.slice(-500), line])
    }

    ws.onerror = () => {
      setConnected(false)
      setError('Connection lost. Retrying...')
    }

    ws.onclose = () => {
      setConnected(false)
    }

    return () => {
      ws.close()
      setConnected(false)
    }
  }, [isOpen, node, containerId, showAll])

  // Clear logs when toggling showAll
  useEffect(() => {
    setLogs([])
  }, [showAll])

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

  return (
    <div
      className={`fixed right-0 top-0 h-full w-1/2 bg-gray-800 border-l border-gray-700 transform transition-transform duration-300 ${
        isOpen ? 'translate-x-0' : 'translate-x-full'
      } z-50`}
    >
      <div className="flex flex-col h-full">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-gray-700">
          <div className="flex items-center gap-3">
            <h3 className="text-white font-medium">
              {containerName} <span className="text-gray-400">({node})</span>
            </h3>
            <span
              className={`w-2 h-2 rounded-full ${connected ? 'bg-green-500' : 'bg-red-500'}`}
            />
          </div>
          <button
            onClick={onClose}
            className="text-gray-400 hover:text-white text-xl"
          >
            &times;
          </button>
        </div>

        {/* Controls */}
        <div className="flex items-center gap-3 px-4 py-2 border-b border-gray-700">
          <label className="flex items-center gap-2 text-sm text-gray-400 cursor-pointer">
            <input
              type="checkbox"
              checked={showAll}
              onChange={(e) => setShowAll(e.target.checked)}
              className="rounded border-gray-600"
            />
            Show all logs (including restarts)
          </label>
        </div>

        {/* Error banner */}
        {error && (
          <div className="px-4 py-2 bg-red-900/50 text-red-400 text-sm">
            {error}
          </div>
        )}

        {/* Logs */}
        <div className="flex-1 overflow-auto p-4 relative" ref={scrollRef} onScroll={handleScroll}>
          <pre className="text-xs font-mono text-gray-300 whitespace-pre-wrap">
            {logs.length === 0 ? (
              <span className="text-gray-500">Waiting for logs...</span>
            ) : (
              logs.join('\n')
            )}
          </pre>
        </div>

        {/* Scroll to bottom button */}
        {!isAtBottom && (
          <div className="absolute bottom-12 right-6 z-10">
            <button
              onClick={scrollToBottom}
              className="bg-gray-700 hover:bg-gray-600 text-white rounded-full w-8 h-8 flex items-center justify-center shadow-lg border border-gray-600"
              title="Scroll to bottom"
            >
              &#8595;
            </button>
          </div>
        )}

        {/* Footer */}
        <div className="p-2 border-t border-gray-700 text-xs text-gray-500 text-center">
          Showing last 500 lines
        </div>
      </div>
    </div>
  )
}
