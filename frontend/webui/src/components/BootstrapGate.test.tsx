import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useQueryClient } from '@tanstack/react-query';
import { ClientRequestError } from '../services/clientWrapper';
import { requestAuthRequired } from '../features/player/sessionEvents';
import BootstrapGate, { BOOTSTRAP_GATE_BYPASS_ROUTES } from './BootstrapGate';
import { ROUTE_MAP, UNLOCK_ROUTE } from '../routes';

const mockUseAppContext = vi.fn();
const mockUseBootstrapConfig = vi.fn();
const mockResetQueries = vi.fn();


vi.mock('../context/AppContext', () => ({
  useAppContext: () => mockUseAppContext(),
}));

vi.mock('../hooks/useServerQueries', () => ({
  useBootstrapConfig: (enabled: boolean) => mockUseBootstrapConfig(enabled),
  queryKeys: {
    bootstrapConfig: ['v3', 'bootstrap', 'config'],
  },
}));

vi.mock('@tanstack/react-query', async () => {
  const actual = await vi.importActual<typeof import('@tanstack/react-query')>('@tanstack/react-query');
  return {
    ...actual,
    useQueryClient: vi.fn(),
  };
});

function renderGate(initialEntries: string[] = [ROUTE_MAP.epg]) {
  return render(
    <MemoryRouter initialEntries={initialEntries}>
      <Routes>
        <Route element={<BootstrapGate />}>
          <Route path={ROUTE_MAP.epg} element={<div>EPG view</div>} />
          <Route path={`${ROUTE_MAP.settings}/*`} element={<div>Settings view</div>} />
          <Route path={UNLOCK_ROUTE} element={<div>Unlock view</div>} />
        </Route>
      </Routes>
    </MemoryRouter>
  );
}

