import { render, screen, waitFor } from '@testing-library/react';
import { useEffect } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AppProvider, useAppContext } from './AppContext';

const {
  setClientAuthToken,
  getServices,
  getServicesBouquets,
  getSystemConfig,
} = vi.hoisted(() => ({
  setClientAuthToken: vi.fn(),
  getServices: vi.fn(),
  getServicesBouquets: vi.fn(),
  getSystemConfig: vi.fn(),
}));

vi.mock('../lib/clientWrapper', async () => {
  const actual = await vi.importActual<typeof import('../lib/clientWrapper')>('../lib/clientWrapper');
  return {
    ...actual,
    setClientAuthToken,
  };
});

vi.mock('../client-ts', () => ({
  getServices,
  getServicesBouquets,
  getSystemConfig,
}));

vi.mock('../utils/tokenStorage', () => ({
  clearStoredToken: vi.fn(),
  getStoredToken: vi.fn(() => 'stored-token'),
  setStoredToken: vi.fn()
}));

function ConfigLoadProbe() {
  const { checkConfigAndLoad, showAuth, view } = useAppContext();

  useEffect(() => {
    void checkConfigAndLoad();
  }, [checkConfigAndLoad]);

  return <div data-testid="config-load-state">{showAuth ? 'auth' : 'no-auth'}:{view}</div>;
}

function ChannelLoadProbe() {
  const { loadChannels, showAuth } = useAppContext();

  useEffect(() => {
    void loadChannels('Premium');
  }, [loadChannels]);

  return <div data-testid="channel-load-state">{showAuth ? 'auth' : 'no-auth'}</div>;
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

    render(
      <AppProvider>
        <RenderProbe />
      </AppProvider>
    );

    expect(callsDuringRender).toBe(0);
    await waitFor(() => {
      expect(setClientAuthToken).toHaveBeenCalledWith('stored-token');
    });
  });

  it('shows auth instead of switching to setup when config loading gets a 401 response', async () => {
    (getSystemConfig as any).mockResolvedValue({
      data: undefined,
      error: {
        status: 401,
        code: 'UNAUTHORIZED',
        title: 'Authentication required',
        requestId: 'req-401',
      },
      response: { status: 401 },
    });

    render(
      <AppProvider>
        <ConfigLoadProbe />
      </AppProvider>
    );

    await waitFor(() => {
      expect(screen.getByTestId('config-load-state')).toHaveTextContent('auth:epg');
    });
  });

  it('shows auth when channel loading returns a 401 response', async () => {
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

    render(
      <AppProvider>
        <ChannelLoadProbe />
      </AppProvider>
    );

    await waitFor(() => {
      expect(screen.getByTestId('channel-load-state')).toHaveTextContent('auth');
    });
  });
});
