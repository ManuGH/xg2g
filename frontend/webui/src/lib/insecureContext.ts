// Pure detection for "is the web app running in an insecure browsing context".
//
// The xg2g web player depends on secure-context-gated browser behaviour and on
// authenticated media that should never traverse cleartext, so it only works
// over HTTPS — or over http://localhost, which browsers treat as a secure
// context. Accessing the app over plain HTTP on a LAN IP/hostname (the common
// self-host mistake) leaves playback broken with no obvious reason. This decides
// whether to surface a one-glance hint. Kept pure (no window) so it is
// unit-tested; the component reads the live values from window.

export interface InsecureContextInput {
  /** window.isSecureContext when the browser exposes it (authoritative). */
  isSecureContext: boolean | undefined;
  /** window.location.protocol, e.g. 'https:' or 'http:' (fallback signal). */
  protocol: string;
  /** window.location.hostname. */
  hostname: string;
}

const LOCAL_HOSTNAMES = new Set(['localhost', '127.0.0.1', '::1', '[::1]', '']);

function isLocalHostname(hostname: string): boolean {
  return LOCAL_HOSTNAMES.has(hostname) || hostname.endsWith('.localhost');
}

export function shouldWarnInsecureContext(input: InsecureContextInput): boolean {
  // localhost is a secure context even over http — never warn there.
  if (isLocalHostname(input.hostname)) {
    return false;
  }
  // The browser's own verdict is authoritative when present: isSecureContext is
  // false exactly when the secure-context-gated features playback relies on are
  // unavailable.
  if (typeof input.isSecureContext === 'boolean') {
    return !input.isSecureContext;
  }
  // Fallback for runtimes without isSecureContext: a non-local host not on https.
  return input.protocol !== 'https:';
}

/** Reads the live browser values; safe to call when window is absent (SSR/tests). */
export function detectInsecureContext(): boolean {
  if (typeof window === 'undefined' || !window.location) {
    return false;
  }
  return shouldWarnInsecureContext({
    isSecureContext: typeof window.isSecureContext === 'boolean' ? window.isSecureContext : undefined,
    protocol: window.location.protocol,
    hostname: window.location.hostname,
  });
}
