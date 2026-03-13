import type { ReactNode } from 'react';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import App from '../src/App';
import { AppProvider } from '../src/context/AppContext';
import { ClientRequestError } from '../src/lib/clientWrapper';
import { ROUTE_MAP } from '../src/routes';

const {
  mockGetSystemConfig,
  mockGetServicesBouquets,
  mockGetServices,
  mockGetEpg,
  mockGetTimers,
  mockAddTimer,
  mockStoredToken,
  mockConfirm,
  mockToast,
} = vi.hoisted(() => ({
  mockGetSystemConfig: vi.fn(),
  mockGetServicesBouquets: vi.fn(),
  mockGetServices: vi.fn(),
  mockGetEpg: vi.fn(),
  mockGetTimers: vi.fn(),
  mockAddTimer: vi.fn(),
  mockStoredToken: { value: 'valid-token' },
  mockConfirm: vi.fn(),
  mockToast: vi.fn(),
}));

vi.mock('../src/components/Dashboard', () => ({
  default: () => <div>Dashboard route ready</div>,
}));

vi.mock('../src/components/Files', () => ({
  default: () => <div>Files route ready</div>,
}));

vi.mock('../src/components/Logs', () => ({
  default: () => <div>Logs route ready</div>,
}));

vi.mock('../src/components/SeriesManager', () => ({
  default: () => <div>Series route ready</div>,
}));

vi.mock('../src/components/Timers', () => ({
  default: () => <div>Timers route ready</div>,
}));

vi.mock('../src/components/RecordingsList', () => ({
  default: () => <div>Recordings route ready</div>,
}));

vi.mock('../src/components/Settings', () => ({
  default: () => <div>Settings route ready</div>,
}));

vi.mock('../src/features/system/SystemInfo', () => ({
  SystemInfo: () => <div>System route ready</div>,
}));

vi.mock('../src/features/epg/components/EpgChannelList', () => ({
  EpgChannelList: ({ mode }: { mode: 'main' | 'search' }) => (
    <div data-testid={`epg-channel-list-${mode}`}>{mode === 'search' ? 'EPG search ready' : 'EPG main ready'}</div>
  ),
}));

vi.mock('../src/context/UiOverlayContext', () => ({
  useUiOverlay: () => ({
    confirm: mockConfirm,
    toast: mockToast,
  }),
  UiOverlayProvider: ({ children }: { children: ReactNode }) => children,
}));

vi.mock('../src/client-ts', async () => {
  const actual = await vi.importActual<typeof import('../src/client-ts')>('../src/client-ts');
  return {
    ...actual,
    getSystemConfig: mockGetSystemConfig,
    getServicesBouquets: mockGetServicesBouquets,
    getServices: mockGetServices,
    getEpg: mockGetEpg,
    getTimers: mockGetTimers,
    addTimer: mockAddTimer,
  };
});

vi.mock('../src/utils/tokenStorage', () => ({
  getStoredToken: vi.fn(() => mockStoredToken.value),
  setStoredToken: vi.fn((token: string) => {
    mockStoredToken.value = token;
  }),
  clearStoredToken: vi.fn(() => {
    mockStoredToken.value = '';
  }),
}));

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="journey-pathname">{location.pathname}</div>;
}

function renderJourney(initialEntry: string) {
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
      <MemoryRouter initialEntries={[initialEntry]}>
        <AppProvider>
          <LocationProbe />
          <App />
        </AppProvider>
      </MemoryRouter>
    </QueryClientProvider>
  );
}

describe('Primary navigation and EPG journeys', () => {
  beforeEach(() => {
    mockStoredToken.value = 'valid-token';
    mockGetSystemConfig.mockReset();
    mockGetServicesBouquets.mockReset();
    mockGetServices.mockReset();
    mockGetEpg.mockReset();
    mockGetTimers.mockReset();
    mockAddTimer.mockReset();
    mockConfirm.mockReset();
    mockToast.mockReset();

    mockGetSystemConfig.mockResolvedValue({
      data: { openWebIF: { baseUrl: 'http://receiver.local' }, bouquets: [] },
      error: undefined,
      response: { status: 200 },
    });
    mockGetServicesBouquets.mockResolvedValue({
      data: [{ name: 'Favorites', services: 1 }],
      error: undefined,
      response: { status: 200 },
    });
    mockGetServices.mockResolvedValue({
      data: [{ serviceRef: '1:0:1:123:456:789:0:0:0:0:', servicereference: '1:0:1:123:456:789:0:0:0:0:', name: 'Das Erste HD' }],
      error: undefined,
      response: { status: 200 },
    });
    mockGetTimers.mockResolvedValue({
      data: { items: [] },
      error: undefined,
      response: { status: 200 },
    });

    vi.stubGlobal('setInterval', vi.fn(() => 0 as unknown as ReturnType<typeof setInterval>));
    vi.stubGlobal('clearInterval', vi.fn());
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
    vi.unstubAllGlobals();
  });

  it('resolves deep links after a fresh mount and navigates from recordings to epg via the desktop nav', async () => {
    mockGetEpg.mockResolvedValue({
      data: [],
      error: undefined,
      response: { status: 200 },
    });

    const first = renderJourney(ROUTE_MAP.recordings);

    await waitFor(() => {
      expect(screen.getByText('Recordings route ready')).toBeInTheDocument();
      expect(screen.getByTestId('journey-pathname')).toHaveTextContent(ROUTE_MAP.recordings);
    });

    first.unmount();

    renderJourney(ROUTE_MAP.recordings);

    await waitFor(() => {
      expect(screen.getByText('Recordings route ready')).toBeInTheDocument();
      expect(screen.getByTestId('journey-pathname')).toHaveTextContent(ROUTE_MAP.recordings);
    });

    const desktopNav = screen.getByRole('navigation', { name: 'Main navigation' });
    fireEvent.click(within(desktopNav).getByRole('link', { name: 'nav.epg' }));

    await waitFor(() => {
      expect(screen.getByTestId('epg-channel-list-main')).toBeInTheDocument();
      expect(screen.getByTestId('journey-pathname')).toHaveTextContent(ROUTE_MAP.epg);
    });
  });

  it('navigates to overflow routes through the mobile more sheet', async () => {
    mockGetEpg.mockResolvedValue({
      data: [],
      error: undefined,
      response: { status: 200 },
    });

    renderJourney(ROUTE_MAP.dashboard);

    await screen.findByText('Dashboard route ready');

    fireEvent.click(screen.getByRole('button', { name: 'More', hidden: true }));
    fireEvent.click(screen.getByRole('link', { name: 'nav.timers' }));

    await waitFor(() => {
      expect(screen.getByText('Timers route ready')).toBeInTheDocument();
      expect(screen.getByTestId('journey-pathname')).toHaveTextContent(ROUTE_MAP.timers);
      expect(screen.queryByRole('dialog', { name: 'Control surfaces' })).toBeNull();
    });
  });

  it('keeps a 403 epg failure local to the route and renders the forbidden surface', async () => {
    mockGetEpg.mockRejectedValue(
      new ClientRequestError({
        status: 403,
        title: 'Forbidden',
        detail: 'Missing scope',
      })
    );

    renderJourney(ROUTE_MAP.epg);

    await waitFor(() => {
      expect(screen.getByText('player.forbidden')).toBeInTheDocument();
      expect(screen.getByTestId('journey-pathname')).toHaveTextContent(ROUTE_MAP.epg);
    });

    expect(screen.queryByRole('heading', { name: 'Session Expired' })).toBeNull();
  });
});
