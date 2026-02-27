import { describe, expect, it } from 'vitest';
import {
  SERVER_PLAYBACK_MODES,
  isLiveEngineAvailable,
  mapServerModeToLiveEngine,
  parseServerPlaybackMode,
  resolveAvailableLiveEngineFromMode,
  resolveLiveEngineFromMode,
  type LiveEngineAvailability,
  type LivePlaybackEngine,
  type ServerPlaybackMode
} from '../../src/components/v3playerModeBridge';

describe('Gate P: V3Player mode bridge', () => {
  it('exposes a complete server mode table', () => {
    expect(SERVER_PLAYBACK_MODES).toEqual(['native_hls', 'hlsjs', 'direct_mp4', 'transcode', 'deny']);
  });

  it('maps backend mode to player engine 1:1 with fail-closed nulls', () => {
    const expected: Record<ServerPlaybackMode, LivePlaybackEngine | null> = {
      native_hls: 'native',
      hlsjs: 'hlsjs',
      direct_mp4: null,
      transcode: 'hlsjs',
      deny: null
    };

    for (const mode of SERVER_PLAYBACK_MODES) {
      expect(mapServerModeToLiveEngine(mode)).toBe(expected[mode]);
    }
  });

  it('parses only known modes and fails closed on drift', () => {
    expect(parseServerPlaybackMode('native_hls')).toBe('native_hls');
    expect(parseServerPlaybackMode('hlsjs')).toBe('hlsjs');
    expect(parseServerPlaybackMode('deny')).toBe('deny');

    expect(parseServerPlaybackMode(undefined)).toBeNull();
    expect(parseServerPlaybackMode(null)).toBeNull();
    expect(parseServerPlaybackMode('legacy_mode')).toBeNull();
    expect(parseServerPlaybackMode('direct_play')).toBeNull();
  });

  it('never infers an engine when mode is missing or invalid', () => {
    expect(resolveLiveEngineFromMode(undefined)).toBeNull();
    expect(resolveLiveEngineFromMode('unknown')).toBeNull();
    expect(resolveLiveEngineFromMode('deny')).toBeNull();
    expect(resolveLiveEngineFromMode('direct_mp4')).toBeNull();
    expect(resolveLiveEngineFromMode('native_hls')).toBe('native');
    expect(resolveLiveEngineFromMode('transcode')).toBe('hlsjs');
  });

  it('exposes explicit engine availability checks (Gate X)', () => {
    const availability: LiveEngineAvailability = { native: true, hlsjs: false };
    expect(isLiveEngineAvailable('native', availability)).toBe(true);
    expect(isLiveEngineAvailable('hlsjs', availability)).toBe(false);

    expect(resolveAvailableLiveEngineFromMode('native_hls', availability)).toBe('native');
    expect(resolveAvailableLiveEngineFromMode('hlsjs', availability)).toBeNull();
    expect(resolveAvailableLiveEngineFromMode('transcode', availability)).toBeNull();
    expect(resolveAvailableLiveEngineFromMode('direct_mp4', availability)).toBeNull();
    expect(resolveAvailableLiveEngineFromMode('deny', availability)).toBeNull();
    expect(resolveAvailableLiveEngineFromMode('unknown_mode', availability)).toBeNull();
  });
});
