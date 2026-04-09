import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { HouseholdProfilesProvider } from '../context/HouseholdProfilesContext';
import { setClientAuthToken } from '../services/clientWrapper';

const {
  getRecordings,
  deleteRecording,
  confirm,
  toast,
  v3Player,
} = vi.hoisted(() => ({
  getRecordings: vi.fn(),
  deleteRecording: vi.fn(),
  confirm: vi.fn(),
  toast: vi.fn(),
  v3Player: vi.fn(({ recordingId, token }: { recordingId?: string; token?: string }) => (
    <div data-testid="v3-player-props">{`${recordingId || ''}|${token || ''}`}</div>
  )),
}));

vi.mock('../client-ts', () => ({
  getRecordings,
  deleteRecording,
}));

vi.mock('../context/AppContext', () => ({
  useAppContext: () => ({
    auth: {
      token: 'test-token',
    },
  }),
}));

vi.mock('../context/UiOverlayContext', () => ({
  useUiOverlay: () => ({
    confirm,
    toast,
  }),
}));


vi.mock('../features/resume/RecordingResumeBar', () => ({
  __esModule: true,
  default: () => null,
  isResumeEligible: () => false,
}));

vi.mock('../features/player/components/V3Player', () => ({
  __esModule: true,
  default: v3Player,
}));

import RecordingsList from './RecordingsList';

function renderWithQueryClient() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
      mutations: {
        retry: false,
      },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <HouseholdProfilesProvider>
        <RecordingsList />
      </HouseholdProfilesProvider>
    </QueryClientProvider>
  );
}

