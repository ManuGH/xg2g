import type { ReactNode } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, beforeEach, vi } from 'vitest';
import { ClientRequestError } from '../services/clientWrapper';
import Dashboard from './Dashboard';
import { buildRecordingsRoute, buildSettingsRoute } from '../routes';

const mockNavigate = vi.fn();
const mockRefetch = vi.fn();
const mockUseSystemHealth = vi.fn();
const mockUseHouseholdProfiles = vi.fn();

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

vi.mock('../features/resume/ContinueWatchingRail', () => ({
  __esModule: true,
  default: () => null,
}));

vi.mock('../context/HouseholdProfilesContext', () => ({
  useHouseholdProfiles: () => mockUseHouseholdProfiles(),
}));

describe('Dashboard', () => {
  beforeEach(() => {
    mockNavigate.mockReset();
    mockRefetch.mockReset();
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

    screen.getByText('Control summary');
    screen.getByText('Choose the task, not the tool');
    screen.getByRole('button', { name: 'Open Live TV' });
    screen.getByRole('button', { name: 'Open Recordings' });
    screen.getByRole('button', { name: 'Open Setup' });
    screen.getByRole('button', { name: 'Household profiles' });
    expect(screen.getByRole('status', { name: 'System healthy - success' })).toBeInTheDocument();
    expect(screen.queryByText('Recent logs')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Refresh' })).toBeNull();
    screen.getByText('Receiver and guide health');
  });

  it('navigates to guided and direct routes from the dashboard', () => {
    render(<Dashboard />);

    fireEvent.click(screen.getByRole('button', { name: 'Open Recordings' }));
    fireEvent.click(screen.getByRole('button', { name: 'Household profiles' }));

    expect(mockNavigate).toHaveBeenNthCalledWith(1, buildRecordingsRoute());
    expect(mockNavigate).toHaveBeenNthCalledWith(2, buildSettingsRoute({ section: 'household' }));
  });

  it('shows restricted start cards and hides direct paths when the profile is limited', () => {
    mockUseHouseholdProfiles.mockReturnValue({
      canAccessDvrPlayback: false,
      canManageDvr: false,
      canAccessSettings: false,
    });

    render(<Dashboard />);

    expect(screen.getByRole('button', { name: 'Open Recordings' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Open Setup' })).toBeDisabled();
    expect(screen.queryByText('Open a specific area directly')).toBeNull();
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
});
