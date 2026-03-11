import { render, screen } from '@testing-library/react';
import { describe, it, vi } from 'vitest';
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
  'dashboard.receiverLink': 'Receiver link',
  'dashboard.healthy': 'Healthy',
  'dashboard.lastSync': 'Last sync {{time}}',
  'dashboard.recorder': 'Recorder',
  'dashboard.idle': 'Idle',
  'dashboard.noActiveRecordingTask': 'No recording',
  'dashboard.metricStreaming': 'Streaming',
  'dashboard.noActiveSessions': 'No active sessions',
  'dashboard.liveTraffic': 'Live traffic',
  'dashboard.metricReceiver': 'Receiver',
  'dashboard.online': 'Online',
  'dashboard.metricEpg': 'EPG',
  'dashboard.synced': 'Synced',
  'dashboard.allChannelsHaveData': 'all channels have data',
  'dashboard.metricRecorder': 'Recorder',
  'dashboard.uptimeLabel': 'Uptime {{time}}',
  'dashboard.receiverOnline': 'Receiver online',
  'dashboard.recorderUnknown': 'Recorder unknown',
  'dashboard.systemHealthy': 'System healthy',
  'dashboard.healthUnknown': 'Health unknown',
  'dashboard.systemDegraded': 'System degraded',
  'dashboard.guideSynced': 'Guide synced',
  'dashboard.guidePartial': 'Guide partial',
  'dashboard.guideOffline': 'Guide offline',
  'dashboard.activeSessions': '{{count}} active sessions',
  'dashboard.noActiveSessionsChip': 'No active sessions',
  'dashboard.signal': 'Signal',
  'dashboard.receiverAndGuideHealth': 'Receiver and guide health',
  'dashboard.receiverLabel': 'Receiver',
  'dashboard.connected': 'Connected',
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
  'dashboard.feed': 'Feed',
  'dashboard.recentLogs': 'Recent logs',
  'dashboard.loadingLogs': 'Loading logs',
  'dashboard.noRecentLogs': 'No recent logs',
  'dashboard.onReceiverNow': 'On receiver now',
  'dashboard.receiverContext': 'Receiver context',
  'dashboard.readyForPlayback': 'Ready for playback',
  'dashboard.timeNever': 'Never',
  'dashboard.timeJustNow': 'Just now',
  'dashboard.logLevelError': 'Fehlerpegel'
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
  useDvrStatus: () => ({ data: null }),
  useLogs: () => ({
    data: [{ level: 'error', message: 'Something happened', time: '2026-03-11T10:00:00Z' }],
    isLoading: false,
    error: null
  })
}));

vi.mock('../context/AppContext', () => ({
  useAppContext: () => ({
    setView: vi.fn()
  })
}));

describe('Dashboard', () => {
  it('renders translated log level labels in the feed', () => {
    render(<Dashboard />);

    screen.getByText('Fehlerpegel');
  });
});
