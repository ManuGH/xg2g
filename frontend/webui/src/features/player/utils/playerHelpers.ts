/**
 * Player Helper Utilities
 *
 * Extracted from V3Player.tsx for testability and reuse.
 * Contains error types, response parsing, token inspection, and
 * browser capability detection helpers.
 */

import type { VideoElementRef } from '../../../types/v3-player';

// --- Error Type ---

export class PlayerError extends Error {
  details?: unknown;
  status?: number;
  code?: string;
  requestId?: string;

  constructor(message: string, details?: unknown) {
    super(message);
    this.name = 'PlayerError';
    this.details = details;
    if (details && typeof details === 'object') {
      const record = details as Record<string, unknown>;
      if (typeof record.status === 'number') {
        this.status = record.status;
      }
      if (typeof record.code === 'string') {
        this.code = record.code;
      }
      if (typeof record.requestId === 'string') {
        this.requestId = record.requestId;
      }
    }
    Object.setPrototypeOf(this, PlayerError.prototype);
  }
}

// --- Response Parsing ---

export async function readResponseBody(res: Response): Promise<{ json: any | null; text: string | null }> {
  const maybeRes = res as unknown as { text?: () => Promise<string>; json?: () => Promise<any> };
  try {
    if (typeof maybeRes.text === 'function') {
      const text = await maybeRes.text();
      if (!text) return { json: null, text: '' };
      try {
        return { json: JSON.parse(text), text };
      } catch {
        return { json: null, text };
      }
    }
  } catch {
    // fall through to json fallback
  }

  try {
    if (typeof maybeRes.json === 'function') {
      const json = await maybeRes.json();
      return {
        json,
        text: json == null ? '' : JSON.stringify(json)
      };
    }
  } catch {
    // ignore and fall through
  }
  return { json: null, text: null };
}

// --- Token Inspection ---

export function extractCapHashFromDecisionToken(token: string): string | null {
  try {
    const parts = token.split('.');
    if (parts.length < 2) return null;
    const payloadB64 = parts[1];
    if (!payloadB64) return null;
    const normalized = payloadB64.replace(/-/g, '+').replace(/_/g, '/');
    const padded = normalized + '='.repeat((4 - (normalized.length % 4)) % 4);
    const payload = JSON.parse(atob(padded));
    if (payload && typeof payload.capHash === 'string' && payload.capHash) {
      return payload.capHash;
    }
  } catch {
    // Decision token parsing is best-effort; server enforces token validity.
  }
  return null;
}

// --- Browser Capability Detection ---

export function hasTouchInput(): boolean {
  try {
    return typeof navigator !== 'undefined' && Number(navigator.maxTouchPoints || 0) > 0;
  } catch {
    return false;
  }
}

export function canUseDesktopWebKitFullscreen(videoEl?: VideoElementRef): boolean {
  if (!videoEl) return false;
  try {
    const webkitVideo = videoEl as unknown as {
      webkitEnterFullscreen?: unknown;
    };
    return typeof webkitVideo.webkitEnterFullscreen === 'function' && !hasTouchInput();
  } catch {
    return false;
  }
}

export function shouldForceNativeMobileHls(videoEl?: VideoElementRef): boolean {
  if (!videoEl) return false;
  try {
    const hasNativeHls = videoEl.canPlayType('application/vnd.apple.mpegurl') !== '';
    if (!hasNativeHls) return false;

    // Feature detection for mobile WebKit controls (no UA sniffing).
    const webkitVideo = videoEl as unknown as {
      webkitEnterFullscreen?: unknown;
      webkitSupportsPresentationMode?: unknown;
      webkitSetPresentationMode?: unknown;
    };
    const hasWebKitFullscreen = typeof webkitVideo.webkitEnterFullscreen === 'function';
    const hasWebKitPresentationMode =
      typeof webkitVideo.webkitSupportsPresentationMode === 'function' ||
      typeof webkitVideo.webkitSetPresentationMode === 'function';

    // Desktop Safari can expose some WebKit fullscreen APIs.
    // Restrict native-mobile forcing to touch devices to keep desktop behavior stable.
    return (hasWebKitFullscreen || hasWebKitPresentationMode) && hasTouchInput();
  } catch {
    return false;
  }
}

export function shouldPreferNativeWebKitHls(videoEl?: VideoElementRef, hlsJsSupported: boolean = false): boolean {
  if (!videoEl) return false;
  try {
    const hasNativeHls = videoEl.canPlayType('application/vnd.apple.mpegurl') !== '';
    if (!hasNativeHls) return false;
    if (shouldForceNativeMobileHls(videoEl)) return true;

    return !hlsJsSupported;
  } catch {
    return false;
  }
}
