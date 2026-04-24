const TOKEN_KEY = 'XG2G_API_TOKEN';
const BOOT_TOKEN_HASH_KEY = 'xg2g_boot_token';
let volatileToken = '';
let bootTokenConsumed = false;

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

function readBootTokenFromLocation(): string {
  if (bootTokenConsumed || typeof window === 'undefined') {
    return '';
  }
  bootTokenConsumed = true;

  const rawHash = window.location.hash.startsWith('#')
    ? window.location.hash.slice(1)
    : window.location.hash;
  if (!rawHash) {
    return '';
  }

  const hashParams = new URLSearchParams(rawHash);
  const bootToken = String(hashParams.get(BOOT_TOKEN_HASH_KEY) || '').trim();
  if (!bootToken) {
    return '';
  }

  hashParams.delete(BOOT_TOKEN_HASH_KEY);
  const nextHash = hashParams.toString();
  const nextUrl = `${window.location.pathname}${window.location.search}${nextHash ? `#${nextHash}` : ''}`;
  window.history.replaceState(window.history.state, document.title, nextUrl);

  return bootToken;
}

function persistTokenBestEffort(token: string): void {
  const session = getStorage('session');
  const local = getStorage('local');

  if (token) {
    local?.setItem(TOKEN_KEY, token);
  } else {
    local?.removeItem(TOKEN_KEY);
  }
  session?.removeItem(TOKEN_KEY);
}

export function getStoredToken(): string {
  const bootToken = readBootTokenFromLocation();
  if (bootToken) {
    volatileToken = bootToken;
    persistTokenBestEffort(bootToken);
    return bootToken;
  }

  const session = getStorage('session');
  const local = getStorage('local');

  const localToken = local?.getItem(TOKEN_KEY);
  if (localToken) {
    volatileToken = localToken;
    return localToken;
  }

  const legacySessionToken = session?.getItem(TOKEN_KEY);
  if (legacySessionToken) {
    volatileToken = legacySessionToken;
    persistTokenBestEffort(legacySessionToken);
    return legacySessionToken;
  }

  return volatileToken;
}

export function setStoredToken(token: string): void {
  volatileToken = token;
  persistTokenBestEffort(token);
}

export function clearStoredToken(): void {
  bootTokenConsumed = false;
  volatileToken = '';
  persistTokenBestEffort('');
}
