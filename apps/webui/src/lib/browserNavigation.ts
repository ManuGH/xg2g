import { setStoredToken } from '../utils/tokenStorage';

export function reloadWindowLocation(token?: string): void {
  if (typeof window !== 'undefined') {
    const normalizedToken = String(token || '').trim();
    if (normalizedToken) {
      setStoredToken(normalizedToken);
      const cleanUrl = `${window.location.pathname}${window.location.search}`;
      window.history.replaceState(window.history.state, document.title, cleanUrl);
    }
    window.location.reload();
  }
}
