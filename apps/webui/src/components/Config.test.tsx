import { render, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

const {
  getSystemConfig,
  putSystemConfig,
  authState,
} = vi.hoisted(() => ({
  getSystemConfig: vi.fn(),
  putSystemConfig: vi.fn(),
  authState: {
    token: 'stored-token',
    isAuthenticated: true,
    isReady: true,
  },
}));

vi.mock('../client-ts', () => ({
  getSystemConfig,
  putSystemConfig,
}));

vi.mock('../context/AppContext', () => ({
  useAppContext: () => ({
    auth: authState,
  }),
}));

import Config from './Config';

describe('Config', () => {
  afterEach(() => {
    vi.clearAllMocks();
    authState.token = 'stored-token';
    authState.isAuthenticated = true;
    authState.isReady = true;
  });

  it('waits for auth header hydration before loading config', () => {
    authState.isReady = false;

    render(<Config />);

    expect(getSystemConfig).not.toHaveBeenCalled();
  });

  it('loads config once auth is ready and authenticated', async () => {
    getSystemConfig.mockResolvedValue({
      data: {
        openWebIF: { baseUrl: 'http://receiver.local' },
        bouquets: [],
      },
    });

    render(<Config />);

    await waitFor(() => {
      expect(getSystemConfig).toHaveBeenCalledTimes(1);
    });
  });
});
