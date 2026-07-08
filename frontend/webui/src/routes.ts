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

export type EpgSection = 'guide' | 'timers';
export type RecordingsSection = 'library' | 'series';
export type SettingsSection =
  | 'setup'
  | 'household'
  | 'android-tv'
  | 'scan'
  | 'streaming'
  | 'advanced';
export type SettingsTool = 'files' | 'logs';

function buildRouteWithQuery(path: string, params: Record<string, string | undefined>): string {
  const query = new URLSearchParams();

  Object.entries(params).forEach(([key, value]) => {
    if (!value) {
      return;
    }
    query.set(key, value);
  });

  const queryString = query.toString();
  return queryString ? `${path}?${queryString}` : path;
}

export function buildEpgRoute(section?: EpgSection): string {
  if (!section || section === 'guide') {
    return ROUTE_MAP.epg;
  }

  return buildRouteWithQuery(ROUTE_MAP.epg, { section });
}

export function buildRecordingsRoute(options?: { section?: RecordingsSection }): string {
  if (!options?.section || options.section === 'library') {
    return ROUTE_MAP.recordings;
  }

  return buildRouteWithQuery(ROUTE_MAP.recordings, { section: options.section });
}

export function buildSettingsRoute(options?: {
  section?: SettingsSection;
  tool?: SettingsTool;
}): string {
  if (!options?.section || options.section === 'setup') {
    return ROUTE_MAP.settings;
  }

  return buildRouteWithQuery(ROUTE_MAP.settings, {
    section: options.section,
    tool: options.section === 'advanced' ? options.tool : undefined,
  });
}

export function normalizePathname(pathname: string): string {
  if (!pathname || pathname === '/') return '/';
  const trimmed = pathname.replace(/\/+$/, '');
  return trimmed || '/';
}
