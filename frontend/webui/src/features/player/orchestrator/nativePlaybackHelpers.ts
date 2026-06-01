import type { HostEnvironment, NativePlaybackState as HostNativePlaybackState } from '../../../lib/hostBridge';
import type { PlayerStatus } from '../../../types/v3-player';

export type NativeVideoRevealThresholds = {
  stableMs: number;
  retryMs: number;
  minBufferSeconds: number;
  minAdvanceSeconds: number;
  requirePlaybackResume: boolean;
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

// Ground-truth reveal watchdog. The status FSM can get pinned at (or oscillate
// around) 'buffering' after a pause→resume on a live stream while the underlying
// <video> is already decoding frames again. When that happens the FSM-gated
// reveal never fires and the element is held at visibility:hidden over a healthy
// picture (device-confirmed 2026-06-01: paused=false, readyState=4, currentTime
// advancing, visibility:hidden). This watchdog reveals whenever the element
// itself proves it is playing, independent of the FSM.
// 250ms (was 500): halves the worst-case visible-black latency after a resume.
// The reveal still requires advancedSeconds >= 0.15, so at 250ms any stream
// playing >= 0.6x realtime meets the threshold and the frozen-frame invariant
// holds. (Irrelevant while backgrounded — Safari coalesces timers to >=1s; that
// case is covered by foregroundResume's play()->'playing'.)
export const NATIVE_VIDEO_WATCHDOG_INTERVAL_MS = 250;
export const NATIVE_VIDEO_WATCHDOG_MIN_ADVANCE_SECONDS = 0.15;

// Minimum buffered headroom (seconds) past the seek target for it to count as
// "already in memory". Mirrors the onWaiting buffer-health gate so the two agree.
export const NATIVE_VIDEO_INMEMORY_SEEK_MIN_HEADROOM_SECONDS = 0.5;

// shouldForceRevealNativeVideo decides whether the hidden native <video> has
// proven itself to be genuinely playing and must therefore be revealed. It only
// returns true when frames are actually moving, so a real rebuffer (currentTime
// frozen) keeps the veil up and we never reveal a stalled/black frame.
export function shouldForceRevealNativeVideo(args: {
  paused: boolean;
  readyState: number;
  advancedSeconds: number;
  minAdvanceSeconds?: number;
}): boolean {
  const minAdvance = args.minAdvanceSeconds ?? NATIVE_VIDEO_WATCHDOG_MIN_ADVANCE_SECONDS;
  return !args.paused && args.readyState >= 3 && args.advancedSeconds >= minAdvance;
}

// isInMemorySeekTarget decides whether a just-issued seek landed on data that is
// already buffered and decodable. When true, the seek resumes instantly and the
// buffering veil must NOT show (Plex-like in-buffer scrubbing). When false (seek
// to un-buffered data, or element not ready), the caller veils as before;
// onWaiting/onStalled remain the safety net if this prediction is wrong.
export function isInMemorySeekTarget(args: {
  paused: boolean;
  readyState: number;
  bufferedAheadSeconds: number;
  minHeadroomSeconds?: number;
}): boolean {
  const minHeadroom = args.minHeadroomSeconds ?? NATIVE_VIDEO_INMEMORY_SEEK_MIN_HEADROOM_SECONDS;
  return !args.paused && args.readyState >= 3 && args.bufferedAheadSeconds > minHeadroom;
}
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
