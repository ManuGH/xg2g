const TOKEN_KEY = 'XG2G_API_TOKEN';

function getStorage(storageType: 'session' | 'local'): Storage | null {
  if (typeof window === 'undefined') {
    return null;
  }
  try {
    return storageType === 'session' ? window.sessionStorage : window.localStorage;
  } catch (_err) {
    return null;
  }
}

export function getStoredToken(): string {
  const session = getStorage('session');
  const local = getStorage('local');

  const localToken = local?.getItem(TOKEN_KEY);
  if (localToken) {
    return localToken;
  }

  const legacySessionToken = session?.getItem(TOKEN_KEY);
  if (legacySessionToken) {
    local?.setItem(TOKEN_KEY, legacySessionToken);
    session?.removeItem(TOKEN_KEY);
    return legacySessionToken;
  }

  return '';
}

export function setStoredToken(token: string): void {
  const session = getStorage('session');
  const local = getStorage('local');

  if (token) {
    local?.setItem(TOKEN_KEY, token);
  } else {
    local?.removeItem(TOKEN_KEY);
  }
  session?.removeItem(TOKEN_KEY);
}

export function clearStoredToken(): void {
  const session = getStorage('session');
  const local = getStorage('local');

  session?.removeItem(TOKEN_KEY);
  local?.removeItem(TOKEN_KEY);
}
