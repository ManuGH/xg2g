import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import RecordingsList from '../src/components/RecordingsList';

const { getRecordings, getSeriesRules, confirm, toast } = vi.hoisted(() => ({
  getRecordings: vi.fn(),
  getSeriesRules: vi.fn().mockResolvedValue({
    data: [{ id: 'rule-cp', keyword: 'Café PULS', retentionDays: 7 }],
  }),
  confirm: vi.fn(),
  toast: vi.fn(),
}));

vi.mock('../src/client-ts', () => ({
  getRecordings,
  getSeriesRules,
}));

vi.mock('../src/context/AppContext', () => ({
  useAppContext: () => ({
    auth: { token: 'test-token' },
  }),
}));

vi.mock('../src/context/UiOverlayContext', () => ({
  useUiOverlay: () => ({
    confirm,
    toast,
  }),
}));

vi.mock('../src/context/HouseholdProfilesContext', () => ({
  useHouseholdProfiles: () => ({
    selectedProfile: { id: 'p1', name: 'Main' },
    canAccessDvrPlayback: true,
    canManageDvr: true,
    isReady: true,
  }),
}));

vi.mock('../src/features/player/components/V3Player', () => ({
  __esModule: true,
  default: () => <div data-testid="v3-player" />,
}));

const mockRecordings = [
  {
    recordingId: 'cp-1',
    title: 'Café PULS mit PULS 4 Aktuell',
    description: 'Das Morgenmagazin mit aktuellen Berichten',
    beginUnixSeconds: 1727600000,
    durationSeconds: 3600,
    status: 'completed' as const,
  },
  {
    recordingId: 'cp-2',
    title: 'Café PULS - Das Magazin',
    description: 'Unterhaltung und Nachrichten am Morgen',
    beginUnixSeconds: 1727686400,
    durationSeconds: 3600,
    status: 'completed' as const,
  },
  {
    recordingId: 'cp-3',
    title: 'Café PULS',
    description: 'Frühfernsehen live aus Wien',
    beginUnixSeconds: 1727772800,
    durationSeconds: 3600,
    status: 'completed' as const,
  },
  {
    recordingId: 'movie-1',
    title: 'Inception',
    description: 'Ein packender Spielfilm von Christopher Nolan.',
    beginUnixSeconds: 1727500000,
    durationSeconds: 8800,
    status: 'completed' as const,
  },
  {
    recordingId: 'sport-1',
    title: 'Bundesliga: Sturm Graz - Rapid Wien',
    description: 'Live aus der Merkur Arena in Graz.',
    beginUnixSeconds: 1727550000,
    durationSeconds: 7200,
    status: 'completed' as const,
  },
];

