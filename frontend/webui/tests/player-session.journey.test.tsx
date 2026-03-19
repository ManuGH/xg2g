import type { ReactNode } from 'react';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import App from '../src/App';
import { AppProvider } from '../src/context/AppContext';
import { ROUTE_MAP } from '../src/routes';
import { findFetchCall } from './helpers/liveFlow';

const {
  mockCreateSession,
  mockGetSystemConfig,
  mockGetServicesBouquets,
  mockGetServices,
  mockStoredToken,
  mockConfirm,
  mockToast,
} = vi.hoisted(() => ({
  mockCreateSession: vi.fn(),
  mockGetSystemConfig: vi.fn(),
  mockGetServicesBouquets: vi.fn(),
  mockGetServices: vi.fn(),
  mockStoredToken: { value: 'valid-token' },
  mockConfirm: vi.fn(),
  mockToast: vi.fn(),
}));

vi.mock('../src/lib/hlsRuntime', () => {
  const HlsMock = vi.fn().mockImplementation(function (this: any) {
    return {
      on: vi.fn(),
      loadSource: vi.fn(),
      attachMedia: vi.fn(),
      destroy: vi.fn(),
      recoverMediaError: vi.fn(),
      startLoad: vi.fn(),
      currentLevel: -1,
      levels: [],
    };
  });

  (HlsMock as any).isSupported = vi.fn().mockReturnValue(true);
  (HlsMock as any).Events = {
    LEVEL_SWITCHED: 'hlsLevelSwitched',
    MANIFEST_PARSED: 'hlsManifestParsed',
    LEVEL_LOADED: 'hlsLevelLoaded',
    FRAG_LOADED: 'hlsFragLoaded',
    ERROR: 'hlsError',
  };
  (HlsMock as any).ErrorTypes = {
    NETWORK_ERROR: 'networkError',
    MEDIA_ERROR: 'mediaError',
  };
  (HlsMock as any).ErrorDetails = {
    MANIFEST_LOAD_ERROR: 'manifestLoadError',
  };

  return { default: HlsMock };
});

vi.mock('../src/features/epg/EPG', () => ({
  default: ({ onPlay }: { onPlay: (channel: Record<string, unknown>) => void }) => (
    <div>
      <h1>EPG launcher ready</h1>
      <button
        type="button"
        onClick={() => onPlay({
          serviceRef: '1:0:1:123:456:789:0:0:0:0:',
          id: '1:0:1:123:456:789:0:0:0:0:',
          name: 'Journey Channel',
        })}
      >
        Launch player
      </button>
    </div>
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
    createSession: mockCreateSession,
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

type ResponseOptions = {
  requestId?: string;
  contentType?: string | null;
};

function jsonResponse(url: string, status: number, body: unknown, options: ResponseOptions = {}) {
  return Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    url,
    headers: {
      get: (name: string) => {
        const lower = name.toLowerCase();
        if (lower === 'content-type') {
          return options.contentType ?? 'application/json';
        }
        if (lower === 'x-request-id') {
          return options.requestId ?? null;
        }
        return null;
      },
    },
    json: async () => body,
    text: async () => JSON.stringify(body),
  });
}

type PlayerFetchScenario = {
  streamInfoStatus?: number;
  streamInfoBody?: Record<string, unknown>;
  intentStatus?: number;
  intentBody?: Record<string, unknown>;
  sessionStatus?: number;
  sessionBody?: Record<string, unknown>;
  heartbeatStatus?: number;
  heartbeatBody?: Record<string, unknown>;
};

const PLAYER_NETWORK_WAIT_MS = 3_000;

function installPlayerFetchMock(scenario: PlayerFetchScenario = {}) {
  const sessionId = 'sess-journey-1';
  const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
    const url = String(input);

    if (url.includes('/live/stream-info')) {
      return jsonResponse(url, scenario.streamInfoStatus ?? 200, scenario.streamInfoBody ?? {
        mode: 'hlsjs',
        requestId: 'req-live-flow-1',
        playbackDecisionToken: 'token-live-flow-1',
        decision: { reasons: ['direct_stream_match'] },
      }, { requestId: 'req-live-flow-1' });
    }

    if (url.includes('/intents')) {
      return jsonResponse(url, scenario.intentStatus ?? 200, scenario.intentBody ?? {
        sessionId,
        requestId: 'req-live-flow-1-intent',
      }, { requestId: 'req-live-flow-1-intent' });
    }

    if (url.includes(`/sessions/${sessionId}`) && !url.includes('/heartbeat') && !url.includes('/feedback')) {
      return jsonResponse(url, scenario.sessionStatus ?? 200, scenario.sessionBody ?? {
        state: 'READY',
        playbackUrl: '/live.m3u8',
        heartbeat_interval: 1,
      });
    }

    if (url.includes(`/sessions/${sessionId}/heartbeat`)) {
      return jsonResponse(url, scenario.heartbeatStatus ?? 200, scenario.heartbeatBody ?? {
        lease_expires_at: 'next',
      });
    }

    return jsonResponse(url, 200, {});
  });

  vi.stubGlobal('fetch', fetchMock as unknown as typeof globalThis.fetch);
  return fetchMock;
}

