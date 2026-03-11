import { afterEach, describe, expect, it } from 'vitest';
import { clearStoredToken, getStoredToken, setStoredToken } from './tokenStorage';

describe('tokenStorage', () => {
  afterEach(() => {
    window.localStorage.clear();
    window.sessionStorage.clear();
  });

  it('persists the API token in localStorage', () => {
    setStoredToken('test01');

    expect(window.localStorage.getItem('XG2G_API_TOKEN')).toBe('test01');
    expect(window.sessionStorage.getItem('XG2G_API_TOKEN')).toBeNull();
    expect(getStoredToken()).toBe('test01');
  });

  it('migrates legacy sessionStorage tokens to localStorage', () => {
    window.sessionStorage.setItem('XG2G_API_TOKEN', 'legacy-token');

    expect(getStoredToken()).toBe('legacy-token');
    expect(window.localStorage.getItem('XG2G_API_TOKEN')).toBe('legacy-token');
    expect(window.sessionStorage.getItem('XG2G_API_TOKEN')).toBeNull();
  });

  it('clears the token from both storages', () => {
    window.localStorage.setItem('XG2G_API_TOKEN', 'persisted');
    window.sessionStorage.setItem('XG2G_API_TOKEN', 'stale-session');

    clearStoredToken();

    expect(window.localStorage.getItem('XG2G_API_TOKEN')).toBeNull();
    expect(window.sessionStorage.getItem('XG2G_API_TOKEN')).toBeNull();
  });
});
