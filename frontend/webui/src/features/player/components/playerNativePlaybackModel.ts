import type {
  HostEnvironment,
  NativePlaybackState as HostNativePlaybackState,
} from '../../../lib/hostBridge';
import type { PlayerStatus } from '../../../types/v3-player';

export type NativeVideoRevealThresholds = {
  stableMs: number;
  retryMs: number;
  minBufferSeconds: number;
  minAdvanceSeconds: number;
  requirePlaybackResume: boolean;
};

export type NativeVideoFrameState = {
  paused: boolean;
  readyState: number;
  videoWidth: number;
  videoHeight: number;
  currentTime: number;
  decodedFrameCount?: number;
};

export const NATIVE_VIDEO_REVEAL_STARTUP: NativeVideoRevealThresholds = {
  stableMs: 650,
  retryMs: 250,
  minBufferSeconds: 0.75,
  minAdvanceSeconds: 0.12,
  requirePlaybackResume: false,
};

export const NATIVE_VIDEO_REVEAL_REBUFFER: NativeVideoRevealThresholds = {
  stableMs: 420,
  retryMs: 160,
  minBufferSeconds: 0.5,
  minAdvanceSeconds: 0.22,
  requirePlaybackResume: true,
};

export const NATIVE_VIDEO_REBUFFER_VEIL_MS = 2300;
export const NATIVE_VIDEO_UNVEIL_AFTER_PLAYING_MS = 140;
export const NATIVE_VISIBILITY_RESUME_RECOVERY_MS = 1400;
export const NATIVE_PLAYER_STATE_IDLE = 1;
export const NATIVE_PLAYER_STATE_BUFFERING = 2;
export const NATIVE_PLAYER_STATE_READY = 3;
export const NATIVE_PLAYER_STATE_ENDED = 4;

export function supportsManagedNativePlayback(environment: HostEnvironment): boolean {
  return environment.supportsNativePlayback
    && (environment.platform === 'android' || environment.platform === 'android-tv');
}

export function resolveNativePlaybackStatus(state: HostNativePlaybackState | null): PlayerStatus | null {
  if (!state?.activeRequest) {
    if (state?.lastError) {
      return 'error';
    }
    if (state?.playerState === NATIVE_PLAYER_STATE_ENDED) {
      return 'stopped';
    }
    return null;
  }

  if (state.lastError) {
    return 'error';
  }

  switch (state.playerState) { // xg2g:allow-webui-logic – maps native browser player states to UI status; not backend FSM
    case NATIVE_PLAYER_STATE_BUFFERING:
      return state.session ? 'buffering' : 'starting';
    case NATIVE_PLAYER_STATE_READY:
      return state.playWhenReady ? 'playing' : 'paused';
    case NATIVE_PLAYER_STATE_ENDED:
      return 'stopped';
    case NATIVE_PLAYER_STATE_IDLE:
    default:
      return state.session ? 'buffering' : 'starting';
  }
}

function hasNativeVideoGeometry(state: NativeVideoFrameState): boolean {
  return state.videoWidth > 0 && state.videoHeight > 0;
}

function hasNativeDecodedFrames(state: NativeVideoFrameState): boolean {
  return typeof state.decodedFrameCount !== 'number' || state.decodedFrameCount > 0;
}

export function canRevealNativeVideoFrame(state: NativeVideoFrameState): boolean {
  if (state.paused) {
    return false;
  }

  return state.readyState >= 3 || (
    hasNativeVideoGeometry(state) &&
    hasNativeDecodedFrames(state)
  );
}

export function canReleaseNativeVideoVeil(
  state: NativeVideoFrameState,
  status: PlayerStatus,
): boolean {
  if (state.paused) {
    return false;
  }

  if (state.readyState >= 3) {
    return true;
  }

  const hasPlaybackProgress = Number.isFinite(state.currentTime) && state.currentTime > 0;
  return status === 'playing' &&
    hasNativeVideoGeometry(state) &&
    hasPlaybackProgress &&
    hasNativeDecodedFrames(state);
}
