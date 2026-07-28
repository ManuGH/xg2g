// Boot recovery: make the app resilient to two failure modes that produced a
// black screen on iOS Safari.
//
// 1) Basename mismatch. The app is served under a base path (Vite base, e.g.
//    "/ui/") and react-router uses that as its basename. If the document URL is
//    a bare path WITHOUT that prefix (e.g. "/epg" — from an old bookmark or a
//    stale service worker serving the shell), react-router refuses to render
//    ANYTHING ("<Router basename> is not able to match the URL ...") and #root
//    stays empty. We rewrite the URL to include the basename BEFORE the router
//    mounts, so such URLs render instead of going black.
//
// 2) Stale service worker. A service worker registered by an earlier version
//    (when the app was served at the site root) can hijack root-scope
//    navigations and serve a cached shell, which then mismatches the basename.
//    The app no longer uses a service worker, so we unregister any that remain
//    and drop their caches.

import { computeRouterBasename } from './AppRouter';

/**
 * Pure: returns the basename-prefixed URL when `pathname` is missing the
 * basename, or null when no rewrite is needed (already under basename, or no
 * basename configured).
 */
export function buildCanonicalPath(
  basename: string | undefined,
  pathname: string,
  search = '',
  hash = '',
): string | null {
  if (!basename) {
    return null;
  }
  if (pathname === basename || pathname.startsWith(`${basename}/`)) {
    return null;
  }
  const normalizedPath = pathname.startsWith('/') ? pathname : `/${pathname}`;
  return `${basename}${normalizedPath}${search}${hash}`;
}

/**
 * If the current path does not start with the router basename, rewrite it so it
 * does. Runs synchronously and must be called before the router mounts.
 */
export function ensureCanonicalBasePath(): void {
  if (typeof window === 'undefined') {
    return;
  }

  const basename = computeRouterBasename(import.meta.env.BASE_URL);
  const { pathname, search, hash } = window.location;
  const next = buildCanonicalPath(basename, pathname, search, hash);
  if (!next) {
    return;
  }

  try {
    window.history.replaceState(window.history.state, '', next);
  } catch {
    // Best effort: if history is unavailable the router still renders the
    // basename root rather than nothing.
  }
}

/**
 * Unregister any leftover service workers and clear their caches. The app does
 * not use a service worker; a stale one from an older build can break startup.
 * Fire-and-forget; never throws.
 */
export function cleanupStaleServiceWorkers(): void {
  if (typeof navigator === 'undefined') {
    return;
  }

  try {
    navigator.serviceWorker?.getRegistrations?.()
      .then((registrations) => {
        registrations.forEach((registration) => {
          void registration.unregister();
        });
      })
      .catch(() => {
        // best effort
      });
  } catch {
    // best effort
  }

  try {
    if (typeof caches !== 'undefined') {
      caches.keys()
        .then((keys) => {
          keys.forEach((key) => {
            void caches.delete(key);
          });
        })
        .catch(() => {
          // best effort
        });
    }
  } catch {
    // best effort
  }
}
