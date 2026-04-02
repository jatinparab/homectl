import { useCallback, useEffect, useRef, useState } from "react";
import type { ReactNode } from "react";

interface BaseLogPanelProps {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  eventSourceUrl: string | null;
  extraControls?: ReactNode;
}

export function BaseLogPanel({ isOpen, onClose, title, eventSourceUrl, extraControls }: BaseLogPanelProps) {
  const [logs, setLogs] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [connected, setConnected] = useState(false);
  const [isAtBottom, setIsAtBottom] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!isOpen || !eventSourceUrl) {
      setLogs([]);
      setError(null);
      setConnected(false);
      return;
    }

    const eventSource = new EventSource(eventSourceUrl);

    eventSource.onopen = () => {
      setConnected(true);
      setError(null);
    };

    eventSource.onmessage = (e) => {
      setLogs((prev) => [...prev.slice(-500), e.data]);
    };

    eventSource.onerror = () => {
      setConnected(false);
      setError("Connection lost. Retrying...");
    };

    return () => {
      eventSource.close();
      setConnected(false);
    };
  }, [isOpen, eventSourceUrl]);

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    setIsAtBottom(atBottom);
  }, []);

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
      className={`fixed right-0 top-0 h-full w-1/2 bg-gray-800 border-l border-gray-700 transform transition-transform duration-300 ${
        isOpen ? "translate-x-0" : "translate-x-full"
      } z-50`}
    >
      <div className="flex flex-col h-full">
        <div className="flex items-center justify-between p-4 border-b border-gray-700">
          <div className="flex items-center gap-3">
            <h3 className="text-white font-medium">{title}</h3>
            <span className={`w-2 h-2 rounded-full ${connected ? "bg-green-500" : "bg-red-500"}`} />
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-white text-xl">
            &times;
          </button>
        </div>

        {extraControls}

        {error && <div className="px-4 py-2 bg-red-900/50 text-red-400 text-sm">{error}</div>}

        <div className="flex-1 overflow-auto p-4 relative" ref={scrollRef} onScroll={handleScroll}>
          <pre className="text-xs font-mono text-gray-300 whitespace-pre-wrap">
            {logs.length === 0 ? <span className="text-gray-500">Waiting for logs...</span> : logs.join("\n")}
          </pre>
        </div>

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

        <div className="p-2 border-t border-gray-700 text-xs text-gray-500 text-center">Showing last 500 lines</div>
      </div>
    </div>
  );
}
