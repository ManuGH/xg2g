import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ClientRequestError, setClientAuthToken } from '../../services/clientWrapper';
import type { EpgEvent, Timer } from './types';
import styles from './EPG.module.css';

const {
  fetchEpgEvents,
  fetchTimers,
  addTimer,
  confirm,
  toast,
  timersView,
  useUiSurfaceMock,
} = vi.hoisted(() => ({
  fetchEpgEvents: vi.fn<(...args: any[]) => Promise<EpgEvent[]>>(),
  fetchTimers: vi.fn<() => Promise<Timer[]>>(),
  addTimer: vi.fn(),
  confirm: vi.fn(),
  toast: vi.fn(),
  timersView: vi.fn(() => <div data-testid="epg-timers-stub">Timers view</div>),
  useUiSurfaceMock: vi.fn(),
}));

vi.mock('./epgApi', () => ({
  fetchEpgEvents,
  fetchTimers,
}));

vi.mock('../../client-ts', () => ({
  addTimer,
}));

vi.mock('../../context/UiOverlayContext', () => ({
  useUiOverlay: () => ({
    confirm,
    toast,
  }),
}));

vi.mock('../../context/UiSurfaceContext', () => ({
  useUiSurface: () => useUiSurfaceMock(),
}));

vi.mock('../../context/HouseholdProfilesContext', () => ({
  useHouseholdProfiles: () => ({
    selectedProfile: { favoriteServiceRefs: [] },
    isReady: true,
    isFavoriteService: () => false,
    toggleFavoriteService: vi.fn(),
    canManageDvr: true,
    canAccessDvrPlayback: true,
    canAccessSettings: true,
    profiles: [],
    selectedProfileId: 'default',
    pinConfigured: false,
    isUnlocked: true,
    selectProfile: vi.fn(),
    ensureUnlocked: vi.fn(),
    saveProfile: vi.fn(),
    deleteProfile: vi.fn(),
  }),
  HouseholdProfilesProvider: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock('./components/EpgChannelList', () => ({
  EpgChannelList: () => <div data-testid="epg-channel-list" />,
}));

vi.mock('../../components/Timers', () => ({
  __esModule: true,
  default: timersView,
}));

import EPG from './EPG';

function createUiSurface(overrides: Record<string, unknown> = {}) {
  return {
    surface: 'large',
    orientation: 'landscape',
    inputMode: 'fine',
    heightClass: 'comfortable',
    navMode: 'rail',
    width: 1440,
    height: 900,
    ...overrides,
  };
}

function renderWithProviders(ui: ReactNode, initialEntries: string[] = ['/epg']) {
  return render(
    <MemoryRouter initialEntries={initialEntries}>
      {ui}
    </MemoryRouter>
  );
}

function createDeferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe('EPG shared primitives', () => {
  beforeEach(() => {
    setClientAuthToken('');
    vi.stubGlobal('setInterval', vi.fn(() => 0 as unknown as ReturnType<typeof setInterval>));
    vi.stubGlobal('clearInterval', vi.fn());
    useUiSurfaceMock.mockReturnValue(createUiSurface());
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    vi.unstubAllGlobals();
    setClientAuthToken('');
    window.localStorage.clear();
  });

  it('renders a section skeleton while the initial EPG load is in progress', async () => {
    fetchTimers.mockResolvedValue([]);
    const deferred = createDeferred<EpgEvent[]>();
    fetchEpgEvents.mockImplementation(() => deferred.promise);

    renderWithProviders(<EPG channels={[]} />);

    expect(screen.getByRole('status', { name: /Loading EPG/i })).toHaveAttribute(
      'data-loading-variant',
      'section'
    );

    deferred.resolve([]);

    await waitFor(() => {
      expect(screen.queryByRole('status', { name: /Loading EPG/i })).not.toBeInTheDocument();
    });
  });

  it('renders a visible section skeleton while search is in progress', async () => {
    fetchTimers.mockResolvedValue([]);
    const deferred = createDeferred<EpgEvent[]>();
    fetchEpgEvents.mockResolvedValueOnce([]);
    fetchEpgEvents.mockImplementationOnce(() => deferred.promise);

    renderWithProviders(<EPG channels={[]} />);

    const input = screen.getByPlaceholderText(/Search Services/i);
    fireEvent.change(input, {
      target: { value: 'news' },
    });
    // Search is triggered via Enter key in the refactored toolbar
    fireEvent.keyDown(input, { key: 'Enter' });

    expect(await screen.findByRole('status', { name: /Loading/i })).toHaveAttribute(
      'data-loading-variant',
      'section'
    );

    deferred.resolve([]);

    await waitFor(() => {
      expect(screen.queryByRole('status', { name: /Loading/i })).not.toBeInTheDocument();
    });
  });

  it('renders a retryable error panel for main EPG load failures', async () => {
    fetchTimers.mockResolvedValue([]);
    fetchEpgEvents.mockRejectedValue(
      new ClientRequestError({
        status: 503,
        title: 'Service unavailable',
        detail: 'epg backend offline',
      })
    );

    renderWithProviders(<EPG channels={[]} />);

    expect(await screen.findByRole('heading', { name: 'Service unavailable' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

    await waitFor(() => {
      expect(fetchEpgEvents).toHaveBeenCalledTimes(2);
    });
  });

  it('renders the embedded timers view for the timers section route', async () => {
    fetchTimers.mockResolvedValue([]);
    fetchEpgEvents.mockResolvedValue([]);

    renderWithProviders(<EPG channels={[]} />, ['/epg?section=timers']);

    expect(await screen.findByTestId('epg-timers-stub')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Back to Live TV' })).toBeInTheDocument();
  });

  it('applies the compact landscape surface classes for phone-width guide layouts', async () => {
    fetchTimers.mockResolvedValue([]);
    fetchEpgEvents.mockResolvedValue([]);
    useUiSurfaceMock.mockReturnValue(createUiSurface({
      surface: 'medium',
      orientation: 'landscape',
      inputMode: 'coarse',
      heightClass: 'compact',
      navMode: 'bottom',
      width: 844,
      height: 390,
    }));

    const { container } = renderWithProviders(<EPG channels={[]} />);
    const page = container.querySelector(`.${styles.page}`) as HTMLElement;
    await waitFor(() => {
      expect(screen.queryByRole('status', { name: /Loading EPG/i })).not.toBeInTheDocument();
    });

    expect(page).toBeTruthy();
    expect(page.className).toContain(styles.surfaceStacked);
    expect(page.className).toContain(styles.surfaceCompact);
    expect(page.className).toContain(styles.surfaceCompactLandscape);
  });
});
