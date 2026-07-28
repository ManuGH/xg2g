import { afterEach, describe, expect, it, vi } from 'vitest';

import { reloadWindowLocation } from './browserNavigation';
import { setStoredToken } from '../utils/tokenStorage';

vi.mock('../utils/tokenStorage', () => ({
  setStoredToken: vi.fn(),
}));

const originalLocation = window.location;

describe('browserNavigation', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.mocked(setStoredToken).mockClear();
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: originalLocation,
    });
    window.history.replaceState({}, '', '/ui/');
  });

  it('persists the token and forces a hard reload for auth bootstrap', () => {
    const reload = vi.fn();
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: {
        ...originalLocation,
        pathname: '/ui/epg',
        search: '?foo=1',
        hash: '#xg2g_boot_token=stale',
        reload,
      },
    });
    const replaceStateSpy = vi.spyOn(window.history, 'replaceState');

    reloadWindowLocation('  new-token  ');

    expect(setStoredToken).toHaveBeenCalledWith('new-token');
    expect(replaceStateSpy).toHaveBeenCalledWith(window.history.state, document.title, '/ui/epg?foo=1');
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it('reloads in place when no token override is supplied', () => {
    const reload = vi.fn();
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: {
        ...originalLocation,
        reload,
      },
    });
    const replaceStateSpy = vi.spyOn(window.history, 'replaceState');

    reloadWindowLocation();

    expect(setStoredToken).not.toHaveBeenCalled();
    expect(replaceStateSpy).not.toHaveBeenCalled();
    expect(reload).toHaveBeenCalledTimes(1);
  });
});
