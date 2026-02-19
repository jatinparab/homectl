import type { AppStatus, UpcomingBackup } from '../api/types'
import { AppRow } from './AppRow'

interface AppsTableProps {
  apps: AppStatus[]
  isLoading: boolean
  upcomingBackup?: UpcomingBackup | null
  onShowLogs?: (node: string, appId: string) => void
}

export function AppsTable({ apps, isLoading, upcomingBackup, onShowLogs }: AppsTableProps) {
  if (isLoading) {
    return (
      <div className="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center text-gray-400">
        Loading...
      </div>
    )
  }

  if (apps.length === 0) {
    return (
      <div className="bg-gray-800 rounded-lg border border-gray-700 p-8 text-center text-gray-400">
        No apps configured
      </div>
    )
  }

  return (
    <div className="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
      <table className="w-full table-fixed">
        <colgroup>
          <col className="w-[14%]" />  {/* App */}
          <col className="w-[8%]" />   {/* Desired */}
          <col className="w-[14%]" />  {/* Node */}
          <col className="w-[9%]" />   {/* Status */}
          <col className="w-[9%]" />   {/* Health */}
          <col className="w-[16%]" />  {/* Phase */}
          <col className="w-[12%]" />  {/* Backup */}
          <col className="w-[5%]" />   {/* Conn */}
          <col className="w-[13%]" />  {/* Actions */}
        </colgroup>
        <thead className="bg-gray-700">
          <tr>
            <th className="px-4 py-2 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">App</th>
            <th className="px-3 py-2 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">Desired</th>
            <th className="px-3 py-2 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">Node</th>
            <th className="px-3 py-2 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">Status</th>
            <th className="px-3 py-2 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">Health</th>
            <th className="px-3 py-2 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">Phase</th>
            <th className="px-3 py-2 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">Backup</th>
            <th className="px-3 py-2 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">Conn</th>
            <th className="px-3 py-2 text-left text-xs font-medium text-gray-300 uppercase tracking-wider"></th>
          </tr>
        </thead>
        <tbody>
          {apps.map((app) => (
            <AppRow
              key={app.app_id}
              app={app}
              isUpcomingBackup={upcomingBackup?.app_id === app.app_id}
              upcomingBackup={upcomingBackup?.app_id === app.app_id ? upcomingBackup : undefined}
              onShowLogs={onShowLogs}
            />
          ))}
        </tbody>
      </table>
    </div>
  )
}
