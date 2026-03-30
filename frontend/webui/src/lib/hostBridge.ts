export type HostPlatform = 'browser' | 'android' | 'android-tv';

export interface HostEnvironment {
  platform: HostPlatform;
  isTv: boolean;
  supportsKeepScreenAwake: boolean;
  supportsHostMediaKeys: boolean;
  supportsInputFocus: boolean;
}

export type HostMediaKeyAction =
  | 'playPause'
  | 'play'
  | 'pause'
  | 'seekBack'
  | 'seekForward'
  | 'stop';

interface AndroidHostBridge {
  getCapabilitiesJson?: () => string;
  setPlaybackActive?: (active: boolean) => void;
  requestInputFocus?: () => void;
}

interface HostMediaKeyEventDetail {
  action: HostMediaKeyAction;
  ts?: number;
}

declare global {
  interface Window {
    Xg2gHost?: AndroidHostBridge;
    __XG2G_HOST__?: HostEnvironment;
  }
}

const DEFAULT_HOST_ENVIRONMENT: HostEnvironment = Object.freeze({
  platform: 'browser',
  isTv: false,
  supportsKeepScreenAwake: false,
  supportsHostMediaKeys: false,
  supportsInputFocus: false,
});

export const HOST_READY_EVENT = 'xg2g:host-ready';
export const HOST_MEDIA_KEY_EVENT = 'xg2g:host-media-key';

function sanitizeHostEnvironment(value: unknown): HostEnvironment {
  if (!value || typeof value !== 'object') {
    return DEFAULT_HOST_ENVIRONMENT;
  }

  const record = value as Record<string, unknown>;
  const platform = record.platform === 'android-tv' || record.platform === 'android'
    ? record.platform
    : DEFAULT_HOST_ENVIRONMENT.platform;

  return {
    platform,
    isTv: record.isTv === true || platform === 'android-tv',
    supportsKeepScreenAwake: record.supportsKeepScreenAwake === true,
    supportsHostMediaKeys: record.supportsHostMediaKeys === true,
    supportsInputFocus: record.supportsInputFocus === true,
  };
}

export function resolveHostEnvironment(): HostEnvironment {
  if (typeof window === 'undefined') {
    return DEFAULT_HOST_ENVIRONMENT;
  }

  if (window.__XG2G_HOST__) {
    return window.__XG2G_HOST__;
  }

  const json = window.Xg2gHost?.getCapabilitiesJson?.();
  if (!json) {
    window.__XG2G_HOST__ = DEFAULT_HOST_ENVIRONMENT;
    return window.__XG2G_HOST__;
  }

  try {
    window.__XG2G_HOST__ = sanitizeHostEnvironment(JSON.parse(json));
  } catch {
    window.__XG2G_HOST__ = DEFAULT_HOST_ENVIRONMENT;
  }

  return window.__XG2G_HOST__;
}

export function applyHostEnvironmentToDocument(environment: HostEnvironment): void {
  if (typeof document === 'undefined') {
    return;
  }

  window.__XG2G_HOST__ = environment;
  const root = document.documentElement;
  root.dataset.xg2gHostPlatform = environment.platform;
  root.dataset.xg2gHostTv = String(environment.isTv);
  root.dataset.xg2gHostMediaKeys = String(environment.supportsHostMediaKeys);
}

export function setHostPlaybackActive(active: boolean): void {
  if (typeof window === 'undefined') {
    return;
  }
  window.Xg2gHost?.setPlaybackActive?.(active);
}

export function requestHostInputFocus(): void {
  if (typeof window === 'undefined') {
    return;
  }
  window.Xg2gHost?.requestInputFocus?.();
}

export function onHostMediaKey(handler: (action: HostMediaKeyAction) => void): () => void {
  if (typeof window === 'undefined') {
    return () => {};
  }

  const listener = (event: Event) => {
    const detail = (event as CustomEvent<HostMediaKeyEventDetail>).detail;
    if (!detail?.action) {
      return;
    }
    handler(detail.action);
  };

  window.addEventListener(HOST_MEDIA_KEY_EVENT, listener);
  return () => window.removeEventListener(HOST_MEDIA_KEY_EVENT, listener);
}
