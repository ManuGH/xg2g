import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { describe, expect, it, vi, afterEach } from 'vitest';

const { fetchContinueWatching, saveResume, mockNavigate, mockToast } = vi.hoisted(() => ({
  fetchContinueWatching: vi.fn(),
  saveResume: vi.fn(),
  mockNavigate: vi.fn(),
  mockToast: vi.fn(),
}));

vi.mock('./api', () => ({
  fetchContinueWatching,
  saveResume,
}));

vi.mock('react-router', async (importOriginal) => ({
  ...(await importOriginal<typeof import('react-router')>()),
  useNavigate: () => mockNavigate,
}));

vi.mock('../../context/AppContext', () => ({
  useAppContext: () => ({
    auth: { isReady: true, isAuthenticated: true, token: 'test-token' },
  }),
}));

vi.mock('../../context/UiOverlayContext', () => ({
  useUiOverlay: () => ({
    toast: mockToast,
    confirm: vi.fn(),
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
      expect(screen.getByText(/70 minutes left/)).toBeInTheDocument();
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

  it('deduplicates multiple episodes of the same show and filters finished items', async () => {
    fetchContinueWatching.mockResolvedValue([
      {
        recordingId: 'rec-cp-1',
        title: 'Café PULS mit PULS 4 Austria News',
        posSeconds: 1200,
        durationSeconds: 3600,
        updatedAt: '2026-10-07T10:00:00Z',
      },
      {
        recordingId: 'rec-cp-2',
        title: 'Café PULS mit PULS 4 Aktuell',
        posSeconds: 500,
        durationSeconds: 3600,
        updatedAt: '2026-10-06T10:00:00Z',
      },
      {
        recordingId: 'rec-finished',
        title: 'Angel Has Fallen',
        posSeconds: 7150,
        durationSeconds: 7200, // 50s left -> virtually finished!
        updatedAt: '2026-10-07T09:00:00Z',
      },
    ]);

    renderRail();

    await waitFor(() => {
      expect(screen.getByText('Café PULS mit PULS 4 Austria News')).toBeInTheDocument();
    });

    // Older duplicate Café PULS and finished Angel Has Fallen must NOT be present
    expect(screen.queryByText('Café PULS mit PULS 4 Aktuell')).toBeNull();
    expect(screen.queryByText('Angel Has Fallen')).toBeNull();
  });

  it('allows dismissing an item and marks it finished', async () => {
    saveResume.mockResolvedValue(undefined);
    fetchContinueWatching.mockResolvedValue([
      {
        recordingId: 'rec-dismiss',
        title: 'Unser Club',
        posSeconds: 600,
        durationSeconds: 1800,
        updatedAt: '2026-10-07T08:00:00Z',
      },
    ]);

    renderRail();

    await waitFor(() => {
      expect(screen.getByText('Unser Club')).toBeInTheDocument();
    });

    const dismissBtn = screen.getByRole('button', { name: /remove|entfernen/i });
    fireEvent.click(dismissBtn);

    expect(saveResume).toHaveBeenCalledWith('rec-dismiss', {
      position: 0,
      finished: true,
    });
  });
});
