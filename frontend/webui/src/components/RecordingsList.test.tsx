import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';

const {
  getRecordings,
  deleteRecording,
  confirm,
  toast,
} = vi.hoisted(() => ({
  getRecordings: vi.fn(),
  deleteRecording: vi.fn(),
  confirm: vi.fn(),
  toast: vi.fn(),
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

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, options?: Record<string, unknown>) => {
      if (key === 'recordings.deleteSelected' && typeof options?.count === 'number') {
        return `recordings.deleteSelected:${String(options.count)}`;
      }
      return key;
    },
  }),
}));

vi.mock('../features/resume/RecordingResumeBar', () => ({
  __esModule: true,
  default: () => null,
  isResumeEligible: () => false,
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
      <RecordingsList />
    </QueryClientProvider>
  );
}

describe('RecordingsList', () => {
  afterEach(() => {
    vi.clearAllMocks();
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

    fireEvent.click(screen.getByTitle('recordings.selectionMode'));
    fireEvent.click(screen.getByText('Movie Night'));
    fireEvent.click(screen.getByRole('button', { name: 'recordings.deleteSelected:1' }));

    await waitFor(() => {
      expect(deleteRecording).toHaveBeenCalledWith({ path: { recordingId: 'rec-1' } });
    });

    await waitFor(() => {
      expect(screen.queryByText('Movie Night')).not.toBeInTheDocument();
    });

    expect(toast).toHaveBeenCalledWith({ kind: 'success', message: 'Deleted 1 recording(s)' });
  });
});
