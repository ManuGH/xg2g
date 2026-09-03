import { renderHook, act, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { usePlaybackEngine } from '../src/features/player/usePlaybackEngine';
import type { PlaybackAttemptToken, PlaybackEngineEvent } from '../src/features/player/playbackEngineContract';

function makeProps(video: HTMLVideoElement, eventSink: (event: PlaybackEngineEvent) => void) {
  const attemptTokenRef = { current: { epoch: 1, attemptId: 'attempt-1' } as PlaybackAttemptToken };
  return {
    videoRef: { current: video },
    hlsRef: { current: null },
    sessionIdRef: { current: 'sess-1' },
    isTeardownRef: { current: false },
    lastDecodedRef: { current: 0 },
    playbackEpochRef: { current: 1 },
    attemptTokenRef,
    eventSink,
    t: ((key: string) => key) as any,
    reportError: vi.fn().mockResolvedValue(undefined),
    waitForSessionReady: vi.fn().mockResolvedValue({} as any),
    shouldPreferNativeHls: vi.fn(() => true),
    setStats: vi.fn(),
  } as any;
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

  it('emits autoplay.blocked when direct-MP4 autoplay is rejected', async () => {
    const video = document.createElement('video');
    const eventSink = vi.fn();
    const { result } = renderHook(() => usePlaybackEngine(makeProps(video, eventSink)));
    eventSink.mockClear();

    await act(async () => {
      result.current.playDirectMp4('http://example.test/recording.mp4');
      await Promise.resolve();
      await Promise.resolve();
    });

    await waitFor(() => {
      const blockedEvent = eventSink.mock.calls
        .map((call) => call[0])
        .find((evt: PlaybackEngineEvent) => evt.type === 'autoplay.blocked');
      expect(blockedEvent).toBeDefined();
      expect(blockedEvent.attempt).toEqual({ epoch: 1, attemptId: 'attempt-1' });
    });
  });

  it('emits autoplay.blocked when native-HLS autoplay is rejected', async () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation((type: string) =>
      type === 'application/vnd.apple.mpegurl' ? 'maybe' : '',
    );

    const video = document.createElement('video');
    const eventSink = vi.fn();
    const { result } = renderHook(() => usePlaybackEngine(makeProps(video, eventSink)));
    eventSink.mockClear();

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
      const blockedEvent = eventSink.mock.calls
        .map((call) => call[0])
        .find((evt: PlaybackEngineEvent) => evt.type === 'autoplay.blocked');
      expect(blockedEvent).toBeDefined();
      expect(blockedEvent.attempt).toEqual({ epoch: 1, attemptId: 'attempt-1' });
    });
  });
});
