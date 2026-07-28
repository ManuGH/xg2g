import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import App from '../src/App';
import { AppProvider } from '../src/context/AppContext';
import { requestAuthRequired } from '../src/features/player/sessionEvents';
import { ROUTE_MAP } from '../src/routes';

const {
  mockNavigation,
  mockGetSystemConfig,
  mockGetServicesBouquets,
  mockGetServices,
  mockStoredToken,
  mockToast,
  mockUseHouseholdProfiles,
} = vi.hoisted(() => ({
  mockNavigation: vi.fn(),
  mockGetSystemConfig: vi.fn(),
  mockGetServicesBouquets: vi.fn(),
  mockGetServices: vi.fn(),
  mockStoredToken: { value: '' },
  mockToast: vi.fn(),
  mockUseHouseholdProfiles: vi.fn(),
}));

vi.mock('../src/components/Navigation', () => ({
  default: () => {
    mockNavigation();
    return <div data-testid="journey-navigation" />;
  },
}));

vi.mock('../src/context/HouseholdProfilesContext', () => ({
  useHouseholdProfiles: () => mockUseHouseholdProfiles(),
}));

vi.mock('../src/context/UiOverlayContext', () => ({
  useUiOverlay: () => ({
    toast: mockToast,
    confirm: vi.fn(),
    promptPin: vi.fn(),
  }),
}));

vi.mock('../src/features/epg/EPG', () => ({
  default: () => <div>EPG route ready</div>,
}));