describe('RecordingsList', () => {
  afterEach(() => {
    vi.clearAllMocks();
    setClientAuthToken('');
    window.localStorage.clear();
  });

  it('refetches recordings on explicit refresh', async () => {
    getRecordings.mockResolvedValue({
      data: {
        currentRoot: 'root-a',
        currentPath: '',
        roots: [{ id: 'root-a', name: 'Root A' }],
        breadcrumbs: [],
        directories: [],
        recordings: [],
      },
    });

    renderWithQueryClient();

    await screen.findByText('No recordings found in this location.');

    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));

    await waitFor(() => {
      expect(getRecordings).toHaveBeenCalledTimes(2);
    });
  });

  it('refetches recordings when navigating into a directory', async () => {
    getRecordings
      .mockResolvedValueOnce({
        data: {
          currentRoot: 'root-a',
          currentPath: '',
          roots: [{ id: 'root-a', name: 'Root A' }],
          breadcrumbs: [],
          directories: [{ name: 'Series', path: 'series' }],
          recordings: [],
        },
      })
      .mockResolvedValueOnce({
        data: {
          currentRoot: 'root-a',
          currentPath: '',
          roots: [{ id: 'root-a', name: 'Root A' }],
          breadcrumbs: [],
          directories: [{ name: 'Series', path: 'series' }],
          recordings: [],
        },
      })
      .mockResolvedValueOnce({
        data: {
          currentRoot: 'root-a',
          currentPath: 'series',
          roots: [{ id: 'root-a', name: 'Root A' }],
          breadcrumbs: [{ name: 'Series', path: 'series' }],
          directories: [],
          recordings: [],
        },
      });

    renderWithQueryClient();

    expect(await screen.findByText('Series')).toBeInTheDocument();

    fireEvent.click(screen.getByText('Series'));

    await waitFor(() => {
      expect(getRecordings).toHaveBeenLastCalledWith({ query: { root: 'root-a', path: 'series' } });
    });
  });

  it('bulk deletes selected recordings and refreshes the listing', async () => {
    getRecordings
      .mockResolvedValueOnce({
        data: {
          currentRoot: 'root-a',
          currentPath: '',
          roots: [{ id: 'root-a', name: 'Root A' }],
          breadcrumbs: [],
          directories: [],
          recordings: [
            {
              recordingId: 'rec-1',
              title: 'Movie Night',
              beginUnixSeconds: 1710000000,
              length: '90m',
              description: 'A test recording',
            },
          ],
        },
      })
      .mockResolvedValueOnce({
        data: {
          currentRoot: 'root-a',
          currentPath: '',
          roots: [{ id: 'root-a', name: 'Root A' }],
          breadcrumbs: [],
          directories: [],
          recordings: [],
        },
      });

    deleteRecording.mockResolvedValue({ data: undefined });
    confirm.mockResolvedValue(true);

    renderWithQueryClient();

    expect(await screen.findByText('Movie Night')).toBeInTheDocument();

    fireEvent.click(screen.getByTitle('Selection Mode'));
    fireEvent.click(screen.getByText('Movie Night'));
    fireEvent.click(screen.getByRole('button', { name: 'Delete Selected (1)' }));

    await waitFor(() => {
      expect(deleteRecording).toHaveBeenCalledWith({ path: { recordingId: 'rec-1' } });
    });

    await waitFor(() => {
      expect(screen.queryByText('Movie Night')).not.toBeInTheDocument();
    });

    expect(toast).toHaveBeenCalledWith({ kind: 'success', message: 'Deleted 1 recording(s)' });
  });

  it('sorts and filters the visible recordings locally', async () => {
    getRecordings.mockResolvedValue({
      data: {
        currentRoot: 'root-a',
        currentPath: '',
        roots: [{ id: 'root-a', name: 'Root A' }],
        breadcrumbs: [],
        directories: [],
        recordings: [
          {
            recordingId: 'rec-older',
            title: 'Older Recording',
            beginUnixSeconds: 1700000000,
            length: '45m',
            description: 'Older item',
            status: 'completed',
          },
          {
            recordingId: 'rec-active',
            title: 'Active Recording',
            beginUnixSeconds: 1710000000,
            length: '30m',
            description: 'Still recording',
            status: 'recording',
          },
          {
            recordingId: 'rec-newest',
            title: 'Newest Recording',
            beginUnixSeconds: 1720000000,
            length: '90m',
            description: 'Newest item',
            status: 'completed',
          },
        ],
      },
    });

    renderWithQueryClient();

    expect(await screen.findByText('Newest Recording')).toBeInTheDocument();

    const getTitles = () =>
      screen.getAllByTestId('recording-title').map((title) => title.textContent);

    expect(getTitles()).toEqual([
      'Newest Recording',
      'Active Recording',
      'Older Recording',
    ]);

    fireEvent.change(screen.getByTestId('recordings-sort-select'), {
      target: { value: 'oldest' },
    });

    expect(getTitles()).toEqual([
      'Older Recording',
      'Active Recording',
      'Newest Recording',
    ]);

    fireEvent.change(screen.getByTestId('recordings-filter-select'), {
      target: { value: 'active' },
    });

    expect(getTitles()).toEqual(['Active Recording']);
  });

  it('passes the auth token into recording playback', async () => {
    getRecordings.mockResolvedValue({
      data: {
        currentRoot: 'root-a',
        currentPath: '',
        roots: [{ id: 'root-a', name: 'Root A' }],
        breadcrumbs: [],
        directories: [],
        recordings: [
          {
            recordingId: 'rec-token',
            title: 'Token Recording',
            beginUnixSeconds: 1710000000,
            durationSeconds: 1800,
            length: '30m',
            description: 'Auth-sensitive playback',
            status: 'completed',
          },
        ],
      },
    });

    renderWithQueryClient();

    fireEvent.click(await screen.findByText('Token Recording'));

    await waitFor(() => {
      expect(screen.getByTestId('v3-player-props')).toHaveTextContent('rec-token|test-token');
    });
  });

  it('does not infer recording status from legacy title tokens', async () => {
    getRecordings.mockResolvedValue({
      data: {
        currentRoot: 'root-a',
        currentPath: '',
        roots: [{ id: 'root-a', name: 'Root A' }],
        breadcrumbs: [],
        directories: [],
        recordings: [
          {
            recordingId: 'rec-legacy',
            title: '[WAIT] Legacy Token',
            beginUnixSeconds: 1710000000,
            length: '30m',
            description: 'No explicit recording truth',
          },
        ],
      },
    });

    renderWithQueryClient();

    expect(await screen.findByText('[WAIT] Legacy Token')).toBeInTheDocument();
    expect(screen.getByText('UNKNOWN')).toBeInTheDocument();
    expect(screen.queryByText('SCHEDULED')).not.toBeInTheDocument();
  });
});
