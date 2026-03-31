import { describe, expect, it } from 'vitest';
import {
  buildPlaybackProfileHeaders,
  resolvePlaybackRequestProfile,
  type PlaybackClientContext,
} from './playbackRequestProfile';
import type { CapabilitySnapshot } from './playbackCapabilities';

function buildCapabilities(overrides: Partial<CapabilitySnapshot> = {}): CapabilitySnapshot {
  return {
    capabilitiesVersion: 3,
    container: ['hls', 'mpegts', 'ts', 'mp4'],
    videoCodecs: ['h264', 'hevc'],
    audioCodecs: ['aac', 'ac3'],
    supportsHls: true,
    supportsRange: true,
    deviceType: 'android_tv',
    runtimeProbeUsed: true,
    allowTranscode: true,
    maxVideo: {
      width: 3840,
      height: 2160,
      fps: 60,
    },
    ...overrides,
  };
}

function buildContext(overrides: Partial<PlaybackClientContext> = {}): PlaybackClientContext {
  return {
    platform: 'android-tv',
    isTv: true,
    isNativePlayback: true,
    network: {
      kind: 'ethernet',
      downlinkMbps: 250,
      internetValidated: true,
      metered: false,
    },
    ...overrides,
  };
}

describe('resolvePlaybackRequestProfile', () => {
  it('prefers quality on robust tv/native playback paths', () => {
    expect(resolvePlaybackRequestProfile(buildContext(), buildCapabilities(), 'live')).toBe('quality');
  });

  it('drops to repair on constrained links', () => {
    expect(resolvePlaybackRequestProfile(
      buildContext({
        network: {
          kind: 'browser',
          effectiveType: '2g',
          downlinkMbps: 1.5,
          saveData: true,
        },
        isTv: false,
        isNativePlayback: false,
        platform: 'browser',
      }),
      buildCapabilities(),
      'live'
    )).toBe('repair');
  });

  it('uses compatible on metered cellular links', () => {
    expect(resolvePlaybackRequestProfile(
      buildContext({
        network: {
          kind: 'cellular',
          effectiveType: '4g',
          downlinkMbps: 18,
          metered: true,
        },
        isTv: false,
        isNativePlayback: false,
        platform: 'browser',
      }),
      buildCapabilities(),
      'recording'
    )).toBe('compatible');
  });
});

describe('buildPlaybackProfileHeaders', () => {
  it('emits the profile header only when a profile is set', () => {
    expect(buildPlaybackProfileHeaders()).toEqual({});
    expect(buildPlaybackProfileHeaders('repair')).toEqual({
      'X-XG2G-Profile': 'repair',
    });
  });
});
