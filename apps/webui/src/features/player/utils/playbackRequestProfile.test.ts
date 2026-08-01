import { describe, expect, it } from 'vitest';
import {
  NETWORK_DOWNGRADE_HOLD_MS,
  buildPlaybackProfileHeaders,
  clearNetworkStarvationHold,
  createAutomaticProfileMemory,
  normalizePlaybackProfileSelection,
  noteNetworkStarvation,
  resolvePlaybackProfileForPreflight,
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

  it('uses the capped bandwidth profile on constrained links', () => {
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
    )).toBe('bandwidth');
  });

  it('uses the capped bandwidth profile on metered cellular links', () => {
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
    )).toBe('bandwidth');
  });

  it('treats an AV1-only client as a modern quality path', () => {
    expect(resolvePlaybackRequestProfile(
      buildContext(),
      buildCapabilities({ videoCodecs: ['av1'] }),
      'live'
    )).toBe('quality');
  });

  it('automatically resolves quality profile on a desktop web browser (e.g. Mac on Wi-Fi)', () => {
    expect(resolvePlaybackRequestProfile(
      buildContext({
        isTv: false,
        isNativePlayback: false,
        platform: 'macos',
        network: {
          kind: 'wifi',
          downlinkMbps: 100,
          metered: false,
        },
      }),
      buildCapabilities({
        videoCodecs: ['av1', 'h264'],
        videoCodecSignals: [
          { codec: 'av1', supported: true, smooth: true, powerEfficient: true },
        ],
      }),
      'live'
    )).toBe('quality');
  });

  it('resolves quality for a Mac on the LAN once the server probe confirmed it', () => {
    // The LAN verdict carries no measured bitrate on purpose; the browser's own
    // resource-timing guess must not be able to veto the quality rung.
    expect(resolvePlaybackRequestProfile(
      buildContext({
        isTv: false,
        isNativePlayback: false,
        platform: 'macos',
        network: {
          kind: 'lan',
          metered: false,
        },
      }),
      buildCapabilities({
        videoCodecs: ['av1', 'h264'],
        videoCodecSignals: [
          { codec: 'av1', supported: true },
        ],
      }),
      'live'
    )).toBe('quality');
  });

  it('resolves quality from a measured server probe', () => {
    expect(resolvePlaybackRequestProfile(
      buildContext({
        isTv: false,
        isNativePlayback: false,
        platform: 'macos',
        network: {
          kind: 'measured',
          downlinkMbps: 120,
          metered: false,
        },
      }),
      buildCapabilities(),
      'live'
    )).toBe('quality');
  });

  it('still caps a measured link that is genuinely slow', () => {
    expect(resolvePlaybackRequestProfile(
      buildContext({
        isTv: false,
        isNativePlayback: false,
        network: {
          kind: 'measured',
          downlinkMbps: 4,
        },
      }),
      buildCapabilities(),
      'live'
    )).toBe('bandwidth');
  });

  it('withholds quality when Media Capabilities reports no smooth modern codec', () => {
    expect(resolvePlaybackRequestProfile(
      buildContext(),
      buildCapabilities({
        videoCodecs: ['h264', 'hevc'],
        videoCodecSignals: [
          { codec: 'h264', supported: true, smooth: false },
          { codec: 'hevc', supported: true, smooth: false },
        ],
      }),
      'live'
    )).toBeUndefined();
  });

  it('keeps quality when at least one modern codec is reported smooth', () => {
    expect(resolvePlaybackRequestProfile(
      buildContext(),
      buildCapabilities({
        videoCodecs: ['h264', 'av1'],
        videoCodecSignals: [
          { codec: 'h264', supported: true, smooth: false },
          { codec: 'av1', supported: true, smooth: true, powerEfficient: true },
        ],
      }),
      'live'
    )).toBe('quality');
  });

  it('ignores signals without a smooth verdict instead of demoting', () => {
    expect(resolvePlaybackRequestProfile(
      buildContext(),
      buildCapabilities({
        videoCodecSignals: [
          { codec: 'h264', supported: true },
        ],
      }),
      'live'
    )).toBe('quality');

  });
});

describe('buildPlaybackProfileHeaders', () => {
  it('emits the profile header only when a profile is set', () => {
    expect(buildPlaybackProfileHeaders()).toEqual({});
    expect(buildPlaybackProfileHeaders('repair')).toEqual({
      'X-XG2G-Profile': 'repair',
    });
    expect(buildPlaybackProfileHeaders('bandwidth')).toEqual({
      'X-XG2G-Profile': 'bandwidth',
    });
    expect(buildPlaybackProfileHeaders('direct')).toEqual({
      'X-XG2G-Profile': 'direct',
    });
  });
});

