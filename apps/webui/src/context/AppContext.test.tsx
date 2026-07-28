import { render, screen, waitFor } from '@testing-library/react';
import type { ReactNode } from 'react';
import { useEffect } from 'react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AppProvider, useAppContext } from './AppContext';

const {
  setClientAuthToken,
  getServices,
  getServicesBouquets,
} = vi.hoisted(() => ({
  setClientAuthToken: vi.fn(),
  getServices: vi.fn(),
  getServicesBouquets: vi.fn(),
}));

vi.mock('../services/clientWrapper', async () => {
  const actual = await vi.importActual<typeof import('../services/clientWrapper')>('../services/clientWrapper');
  return {
    ...actual,
    setClientAuthToken,
  };
});

vi.mock('../client-ts', () => ({
  getServices,
  getServicesBouquets,
}));

vi.mock('../utils/tokenStorage', () => ({
  clearStoredToken: vi.fn(),
  getStoredToken: vi.fn(() => 'stored-token'),
  setStoredToken: vi.fn()
}));

function ChannelLoadProbe() {
  const { loadChannels } = useAppContext();

  useEffect(() => {
    void loadChannels('Premium');
  }, [loadChannels]);

  return <div data-testid="channel-load-state">ready</div>;
}

function HydrationProbe() {
  const { channels, dataLoaded, loadBouquetsAndChannels } = useAppContext();

  useEffect(() => {
    void loadBouquetsAndChannels();
  }, [loadBouquetsAndChannels]);

  return (
    <div data-testid="hydration-state">
      {channels.bouquets.length}:{channels.channels.length}:{dataLoaded ? 'loaded' : 'idle'}
    </div>
  );
}

function ResetOnLogoutProbe() {
  const { channels, dataLoaded, loadBouquetsAndChannels, setToken } = useAppContext();

  useEffect(() => {
    void loadBouquetsAndChannels();
  }, [loadBouquetsAndChannels]);

  useEffect(() => {
    if (dataLoaded) {
      setToken('');
    }
  }, [dataLoaded, setToken]);

  return (
    <div data-testid="reset-state">
      {channels.bouquets.length}:{channels.channels.length}:{dataLoaded ? 'loaded' : 'idle'}
    </div>
  );
}

function renderWithRouter(children: ReactNode) {
  return render(
    <MemoryRouter initialEntries={['/epg']}>
      <AppProvider>
        {children}
      </AppProvider>
    </MemoryRouter>
  );
}

describe('AppProvider', () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it('does not initialize auth token during render', async () => {
    let callsDuringRender = -1;

    function RenderProbe() {
      callsDuringRender = setClientAuthToken.mock.calls.length;
      return <div>probe</div>;
    }

    renderWithRouter(<RenderProbe />);

    expect(callsDuringRender).toBe(0);
    await waitFor(() => {
      expect(setClientAuthToken).toHaveBeenCalledWith('stored-token');
    });
  });

  it('dispatches auth-required when channel loading returns a 401 response', async () => {
    const authRequired = vi.fn();
    window.addEventListener('auth-required', authRequired);

    (getServices as any).mockResolvedValue({
      data: undefined,
      error: {
        status: 401,
        code: 'UNAUTHORIZED',
        title: 'Authentication required',
        requestId: 'req-chan-401',
      },
      response: { status: 401 },
    });

    renderWithRouter(<ChannelLoadProbe />);

    await waitFor(() => {
      expect(authRequired).toHaveBeenCalledTimes(1);
      expect(screen.getByTestId('channel-load-state')).toHaveTextContent('ready');
    });

    window.removeEventListener('auth-required', authRequired);
  });

  it('hydrates bouquets and channels without going through the bootstrap gate', async () => {
    (getServicesBouquets as any).mockResolvedValue({
      data: [{ name: 'Favorites', services: 2 }],
      error: undefined,
      response: { status: 200 },
    });
    (getServices as any).mockResolvedValue({
      data: [{ servicereference: '1:0:1', name: 'Das Erste HD' }],
      error: undefined,
      response: { status: 200 },
    });

    renderWithRouter(<HydrationProbe />);

    await waitFor(() => {
      expect(screen.getByTestId('hydration-state')).toHaveTextContent('1:1:loaded');
    });
  });

  it('clears hydrated channel state when the token is removed', async () => {
    (getServicesBouquets as any).mockResolvedValue({
      data: [{ name: 'Favorites', services: 2 }],
      error: undefined,
      response: { status: 200 },
    });
    (getServices as any).mockResolvedValue({
      data: [{ servicereference: '1:0:1', name: 'Das Erste HD' }],
      error: undefined,
      response: { status: 200 },
    });

    renderWithRouter(<ResetOnLogoutProbe />);

    await waitFor(() => {
      expect(screen.getByTestId('reset-state')).toHaveTextContent('1:1:loaded');
    });
    await waitFor(() => {
      expect(screen.getByTestId('reset-state')).toHaveTextContent('0:0:idle');
    });
    await waitFor(() => {
      expect(setClientAuthToken).toHaveBeenCalledWith('');
    });
  });
});