function renderRecordings(initialEntries: string[] = ['/recordings']) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={initialEntries}>
        <Routes>
          <Route path="/recordings" element={<RecordingsList />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

describe('RecordingsList category filtering and series subfolder grouping', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    getRecordings.mockResolvedValue({
      data: {
        currentRoot: 'root-1',
        currentPath: '',
        roots: [{ id: 'root-1', name: 'HDD' }],
        breadcrumbs: [],
        directories: [],
        recordings: mockRecordings,
      },
    });
  });

  it('renders category tabs and filters between All, Movies, Series, and Sports', async () => {
    renderRecordings();

    // Verify category buttons are rendered
    expect(await screen.findByRole('tab', { name: /^all$/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /^movies$/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /^series$/i })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: /^sports$/i })).toBeInTheDocument();

    // Initially in "All" view: items of different categories are present
    expect(screen.getByText('Inception')).toBeInTheDocument();
    expect(screen.getByText('Bundesliga: Sturm Graz - Rapid Wien')).toBeInTheDocument();
    expect(screen.getAllByText(/Café PULS/).length).toBeGreaterThan(0);

    // Filter to Movies
    fireEvent.click(screen.getByRole('tab', { name: /^movies$/i }));
    await waitFor(() => {
      expect(screen.getByText('Inception')).toBeInTheDocument();
      expect(screen.queryByText('Bundesliga: Sturm Graz - Rapid Wien')).not.toBeInTheDocument();
      expect(screen.queryByText('Café PULS mit PULS 4 Aktuell')).not.toBeInTheDocument();
    });

    // Filter to Sports
    fireEvent.click(screen.getByRole('tab', { name: /^sports$/i }));
    await waitFor(() => {
      expect(screen.getByText('Bundesliga: Sturm Graz - Rapid Wien')).toBeInTheDocument();
      expect(screen.queryByText('Inception')).not.toBeInTheDocument();
      expect(screen.queryByText('Café PULS mit PULS 4 Aktuell')).not.toBeInTheDocument();
    });
  });

  it('groups multiple episodes into a series folder and allows drill-down into series episodes', async () => {
    renderRecordings(['/recordings?category=series']);

    // Should display the series section with Café PULS folder card
    expect(await screen.findByRole('heading', { level: 2, name: 'Series' })).toBeInTheDocument();
    expect(screen.getByText('3 episodes')).toBeInTheDocument();

    // Find the series folder card for Café PULS and click it
    const seriesFolderCard = screen.getByText('3 episodes').closest('[data-ui="card"]');
    expect(seriesFolderCard).toBeInTheDocument();
    fireEvent.click(seriesFolderCard!);

    // Inside the subfolder view:
    // 1. Subfolder header should be rendered with back button to all series
    await waitFor(() => {
      expect(screen.getByText('Back to all series')).toBeInTheDocument();
    });

    // 2. All 3 episodes of Café PULS should be visible
    const episodeTitles = screen.getAllByTestId('recording-title').map((el) => el.textContent);
    expect(episodeTitles).toContain('Café PULS mit PULS 4 Aktuell');
    expect(episodeTitles).toContain('Café PULS - Das Magazin');
    expect(episodeTitles).toContain('Café PULS');

    // Non-series items must not be shown
    expect(screen.queryByText('Inception')).not.toBeInTheDocument();
    expect(screen.queryByText('Bundesliga: Sturm Graz - Rapid Wien')).not.toBeInTheDocument();

    // Clicking "Back to all series" should return back to the series overview
    fireEvent.click(screen.getByText('Back to all series'));
    await waitFor(() => {
      expect(screen.getByRole('heading', { level: 2, name: 'Series' })).toBeInTheDocument();
      expect(screen.getByText('3 episodes')).toBeInTheDocument();
    });
  });

  it('displays single episode series as normal recording cards under Single Shows without folder badge', async () => {
    const recordingsWithSingleShow = [
      ...mockRecordings,
      {
        recordingId: 'charly-1',
        title: 'Unser Charly - S01E01 - Heimkehr',
        description: 'Tierarztserie mit Charly dem Schimpansen',
        beginUnixSeconds: 1727700000,
        durationSeconds: 2700,
        status: 'completed' as const,
      },
    ];

    getRecordings.mockResolvedValue({
      data: {
        currentRoot: 'root-1',
        currentPath: '',
        roots: [{ id: 'root-1', name: 'HDD' }],
        breadcrumbs: [],
        directories: [],
        recordings: recordingsWithSingleShow,
      },
    });

    renderRecordings(['/recordings?category=series']);

    // Should display both Series (for multi-episode series) and Single Shows
    expect(await screen.findByRole('heading', { level: 2, name: 'Series' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { level: 2, name: 'Single Shows' })).toBeInTheDocument();

    // Café PULS should have folder card with "3 episodes"
    expect(screen.getByText('3 episodes')).toBeInTheDocument();

    // "Unser Charly" should NOT have a folder card with "1 episode"
    expect(screen.queryByText('1 episode')).not.toBeInTheDocument();

    // "Unser Charly" must be rendered directly as a recording card in the root view
    expect(screen.getByText('Unser Charly - S01E01 - Heimkehr')).toBeInTheDocument();

    // The individual episodes of Café PULS must NOT be rendered in this root view
    expect(screen.queryByText('Café PULS mit PULS 4 Aktuell')).not.toBeInTheDocument();
    expect(screen.queryByText('Café PULS - Das Magazin')).not.toBeInTheDocument();
  });

  it('displays multi-episode series rail in All category and allows direct drill-down', async () => {
    renderRecordings(['/recordings']);

    // In All category, multi-episode series rail is shown for quick subfolder access
    expect(await screen.findByText('Series & Shows')).toBeInTheDocument();
    expect(screen.getByText('3 episodes')).toBeInTheDocument();

    // Click the series folder from the rail
    const seriesFolderCard = screen.getByText('3 episodes').closest('[data-ui="card"]');
    fireEvent.click(seriesFolderCard!);

    // Should drill down into Café PULS subfolder with back button to All recordings
    await waitFor(() => {
      expect(screen.getByText('Back to all recordings')).toBeInTheDocument();
    });

    // Episodes are shown
    expect(screen.getByText('Café PULS mit PULS 4 Aktuell')).toBeInTheDocument();

    // Clicking "Back to all recordings" returns to the All view
    fireEvent.click(screen.getByText('Back to all recordings'));
    await waitFor(() => {
      expect(screen.getByText('Inception')).toBeInTheDocument();
    });
  });
});
