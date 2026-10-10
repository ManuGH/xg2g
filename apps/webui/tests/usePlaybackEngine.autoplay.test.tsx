import { renderHook, act, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { usePlaybackEngine } from '../src/features/player/usePlaybackEngine';

vi.mock('../src/features/player/lib/hlsRuntime', () => {
  const HlsMock = vi.fn().mockImplementation(function (this: any) {
    return {
      on: vi.fn(),
      loadSource: vi.fn(),
      attachMedia: vi.fn(),
      destroy: vi.fn(),
      recoverMediaError: vi.fn(),
    };
  });

  (HlsMock as any).isSupported = vi.fn().mockReturnValue(true);
  (HlsMock as any).Events = {
    LEVEL_SWITCHED: 'hlsLevelSwitched',
    MANIFEST_PARSED: 'hlsManifestParsed',
    BUFFER_APPENDED: 'hlsBufferAppended',
    BUFFER_CODECS: 'hlsBufferCodecs',
    LEVEL_LOADED: 'hlsLevelLoaded',
    FRAG_LOADED: 'hlsFragLoaded',
    AUDIO_TRACKS_UPDATED: 'hlsAudioTracksUpdated',
    AUDIO_TRACK_SWITCHED: 'hlsAudioTrackSwitched',
    ERROR: 'hlsError',
  };
  (HlsMock as any).ErrorTypes = { NETWORK_ERROR: 'networkError', MEDIA_ERROR: 'mediaError' };
  (HlsMock as any).ErrorDetails = { MANIFEST_LOAD_ERROR: 'manifestLoadError' };

  return { default: HlsMock };
});

function makeProps(video: HTMLVideoElement, setStatus: ReturnType<typeof vi.fn>) {
  return {
    videoRef: { current: video },
    hlsRef: { current: null },
    sessionIdRef: { current: 'sess-1' },
    isTeardownRef: { current: false },
    lastDecodedRef: { current: 0 },
    playbackEpochRef: { current: 0 },
    t: ((key: string) => key) as any,
    reportError: vi.fn().mockResolvedValue(undefined),
    waitForSessionReady: vi.fn().mockResolvedValue({} as any),
    shouldPreferNativeHls: vi.fn(() => true),
    setStats: vi.fn(),
    setStatus,
    clearPlaybackFailure: vi.fn(),
    reportPlaybackFailure: vi.fn(),
  } as any;
}

// The fix sets status via a functional updater: (prev) => prev === 'error' ? prev : 'ready'.
function findFunctionalUpdater(setStatus: ReturnType<typeof vi.fn>) {
  return setStatus.mock.calls
    .map((call) => call[0])
    .find((arg) => typeof arg === 'function') as ((prev: string) => string) | undefined;
}

describe('usePlaybackEngine autoplay-rejection recovery', () => {
  beforeEach(() => {
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {});
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
    // Autoplay is rejected (Safari/iOS gesture or Low-Power-Mode policy).
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockRejectedValue(
      new DOMException('autoplay blocked', 'NotAllowedError'),
    );
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('transitions buffering -> ready when direct-MP4 autoplay is rejected', async () => {
    const video = document.createElement('video');
    const setStatus = vi.fn();
    const { result } = renderHook(() => usePlaybackEngine(makeProps(video, setStatus)));
    setStatus.mockClear();

    await act(async () => {
      result.current.playDirectMp4('http://example.test/recording.mp4');
      await Promise.resolve();
      await Promise.resolve();
    });

    await waitFor(() => {
      const updater = findFunctionalUpdater(setStatus);
      expect(updater).toBeDefined();
      expect(updater!('buffering')).toBe('ready');
      // Must not clobber a terminal error state.
      expect(updater!('error')).toBe('error');
    });
  });

  it('transitions buffering -> ready when native-HLS autoplay is rejected', async () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation((type: string) =>
      type === 'application/vnd.apple.mpegurl' ? 'maybe' : '',
    );

    const video = document.createElement('video');
    const setStatus = vi.fn();
    const { result } = renderHook(() => usePlaybackEngine(makeProps(video, setStatus)));
    setStatus.mockClear();

    await act(async () => {
      result.current.playHls('http://example.test/stream.m3u8', 'native');
      await Promise.resolve();
      await Promise.resolve();
    });

    // The native engine schedules autoplay on 'loadedmetadata'. The listener is attached
    // asynchronously (after auth priming), so re-dispatch until it fires — the listener is
    // { once: true } and a dispatch before attachment is a harmless no-op.
    await waitFor(async () => {
      video.dispatchEvent(new Event('loadedmetadata'));
      await Promise.resolve();
      const updater = findFunctionalUpdater(setStatus);
      expect(updater).toBeDefined();
      expect(updater!('buffering')).toBe('ready');
    });
  });

  it('clears reveal hold, transitions to paused, and sets autoplayBlocked when pause occurs during startup reveal hold', async () => {
    const video = document.createElement('video');
    const setStatus = vi.fn();
    const { result } = renderHook(() =>
      usePlaybackEngine({
        ...makeProps(video, setStatus),
        shouldPreferNativeHls: vi.fn(() => false),
        revealHoldMs: 1800,
      }),
    );
    setStatus.mockClear();

    // Start playback with HLS.js engine to activate the startup reveal hold.
    await act(async () => {
      result.current.playHls('http://example.test/stream.m3u8', 'hlsjs');
      await Promise.resolve();
    });

    expect(result.current.autoplayBlocked).toBe(false);
    setStatus.mockClear();

    // Simulate video emitting pause while startup reveal hold is active (e.g. browser autoplay/pause intervention).
    await act(async () => {
      video.dispatchEvent(new Event('pause'));
      await Promise.resolve();
    });

    // autoplayBlocked must now be true to unblock UI controls instead of showing an infinite buffering veil.
    expect(result.current.autoplayBlocked).toBe(true);

    // setStatus must transition to 'paused' and never remain stuck in 'buffering'.
    await waitFor(() => {
      const updater = findFunctionalUpdater(setStatus);
      expect(updater).toBeDefined();
      expect(updater!('buffering')).toBe('paused');
      expect(updater!('error')).toBe('error');
    });

    // When the user/engine resumes playback, 'playing' resets autoplayBlocked.
    await act(async () => {
      video.dispatchEvent(new Event('playing'));
      await Promise.resolve();
    });

    expect(result.current.autoplayBlocked).toBe(false);
  });
});

