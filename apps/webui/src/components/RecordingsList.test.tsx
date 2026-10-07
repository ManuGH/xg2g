import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { HouseholdProfilesProvider } from '../context/HouseholdProfilesContext';
import { setClientAuthToken } from '../services/clientWrapper';

const {
  getRecordings,
  getSeriesRules,
  confirm,
  toast,
  v3Player,
} = vi.hoisted(() => ({
  getRecordings: vi.fn(),
  getSeriesRules: vi.fn().mockResolvedValue({ data: [] }),
  confirm: vi.fn(),
  toast: vi.fn(),
  v3Player: vi.fn(({
    recordingId,
    token,
    recordingTitle,
    startPositionSeconds,
    suppressResumePrompt,
  }: {
    recordingId?: string;
    token?: string;
    recordingTitle?: string;
    startPositionSeconds?: number;
    suppressResumePrompt?: boolean;
  }) => (
    <div data-testid="v3-player-props">
      {`${recordingId || ''}|${token || ''}|${recordingTitle || ''}|${startPositionSeconds ?? ''}|${suppressResumePrompt ? 'suppress' : 'prompt'}`}
    </div>
  )),
}));

vi.mock('../client-ts', () => ({
  getRecordings,
  getSeriesRules,
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
  isResumeEligible: (resume?: { posSeconds?: number; finished?: boolean }) => Boolean(resume && !resume.finished && (resume.posSeconds || 0) >= 15),
}));

vi.mock('../features/player/components/V3Player', () => ({
  __esModule: true,
  default: v3Player,
}));

import { MemoryRouter } from 'react-router';
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
    <MemoryRouter>
      <QueryClientProvider client={queryClient}>
        <HouseholdProfilesProvider>
          <RecordingsList />
        </HouseholdProfilesProvider>
      </QueryClientProvider>
    </MemoryRouter>
  );
}

