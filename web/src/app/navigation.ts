export type Tab = 'dashboard' | 'backups' | 'storage' | 'containers' | 'routing' | 'crowdsec' | 'logs';

export const TAB_PATHS: Record<Tab, string> = {
  dashboard: '/',
  backups: '/backups',
  storage: '/storage',
  containers: '/containers',
  routing: '/routing',
  crowdsec: '/crowdsec',
  logs: '/logs',
};

const PATH_TABS: Record<string, Tab> = {
  '/': 'dashboard',
  '/backups': 'backups',
  '/storage': 'storage',
  '/containers': 'containers',
  '/routing': 'routing',
  '/crowdsec': 'crowdsec',
  '/logs': 'logs',
};

export function tabFromPath(path: string): Tab {
  if (path.startsWith('/apps/')) return 'dashboard';
  return PATH_TABS[path] ?? 'dashboard';
}

export function pathFromTab(tab: Tab): string {
  return TAB_PATHS[tab];
}
