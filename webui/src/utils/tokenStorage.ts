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

  const sessionToken = session?.getItem(TOKEN_KEY);
  if (sessionToken) {
    return sessionToken;
  }

  const legacyToken = local?.getItem(TOKEN_KEY);
  if (legacyToken) {
    session?.setItem(TOKEN_KEY, legacyToken);
    local?.removeItem(TOKEN_KEY);
    return legacyToken;
  }

  return '';
}

export function setStoredToken(token: string): void {
  const session = getStorage('session');
  const local = getStorage('local');

  if (token) {
    session?.setItem(TOKEN_KEY, token);
  } else {
    session?.removeItem(TOKEN_KEY);
  }
  local?.removeItem(TOKEN_KEY);
}

export function clearStoredToken(): void {
  const session = getStorage('session');
  const local = getStorage('local');

  session?.removeItem(TOKEN_KEY);
  local?.removeItem(TOKEN_KEY);
}
