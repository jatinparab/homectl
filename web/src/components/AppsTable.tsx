import type { AppStatus } from '../api/types'
import { AppRow } from './AppRow'

interface AppsTableProps {
  apps: AppStatus[]
  isLoading: boolean
  onShowLogs?: (node: string, appId: string) => void
}

export function AppsTable({ apps, isLoading, onShowLogs }: AppsTableProps) {
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
      <table className="w-full">
        <thead className="bg-gray-700">
          <tr>
            <th className="px-4 py-3 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">
              App Name
            </th>
            <th className="px-4 py-3 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">
              Desired
            </th>
            <th className="px-4 py-3 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">
              Status
            </th>
            <th className="px-4 py-3 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">
              Health
            </th>
            <th className="px-4 py-3 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">
              Phase
            </th>
            <th className="px-4 py-3 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">
              Nodes
            </th>
            <th className="px-4 py-3 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">
              Restarts
            </th>
          </tr>
        </thead>
        <tbody>
          {apps.map((app) => (
            <AppRow key={app.app_id} app={app} onShowLogs={onShowLogs} />
          ))}
        </tbody>
      </table>
    </div>
  )
}