describe('RecordingsList', () => {
  const fetchMock = vi.fn();

  afterEach(() => {
    vi.clearAllMocks();
    vi.unstubAllGlobals();
    setClientAuthToken('');
    window.localStorage.clear();
  });

  beforeEach(() => {
    fetchMock.mockImplementation(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes('/thumbnail.jpg')) {
        return {
          ok: true,
          blob: async () => new Blob(['thumb'], { type: 'image/jpeg' }),
        };
      }

      return {
        ok: true,
        json: async () => ({}),
      };
    });
    vi.stubGlobal('fetch', fetchMock);
    Object.defineProperty(URL, 'createObjectURL', {
      value: vi.fn(() => 'blob:recording-thumbnail'),
      configurable: true,
    });
    Object.defineProperty(URL, 'revokeObjectURL', {
      value: vi.fn(),
      configurable: true,
    });
  });

  it('renders recording descriptions without the leading internal rule marker', async () => {
    getRecordings.mockResolvedValue({
      data: {
        currentRoot: 'root-a', currentPath: '', roots: [{ id: 'root-a', name: 'Root A' }],
        breadcrumbs: [], directories: [],
        recordings: [{
          recordingId: 'rec-rule-marker', title: 'Example Show', beginUnixSeconds: 1710000000,
          length: '30m', description: '[xg2g-rule:123e4567-e89b-12d3-a456-426614174000] Episode description',
        }],
      },
    });

    renderWithQueryClient();

    fireEvent.click(await screen.findByRole('button', { name: /Example Show/ }));
    expect(await screen.findByText('Episode description')).toBeInTheDocument();
    expect(screen.queryByText(/xg2g-rule:/)).not.toBeInTheDocument();
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

    const seriesElements = await screen.findAllByText('Series');
    const directoryCard = seriesElements.find((el) => el.closest('[data-ui="card"]'));
    expect(directoryCard).toBeDefined();

    fireEvent.click(directoryCard!);

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

    confirm.mockResolvedValue(true);

    renderWithQueryClient();

    expect(await screen.findByText('Movie Night')).toBeInTheDocument();

    fireEvent.click(screen.getByTitle('Selection Mode'));
    fireEvent.click(screen.getByText('Movie Night'));
    fireEvent.click(screen.getByRole('button', { name: 'Delete Selected (1)' }));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/v3/recordings/rec-1/delete',
        expect.objectContaining({
          method: 'POST',
          credentials: 'same-origin',
          headers: expect.objectContaining({
            Authorization: 'Bearer test-token',
          }),
        })
      );
    });

    await waitFor(() => {
      expect(screen.queryByText('Movie Night')).not.toBeInTheDocument();
    });

    expect(toast).toHaveBeenCalledWith({ kind: 'success', message: 'Successfully deleted 1 recordings.' });
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

    fireEvent.change(screen.getByLabelText('Sort'), { target: { value: 'oldest' } });

    expect(getTitles()).toEqual([
      'Older Recording',
      'Active Recording',
      'Newest Recording',
    ]);

    fireEvent.change(screen.getByLabelText('View'), { target: { value: 'active' } });

    expect(getTitles()).toEqual(['Active Recording']);
  });

  it('passes the auth token and title into recording playback', async () => {
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
      expect(screen.getByTestId('v3-player-props')).toHaveTextContent('rec-token|test-token|Token Recording|0|suppress');
    });
  });

  it('fetches a thumbnail image for recordings with auth', async () => {
    getRecordings.mockResolvedValue({
      data: {
        currentRoot: 'root-a',
        currentPath: '',
        roots: [{ id: 'root-a', name: 'Root A' }],
        breadcrumbs: [],
        directories: [],
        recordings: [
          {
            recordingId: 'rec-thumb',
            title: 'Thumbnail Recording',
            beginUnixSeconds: 1710000000,
            length: '30m',
            description: 'Has a real preview route',
            status: 'completed',
          },
        ],
      },
    });

    renderWithQueryClient();

    expect(await screen.findByText('Thumbnail Recording')).toBeInTheDocument();
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/v3/recordings/rec-thumb/thumbnail.jpg',
        expect.objectContaining({
          method: 'GET',
          credentials: 'same-origin',
          headers: expect.objectContaining({
            Authorization: 'Bearer test-token',
          }),
        })
      );
    });
  });

  it('revokes the thumbnail object URL on unmount (no blob leak)', async () => {
    getRecordings.mockResolvedValue({
      data: {
        currentRoot: 'root-a',
        currentPath: '',
        roots: [{ id: 'root-a', name: 'Root A' }],
        breadcrumbs: [],
        directories: [],
        recordings: [
          {
            recordingId: 'rec-thumb',
            title: 'Thumbnail Recording',
            beginUnixSeconds: 1710000000,
            length: '30m',
            description: 'Has a real preview route',
            status: 'completed',
          },
        ],
      },
    });

    const view = renderWithQueryClient();

    await screen.findByTestId('recording-thumbnail');
    await waitFor(() => {
      expect(URL.createObjectURL).toHaveBeenCalled();
    });

    view.unmount();

    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:recording-thumbnail');
  });

  it('renames a recording from the admin card action', async () => {
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
              recordingId: 'rec-rename',
              title: 'Old Name',
              beginUnixSeconds: 1710000000,
              length: '30m',
              description: 'Rename me',
              localWritable: true,
              status: 'completed',
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
          recordings: [
            {
              recordingId: 'rec-rename-new',
              title: 'New Name',
              beginUnixSeconds: 1710000000,
              length: '30m',
              description: 'Rename me',
              localWritable: true,
              status: 'completed',
            },
          ],
        },
      });

    const promptSpy = vi.spyOn(window, 'prompt').mockReturnValue('New Name');

    renderWithQueryClient();

    expect(await screen.findByText('Old Name')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Rename' }));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledWith(
        '/api/v3/recordings/rec-rename/rename',
        expect.objectContaining({
          method: 'POST',
          credentials: 'same-origin',
          body: JSON.stringify({ title: 'New Name' }),
          headers: expect.objectContaining({
            Authorization: 'Bearer test-token',
            'Content-Type': 'application/json',
          }),
        })
      );
    });

    await waitFor(() => {
      expect(screen.getByText('New Name')).toBeInTheDocument();
    });

    expect(promptSpy).toHaveBeenCalledWith('New name for "Old Name"', 'Old Name');
  });

  it('hides rename when the recording is not locally writable', async () => {
    getRecordings.mockResolvedValue({
      data: {
        currentRoot: 'root-a',
        currentPath: '',
        roots: [{ id: 'root-a', name: 'Root A' }],
        breadcrumbs: [],
        directories: [],
        recordings: [
          {
            recordingId: 'rec-delete-only',
            title: 'Receiver Delete Only',
            beginUnixSeconds: 1710000000,
            length: '45m',
            description: 'Delete should still be available',
            localWritable: false,
            status: 'completed',
          },
        ],
      },
    });

    renderWithQueryClient();

    expect(await screen.findByText('Receiver Delete Only')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Rename' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Delete' })).toBeInTheDocument();
  });

  it('asks for resume or restart before opening a partially watched recording', async () => {
    getRecordings.mockResolvedValue({
      data: {
        currentRoot: 'root-a',
        currentPath: '',
        roots: [{ id: 'root-a', name: 'Root A' }],
        breadcrumbs: [],
        directories: [],
        recordings: [
          {
            recordingId: 'rec-resume',
            title: 'Resume Recording',
            beginUnixSeconds: 1710000000,
            durationSeconds: 1800,
            length: '30m',
            description: 'Partially watched',
            status: 'completed',
            resume: {
              posSeconds: 120,
              durationSeconds: 1800,
              finished: false,
            },
          },
        ],
      },
    });

    renderWithQueryClient();

    expect(await screen.findByText('Resume Recording')).toBeInTheDocument();
    expect(screen.queryByTestId('v3-player-props')).not.toBeInTheDocument();

    fireEvent.click(screen.getByText('Resume Recording'));
    fireEvent.click(screen.getByRole('button', { name: 'Resume from 02:00' }));

    await waitFor(() => {
      expect(screen.getByTestId('v3-player-props')).toHaveTextContent('rec-resume|test-token|Resume Recording|120|suppress');
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

  it('suppresses duration badge when length is 0m or unknown and formats valid duration cleanly', async () => {
    getRecordings.mockResolvedValue({
      data: {
        currentRoot: 'root-a',
        currentPath: '',
        roots: [{ id: 'root-a', name: 'Root A' }],
        breadcrumbs: [],
        directories: [],
        recordings: [
          {
            recordingId: 'rec-zero',
            title: 'Zero Duration Show',
            beginUnixSeconds: 1710000000,
            length: '0m',
            description: 'No valid duration',
          },
          {
            recordingId: 'rec-valid',
            title: 'Valid Duration Show',
            beginUnixSeconds: 1710000000,
            length: '45m',
            description: '45 minutes show',
          },
        ],
      },
    });

    renderWithQueryClient();

    expect(await screen.findByText('Zero Duration Show')).toBeInTheDocument();
    expect(screen.getByText('Valid Duration Show')).toBeInTheDocument();

    // Verify '0m' is never rendered anywhere in the document
    expect(screen.queryByText('0m')).not.toBeInTheDocument();
    expect(screen.queryByText(/0\s*Min/i)).not.toBeInTheDocument();

    // Verify valid duration is rendered nicely
    expect(screen.getByText('45 Min.')).toBeInTheDocument();
  });

  it('groups multi-episode series and allows drilling down into episodes', async () => {
    getRecordings.mockResolvedValue({
      data: {
        currentRoot: 'root-a',
        currentPath: '',
        roots: [{ id: 'root-a', name: 'Root A' }],
        breadcrumbs: [],
        directories: [],
        recordings: [
          {
            recordingId: 'rec-cafe-1',
            title: 'Café PULS',
            beginUnixSeconds: 1710000000,
            length: '60m',
            description: 'Episode 1',
          },
          {
            recordingId: 'rec-cafe-2',
            title: 'Café PULS',
            beginUnixSeconds: 1710086400,
            length: '60m',
            description: 'Episode 2',
          },
          {
            recordingId: 'rec-movie-1',
            title: 'Standalone Movie',
            beginUnixSeconds: 1710000000,
            length: '90m',
            description: 'A single movie',
          },
        ],
      },
    });

    renderWithQueryClient();

    // Standalone movie is shown under standalone recordings
    expect(await screen.findByText('Standalone Movie')).toBeInTheDocument();

    // Multi-episode series is grouped
    expect(screen.getByText('Café PULS')).toBeInTheDocument();
    expect(screen.getByText('2 episodes')).toBeInTheDocument();

    // Clicking the series card navigates into the series view
    const seriesCard = screen.getByRole('button', { name: /Café PULS/i });
    fireEvent.click(seriesCard);

    // Check if back to recordings button is present
    expect(await screen.findByText('Back to all recordings')).toBeInTheDocument();
    expect(screen.getAllByTestId('recording-title')).toHaveLength(2);
  });

  it('filters recordings in real time using search input', async () => {
    getRecordings.mockResolvedValue({
      data: {
        currentRoot: 'root-a',
        currentPath: '',
        roots: [{ id: 'root-a', name: 'Root A' }],
        breadcrumbs: [],
        directories: [],
        recordings: [
          {
            recordingId: 'rec-monk',
            title: 'Monk',
            beginUnixSeconds: 1710000000,
            length: '45m',
            description: 'Mr. Monk and the Candidate',
          },
          {
            recordingId: 'rec-charly',
            title: 'Unser Charly',
            beginUnixSeconds: 1710000000,
            length: '45m',
            description: 'Charly feiert Geburtstag',
          },
        ],
      },
    });

    renderWithQueryClient();

    expect(await screen.findByText('Monk')).toBeInTheDocument();
    expect(screen.getByText('Unser Charly')).toBeInTheDocument();

    const searchInput = screen.getByPlaceholderText(/search/i);
    fireEvent.change(searchInput, { target: { value: 'Monk' } });

    expect(screen.getByText('Monk')).toBeInTheDocument();
    expect(screen.queryByText('Unser Charly')).not.toBeInTheDocument();

    // Clear search
    fireEvent.change(searchInput, { target: { value: '' } });
    expect(screen.getByText('Monk')).toBeInTheDocument();
    expect(screen.getByText('Unser Charly')).toBeInTheDocument();
  });
});
