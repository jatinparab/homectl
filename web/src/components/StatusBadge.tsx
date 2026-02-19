import type { ObservedState, HealthStatus, ResticProgress } from "../api/types";

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const value = bytes / Math.pow(1024, i);
  return `${value.toFixed(i > 0 ? 1 : 0)} ${units[i]}`;
}

const statusColors: Record<ObservedState, string> = {
  running: "bg-green-500",
  stopped: "bg-gray-500",
  restarting: "bg-yellow-500",
  error: "bg-red-500",
  unknown: "bg-gray-500",
};

const healthColors: Record<HealthStatus, string> = {
  healthy: "bg-green-500",
  unhealthy: "bg-red-500",
  unknown: "bg-yellow-500",
};

const phaseColors: Record<string, string> = {
  running: "bg-green-500",
  ready: "bg-blue-500",
  restoring: "bg-yellow-500",
  error: "bg-red-500",
  empty: "bg-gray-500",
  backup_stopping: "bg-amber-500",
  backup_stopped: "bg-amber-500",
  backing_up: "bg-amber-500",
  backup_pruning: "bg-purple-500",
  backup_checking: "bg-purple-500",
  pulling: "bg-blue-500",
};

const postBackupPhaseColors: Record<string, string> = {
  backup_pruning: "bg-purple-500",
  backup_checking: "bg-purple-500",
};

const desiredColors: Record<string, string> = {
  running: "bg-green-600",
  stopped: "bg-gray-600",
};

interface StatusBadgeProps {
  status: ObservedState;
}

export function StatusBadge({ status }: StatusBadgeProps) {
  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium text-white ${statusColors[status]}`}
    >
      {status}
    </span>
  );
}

interface HealthBadgeProps {
  health: HealthStatus;
}

export function HealthBadge({ health }: HealthBadgeProps) {
  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium text-white ${healthColors[health]}`}
    >
      {health}
    </span>
  );
}

interface PhaseBadgeProps {
  phase: string;
  progress?: ResticProgress;
}

export function PhaseBadge({ phase, progress }: PhaseBadgeProps) {
  if (!phase) {
    return <span className="text-gray-500">-</span>;
  }
  const color = phaseColors[phase] || "bg-gray-500";

  if (progress && (phase === "restoring" || phase === "backing_up")) {
    const pct = Math.round(progress.percent_done * 100);
    return (
      <div className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5">
        <span
          className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium text-white ${color}`}
        >
          {phase}
        </span>
        <span className="inline-flex items-center gap-1 text-xs text-gray-300">
          <span className="w-16 h-1.5 bg-gray-600 rounded-full overflow-hidden">
            <span
              className="block h-full bg-yellow-400 rounded-full transition-all"
              style={{ width: `${pct}%` }}
            />
          </span>
          <span>{pct}%</span>
          <span className="text-gray-500 whitespace-nowrap">
            {formatBytes(progress.bytes_done)}/{formatBytes(progress.total_bytes)}
          </span>
        </span>
      </div>
    );
  }

  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium text-white ${color}`}
    >
      {phase}
    </span>
  );
}

interface DesiredBadgeProps {
  state: string;
}

export function DesiredBadge({ state }: DesiredBadgeProps) {
  const color = desiredColors[state] || "bg-gray-600";
  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium text-white ${color}`}
    >
      {state}
    </span>
  );
}

const containerStateColors: Record<string, string> = {
  running: "bg-green-500",
  exited: "bg-red-500",
  restarting: "bg-yellow-500",
  created: "bg-gray-500",
  paused: "bg-yellow-500",
  dead: "bg-red-500",
};

interface ContainerStateBadgeProps {
  state: string;
}

export function ContainerStateBadge({ state }: ContainerStateBadgeProps) {
  const color = containerStateColors[state] || "bg-gray-500";
  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium text-white ${color}`}
    >
      {state}
    </span>
  );
}

interface PostBackupPhaseBadgeProps {
  phase: string;
  onClick?: () => void;
  progress?: ResticProgress;
}

export function PostBackupPhaseBadge({ phase, onClick, progress }: PostBackupPhaseBadgeProps) {
  const color = postBackupPhaseColors[phase] || "bg-purple-500";
  const showProgress = !!progress;
  const pct = progress ? Math.round(progress.percent_done * 100) : 0;

  if (onClick) {
    return (
      <button onClick={onClick} className="relative cursor-pointer hover:opacity-80" title="Toggle post-backup logs">
        <div className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5">
          <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium text-white ${color}`}>
            {phase}
          </span>
          {showProgress && (
            <span className="inline-flex items-center gap-1 text-xs text-gray-300">
              <span className="w-16 h-1.5 bg-gray-600 rounded-full overflow-hidden">
                <span
                  className="block h-full bg-purple-300 rounded-full transition-all"
                  style={{ width: `${pct}%` }}
                />
              </span>
              <span>{pct}%</span>
              {progress && progress.total_files > 0 && (
                <span className="text-gray-500 whitespace-nowrap">
                  {progress.files_done}/{progress.total_files}
                </span>
              )}
            </span>
          )}
        </div>
        <span className="absolute -top-0.5 -right-1.5 w-1.5 h-1.5 rounded-full bg-purple-400" />
      </button>
    );
  }

  if (showProgress) {
    return (
      <div className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5">
        <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium text-white ${color}`}>
          {phase}
        </span>
        <span className="inline-flex items-center gap-1 text-xs text-gray-300">
          <span className="w-16 h-1.5 bg-gray-600 rounded-full overflow-hidden">
            <span
              className="block h-full bg-purple-300 rounded-full transition-all"
              style={{ width: `${pct}%` }}
            />
          </span>
          <span>{pct}%</span>
          {progress && progress.total_files > 0 && (
            <span className="text-gray-500 whitespace-nowrap">
              {progress.files_done}/{progress.total_files}
            </span>
          )}
        </span>
      </div>
    );
  }

  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded text-xs font-medium text-white ${color}`}>
      {phase}
    </span>
  );
}
