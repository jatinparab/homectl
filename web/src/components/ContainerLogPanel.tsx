import { useEffect, useState, useRef } from 'react'

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
  const logsEndRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!isOpen || !node || !containerId) {
      setLogs([])
      setError(null)
      setConnected(false)
      return
    }

    const eventSource = new EventSource(
      `/api/v1/containers/${containerId}/logs?node=${node}&all=${showAll}`
    )

    eventSource.onopen = () => {
      setConnected(true)
      setError(null)
    }

    eventSource.onmessage = (e) => {
      setLogs((prev) => [...prev.slice(-500), e.data])
    }

    eventSource.onerror = () => {
      setConnected(false)
      setError('Connection lost. Retrying...')
    }

    return () => {
      eventSource.close()
      setConnected(false)
    }
  }, [isOpen, node, containerId, showAll])

  // Clear logs when toggling showAll
  useEffect(() => {
    setLogs([])
  }, [showAll])

  // Auto-scroll
  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [logs])

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
        <div className="flex-1 overflow-auto p-4">
          <pre className="text-xs font-mono text-gray-300 whitespace-pre-wrap">
            {logs.length === 0 ? (
              <span className="text-gray-500">Waiting for logs...</span>
            ) : (
              logs.join('\n')
            )}
            <div ref={logsEndRef} />
          </pre>
        </div>

        {/* Footer */}
        <div className="p-2 border-t border-gray-700 text-xs text-gray-500 text-center">
          Showing last 500 lines
        </div>
      </div>
    </div>
  )
}