describe('BootstrapGate', () => {
  beforeEach(() => {
    vi.mocked(useQueryClient).mockReturnValue({
      resetQueries: mockResetQueries,
    } as unknown as ReturnType<typeof useQueryClient>);
    mockResetQueries.mockReset();
    mockUseAppContext.mockReturnValue({
      auth: { token: 'stored-token', isAuthenticated: true, isReady: true },
      setToken: vi.fn(),
      setPlayingChannel: vi.fn(),
    });
    mockUseBootstrapConfig.mockReturnValue({
      data: { openWebIF: { baseUrl: 'http://receiver.local' } },
      error: null,
      isLoading: false,
      refetch: vi.fn(),
    });
  });

  afterEach(() => {
    delete window.__XG2G_HOST__;
    vi.clearAllMocks();
  });

  it('shows the auth surface when no token is available', () => {
    mockUseAppContext.mockReturnValue({
      auth: { token: '', isAuthenticated: false, isReady: true },
      setToken: vi.fn(),
      setPlayingChannel: vi.fn(),
    });

    renderGate();

    expect(screen.getByRole('heading', { name: 'Authentication Required' })).toBeInTheDocument();
    expect(screen.getByText('Enter your API token to open the xg2g control surface.')).toBeInTheDocument();
    expect(screen.getByLabelText('API Token')).toHaveFocus();
    expect(mockUseBootstrapConfig).toHaveBeenCalledWith(false);
  });

  it('submits a trimmed token from the auth surface', () => {
    const setToken = vi.fn();

    mockUseAppContext.mockReturnValue({
      auth: { token: '', isAuthenticated: false, isReady: true },
      setToken,
      setPlayingChannel: vi.fn(),
    });

    renderGate();

    fireEvent.change(screen.getByLabelText('API Token'), {
      target: { value: '  new-token  ' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Authenticate' }));

    expect(mockResetQueries).toHaveBeenCalledWith({
      queryKey: ['v3', 'bootstrap', 'config'],
      exact: true,
    });
    expect(setToken).toHaveBeenCalledWith('new-token');
    expect(screen.getByLabelText('API Token')).toHaveValue('new-token');
  });

  it('clears stale bootstrap auth errors before re-authenticating with a new token', async () => {
    const setPlayingChannel = vi.fn();
    const unauthorized = new ClientRequestError({
      status: 401,
      code: 'UNAUTHORIZED',
      title: 'Authentication required',
      requestId: 'req-bootstrap-401',
    });
    const authState = { token: 'stale-token', isAuthenticated: true, isReady: true };
    const setToken = vi.fn((nextToken: string) => {
      authState.token = nextToken;
      authState.isAuthenticated = nextToken.length > 0;
      authState.isReady = nextToken.length === 0;
    });

    let bootstrapError: ClientRequestError | null = unauthorized;
    mockResetQueries.mockImplementation(() => {
      bootstrapError = null;
    });
    mockUseAppContext.mockImplementation(() => ({
      auth: { ...authState },
      setToken,
      setPlayingChannel,
    }));
    mockUseBootstrapConfig.mockImplementation(() => ({
      data: bootstrapError ? null : { openWebIF: { baseUrl: 'http://receiver.local' } },
      error: bootstrapError,
      isLoading: false,
      refetch: vi.fn(),
    }));

    const view = renderGate();

    await waitFor(() => {
      expect(setToken).toHaveBeenCalledWith('');
    });

    view.rerender(
      <MemoryRouter initialEntries={[ROUTE_MAP.epg]}>
        <Routes>
          <Route element={<BootstrapGate />}>
            <Route path={ROUTE_MAP.epg} element={<div>EPG view</div>} />
            <Route path={ROUTE_MAP.settings} element={<div>Settings view</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    );

    fireEvent.change(screen.getByLabelText('API Token'), {
      target: { value: 'dev-token' },
    });
    const callsBeforeSubmit = setToken.mock.calls.length;
    fireEvent.click(screen.getByRole('button', { name: 'Authenticate' }));

    view.rerender(
      <MemoryRouter initialEntries={[ROUTE_MAP.epg]}>
        <Routes>
          <Route element={<BootstrapGate />}>
            <Route path={ROUTE_MAP.epg} element={<div>EPG view</div>} />
            <Route path={ROUTE_MAP.settings} element={<div>Settings view</div>} />
          </Route>
        </Routes>
      </MemoryRouter>
    );

    expect(mockResetQueries).toHaveBeenCalledWith({
      queryKey: ['v3', 'bootstrap', 'config'],
      exact: true,
    });
    expect(setToken.mock.calls.slice(callsBeforeSubmit).map(([token]) => token)).toEqual(['dev-token']);
    expect(setPlayingChannel).toHaveBeenCalledWith(null);
  });

  it('redirects unconfigured routes to settings', async () => {
    mockUseBootstrapConfig.mockReturnValue({
      data: { openWebIF: { baseUrl: '' } },
      error: null,
      isLoading: false,
      refetch: vi.fn(),
    });

    renderGate([ROUTE_MAP.epg]);

    expect(await screen.findByText('Settings view')).toBeInTheDocument();
  });

  it('shows the gate skeleton while bootstrap validation is loading', () => {
    mockUseBootstrapConfig.mockReturnValue({
      data: null,
      error: null,
      isLoading: true,
      refetch: vi.fn(),
    });

    renderGate();

    expect(screen.getByRole('status', { name: 'Initializing...' })).toHaveAttribute('data-loading-variant', 'gate');
  });

  it('allows the settings route when the system is unconfigured', async () => {
    mockUseBootstrapConfig.mockReturnValue({
      data: { openWebIF: { baseUrl: '' } },
      error: null,
      isLoading: false,
      refetch: vi.fn(),
    });

    renderGate([ROUTE_MAP.settings]);

    expect(await screen.findByText('Settings view')).toBeInTheDocument();
  });

  it('blocks configured app routes when monetization enforcement requires an unlock', () => {
    mockUseBootstrapConfig.mockReturnValue({
      data: {
        openWebIF: { baseUrl: 'http://receiver.local' },
        monetization: {
          enabled: true,
          model: 'one_time_unlock',
          productName: 'xg2g Unlock',
          purchaseUrl: 'https://example.com/unlock',
          enforcement: 'required',
          unlocked: false,
        },
      },
      error: null,
      isLoading: false,
      refetch: vi.fn(),
    });

    renderGate([ROUTE_MAP.epg]);

    expect(screen.getByRole('heading', { name: 'xg2g Unlock required' })).toBeInTheDocument();
    expect(screen.queryByText('EPG view')).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Open Unlock Info' })).toHaveAttribute('href', 'https://example.com/unlock');
  });

  it('still allows the settings route while the unlock gate is active', async () => {
    mockUseBootstrapConfig.mockReturnValue({
      data: {
        openWebIF: { baseUrl: 'http://receiver.local' },
        monetization: {
          enabled: true,
          model: 'one_time_unlock',
          productName: 'xg2g Unlock',
          enforcement: 'required',
          unlocked: false,
        },
      },
      error: null,
      isLoading: false,
      refetch: vi.fn(),
    });

    renderGate([ROUTE_MAP.settings]);

    expect(await screen.findByText('Settings view')).toBeInTheDocument();
  });

  it('allows configured app routes once the unlock status is active', async () => {
    mockUseBootstrapConfig.mockReturnValue({
      data: {
        openWebIF: { baseUrl: 'http://receiver.local' },
        monetization: {
          enabled: true,
          model: 'one_time_unlock',
          productName: 'xg2g Unlock',
          enforcement: 'required',
          unlocked: true,
        },
      },
      error: null,
      isLoading: false,
      refetch: vi.fn(),
    });

    renderGate([ROUTE_MAP.epg]);

    expect(await screen.findByText('EPG view')).toBeInTheDocument();
  });

  it('allows nested bypass routes while the unlock gate is active', async () => {
    mockUseBootstrapConfig.mockReturnValue({
      data: {
        openWebIF: { baseUrl: 'http://receiver.local' },
        monetization: {
          enabled: true,
          model: 'one_time_unlock',
          productName: 'xg2g Unlock',
          enforcement: 'required',
          unlocked: false,
        },
      },
      error: null,
      isLoading: false,
      refetch: vi.fn(),
    });

    renderGate([`${BOOTSTRAP_GATE_BYPASS_ROUTES[0]}/network`]);

    expect(await screen.findByText('Settings view')).toBeInTheDocument();
  });

  it('allows the unlock status route while the unlock gate is active', async () => {
    mockUseBootstrapConfig.mockReturnValue({
      data: {
        openWebIF: { baseUrl: 'http://receiver.local' },
        monetization: {
          enabled: true,
          model: 'one_time_unlock',
          productName: 'xg2g Unlock',
          enforcement: 'required',
          unlocked: false,
        },
      },
      error: null,
      isLoading: false,
      refetch: vi.fn(),
    });

    renderGate([UNLOCK_ROUTE]);

    expect(await screen.findByText('Unlock view')).toBeInTheDocument();
  });

  it('converts bootstrap 401 responses into an auth prompt', async () => {
    const setToken = vi.fn();
    const setPlayingChannel = vi.fn();

    mockUseAppContext.mockReturnValue({
      auth: { token: 'stale-token', isAuthenticated: true, isReady: true },
      setToken,
      setPlayingChannel,
    });
    mockUseBootstrapConfig.mockReturnValue({
      data: null,
      error: new ClientRequestError({
        status: 401,
        code: 'UNAUTHORIZED',
        title: 'Authentication required',
        requestId: 'req-bootstrap-401',
      }),
      isLoading: false,
      refetch: vi.fn(),
    });

    renderGate();

    expect(screen.getByRole('heading', { name: 'Session Expired' })).toBeInTheDocument();
    expect(
      screen.getByText('Your saved API token was rejected. Enter a valid token to continue.')
    ).toBeInTheDocument();
    await waitFor(() => {
      expect(setToken).toHaveBeenCalledWith('');
      expect(setPlayingChannel).toHaveBeenCalledWith(null);
    });
  });

  it('handles runtime auth-required events through the gate', async () => {
    const setToken = vi.fn();
    const setPlayingChannel = vi.fn();

    mockUseAppContext.mockReturnValue({
      auth: { token: 'live-token', isAuthenticated: true, isReady: true },
      setToken,
      setPlayingChannel,
    });

    renderGate();
    act(() => {
      requestAuthRequired({ source: 'runtime-test', status: 401, code: 'AUTH_REQUIRED' });
    });

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Session Expired' })).toBeInTheDocument();
      expect(setToken).toHaveBeenCalledWith('');
      expect(setPlayingChannel).toHaveBeenCalledWith(null);
    });
    expect(screen.getByLabelText('API Token')).toHaveValue('live-token');
  });

  it('shows the token in clear text on TV hosts and supports clearing it', () => {
    window.__XG2G_HOST__ = {
      platform: 'android-tv',
      isTv: true,
      supportsKeepScreenAwake: false,
      supportsHostMediaKeys: true,
      supportsInputFocus: true,
      supportsNativePlayback: false,
    };

    mockUseAppContext.mockReturnValue({
      auth: { token: '', isAuthenticated: false, isReady: true },
      setToken: vi.fn(),
      setPlayingChannel: vi.fn(),
    });

    renderGate();

    const input = screen.getByLabelText('API Token') as HTMLInputElement;
    expect(input.type).toBe('text');
    expect(screen.getByRole('button', { name: 'Hide token' })).toBeInTheDocument();

    fireEvent.change(input, { target: { value: 'test01' } });
    fireEvent.click(screen.getByRole('button', { name: 'Clear' }));

    expect(input).toHaveValue('');
  });

  it('shows a retryable recovery surface for bootstrap errors', async () => {
    const refetch = vi.fn();

    mockUseBootstrapConfig.mockReturnValue({
      data: null,
      error: new ClientRequestError({
        status: 503,
        title: 'Service unavailable',
        detail: 'Bootstrap backend offline',
        requestId: 'req-bootstrap-503',
      }),
      isLoading: false,
      refetch,
    });

    renderGate();

    expect(screen.getByRole('heading', { name: 'Unable to start xg2g' })).toBeInTheDocument();
    expect(screen.getByText('Bootstrap backend offline')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it('waits for auth header synchronization before starting bootstrap validation', () => {
    mockUseAppContext.mockReturnValue({
      auth: { token: 'stored-token', isAuthenticated: true, isReady: false },
      setToken: vi.fn(),
      setPlayingChannel: vi.fn(),
    });
    mockUseBootstrapConfig.mockReturnValue({
      data: null,
      error: null,
      isLoading: false,
      refetch: vi.fn(),
    });

    renderGate();

    expect(screen.getByRole('status', { name: 'Initializing...' })).toHaveAttribute('data-loading-variant', 'gate');
    expect(mockUseBootstrapConfig).toHaveBeenCalledWith(false);
  });
});
