import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ClientRequestError } from '../lib/clientWrapper';
import { requestAuthRequired } from '../lib/sessionEvents';
import BootstrapGate from './BootstrapGate';
import { ROUTE_MAP } from '../routes';

const mockUseAppContext = vi.fn();
const mockUseBootstrapConfig = vi.fn();


vi.mock('../context/AppContext', () => ({
  useAppContext: () => mockUseAppContext(),
}));

vi.mock('../hooks/useServerQueries', () => ({
  useBootstrapConfig: (enabled: boolean) => mockUseBootstrapConfig(enabled),
}));

function renderGate(initialEntries: string[] = [ROUTE_MAP.epg]) {
  return render(
    <MemoryRouter initialEntries={initialEntries}>
      <Routes>
        <Route element={<BootstrapGate />}>
          <Route path={ROUTE_MAP.epg} element={<div>EPG view</div>} />
          <Route path={ROUTE_MAP.settings} element={<div>Settings view</div>} />
        </Route>
      </Routes>
    </MemoryRouter>
  );
}

describe('BootstrapGate', () => {
  beforeEach(() => {
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

    expect(setToken).toHaveBeenCalledWith('new-token');
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
