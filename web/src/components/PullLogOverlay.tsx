import { useEffect, useRef, useState } from "react";
import type { ResticProgress } from "../api/types";
import { decodeStreamMessage, openStreamSocket } from "../api/ws";

interface PhaseLogOverlayProps {
  appId: string;
  node: string;
  phase: string;
  onClose: () => void;
  onProgress?: (progress: ResticProgress) => void;
}

const phaseLabels: Record<string, string> = {
  pulling: "Pull logs",
  restoring: "Restore logs",
  backing_up: "Backup logs",
  backup_pruning: "Prune logs",
  backup_checking: "Check logs",
};

export function PhaseLogOverlay({ appId, node, phase, onClose, onProgress }: PhaseLogOverlayProps) {
  const [lines, setLines] = useState<string[]>([]);
  const preRef = useRef<HTMLPreElement>(null);
  const maxLines = 200;

  useEffect(() => {
    const path = `/api/v1/apps/${encodeURIComponent(appId)}/phase-logs?node=${encodeURIComponent(node)}`;
    const ws = openStreamSocket(path);

    ws.onmessage = (e) => {
      const line = decodeStreamMessage(e.data);
      if (line == null) {
        return;
      }

      if (line.startsWith("__PROGRESS__:") && onProgress) {
        try {
          const progress = JSON.parse(line.slice("__PROGRESS__:".length)) as ResticProgress;
          onProgress(progress);
        } catch {
          // ignore parse errors
        }
        return;
      }

      setLines((prev) => {
        const next = [...prev, line];
        return next.length > maxLines ? next.slice(-maxLines) : next;
      });
    };

    ws.onerror = () => {
      ws.close();
    };

    return () => {
      ws.close();
    };
  }, [appId, node, onProgress]);

  useEffect(() => {
    if (preRef.current) {
      preRef.current.scrollTop = preRef.current.scrollHeight;
    }
  }, [lines]);

  const label = phaseLabels[phase] || "Phase logs";

  return (
    <div className="relative bg-gray-900 border border-gray-700 rounded mx-4 my-1">
      <div className="flex items-center justify-between px-3 py-1 border-b border-gray-700">
        <span className="text-xs text-gray-400">
          {label} — {appId} on {node}
        </span>
        <button
          onClick={onClose}
          className="text-gray-500 hover:text-gray-300 text-sm px-1"
        >
          ✕
        </button>
      </div>
      <pre
        ref={preRef}
        className="text-xs text-gray-300 font-mono p-2 overflow-auto max-h-40 whitespace-pre-wrap"
      >
        {lines.length === 0 ? (
          <span className="text-gray-500">Waiting for output...</span>
        ) : (
          lines.join("\n")
        )}
      </pre>
    </div>
  );
}
