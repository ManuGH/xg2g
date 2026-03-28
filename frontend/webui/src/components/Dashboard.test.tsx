import type { ReactNode } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, beforeEach, vi } from 'vitest';
import { ClientRequestError } from '../services/clientWrapper';
import Dashboard from './Dashboard';

const mockNavigate = vi.fn();
const mockRefetch = vi.fn();
const mockUseSystemHealth = vi.fn();

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

    screen.getByText('Control summary');
    expect(screen.getByRole('status', { name: 'System healthy - success' })).toBeInTheDocument();
    expect(screen.queryByText('Recent logs')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Refresh' })).toBeNull();
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
