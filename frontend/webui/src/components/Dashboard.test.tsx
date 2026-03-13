import type { ReactNode } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, beforeEach, vi } from 'vitest';
import { ClientRequestError } from '../lib/clientWrapper';
import Dashboard from './Dashboard';

const mockNavigate = vi.fn();
const mockRefetch = vi.fn();
const mockUseSystemHealth = vi.fn();

const translations: Record<string, string> = {
  'common.loading': 'Laden',
  'common.refresh': 'Aktualisieren',
  'common.retry': 'Erneut versuchen',
  'common.receiverStandby': 'Standby',
  'common.receiverUnavailable': 'Receiver nicht verfuegbar',
  'nav.dashboard': 'Dashboard',
  'nav.epg': 'TV/EPG',
  'nav.recordings': 'Aufnahmen',
  'nav.timers': 'Timer',
  'dashboard.heroControlDeck': 'Control deck',
  'dashboard.heroDefaultSummary': 'Default summary',
  'dashboard.connectedDevices': 'Connected devices',
  'dashboard.readyForFirstSession': 'ready',
  'dashboard.lastSync': 'Last sync {{time}}',
  'dashboard.recorder': 'Recorder',
  'dashboard.idle': 'Idle',
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
  'dashboard.timeHoursAgo': '{{count}}h ago',
  'dashboard.loadErrorTitle': 'Unable to load system health',
  'dashboard.loadErrorDetail': 'Try again to refresh the current receiver and guide status.'
};

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, options?: { count?: number; time?: string; title?: string; defaultValue?: string }) => {
      const template = translations[key] ?? options?.defaultValue ?? key;
      return template
        .replace('{{count}}', String(options?.count ?? ''))
        .replace('{{time}}', String(options?.time ?? ''))
        .replace('{{title}}', String(options?.title ?? ''));
    },
  }),
}));

vi.mock('react-router-dom', () => ({
  Link: ({ to, children, ...props }: { to: string; children: ReactNode }) => <a href={to} {...props}>{children}</a>,
  useNavigate: () => mockNavigate,
}));

vi.mock('../hooks/useServerQueries', () => ({
  useSystemHealth: () => mockUseSystemHealth(),
  useReceiverCurrent: () => ({
    data: {
      status: 'available',
      channel: { name: 'Das Erste' }
    }
  }),
  useStreams: () => ({ data: [] }),
  useDvrStatus: () => ({ data: null })
}));

describe('Dashboard', () => {
  beforeEach(() => {
    mockNavigate.mockReset();
    mockRefetch.mockReset();
    mockUseSystemHealth.mockReturnValue({
      data: {
        status: 'ok',
        epg: { status: 'ok', missingChannels: 0 },
        receiver: { lastCheck: '2026-03-11T10:00:00Z' },
        version: 'v3.0.0',
        uptimeSeconds: 120
      },
      error: null,
      isLoading: false,
      refetch: mockRefetch
    });
  });

  it('renders a compact dashboard without duplicate health or log panels', () => {
    render(<Dashboard />);

    expect(screen.getAllByText('System healthy')).toHaveLength(1);
    expect(screen.queryByText('Recent logs')).toBeNull();
    screen.getByText('Receiver and guide health');
  });

  it('renders the section skeleton while health data is loading', () => {
    mockUseSystemHealth.mockReturnValue({
      data: undefined,
      error: null,
      isLoading: true,
      refetch: mockRefetch
    });

    render(<Dashboard />);

    expect(screen.getByRole('status', { name: 'Laden' })).toHaveAttribute('data-loading-variant', 'section');
  });

  it('renders an error panel and retries the dashboard query', () => {
    mockUseSystemHealth.mockReturnValue({
      data: undefined,
      error: new ClientRequestError({
        status: 503,
        title: 'Service unavailable',
        detail: 'system health backend offline',
      }),
      isLoading: false,
      refetch: mockRefetch
    });

    render(<Dashboard />);

    screen.getByRole('heading', { name: 'Unable to load system health' });
    fireEvent.click(screen.getByRole('button', { name: 'Erneut versuchen' }));
    expect(mockRefetch).toHaveBeenCalledTimes(1);
  });
});
