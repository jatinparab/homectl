export type Tab = "dashboard" | "backups" | "storage" | "containers" | "routing"

const TAB_PATHS: Record<Tab, string> = {
  dashboard: "/",
  backups: "/backups",
  storage: "/storage",
  containers: "/containers",
  routing: "/routing",
}

const PATH_TABS: Record<string, Tab> = {
  "/": "dashboard",
  "/backups": "backups",
  "/storage": "storage",
  "/containers": "containers",
  "/routing": "routing",
}

export function tabFromPath(path: string): Tab {
  if (path.startsWith("/apps/")) return "dashboard"
  return PATH_TABS[path] ?? "dashboard"
}

export function pathFromTab(tab: Tab): string {
  return TAB_PATHS[tab]
}

interface HeaderProps {
  activeTab: Tab
  onTabChange: (tab: Tab) => void
}

const tabs: { id: Tab; label: string }[] = [
  { id: "dashboard", label: "Dashboard" },
  { id: "backups", label: "Backups" },
  { id: "storage", label: "Storage" },
  { id: "containers", label: "Containers" },
  { id: "routing", label: "Routing" },
]

export function Header({ activeTab, onTabChange }: HeaderProps) {
  return (
    <header className="bg-white border-b border-[#e2ddd7] sticky top-0 z-40">
      <div className="max-w-6xl mx-auto px-4 h-12 flex items-center justify-between">
        <div className="flex items-center gap-3">
          <span className="text-[13px] font-semibold tracking-widest text-[#1c1917] uppercase">
            homectl
          </span>
          <span className="w-px h-4 bg-[#e2ddd7]" />
          <nav className="flex items-center gap-0.5">
            {tabs.map((tab) => (
              <a
                key={tab.id}
                href={pathFromTab(tab.id)}
                onClick={(e) => { e.preventDefault(); onTabChange(tab.id) }}
                className={`px-3 py-1 text-[11px] font-medium tracking-wider uppercase rounded transition-colors ${
                  activeTab === tab.id
                    ? "text-[#0d9488] bg-[#ccfbf1]"
                    : "text-[#78716c] hover:text-[#1c1917] hover:bg-[#f0ede8]"
                }`}
              >
                {tab.label}
              </a>
            ))}
          </nav>
        </div>
      </div>
    </header>
  )
}
