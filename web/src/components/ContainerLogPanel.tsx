import { useEffect, useState, useRef, useCallback, useMemo } from "react";
import { useStreamSubscription } from "../api/live";

interface ContainerLogPanelProps {
  isOpen: boolean;
  onClose: () => void;
  node: string;
  containerId: string;
  containerName: string;
}

export function ContainerLogPanel({
  isOpen,
  onClose,
  node,
  containerId,
  containerName,
}: ContainerLogPanelProps) {
  const [logs, setLogs] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [connected, setConnected] = useState(false);
  const [showAll, setShowAll] = useState(false);
  const [isAtBottom, setIsAtBottom] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);
  const logsRef = useRef<string[]>([]);
  useEffect(() => {
    logsRef.current = logs;
  }, [logs]);

  const streamEnabled = isOpen && !!node && !!containerId;
  const streamParams = useMemo(() => {
    if (!streamEnabled) return null;
    return { containerId, node, all: showAll ? "true" : "false" };
  }, [streamEnabled, containerId, node, showAll]);

  useEffect(() => {
    if (!streamEnabled) {
      setLogs([]);
      setError(null);
      setConnected(false);
      return;
    }
    setLogs([]);
    setConnected(true);
    setError(null);
  }, [streamEnabled, showAll, containerId, node]);

  const handleStream = useCallback((line: string, _source?: string, done?: boolean) => {
    let shouldShowError = false;
    if (line) {
      setLogs((prev) => {
        if (done && prev.length === 0) {
          shouldShowError = true;
        }
        return [...prev.slice(-499), line];
      });
      if (shouldShowError) {
        setError(line);
      }
    }
    if (done) {
      setConnected(false);
      if (!line && logsRef.current.length === 0) {
        setError("log stream ended with no output");
      }
      return;
    }
  }, []);

  useStreamSubscription(
    streamEnabled ? "container-logs" : null,
    streamParams,
    streamEnabled ? handleStream : null,
  );

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    setIsAtBottom(atBottom);
  }, []);

  // Auto-scroll only when at bottom
  useEffect(() => {
    if (isAtBottom && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [logs, isAtBottom]);

  const scrollToBottom = () => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
      setIsAtBottom(true);
    }
  };

  return (
    <div
      className={`fixed right-0 top-0 h-full w-1/2 bg-white border-l border-[#e2ddd7] transform transition-transform duration-300 ${
        isOpen ? "translate-x-0" : "translate-x-full"
      } z-50 flex flex-col`}
    >
      {/* Header */}
      <div className="flex items-center justify-between px-4 h-12 border-b border-[#e2ddd7] flex-shrink-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-semibold text-[#1c1917]">
            {containerName}
          </span>
          <span className="text-stone-400 text-xs">({node})</span>
          <span
            className={`w-2 h-2 rounded-full ${connected ? "bg-emerald-500" : "bg-stone-300"}`}
          />
        </div>
        <button
          onClick={onClose}
          className="text-stone-400 hover:text-stone-700 text-lg leading-none"
        >
          ×
        </button>
      </div>

      {/* Controls */}
      <div className="flex items-center gap-3 px-4 py-2 border-b border-[#f0ede8] flex-shrink-0">
        <label className="flex items-center gap-2 text-[11px] text-stone-500 cursor-pointer">
          <input
            type="checkbox"
            checked={showAll}
            onChange={(e) => setShowAll(e.target.checked)}
            className="rounded border-stone-300"
          />
          Show all logs (including restarts)
        </label>
      </div>

      {error && (
        <div className="px-4 py-2 bg-red-50 text-red-500 text-[11px] border-b border-red-100">
          {error}
        </div>
      )}

      <div
        className="flex-1 overflow-auto p-4 log-scroll relative"
        ref={scrollRef}
        onScroll={handleScroll}
      >
        <pre className="text-[11px] font-mono text-stone-600 whitespace-pre-wrap leading-relaxed">
          {logs.length === 0 ? (
            <span className="text-stone-400">Waiting for logs...</span>
          ) : (
            logs.join("\n")
          )}
        </pre>
      </div>

      {!isAtBottom && (
        <div className="absolute bottom-10 right-4 z-10">
          <button
            onClick={scrollToBottom}
            className="bg-white border border-[#e2ddd7] text-stone-500 hover:text-stone-800 rounded-full w-7 h-7 flex items-center justify-center shadow-sm text-xs"
          >
            ↓
          </button>
        </div>
      )}

      <div className="px-4 py-2 border-t border-[#f0ede8] text-[10px] text-stone-400 text-center">
        last 500 lines
      </div>
    </div>
  );
}
