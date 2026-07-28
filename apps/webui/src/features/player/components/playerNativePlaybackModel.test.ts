import { describe, expect, it } from 'vitest';
import type {
  HostEnvironment,
  NativePlaybackState as HostNativePlaybackState,
} from '../../../lib/hostBridge';

import {
  NATIVE_PLAYER_STATE_BUFFERING,
  NATIVE_PLAYER_STATE_ENDED,
  NATIVE_PLAYER_STATE_IDLE,
  NATIVE_PLAYER_STATE_READY,
  NATIVE_VIDEO_REVEAL_REBUFFER,
  NATIVE_VIDEO_REVEAL_STARTUP,
  canReleaseNativeVideoVeil,
  canRevealNativeVideoFrame,
  resolveNativePlaybackStatus,
  supportsManagedNativePlayback,
} from './playerNativePlaybackModel';

function hostEnvironment(overrides: Partial<HostEnvironment> = {}): HostEnvironment {
  return {
    platform: 'browser',
    isTv: false,
    supportsKeepScreenAwake: false,
    supportsHostMediaKeys: false,
    supportsInputFocus: false,
    supportsNativePlayback: false,
    ...overrides,
  };
}

function playbackState(overrides: Partial<HostNativePlaybackState> = {}): HostNativePlaybackState {
  return {
    activeRequest: {
      kind: 'live',
      serviceRef: '1:0:1:test',
    },
    session: null,
    diagnostics: null,
    playerState: NATIVE_PLAYER_STATE_IDLE,
    playWhenReady: true,
    isInPip: false,
    lastError: null,
    ...overrides,
  };
}

describe('playerNativePlaybackModel', () => {
  it('enables managed native playback only for Android hosts with native support', () => {
    expect(supportsManagedNativePlayback(hostEnvironment())).toBe(false);
    expect(supportsManagedNativePlayback(hostEnvironment({
      platform: 'browser',
      supportsNativePlayback: true,
    }))).toBe(false);
    expect(supportsManagedNativePlayback(hostEnvironment({
      platform: 'android',
      supportsNativePlayback: true,
    }))).toBe(true);
    expect(supportsManagedNativePlayback(hostEnvironment({
      platform: 'android-tv',
      supportsNativePlayback: true,
    }))).toBe(true);
  });

  it('maps inactive native states without inventing active playback', () => {
    expect(resolveNativePlaybackStatus(null)).toBeNull();
    expect(resolveNativePlaybackStatus(playbackState({ activeRequest: null }))).toBeNull();
    expect(resolveNativePlaybackStatus(playbackState({
      activeRequest: null,
      lastError: 'decoder_failed',
    }))).toBe('error');
    expect(resolveNativePlaybackStatus(playbackState({
      activeRequest: null,
      playerState: NATIVE_PLAYER_STATE_ENDED,
    }))).toBe('stopped');
  });

  it('maps active native player states to V3 player statuses', () => {
    expect(resolveNativePlaybackStatus(playbackState({
      playerState: NATIVE_PLAYER_STATE_BUFFERING,
    }))).toBe('starting');
    expect(resolveNativePlaybackStatus(playbackState({
      playerState: NATIVE_PLAYER_STATE_BUFFERING,
      session: { sessionId: 's1', state: 'running' },
    }))).toBe('buffering');
    expect(resolveNativePlaybackStatus(playbackState({
      playerState: NATIVE_PLAYER_STATE_READY,
      playWhenReady: true,
    }))).toBe('playing');
    expect(resolveNativePlaybackStatus(playbackState({
      playerState: NATIVE_PLAYER_STATE_READY,
      playWhenReady: false,
    }))).toBe('paused');
    expect(resolveNativePlaybackStatus(playbackState({
      playerState: NATIVE_PLAYER_STATE_ENDED,
    }))).toBe('stopped');
    expect(resolveNativePlaybackStatus(playbackState({
      lastError: 'player_failed',
    }))).toBe('error');
  });

  it('keeps startup and rebuffer reveal thresholds distinct', () => {
    expect(NATIVE_VIDEO_REVEAL_STARTUP.requirePlaybackResume).toBe(false);
    expect(NATIVE_VIDEO_REVEAL_REBUFFER.requirePlaybackResume).toBe(true);
    expect(NATIVE_VIDEO_REVEAL_STARTUP.stableMs).toBeGreaterThan(NATIVE_VIDEO_REVEAL_REBUFFER.stableMs);
  });

  it('reveals native video when a renderable frame is available', () => {
    expect(canRevealNativeVideoFrame({
      paused: true,
      readyState: 4,
      videoWidth: 1920,
      videoHeight: 1080,
      currentTime: 0,
    })).toBe(false);

    expect(canRevealNativeVideoFrame({
      paused: false,
      readyState: 3,
      videoWidth: 0,
      videoHeight: 0,
      currentTime: 0,
      decodedFrameCount: 0,
    })).toBe(true);

    expect(canRevealNativeVideoFrame({
      paused: false,
      readyState: 2,
      videoWidth: 1920,
      videoHeight: 1080,
      currentTime: 0,
      decodedFrameCount: 1,
    })).toBe(true);

    expect(canRevealNativeVideoFrame({
      paused: false,
      readyState: 2,
      videoWidth: 1920,
      videoHeight: 1080,
      currentTime: 0,
      decodedFrameCount: 0,
    })).toBe(false);
  });

  it('releases the native veil only after ready data or playing frame progress', () => {
    expect(canReleaseNativeVideoVeil({
      paused: false,
      readyState: 3,
      videoWidth: 0,
      videoHeight: 0,
      currentTime: 0,
      decodedFrameCount: 0,
    }, 'buffering')).toBe(true);

    expect(canReleaseNativeVideoVeil({
      paused: false,
      readyState: 2,
      videoWidth: 1920,
      videoHeight: 1080,
      currentTime: 0,
      decodedFrameCount: 1,
    }, 'playing')).toBe(false);

    expect(canReleaseNativeVideoVeil({
      paused: false,
      readyState: 2,
      videoWidth: 1920,
      videoHeight: 1080,
      currentTime: 0.2,
      decodedFrameCount: 1,
    }, 'playing')).toBe(true);

    expect(canReleaseNativeVideoVeil({
      paused: false,
      readyState: 2,
      videoWidth: 1920,
      videoHeight: 1080,
      currentTime: 0.2,
      decodedFrameCount: 1,
    }, 'buffering')).toBe(false);
  });
});
