import { render, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AppProvider } from './AppContext';

const { setClientAuthToken } = vi.hoisted(() => ({
  setClientAuthToken: vi.fn()
}));

vi.mock('../lib/clientWrapper', () => ({
  setClientAuthToken
}));

vi.mock('../utils/tokenStorage', () => ({
  clearStoredToken: vi.fn(),
  getStoredToken: vi.fn(() => 'stored-token'),
  setStoredToken: vi.fn()
}));

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
});