describe('planner-bound profile selection', () => {
  it('keeps only public playback intents and migrates copy aliases', () => {
    expect(normalizePlaybackProfileSelection('copy')).toBe('direct');
    expect(normalizePlaybackProfileSelection('quality')).toBe('quality');
    expect(normalizePlaybackProfileSelection('compatible')).toBe('compatible');
    expect(normalizePlaybackProfileSelection('repair')).toBe('repair');
  });

  it('drops legacy encoder profile ids instead of bypassing the planner', () => {
    expect(normalizePlaybackProfileSelection('av1_hw')).toBe('auto');
    expect(normalizePlaybackProfileSelection('hevc_hw')).toBe('auto');
    expect(normalizePlaybackProfileSelection('h264_fmp4')).toBe('auto');
  });

  it('binds an explicit profile before preflight and preserves automatic policy for auto', () => {
    expect(resolvePlaybackProfileForPreflight('repair', 'quality')).toBe('repair');
    expect(resolvePlaybackProfileForPreflight('auto', 'bandwidth')).toBe('bandwidth');
    expect(resolvePlaybackProfileForPreflight('auto')).toBeUndefined();
  });
});

// A single-rendition output means every profile change is a new encoder
// session. On mobile the measured downlink walks across the thresholds
// continuously, so without memory the resolver turns ordinary cellular jitter
// into repeated transcode restarts.
describe('resolvePlaybackRequestProfile memory', () => {
  const mobile = (downlinkMbps: number) => buildContext({
    platform: 'ios',
    isTv: false,
    isNativePlayback: false,
    network: { kind: 'measured', downlinkMbps },
  });

  it('keeps quality through a dip that a bare threshold would have flipped', () => {
    const memory = createAutomaticProfileMemory();
    expect(resolvePlaybackRequestProfile(mobile(40), buildCapabilities(), 'live', memory)).toBe('quality');
    // 30 is below the entry threshold of 35 but above the exit threshold of 28.
    expect(resolvePlaybackRequestProfile(mobile(30), buildCapabilities(), 'live', memory)).toBe('quality');
  });

  it('still gives quality up on a genuine drop', () => {
    const memory = createAutomaticProfileMemory();
    resolvePlaybackRequestProfile(mobile(40), buildCapabilities(), 'live', memory);
    expect(resolvePlaybackRequestProfile(mobile(25), buildCapabilities(), 'live', memory)).toBeUndefined();
  });

  it('does not climb out of bandwidth on a marginal improvement', () => {
    const memory = createAutomaticProfileMemory();
    expect(resolvePlaybackRequestProfile(mobile(12), buildCapabilities(), 'live', memory)).toBe('bandwidth');
    // 18 clears the 15 entry threshold but not the 20 exit threshold.
    expect(resolvePlaybackRequestProfile(mobile(18), buildCapabilities(), 'live', memory)).toBe('bandwidth');
    expect(resolvePlaybackRequestProfile(mobile(22), buildCapabilities(), 'live', memory)).toBeUndefined();
  });

  it('behaves exactly as before when no memory is supplied', () => {
    expect(resolvePlaybackRequestProfile(mobile(30), buildCapabilities(), 'live')).toBeUndefined();
    expect(resolvePlaybackRequestProfile(mobile(18), buildCapabilities(), 'live')).toBeUndefined();
    expect(resolvePlaybackRequestProfile(mobile(12), buildCapabilities(), 'live')).toBe('bandwidth');
  });

  it('holds the safe rung after playback died of starvation, whatever the probe now claims', () => {
    const memory = createAutomaticProfileMemory();
    noteNetworkStarvation(memory, 1_000);
    expect(resolvePlaybackRequestProfile(mobile(120), buildCapabilities(), 'live', memory, 2_000))
      .toBe('bandwidth');
  });

  it('lets the hold lapse', () => {
    const memory = createAutomaticProfileMemory();
    noteNetworkStarvation(memory, 1_000);
    const afterHold = 1_000 + NETWORK_DOWNGRADE_HOLD_MS + 1;
    expect(resolvePlaybackRequestProfile(mobile(120), buildCapabilities(), 'live', memory, afterHold))
      .toBe('quality');
  });

  it('releases the hold when the viewing intent changes', () => {
    const memory = createAutomaticProfileMemory();
    noteNetworkStarvation(memory, 1_000);
    clearNetworkStarvationHold(memory);
    expect(resolvePlaybackRequestProfile(mobile(120), buildCapabilities(), 'live', memory, 2_000))
      .toBe('quality');
  });
});
