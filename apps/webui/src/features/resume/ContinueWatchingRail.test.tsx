import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { describe, expect, it, vi, afterEach } from 'vitest';

const { fetchContinueWatching, mockNavigate } = vi.hoisted(() => ({
  fetchContinueWatching: vi.fn(),
  mockNavigate: vi.fn(),
}));

vi.mock('./api', () => ({
  fetchContinueWatching,
}));

vi.mock('react-router-dom', async (importOriginal) => ({
  ...(await importOriginal<typeof import('react-router-dom')>()),
  useNavigate: () => mockNavigate,
}));

vi.mock('../../context/AppContext', () => ({
  useAppContext: () => ({
    auth: { isReady: true, isAuthenticated: true, token: 'test-token' },
  }),
}));

import ContinueWatchingRail from './ContinueWatchingRail';

function renderRail() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <ContinueWatchingRail />
      </QueryClientProvider>
    </MemoryRouter>
  );
}

describe('ContinueWatchingRail', () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders resumable recordings and deep-links into playback', async () => {
    fetchContinueWatching.mockResolvedValue([
      {
        recordingId: 'rec-abc',
        title: 'Tatort: Höllenfahrt',
        channel: 'Das Erste HD',
        posSeconds: 1200,
        durationSeconds: 5400,
        updatedAt: '2026-07-01T20:00:00Z',
      },
    ]);

    renderRail();

    await waitFor(() => {
      expect(screen.getByText('Tatort: Höllenfahrt')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Tatort: Höllenfahrt'));
    expect(mockNavigate).toHaveBeenCalledWith(
      expect.stringMatching(/play=rec-abc/)
    );
    expect(mockNavigate).toHaveBeenCalledWith(
      expect.stringMatching(/pos=1200/)
    );
  });

  it('renders nothing while empty', async () => {
    fetchContinueWatching.mockResolvedValue([]);

    const { container } = renderRail();

    await waitFor(() => {
      expect(fetchContinueWatching).toHaveBeenCalled();
    });
    expect(container.firstChild).toBeNull();
  });

  it('filters out entries below the resume threshold', async () => {
    fetchContinueWatching.mockResolvedValue([
      { recordingId: 'rec-early', title: 'Barely started', posSeconds: 5 },
    ]);

    const { container } = renderRail();

    await waitFor(() => {
      expect(fetchContinueWatching).toHaveBeenCalled();
    });
    expect(container.firstChild).toBeNull();
  });
});
