import type { AppStatus } from '../api/types'
import { StatusBadge, HealthBadge } from './StatusBadge'

interface NodeSectionProps {
  nodeName: string
  apps: AppStatus[]
  onShowLogs: (appId: string) => void
}

export function NodeSection({ nodeName, apps, onShowLogs }: NodeSectionProps) {
  // Get the node report for each app that matches this node
  const getNodeReport = (app: AppStatus) => {
    return app.node_reports.find((nr) => nr.node === nodeName)
  }

  return (
    <section className="mt-8">
      <h2 className="text-lg font-semibold text-white mb-4">Node: {nodeName}</h2>
      <div className="bg-gray-800 rounded-lg border border-gray-700 overflow-hidden">
        <table className="w-full">
          <thead className="bg-gray-700">
            <tr>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">
                App
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">
                Status
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">
                Health
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">
                Port
              </th>
              <th className="px-4 py-3 text-left text-xs font-medium text-gray-300 uppercase tracking-wider">
                Actions
              </th>
            </tr>
          </thead>
          <tbody>
            {apps.map((app) => {
              const nr = getNodeReport(app)
              if (!nr) return null
              return (
                <tr key={app.app_id} className="border-b border-gray-700">
                  <td className="px-4 py-3 whitespace-nowrap">
                    <span className="font-medium text-white">{app.app_id}</span>
                  </td>
                  <td className="px-4 py-3 whitespace-nowrap">
                    <StatusBadge status={nr.observed_state} />
                  </td>
                  <td className="px-4 py-3 whitespace-nowrap">
                    <HealthBadge health={nr.health} />
                  </td>
                  <td className="px-4 py-3 whitespace-nowrap text-gray-300">
                    {nr.port > 0 ? nr.port : '-'}
                  </td>
                  <td className="px-4 py-3 whitespace-nowrap">
                    <button
                      onClick={() => onShowLogs(app.app_id)}
                      className="px-3 py-1 text-sm bg-gray-700 hover:bg-gray-600 rounded text-gray-300"
                    >
                      Logs
                    </button>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </section>
  )
}
