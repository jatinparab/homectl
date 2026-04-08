import type { AppStatus, UpcomingBackup, UpcomingPrune, Agent } from '../api/types'
import { AppRow } from './AppRow'

interface AppsTableProps {
  apps: AppStatus[]
  isLoading: boolean
  upcomingBackup?: UpcomingBackup | null
  upcomingPrune?: UpcomingPrune | null
  onShowLogs?: (node: string, appId: string) => void
  onNavigate?: (path: string) => void
  agents?: Agent[]
}

export function AppsTable({ apps, isLoading, upcomingBackup, upcomingPrune, onShowLogs, onNavigate, agents }: AppsTableProps) {
  if (isLoading) {
    return (
      <div className="bg-white border border-[#e2ddd7] rounded-lg p-8 text-center text-stone-400 text-sm">
        Loading...
      </div>
    )
  }

  if (apps.length === 0) {
    return (
      <div className="bg-white border border-[#e2ddd7] rounded-lg p-8 text-center text-stone-400 text-sm">
        No apps configured
      </div>
    )
  }

  return (
    <div className="bg-white border border-[#e2ddd7] rounded-lg overflow-hidden">
      <table className="w-full table-fixed">
        <colgroup>
          <col className="w-[15%]" />  {/* App */}
          <col className="w-[7%]" />   {/* Run */}
          <col className="w-[13%]" />  {/* Node */}
          <col className="w-[8%]" />   {/* Status */}
          <col className="w-[6%]" />   {/* Health */}
          <col className="w-[16%]" />  {/* Phase */}
          <col className="w-[13%]" />  {/* Backup */}
          <col className="w-[9%]" />   {/* Conn */}
          <col className="w-[13%]" />  {/* Actions */}
        </colgroup>
        <thead>
          <tr className="border-b border-[#e2ddd7]">
            <th className="px-4 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">App</th>
            <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Run</th>
            <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Node</th>
            <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Status</th>
            <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Health</th>
            <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Phase</th>
            <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Backup</th>
            <th className="px-3 py-2.5 text-left text-[10px] font-semibold text-stone-400 uppercase tracking-widest">Conn</th>
            <th className="px-3 py-2.5"></th>
          </tr>
        </thead>
        <tbody>
          {apps.map((app) => (
            <AppRow
              key={app.app_id}
              app={app}
              isUpcomingBackup={upcomingBackup?.app_id === app.app_id}
              upcomingBackup={upcomingBackup?.app_id === app.app_id ? upcomingBackup : undefined}
              isUpcomingPrune={upcomingPrune?.app_id === app.app_id}
              upcomingPrune={upcomingPrune?.app_id === app.app_id ? upcomingPrune : undefined}
              onShowLogs={onShowLogs}
              onNavigate={onNavigate}
              agents={agents}
            />
          ))}
        </tbody>
      </table>
    </div>
  )
}
