import type {
  NativeVideoRevealThresholds,
} from '../components/playerNativePlaybackModel';
import {
  NATIVE_PLAYER_STATE_ENDED,
  NATIVE_PLAYER_STATE_IDLE,
  NATIVE_VIDEO_REBUFFER_VEIL_MS,
  NATIVE_VIDEO_REVEAL_REBUFFER,
  NATIVE_VIDEO_REVEAL_STARTUP,
  NATIVE_VIDEO_UNVEIL_AFTER_PLAYING_MS,
  supportsManagedNativePlayback,
} from '../components/playerNativePlaybackModel';

export type { NativeVideoRevealThresholds };
export {
  NATIVE_PLAYER_STATE_ENDED,
  NATIVE_PLAYER_STATE_IDLE,
  NATIVE_VIDEO_REBUFFER_VEIL_MS,
  NATIVE_VIDEO_REVEAL_REBUFFER,
  NATIVE_VIDEO_REVEAL_STARTUP,
  NATIVE_VIDEO_UNVEIL_AFTER_PLAYING_MS,
  supportsManagedNativePlayback,
};

export { NATIVE_PLAYER_STATE_BUFFERING, NATIVE_PLAYER_STATE_READY, resolveNativePlaybackStatus } from '../components/playerNativePlaybackModel';
