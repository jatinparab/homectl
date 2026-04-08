import type { ObservedState, HealthStatus, ResticProgress } from "../api/types";

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const value = bytes / Math.pow(1024, i);
  return `${value.toFixed(i > 0 ? 1 : 0)} ${units[i]}`;
}

const statusTextColors: Record<ObservedState, string> = {
  running: "text-emerald-700",
  stopped: "text-stone-400",
  restarting: "text-amber-600",
  error: "text-red-600",
  unknown: "text-stone-400",
};

interface StatusTextProps {
  status: ObservedState;
}

export function StatusBadge({ status }: StatusTextProps) {
  return (
    <span className={`text-xs font-medium ${statusTextColors[status]}`}>
      {status}
    </span>
  );
}

interface HealthDotProps {
  health: HealthStatus;
}

export function HealthBadge({ health }: HealthDotProps) {
  const dotColors: Record<HealthStatus, string> = {
    healthy: "bg-emerald-500",
    unhealthy: "bg-red-500",
    unknown: "bg-amber-400",
  };

  return (
    <div className="tooltip-container inline-flex items-center">
      <span
        className={`inline-block w-2.5 h-2.5 rounded-full ${dotColors[health]} ${health === "healthy" ? "health-glow" : ""}`}
      />
      <span className="tooltip-content">{health}</span>
    </div>
  );
}

const phaseTextColors: Record<string, string> = {
  running: "text-emerald-700",
  ready: "text-blue-600",
  restoring: "text-amber-600",
  error: "text-red-600",
  empty: "text-stone-400",
  backup_stopping: "text-amber-600",
  backup_stopped: "text-amber-600",
  backing_up: "text-amber-600",
  backup_pruning: "text-violet-600",
  backup_checking: "text-violet-600",
  pulling: "text-blue-600",
};

interface PhaseBadgeProps {
  phase: string;
  progress?: ResticProgress;
}

export function PhaseBadge({ phase, progress }: PhaseBadgeProps) {
  if (!phase) return <span className="text-stone-400 text-xs">—</span>;
  const color = phaseTextColors[phase] || "text-stone-500";

  if (progress && (phase === "restoring" || phase === "backing_up")) {
    const pct = Math.round(progress.percent_done * 100);
    return (
      <div className="flex items-center gap-2">
        <span className={`text-xs font-medium ${color}`}>{phase}</span>
        <span className="flex items-center gap-1 text-xs text-stone-400">
          <span className="w-14 h-1 bg-stone-200 rounded-full overflow-hidden">
            <span
              className="block h-full bg-amber-400 rounded-full transition-all"
              style={{ width: `${pct}%` }}
            />
          </span>
          <span className="text-[10px]">{pct}%</span>
          <span className="text-[10px] text-stone-400">
            {formatBytes(progress.bytes_done)}/{formatBytes(progress.total_bytes)}
          </span>
        </span>
      </div>
    );
  }

  return <span className={`text-xs font-medium ${color}`}>{phase}</span>;
}

interface DesiredToggleProps {
  state: string;
  onToggle: () => void;
  disabled?: boolean;
}

export function DesiredToggle({ state, onToggle, disabled }: DesiredToggleProps) {
  const isRunning = state === "running";
  return (
    <div className="tooltip-container inline-flex items-center">
      <button
        role="switch"
        aria-checked={isRunning}
        onClick={onToggle}
        disabled={disabled}
        className="relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer items-center rounded-full transition-colors disabled:opacity-40 disabled:cursor-not-allowed focus:outline-none"
        style={{ backgroundColor: isRunning ? "#0d9488" : "#d6d3d1" }}
      >
        <span
          className="toggle-thumb inline-block h-3.5 w-3.5 rounded-full bg-white"
          style={{ transform: isRunning ? "translateX(18px)" : "translateX(2px)" }}
        />
      </button>
      <span className="tooltip-content">{isRunning ? "should be running" : "should be stopped"}</span>
    </div>
  );
}

interface PostBackupPhaseBadgeProps {
  phase: string;
  onClick?: () => void;
  progress?: ResticProgress;
}

const postBackupColors: Record<string, string> = {
  backup_pruning: "text-violet-600",
  backup_checking: "text-violet-600",
};

export function PostBackupPhaseBadge({ phase, onClick, progress }: PostBackupPhaseBadgeProps) {
  const color = postBackupColors[phase] || "text-violet-600";
  const pct = progress ? Math.round(progress.percent_done * 100) : 0;

  const content = (
    <div className="flex items-center gap-1.5">
      <span className={`text-xs font-medium ${color}`}>{phase}</span>
      {progress && (
        <span className="flex items-center gap-1 text-xs text-stone-400">
          <span className="w-12 h-1 bg-stone-200 rounded-full overflow-hidden">
            <span
              className="block h-full bg-violet-400 rounded-full transition-all"
              style={{ width: `${pct}%` }}
            />
          </span>
          <span className="text-[10px]">{pct}%</span>
        </span>
      )}
    </div>
  );

  if (onClick) {
    return (
      <button onClick={onClick} className="relative hover:opacity-70 cursor-pointer">
        {content}
        <span className="absolute -top-0.5 -right-1.5 w-1.5 h-1.5 rounded-full bg-violet-400" />
      </button>
    );
  }

  return content;
}

const containerStateColors: Record<string, string> = {
  running: "text-emerald-700",
  exited: "text-red-600",
  restarting: "text-amber-600",
  created: "text-stone-500",
  paused: "text-amber-600",
  dead: "text-red-600",
};

interface ContainerStateBadgeProps {
  state: string;
}

export function ContainerStateBadge({ state }: ContainerStateBadgeProps) {
  const color = containerStateColors[state] || "text-stone-500";
  return <span className={`text-xs font-medium ${color}`}>{state}</span>;
}
