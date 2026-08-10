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
  profile?: string;
  playbackDecisionToken?: string;
  authToken?: string;
  hwaccel?: 'auto' | 'force' | 'off';
  correlationId?: string;
  title?: string;
  logoUrl?: string;
  params?: Record<string, string>;
}

export interface NativePlaybackRequestRecording {
  kind: 'recording';
  recordingId: string;
  profile?: string;
  startPositionMs?: number;
  authToken?: string;
  correlationId?: string;
  title?: string;
  logoUrl?: string;
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
  getPlaybackCapabilitiesJson?: () => string;
  setPlaybackActive?: (active: boolean) => void;
  requestInputFocus?: () => void;
  startNativePlayback?: (payloadJson: string) => void;
  stopNativePlayback?: () => void;
  getNativePlaybackStateJson?: () => string;
}

interface AndroidWebMessageBridge {
  postMessage: (messageJson: string) => void;
  onmessage: ((event: { data: unknown }) => void) | null;
}

interface HostMediaKeyEventDetail {
  action: HostMediaKeyAction;
  ts?: number;
}

declare global {
  interface Window {
    Xg2gHost?: AndroidHostBridge;
    Xg2gHostBridge?: AndroidWebMessageBridge;
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

const HOST_BRIDGE_PROTOCOL_VERSION = 1;
const HOST_BRIDGE_HANDSHAKE_TIMEOUT_MS = 1500;
let bridgeInitialization: Promise<void> | null = null;
let finishBridgeInitialization: (() => void) | null = null;
let bridgeInitializationTimer: number | null = null;
let cachedPlaybackCapabilities: Record<string, unknown> | null = null;
let cachedNativePlaybackState: NativePlaybackState | null = null;

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

function objectRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' ? value as Record<string, unknown> : null;
}

function finishHandshake(): void {
  if (bridgeInitializationTimer !== null && typeof window !== 'undefined') {
    window.clearTimeout(bridgeInitializationTimer);
  }
  bridgeInitializationTimer = null;
  finishBridgeInitialization?.();
  finishBridgeInitialization = null;
}

function dispatchHostEvent(eventName: string, detail: unknown): void {
  window.dispatchEvent(new CustomEvent(eventName, { detail }));
}

function receiveWebMessage(event: { data: unknown }): void {
  if (typeof event.data !== 'string') {
    return;
  }

  let message: Record<string, unknown>;
  try {
    const parsed = JSON.parse(event.data);
    const record = objectRecord(parsed);
    if (!record || record.protocolVersion !== HOST_BRIDGE_PROTOCOL_VERSION) {
      return;
    }
    message = record;
  } catch {
    return;
  }

  if (message.type === 'snapshot') {
    const environment = sanitizeHostEnvironment(message.host);
    cachedPlaybackCapabilities = objectRecord(message.playbackCapabilities);
    cachedNativePlaybackState = objectRecord(message.nativePlaybackState) as NativePlaybackState | null;
    applyHostEnvironmentToDocument(environment);
    dispatchHostEvent(HOST_READY_EVENT, environment);
    if (cachedNativePlaybackState) {
      dispatchHostEvent(HOST_NATIVE_PLAYBACK_STATE_EVENT, cachedNativePlaybackState);
    }
    finishHandshake();
    return;
  }

  if (message.type !== 'event') {
    return;
  }

  if (message.event === 'hostMediaKey') {
    dispatchHostEvent(HOST_MEDIA_KEY_EVENT, message.payload);
  } else if (message.event === 'nativePlaybackState') {
    cachedNativePlaybackState = objectRecord(message.payload) as NativePlaybackState | null;
    if (cachedNativePlaybackState) {
      dispatchHostEvent(HOST_NATIVE_PLAYBACK_STATE_EVENT, cachedNativePlaybackState);
    }
  }
}

export function initializeHostBridge(): Promise<void> {
  if (typeof window === 'undefined' || !window.Xg2gHostBridge?.postMessage) {
    return Promise.resolve();
  }
  if (bridgeInitialization) {
    return bridgeInitialization;
  }

  bridgeInitialization = new Promise<void>((resolve) => {
    finishBridgeInitialization = resolve;
    bridgeInitializationTimer = window.setTimeout(finishHandshake, HOST_BRIDGE_HANDSHAKE_TIMEOUT_MS);
  });
  window.Xg2gHostBridge.onmessage = receiveWebMessage;
  try {
    window.Xg2gHostBridge.postMessage(JSON.stringify({
      protocolVersion: HOST_BRIDGE_PROTOCOL_VERSION,
      type: 'hello',
    }));
  } catch {
    finishHandshake();
  }
  return bridgeInitialization;
}

function postWebMessageCommand(command: string, payload?: Record<string, unknown>): boolean {
  const bridge = window.Xg2gHostBridge;
  if (!bridge?.postMessage) {
    return false;
  }
  void initializeHostBridge();
  try {
    bridge.postMessage(JSON.stringify({
      protocolVersion: HOST_BRIDGE_PROTOCOL_VERSION,
      type: 'command',
      command,
      payload: payload ?? {},
    }));
    return true;
  } catch {
    return false;
  }
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
  if (postWebMessageCommand('setPlaybackActive', { active })) {
    return;
  }
  window.Xg2gHost?.setPlaybackActive?.(active);
}

export function requestHostInputFocus(): void {
  if (typeof window === 'undefined') {
    return;
  }
  if (postWebMessageCommand('requestInputFocus')) {
    return;
  }
  window.Xg2gHost?.requestInputFocus?.();
}

export function startNativePlayback(request: NativePlaybackRequest): boolean {
  if (typeof window === 'undefined') {
    return false;
  }

  if (postWebMessageCommand('startNativePlayback', { request })) {
    return true;
  }
  if (!window.Xg2gHost?.startNativePlayback) {
    return false;
  }

  window.Xg2gHost.startNativePlayback(JSON.stringify(request));
  return true;
}

export function stopNativePlayback(): void {
  if (typeof window === 'undefined') {
    return;
  }
  if (postWebMessageCommand('stopNativePlayback')) {
    return;
  }
  window.Xg2gHost?.stopNativePlayback?.();
}

export function getNativePlaybackState(): NativePlaybackState | null {
  if (typeof window === 'undefined') {
    return null;
  }

  if (cachedNativePlaybackState) {
    return cachedNativePlaybackState;
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

export function getNativePlaybackCapabilities(): Record<string, unknown> | null {
  if (typeof window === 'undefined') {
    return null;
  }

  if (cachedPlaybackCapabilities) {
    return cachedPlaybackCapabilities;
  }

  const raw = window.Xg2gHost?.getPlaybackCapabilitiesJson?.();
  if (!raw) {
    return null;
  }

  try {
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === 'object' ? parsed as Record<string, unknown> : null;
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

export function resetHostBridgeForTests(): void {
  if (typeof window !== 'undefined') {
    if (bridgeInitializationTimer !== null) {
      window.clearTimeout(bridgeInitializationTimer);
    }
    if (window.Xg2gHostBridge) {
      window.Xg2gHostBridge.onmessage = null;
    }
    delete window.__XG2G_HOST__;
  }
  bridgeInitialization = null;
  finishBridgeInitialization = null;
  bridgeInitializationTimer = null;
  cachedPlaybackCapabilities = null;
  cachedNativePlaybackState = null;
}
