import { render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import Dashboard from './Dashboard';

const translations: Record<string, string> = {
  'common.loading': 'Laden',
  'common.refresh': 'Aktualisieren',
  'common.receiverStandby': 'Standby',
  'common.receiverUnavailable': 'Receiver nicht verfuegbar',
  'nav.epg': 'TV/EPG',
  'nav.recordings': 'Aufnahmen',
  'nav.timers': 'Timer',
  'dashboard.heroControlDeck': 'Control deck',
  'dashboard.heroDefaultSummary': 'Default summary',
  'dashboard.connectedDevices': 'Connected devices',
  'dashboard.readyForFirstSession': 'ready',
  'dashboard.healthy': 'Healthy',
  'dashboard.lastSync': 'Last sync {{time}}',
  'dashboard.recorder': 'Recorder',
  'dashboard.idle': 'Idle',
  'dashboard.noActiveRecordingTask': 'No recording',
  'dashboard.systemHealthy': 'System healthy',
  'dashboard.healthUnknown': 'Health unknown',
  'dashboard.systemDegraded': 'System degraded',
  'dashboard.signal': 'Signal',
  'dashboard.receiverAndGuideHealth': 'Receiver and guide health',
  'dashboard.lastSyncLabel': 'Last sync',
  'dashboard.guideGaps': 'Guide gaps',
  'dashboard.none': 'None',
  'dashboard.missing': '{{count}} missing',
  'dashboard.versionLabel': 'Version',
  'dashboard.diagnostics': 'Diagnostics',
  'dashboard.activeStreams': 'Active streams',
  'dashboard.sessions': '{{count}} sessions',
  'dashboard.noSessions': 'No sessions',
  'dashboard.noActiveStreams': 'No active streams',
  'dashboard.startPlaybackHint': 'Start playback',
  'dashboard.onReceiverNow': 'On receiver now',
  'dashboard.receiverContext': 'Receiver context',
  'dashboard.readyForPlayback': 'Ready for playback',
  'dashboard.timeNever': 'Never',
  'dashboard.timeJustNow': 'Just now',
  'dashboard.timeMinutesAgo': '{{count}}m ago',
  'dashboard.timeHoursAgo': '{{count}}h ago'
};

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, options?: { count?: number; time?: string; title?: string }) => {
      const template = translations[key] ?? key;
      return template
        .replace('{{count}}', String(options?.count ?? ''))
        .replace('{{time}}', String(options?.time ?? ''))
        .replace('{{title}}', String(options?.title ?? ''));
    },
  }),
}));

vi.mock('../hooks/useServerQueries', () => ({
  useSystemHealth: () => ({
    data: {
      status: 'ok',
      epg: { status: 'ok', missingChannels: 0 },
      receiver: { lastCheck: '2026-03-11T10:00:00Z' },
      version: 'v3.0.0',
      uptimeSeconds: 120
    },
    error: null,
    isLoading: false,
    refetch: vi.fn()
  }),
  useReceiverCurrent: () => ({
    data: {
      status: 'available',
      channel: { name: 'Das Erste' }
    }
  }),
  useStreams: () => ({ data: [] }),
  useDvrStatus: () => ({ data: null })
}));

vi.mock('../context/AppContext', () => ({
  useAppContext: () => ({
    setView: vi.fn()
  })
}));

describe('Dashboard', () => {
  it('renders a compact dashboard without duplicate health or log panels', () => {
    render(<Dashboard />);

    expect(screen.getAllByText('System healthy')).toHaveLength(1);
    expect(screen.queryByText('Recent logs')).toBeNull();
    screen.getByText('Receiver and guide health');
  });
});
