// Storage access that never throws.
//
// `window.localStorage` is not always reachable: iOS Safari with "Block All
// Cookies" enabled, private/strict privacy modes, and some embedded webviews
// make the getter itself throw a SecurityError, and a full quota makes
// setItem throw QuotaExceededError. When such a throw happens synchronously
// during module load or render — before the React tree (and its
// ErrorBoundary) mounts — the whole app fails to boot and the user sees a
// blank/black page. Route every boot- and render-critical storage access
// through these helpers so storage is always best-effort and the UI mounts
// regardless.

function resolveLocalStorage(): Storage | null {
  if (typeof window === 'undefined') {
    return null;
  }

  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

/**
 * The localStorage object, or null when it is unavailable/blocked. Never
 * throws. Use only when you need the object itself (e.g. identity comparison
 * against `StorageEvent.storageArea`); prefer the item helpers otherwise.
 */
export function safeLocalStorage(): Storage | null {
  return resolveLocalStorage();
}

/** Reads a key, returning null when storage is unavailable or the read throws. */
export function readLocalStorageItem(key: string): string | null {
  const storage = resolveLocalStorage();
  if (!storage) {
    return null;
  }

  try {
    return storage.getItem(key);
  } catch {
    return null;
  }
}

/** Writes a key best-effort, swallowing unavailable-storage and quota errors. */
export function writeLocalStorageItem(key: string, value: string): void {
  const storage = resolveLocalStorage();
  if (!storage) {
    return;
  }

  try {
    storage.setItem(key, value);
  } catch {
    // Best effort: storage blocked or quota exceeded.
  }
}

/** Removes a key best-effort, swallowing unavailable-storage errors. */
export function removeLocalStorageItem(key: string): void {
  const storage = resolveLocalStorage();
  if (!storage) {
    return;
  }

  try {
    storage.removeItem(key);
  } catch {
    // Best effort: storage blocked.
  }
}
