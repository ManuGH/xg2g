import { renderHook, act, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { usePlaybackEngine } from '../src/features/player/usePlaybackEngine';

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
});