describe('Player session journeys', () => {
  beforeEach(() => {
    mockStoredToken.value = 'valid-token';
    mockCreateSession.mockReset();
    mockGetSystemConfig.mockReset();
    mockGetServicesBouquets.mockReset();
    mockGetServices.mockReset();
    mockConfirm.mockReset();
    mockToast.mockReset();

    mockCreateSession.mockResolvedValue({
      data: {},
      error: undefined,
      response: { status: 200 },
    });
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
      data: [{ serviceRef: '1:0:1:123:456:789:0:0:0:0:', servicereference: '1:0:1:123:456:789:0:0:0:0:', name: 'Journey Channel' }],
      error: undefined,
      response: { status: 200 },
    });

    vi.spyOn(window, 'setInterval').mockImplementation((() => {
      return 1 as unknown as ReturnType<typeof setInterval>;
    }) as typeof window.setInterval);
    vi.spyOn(window, 'clearInterval').mockImplementation(() => undefined);
  });

  afterEach(() => {
    cleanup();
    vi.useRealTimers();
    vi.restoreAllMocks();
    vi.clearAllMocks();
    vi.unstubAllGlobals();
  });

  it('starts a live player session from the epg route', async () => {
    const fetchMock = installPlayerFetchMock();

    renderJourney(ROUTE_MAP.epg);

    await screen.findByText('EPG launcher ready');
    fireEvent.click(screen.getByRole('button', { name: 'Launch player' }));

    await screen.findByRole('button', { name: /player\.closePlayer|Close Player/i });
    await screen.findByText('Journey Channel');

    await waitFor(() => {
      expect(findFetchCall(fetchMock, '/intents')).toBeDefined();
    }, { timeout: PLAYER_NETWORK_WAIT_MS });

    await waitFor(() => {
      expect(findFetchCall(fetchMock, '/sessions/sess-journey-1')).toBeDefined();
    }, { timeout: PLAYER_NETWORK_WAIT_MS });
  });

  it('routes a player 401 start intent to the session-expired auth surface', async () => {
    installPlayerFetchMock({
      intentStatus: 401,
      intentBody: {
        status: 401,
        title: 'Unauthorized',
        detail: 'Token expired',
        requestId: 'req-unauthorized',
      },
    });

    renderJourney(ROUTE_MAP.epg);

    await screen.findByText('EPG launcher ready');
    fireEvent.click(screen.getByRole('button', { name: 'Launch player' }));

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Session Expired' })).toBeInTheDocument();
      expect(screen.getByTestId('journey-pathname')).toHaveTextContent(ROUTE_MAP.epg);
    });
  });

  it('keeps a player 403 start intent local to the overlay', async () => {
    installPlayerFetchMock({
      intentStatus: 403,
      intentBody: {
        status: 403,
        title: 'Forbidden',
        detail: 'Missing stream scope',
        requestId: 'req-forbidden',
      },
    });

    renderJourney(ROUTE_MAP.epg);

    await screen.findByText('EPG launcher ready');
    fireEvent.click(screen.getByRole('button', { name: 'Launch player' }));

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent('Forbidden');
    });

    expect(screen.queryByRole('heading', { name: 'Session Expired' })).toBeNull();
    expect(screen.getByTestId('journey-pathname')).toHaveTextContent(ROUTE_MAP.epg);
  });

  it('surfaces session expiry locally when the session becomes unavailable after start intent creation', async () => {
    const fetchMock = installPlayerFetchMock({
      sessionStatus: 410,
      sessionBody: {
        status: 410,
        code: 'SESSION_GONE',
        reason: 'LEASE_EXPIRED',
        reason_detail: 'Lease expired',
        requestId: 'req-session-expired',
      },
    });

    renderJourney(ROUTE_MAP.epg);

    await screen.findByText('EPG launcher ready');
    fireEvent.click(screen.getByRole('button', { name: 'Launch player' }));

    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(/player\.sessionFailed|Session failed/i);
      expect(screen.getByRole('button', { name: /common\.retry|Retry/i })).toBeInTheDocument();
      expect(findFetchCall(fetchMock, '/sessions/sess-journey-1')).toBeDefined();
    });

    expect(screen.queryByRole('heading', { name: 'Session Expired' })).toBeNull();
  });
});
