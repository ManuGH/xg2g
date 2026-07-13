import { lazy, type ComponentType } from 'react';

// React.lazy whose dynamic import is hardened against two production failure
// modes seen on this app:
//   1. A hashed chunk 404s after a deploy (the old hash is gone) — common for
//      tabs left open across a release.
//   2. iOS Safari can leave an in-flight import() pending indefinitely.
// Each attempt races the import against a timeout; transient failures retry,
// and as a last resort a single full reload fetches a fresh chunk manifest.
// The reload is guarded by a session flag so it can never loop.

const RELOAD_FLAG = 'xg2g_chunk_reload';

export interface RetryOptions {
  retries?: number;
  timeoutMs?: number;
}

export function importWithRetry<T>(
  factory: () => Promise<T>,
  { retries = 2, timeoutMs = 12000 }: RetryOptions = {},
): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    let attempt = 0;

    const fail = (err: unknown) => {
      try {
        if (typeof window !== 'undefined' && !window.sessionStorage.getItem(RELOAD_FLAG)) {
          window.sessionStorage.setItem(RELOAD_FLAG, '1');
          window.location.reload();
          return;
        }
      } catch {
        // sessionStorage/reload unavailable — fall through to reject
      }
      reject(err instanceof Error ? err : new Error(String(err)));
    };

    const run = () => {
      attempt += 1;
      let settled = false;
      const clearTimer = typeof window !== 'undefined'
        ? ((id: number) => window.clearTimeout(id))
        : () => {};
      const timer = typeof window !== 'undefined'
        ? window.setTimeout(() => {
            if (settled) return;
            settled = true;
            if (attempt <= retries) run();
            else fail(new Error('chunk import timed out'));
          }, timeoutMs)
        : undefined;

      factory().then(
        (mod) => {
          if (settled) return;
          settled = true;
          if (timer !== undefined) clearTimer(timer);
          resolve(mod);
        },
        (err) => {
          if (settled) return;
          settled = true;
          if (timer !== undefined) clearTimer(timer);
          if (attempt <= retries) run();
          else fail(err);
        },
      );
    };

    run();
  });
}

export function lazyWithRetry<T extends ComponentType<any>>(
  factory: () => Promise<{ default: T }>,
  options?: RetryOptions,
) {
  return lazy(() => importWithRetry(factory, options));
}
