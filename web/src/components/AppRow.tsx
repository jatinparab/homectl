import { useState } from "react";
import type { AppStatus } from "../api/types";
import { useTriggerBackup } from "../api/hooks";
import {
  StatusBadge,
  HealthBadge,
  PhaseBadge,
  DesiredBadge,
} from "./StatusBadge";

const BACKUP_PHASES = ["backup_stopping", "backup_stopped", "backing_up"];

interface AppRowProps {
  app: AppStatus;
  onShowLogs?: (node: string, appId: string) => void;
}

export function AppRow({ app, onShowLogs }: AppRowProps) {
  const [expanded, setExpanded] = useState(false);
  const backupMutation = useTriggerBackup();

  // Check if there are details to show (node reports with errors, quarantine, or multiple nodes)
  const hasDetails =
    app.node_reports.length > 0 &&
    (app.node_reports.some(
      (nr) => nr.last_error || nr.quarantined || nr.port > 0,
    ) ||
      app.node_reports.length > 1);

  // Get total restart count from all nodes
  const totalRestarts = app.node_reports.reduce(
    (sum, nr) => sum + nr.restart_count,
    0,
  );

  // Get node names for display
  const nodeNames = app.node_reports.map((nr) => nr.node).join(", ") || "-";

  return (
    <>
      <tr
        className={`border-b border-gray-700 hover:bg-gray-750 cursor-pointer ${expanded ? "bg-gray-750" : ""}`}
        onClick={() => hasDetails && setExpanded(!expanded)}
      >
        <td className="px-4 py-3 whitespace-nowrap">
          <div className="flex items-center gap-2">
            {hasDetails && (
              <span
                className={`text-gray-400 transition-transform ${expanded ? "rotate-90" : ""}`}
              >
                {">"}
              </span>
            )}
            {!hasDetails && <span className="w-3" />}
            <span className="font-medium text-white">{app.app_id}</span>
          </div>
        </td>
        <td className="px-4 py-3 whitespace-nowrap">
          <DesiredBadge state={app.desired_state} />
        </td>
        <td className="px-4 py-3 whitespace-nowrap">
          <StatusBadge status={app.observed_state} />
        </td>
        <td className="px-4 py-3 whitespace-nowrap">
          <HealthBadge health={app.health} />
        </td>
        <td className="px-4 py-3 whitespace-nowrap">
          <PhaseBadge phase={app.phase} />
        </td>
        <td className="px-4 py-3 whitespace-nowrap text-gray-300">
          {nodeNames}
        </td>
        <td className="px-4 py-3 whitespace-nowrap text-gray-300">
          {totalRestarts}
        </td>
      </tr>
      {expanded && hasDetails && (
        <tr className="bg-gray-800">
          <td colSpan={7} className="px-4 py-3 pl-12">
            <div className="space-y-2 text-sm">
              {app.node_reports.map((nr) => (
                <div
                  key={nr.node}
                  className="flex items-center gap-4 text-gray-400"
                >
                  <span className="text-gray-500">└─</span>
                  <span
                    className={`font-medium ${nr.node_online ? "text-green-400" : "text-gray-500"}`}
                  >
                    {nr.node}
                  </span>
                  <StatusBadge status={nr.observed_state} />
                  <HealthBadge health={nr.health} />
                  {nr.phase && (
                    <span className="text-gray-500">Phase: {nr.phase}</span>
                  )}
                  {nr.port > 0 && (
                    <span className="text-gray-500">Port: {nr.port}</span>
                  )}
                  {nr.restart_count > 0 && (
                    <span className="text-gray-500">
                      Restarts: {nr.restart_count}
                    </span>
                  )}
                  {nr.quarantined && (
                    <span className="text-yellow-400">Quarantined</span>
                  )}
                  {nr.last_error && (
                    <span className="text-red-400">Error: {nr.last_error}</span>
                  )}
                  {onShowLogs && (
                    <button
                      onClick={(e) => {
                        e.stopPropagation();
                        onShowLogs(nr.node, app.app_id);
                      }}
                      className="px-2 py-1 text-xs bg-gray-700 hover:bg-gray-600 rounded text-gray-300"
                    >
                      Logs
                    </button>
                  )}
                  {!BACKUP_PHASES.includes(nr.phase) &&
                    nr.observed_state === "running" && (
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          backupMutation.mutate({
                            appId: app.app_id,
                            node: nr.node,
                          });
                        }}
                        disabled={backupMutation.isPending}
                        className="px-2 py-1 text-xs bg-amber-700 hover:bg-amber-600 disabled:opacity-50 disabled:cursor-not-allowed rounded text-white"
                      >
                        {backupMutation.isPending ? "Triggering..." : "Backup"}
                      </button>
                    )}
                </div>
              ))}
            </div>
          </td>
        </tr>
      )}
    </>
  );
}
