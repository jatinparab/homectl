export type Tab = "dashboard" | "backups" | "storage"

interface HeaderProps {
  activeTab: Tab
  onTabChange: (tab: Tab) => void
}

export function Header({ activeTab, onTabChange }: HeaderProps) {
  return (
    <header className="bg-gray-800 border-b border-gray-700">
      <div className="max-w-6xl mx-auto px-4 py-4 flex items-center justify-between">
        <h1 className="text-xl font-bold text-white">Homectl</h1>
        <nav className="flex gap-1">
          <button
            onClick={() => onTabChange("dashboard")}
            className={`px-3 py-1.5 text-sm rounded ${
              activeTab === "dashboard"
                ? "bg-gray-600 text-white"
                : "text-gray-400 hover:text-gray-200 hover:bg-gray-700"
            }`}
          >
            Dashboard
          </button>
          <button
            onClick={() => onTabChange("backups")}
            className={`px-3 py-1.5 text-sm rounded ${
              activeTab === "backups"
                ? "bg-gray-600 text-white"
                : "text-gray-400 hover:text-gray-200 hover:bg-gray-700"
            }`}
          >
            Backups
          </button>
          <button
            onClick={() => onTabChange("storage")}
            className={`px-3 py-1.5 text-sm rounded ${
              activeTab === "storage"
                ? "bg-gray-600 text-white"
                : "text-gray-400 hover:text-gray-200 hover:bg-gray-700"
            }`}
          >
            Storage
          </button>
        </nav>
      </div>
    </header>
  )
}