vi.mock('../src/components/Settings', () => ({
  default: () => <div>Settings route ready</div>,
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

vi.mock('../src/features/system/SystemInfo', () => ({
  SystemInfo: () => <div>System route ready</div>,
}));

vi.mock('../src/client-ts', async () => {
  const actual = await vi.importActual<typeof import('../src/client-ts')>('../src/client-ts');
  return {
    ...actual,
    getSystemConfig: mockGetSystemConfig,
    getServicesBouquets: mockGetServicesBouquets,
    getServices: mockGetServices,
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

describe('Bootstrap and Auth journeys', () => {
  beforeEach(() => {
    const defaultProfile = {
      id: 'household-default',
      name: 'Haushalt',
      kind: 'adult' as const,
      maxFsk: null,
      allowedBouquets: [],
      allowedServiceRefs: [],
      favoriteServiceRefs: [],
      permissions: {
        dvrPlayback: true,
        dvrManage: true,
        settings: true,
      },
    };

    mockStoredToken.value = '';
    mockNavigation.mockClear();
    mockToast.mockReset();
    mockGetServicesBouquets.mockReset();
    mockGetServices.mockReset();
    mockGetSystemConfig.mockReset();
    mockUseHouseholdProfiles.mockReturnValue({
      profiles: [defaultProfile],
      selectedProfile: defaultProfile,
      selectedProfileId: defaultProfile.id,
      isReady: true,
      pinConfigured: false,
      isUnlocked: true,
      selectProfile: vi.fn().mockResolvedValue(true),
      ensureUnlocked: vi.fn().mockResolvedValue(true),
      saveProfile: vi.fn().mockResolvedValue(undefined),
      deleteProfile: vi.fn().mockResolvedValue(undefined),
      toggleFavoriteService: vi.fn(),
      isFavoriteService: vi.fn().mockReturnValue(false),
      canAccessDvrPlayback: true,
      canManageDvr: true,
      canAccessSettings: true,
    });

    mockGetServicesBouquets.mockResolvedValue({
      data: [{ name: 'Favorites', services: 1 }],
      error: undefined,
      response: { status: 200 },
    });
    mockGetServices.mockResolvedValue({
      data: [{ servicereference: '1:0:1', name: 'Das Erste HD' }],
      error: undefined,
      response: { status: 200 },
    });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('shows the auth surface when a protected route opens without a stored token', async () => {
    mockGetSystemConfig.mockResolvedValue({
      data: undefined,
      error: undefined,
      response: { status: 200 },
    });

    renderJourney(ROUTE_MAP.epg);

    expect(screen.getByRole('heading', { name: 'Authentication Required' })).toBeInTheDocument();
    expect(screen.getByText('Enter your API token to open the xg2g control surface.')).toBeInTheDocument();
    expect(screen.getByTestId('journey-pathname')).toHaveTextContent(ROUTE_MAP.epg);
  });

  it('recovers from a stale token and continues to the epg route after re-authentication', async () => {
    mockStoredToken.value = 'stale-token';
    mockGetSystemConfig
      .mockResolvedValueOnce({
        data: undefined,
        error: {
          code: 'AUTH_REQUIRED',
          message: 'Authentication required',
          requestId: 'req-bootstrap-401',
        },
        response: { status: 401 },
      })
      .mockResolvedValue({
        data: { openWebIF: { baseUrl: 'http://receiver.local' }, bouquets: [] },
        error: undefined,
        response: { status: 200 },
      });

    renderJourney(ROUTE_MAP.epg);

    await screen.findByRole('heading', { name: 'Session Expired' });
    fireEvent.change(screen.getByLabelText('API Token'), {
      target: { value: '  fresh-token  ' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Authenticate' }));

    await waitFor(() => {
      expect(screen.getByText('EPG route ready')).toBeInTheDocument();
      expect(screen.getByTestId('journey-pathname')).toHaveTextContent(ROUTE_MAP.epg);
    });
  });

  it('redirects an unconfigured protected route to settings', async () => {
    mockStoredToken.value = 'valid-token';
    mockGetSystemConfig.mockResolvedValue({
      data: { openWebIF: { baseUrl: '' }, bouquets: [] },
      error: undefined,
      response: { status: 200 },
    });

    renderJourney(ROUTE_MAP.epg);

    await waitFor(() => {
      expect(screen.getByText('Settings route ready')).toBeInTheDocument();
      expect(screen.getByTestId('journey-pathname')).toHaveTextContent(ROUTE_MAP.settings);
    });
  });

  it('keeps the settings route reachable while the system is unconfigured', async () => {
    mockStoredToken.value = 'valid-token';
    mockGetSystemConfig.mockResolvedValue({
      data: { openWebIF: { baseUrl: '' }, bouquets: [] },
      error: undefined,
      response: { status: 200 },
    });

    renderJourney(ROUTE_MAP.settings);

    await waitFor(() => {
      expect(screen.getByText('Settings route ready')).toBeInTheDocument();
      expect(screen.getByTestId('journey-pathname')).toHaveTextContent(ROUTE_MAP.settings);
    });
  });

  it('shows a retryable recovery surface for bootstrap errors and continues after retry', async () => {
    mockStoredToken.value = 'valid-token';
    mockGetSystemConfig
      .mockResolvedValueOnce({
        data: undefined,
        error: {
          type: 'about:blank',
          title: 'Service unavailable',
          status: 503,
          requestId: 'req-bootstrap-503',
          detail: 'Bootstrap backend offline'
        },
        response: { status: 503 },
      })
      .mockResolvedValue({
        data: { openWebIF: { baseUrl: 'http://receiver.local' }, bouquets: [] },
        error: undefined,
        response: { status: 200 },
      });

    renderJourney(ROUTE_MAP.epg);

    await screen.findByRole('heading', { name: 'Unable to start xg2g' });
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

    await waitFor(() => {
      expect(screen.getByText('EPG route ready')).toBeInTheDocument();
    });
  });

  it('switches a running session back to the expired auth surface when session expiry is signaled at runtime', async () => {
    mockStoredToken.value = 'valid-token';
    mockGetSystemConfig.mockResolvedValue({
      data: { openWebIF: { baseUrl: 'http://receiver.local' }, bouquets: [] },
      error: undefined,
      response: { status: 200 },
    });

    renderJourney(ROUTE_MAP.epg);

    await screen.findByText('EPG route ready');

    await act(async () => {
      requestAuthRequired({ source: 'journey-runtime', status: 401, code: 'AUTH_REQUIRED' });
    });

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Session Expired' })).toBeInTheDocument();
      expect(screen.getByTestId('journey-pathname')).toHaveTextContent(ROUTE_MAP.epg);
    });
  });
});
