import {
  ROUTE_MAP,
  UNLOCK_ROUTE,
  normalizePathname,
  type SettingsSection,
  type SettingsTool,
} from '../routes';

type TranslateOptions = {
  defaultValue?: string;
  [key: string]: unknown;
};

type TranslateFn = (key: string, options?: TranslateOptions) => string;

export interface AppRouteContext {
  documentTitle: string;
  mobileContextLabel: string | null;
  showMobileContext: boolean;
  surfaceLabel: string;
  title: string;
  trail: string[];
}

const APP_NAME = 'xg2g';

const SETTINGS_SECTIONS: SettingsSection[] = [
  'setup',
  'household',
  'android-tv',
  'scan',
  'streaming',
  'advanced',
];

const SETTINGS_TOOLS: SettingsTool[] = ['files', 'logs'];

function isSettingsSection(value: string | null): value is SettingsSection {
  return value !== null && SETTINGS_SECTIONS.includes(value as SettingsSection);
}

function isSettingsTool(value: string | null): value is SettingsTool {
  return value !== null && SETTINGS_TOOLS.includes(value as SettingsTool);
}

function buildRouteContext(trail: string[]): AppRouteContext {
  const safeTrail = trail.filter(Boolean);
  const surfaceLabel = safeTrail[0] ?? APP_NAME;
  const title = safeTrail[safeTrail.length - 1] ?? surfaceLabel;
  const titleParts = title === surfaceLabel ? [title] : [title, surfaceLabel];

  return {
    documentTitle: [...titleParts, APP_NAME].join(' · '),
    mobileContextLabel: safeTrail.length > 1 ? safeTrail.join(' / ') : null,
    showMobileContext: safeTrail.length > 1,
    surfaceLabel,
    title,
    trail: safeTrail,
  };
}

export function getSettingsSectionLabel(section: SettingsSection, t: TranslateFn): string {
  switch (section) {
    case 'setup':
      return t('setup.title', { defaultValue: 'Setup' });
    case 'household':
      return t('settings.household.title', { defaultValue: 'Household profiles' });
    case 'android-tv':
      return t('settings.androidTv.title', { defaultValue: 'Android TV' });
    case 'scan':
      return t('settings.streaming.scan.title', { defaultValue: 'Media Truth Scan' });
    case 'streaming':
      return t('settings.streaming.title', { defaultValue: 'Streaming' });
    case 'advanced':
      return t('settings.advanced.title', { defaultValue: 'Advanced tools' });
  }
}

export function getSettingsToolLabel(tool: SettingsTool, t: TranslateFn): string {
  switch (tool) {
    case 'files':
      return t('nav.files', { defaultValue: 'Files' });
    case 'logs':
      return t('nav.logs', { defaultValue: 'Logs' });
  }
}

function buildSettingsTrail(search: string, t: TranslateFn): string[] {
  const searchParams = new URLSearchParams(search);
  const requestedSection = searchParams.get('section');
  const activeSection = isSettingsSection(requestedSection) ? requestedSection : 'setup';
  const requestedTool = searchParams.get('tool');
  const activeTool = activeSection === 'advanced' && isSettingsTool(requestedTool) ? requestedTool : null;
  const trail = [t('settings.title', { defaultValue: 'Settings' })];

  trail.push(getSettingsSectionLabel(activeSection, t));

  if (activeTool) {
    trail.push(getSettingsToolLabel(activeTool, t));
  }

  return trail;
}

export function resolveAppRouteContext(pathname: string, search: string, t: TranslateFn): AppRouteContext {
  const normalizedPathname = normalizePathname(pathname);
  const searchParams = new URLSearchParams(search);

  switch (normalizedPathname) {
    case '/':
    case ROUTE_MAP.dashboard:
      return buildRouteContext([
        t('nav.dashboard', { defaultValue: 'Start' }),
      ]);
    case ROUTE_MAP.epg: {
      const trail = [t('nav.epg', { defaultValue: 'Live TV' })];

      if (searchParams.get('section') === 'timers') {
        trail.push(t('nav.timers', { defaultValue: 'Timers' }));
      }

      return buildRouteContext(trail);
    }
    case ROUTE_MAP.timers:
      return buildRouteContext([
        t('nav.epg', { defaultValue: 'Live TV' }),
        t('nav.timers', { defaultValue: 'Timers' }),
      ]);
    case ROUTE_MAP.recordings: {
      const trail = [t('nav.recordings', { defaultValue: 'Recordings' })];

      if (searchParams.get('section') === 'series') {
        trail.push(t('recordings.seriesRulesAction', { defaultValue: 'Series Rules' }));
      }

      return buildRouteContext(trail);
    }
    case ROUTE_MAP.series:
      return buildRouteContext([
        t('nav.recordings', { defaultValue: 'Recordings' }),
        t('recordings.seriesRulesAction', { defaultValue: 'Series Rules' }),
      ]);
    case ROUTE_MAP.settings:
      return buildRouteContext(buildSettingsTrail(search, t));
    case ROUTE_MAP.files:
      return buildRouteContext([
        t('settings.title', { defaultValue: 'Settings' }),
        getSettingsSectionLabel('advanced', t),
        getSettingsToolLabel('files', t),
      ]);
    case ROUTE_MAP.logs:
      return buildRouteContext([
        t('settings.title', { defaultValue: 'Settings' }),
        getSettingsSectionLabel('advanced', t),
        getSettingsToolLabel('logs', t),
      ]);
    case ROUTE_MAP.system:
      return buildRouteContext([
        t('nav.system', { defaultValue: 'System' }),
      ]);
    case UNLOCK_ROUTE:
      return buildRouteContext([
        t('unlock.pageTitle', { defaultValue: 'Unlock Status' }),
      ]);
    default:
      return buildRouteContext([APP_NAME]);
  }
}
