export type HostPlatform = 'browser' | 'android' | 'android-tv';

export interface HostEnvironment {
  platform: HostPlatform;
  isTv: boolean;
  supportsKeepScreenAwake: boolean;
  supportsHostMediaKeys: boolean;
  supportsInputFocus: boolean;
  supportsNativePlayback: boolean;
}

export type HostMediaKeyAction =
  | 'playPause'
  | 'play'
  | 'pause'
  | 'seekBack'
  | 'seekForward'
  | 'stop';

export interface NativePlaybackRequestLive {
  kind: 'live';
  serviceRef: string;
  playbackDecisionToken?: string;
  authToken?: string;
  hwaccel?: 'auto' | 'force' | 'off';
  correlationId?: string;
  title?: string;
  params?: Record<string, string>;
}

export interface NativePlaybackRequestRecording {
  kind: 'recording';
  recordingId: string;
  startPositionMs?: number;
  authToken?: string;
  correlationId?: string;
  title?: string;
}

export type NativePlaybackRequest = NativePlaybackRequestLive | NativePlaybackRequestRecording;

export interface NativePlaybackState {
  activeRequest?: NativePlaybackRequest | null;
  session?: {
    sessionId: string;
    state: string;
    playbackUrl?: string | null;
    mode?: string | null;
    requestId?: string | null;
    profileReason?: string | null;
    trace?: Record<string, unknown> | null;
  } | null;
  diagnostics?: {
    requestId?: string | null;
    playbackMode?: string | null;
    profileReason?: string | null;
    capHash?: string | null;
    playbackInfo?: Record<string, unknown> | null;
    trace?: Record<string, unknown> | null;
  } | null;
  playerState: number;
  playWhenReady: boolean;
  isInPip: boolean;
  lastError?: string | null;
}

interface AndroidHostBridge {
  getCapabilitiesJson?: () => string;
  setPlaybackActive?: (active: boolean) => void;
  requestInputFocus?: () => void;
  startNativePlayback?: (payloadJson: string) => void;
  stopNativePlayback?: () => void;
  getNativePlaybackStateJson?: () => string;
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
  supportsNativePlayback: false,
});

export const HOST_READY_EVENT = 'xg2g:host-ready';
export const HOST_MEDIA_KEY_EVENT = 'xg2g:host-media-key';
export const HOST_NATIVE_PLAYBACK_STATE_EVENT = 'xg2g:native-playback-state';

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
    supportsNativePlayback: record.supportsNativePlayback === true,
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

export function startNativePlayback(request: NativePlaybackRequest): boolean {
  if (typeof window === 'undefined' || !window.Xg2gHost?.startNativePlayback) {
    return false;
  }

  window.Xg2gHost.startNativePlayback(JSON.stringify(request));
  return true;
}

export function stopNativePlayback(): void {
  if (typeof window === 'undefined') {
    return;
  }
  window.Xg2gHost?.stopNativePlayback?.();
}

export function getNativePlaybackState(): NativePlaybackState | null {
  if (typeof window === 'undefined') {
    return null;
  }

  const raw = window.Xg2gHost?.getNativePlaybackStateJson?.();
  if (!raw) {
    return null;
  }

  try {
    return JSON.parse(raw) as NativePlaybackState;
  } catch {
    return null;
  }
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

export function onNativePlaybackState(handler: (state: NativePlaybackState) => void): () => void {
  if (typeof window === 'undefined') {
    return () => {};
  }

  const listener = (event: Event) => {
    const detail = (event as CustomEvent<NativePlaybackState>).detail;
    if (!detail) {
      return;
    }
    handler(detail);
  };

  window.addEventListener(HOST_NATIVE_PLAYBACK_STATE_EVENT, listener);
  return () => window.removeEventListener(HOST_NATIVE_PLAYBACK_STATE_EVENT, listener);
}
