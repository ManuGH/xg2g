import type { ReactNode } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, beforeEach, vi } from 'vitest';
import { ClientRequestError } from '../services/clientWrapper';
import Dashboard from './Dashboard';
import { buildEpgRoute, buildSettingsRoute } from '../routes';

const mockNavigate = vi.fn();
const mockRefetch = vi.fn();
const mockUseSystemHealth = vi.fn();
const mockUseHouseholdProfiles = vi.fn();
const mockUseDvrStatus = vi.fn();
const mockUseTimers = vi.fn();
const mockUseStreams = vi.fn();

vi.mock('react-router', () => ({
  Link: ({ to, children, ...props }: { to: string; children: ReactNode }) => <a href={to} {...props}>{children}</a>,
  useNavigate: () => mockNavigate,
}));

vi.mock('../hooks/useServerQueries', () => ({
  useSystemHealth: () => mockUseSystemHealth(),
  useReceiverCurrent: () => ({
    data: {
      status: 'available',
      channel: { name: 'Channel Two' }
    }
  }),
  useStreams: () => mockUseStreams(),
  useDvrStatus: () => mockUseDvrStatus(),
  useTimers: () => mockUseTimers(),
}));

vi.mock('../features/resume/ContinueWatchingRail', () => ({
  __esModule: true,
  default: () => null,
}));

vi.mock('./StreamsList', () => ({
  __esModule: true,
  default: () => <div data-testid="streams-list">StreamsList</div>,
}));

vi.mock('../context/HouseholdProfilesContext', () => ({
  useHouseholdProfiles: () => mockUseHouseholdProfiles(),
}));

describe('Dashboard', () => {
  beforeEach(() => {
    mockNavigate.mockReset();
    mockRefetch.mockReset();
    mockUseDvrStatus.mockReturnValue({ data: null });
    mockUseTimers.mockReturnValue({ data: [] });
    mockUseStreams.mockReturnValue({ data: [] });
    mockUseHouseholdProfiles.mockReturnValue({
      canAccessDvrPlayback: true,
      canManageDvr: true,
      canAccessSettings: true,
    });
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

    screen.getByRole('button', { name: 'Open Live TV' });
    screen.getByRole('button', { name: 'Household profiles' });
    screen.getByRole('button', { name: 'Timers' });
    expect(screen.getByRole('status')).toBeInTheDocument();
    expect(screen.getByText('System ready')).toBeInTheDocument();
    expect(screen.getByText('Receiver connected')).toBeInTheDocument();
    expect(screen.getByText('Guide up to date')).toBeInTheDocument();
    expect(screen.queryByText('Recent logs')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Refresh' })).toBeNull();
    // Idle sessions row is completely hidden when no active streams exist
    expect(screen.queryByText('Operator sessions')).toBeNull();
  });

  it('renders active operator sessions when streams exist', () => {
    mockUseStreams.mockReturnValue({
      data: [
        {
          id: 'stream-1',
          clientIp: '192.168.1.100',
          channelName: 'Channel Two',
          deviceType: 'ios',
        },
      ],
    });

    render(<Dashboard />);
    expect(screen.getByText('Operator sessions')).toBeInTheDocument();
  });

  it('navigates to guided and direct routes from the dashboard', () => {
    render(<Dashboard />);

    fireEvent.click(screen.getByRole('button', { name: 'Timers' }));
    fireEvent.click(screen.getByRole('button', { name: 'Household profiles' }));

    expect(mockNavigate).toHaveBeenNthCalledWith(1, buildEpgRoute('timers'));
    expect(mockNavigate).toHaveBeenNthCalledWith(2, buildSettingsRoute({ section: 'household' }));
  });

  it('shows restricted start cards and hides direct paths when the profile is limited', () => {
    mockUseHouseholdProfiles.mockReturnValue({
      canAccessDvrPlayback: false,
      canManageDvr: false,
      canAccessSettings: false,
    });

    render(<Dashboard />);

    expect(screen.queryByRole('button', { name: 'Household profiles' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Timers' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Series Rules' })).toBeNull();
  });

  it('renders the section skeleton while health data is loading', () => {
    mockUseSystemHealth.mockReturnValue({
      data: undefined,
      error: null,
      isLoading: true,
      refetch: mockRefetch
    });

    render(<Dashboard />);

    expect(screen.getByRole('status', { name: 'Loading...' })).toHaveAttribute('data-loading-variant', 'section');
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

    screen.getByRole('heading', { name: 'Service unavailable' });
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(mockRefetch).toHaveBeenCalledTimes(1);
  });

  it('shows the recording in status strip only while isRecording is active', () => {
    // 1. Idle state: no recording shown
    mockUseDvrStatus.mockReturnValue({
      data: {
        isRecording: false,
        serviceName: 'Channel One',
      },
    });

    const { rerender } = render(<Dashboard />);
    expect(screen.queryByText('Channel One')).toBeNull();

    // 2. Active recording: displays the service name in the status strip
    mockUseDvrStatus.mockReturnValue({
      data: {
        isRecording: true,
        serviceName: 'Channel One',
      },
    });

    rerender(<Dashboard />);
    expect(screen.getByText('Channel One')).toBeInTheDocument();
  });

  it('displays upcoming timer pill when scheduled timers exist and navigates on click', () => {
    mockUseTimers.mockReturnValue({
      data: [
        {
          timerId: 'timer-1',
          name: 'Tatort',
          begin: 1710000000,
          end: 1710003600,
          state: 'scheduled',
        },
      ],
    });

    render(<Dashboard />);
    const timerButton = screen.getByRole('button', { name: /Tatort/i });
    expect(timerButton).toBeInTheDocument();
    fireEvent.click(timerButton);
    expect(mockNavigate).toHaveBeenCalledWith(buildEpgRoute('timers'));
  });
});
