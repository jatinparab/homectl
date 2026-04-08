import { useEffect, useRef, useState } from "react";
import type { ResticProgress } from "../api/types";
import { useLiveState } from "../api/live";

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
  const { subscribe, unsubscribe } = useLiveState();

  useEffect(() => {
    const id = subscribe('phase-logs', { appId, node }, (line, _source, done) => {
      if (done || !line) return;

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
    });

    return () => {
      unsubscribe(id);
    };
  }, [appId, node, onProgress, subscribe, unsubscribe]);

  useEffect(() => {
    if (preRef.current) {
      preRef.current.scrollTop = preRef.current.scrollHeight;
    }
  }, [lines]);

  const label = phaseLabels[phase] || "Phase logs";

  return (
    <div className="relative bg-[#faf9f7] border border-[#e2ddd7] rounded mx-4 my-1">
      <div className="flex items-center justify-between px-3 py-1.5 border-b border-[#e2ddd7]">
        <span className="text-[11px] text-stone-500">
          {label} — {appId} on {node}
        </span>
        <button
          onClick={onClose}
          className="text-stone-400 hover:text-stone-700 text-xs px-1"
        >
          ✕
        </button>
      </div>
      <pre
        ref={preRef}
        className="text-[11px] text-stone-600 font-mono p-2 overflow-auto max-h-40 whitespace-pre-wrap log-scroll"
      >
        {lines.length === 0 ? (
          <span className="text-stone-400">Waiting for output...</span>
        ) : (
          lines.join("\n")
        )}
      </pre>
    </div>
  );
}
