export type AppView =
  | 'dashboard'
  | 'epg'
  | 'timers'
  | 'recordings'
  | 'series'
  | 'files'
  | 'logs'
  | 'settings'
  | 'system';

export const ROUTE_MAP: Record<AppView, string> = {
  dashboard: '/dashboard',
  epg: '/epg',
  timers: '/timers',
  recordings: '/recordings',
  series: '/series',
  files: '/files',
  logs: '/logs',
  settings: '/settings',
  system: '/system'
};

export const UNLOCK_ROUTE = '/unlock';

export function normalizePathname(pathname: string): string {
  if (!pathname || pathname === '/') return '/';
  const trimmed = pathname.replace(/\/+$/, '');
  return trimmed || '/';
}
