import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import {
  useDvrTimelineController,
  formatClock,
  formatTimeOfDay,
  readActualSeekableBounds,
} from './useDvrTimelineController';

describe('useDvrTimelineController', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it('formats clocks accurately', () => {
    expect(formatClock(-1)).toBe('--:--');
    expect(formatClock(0)).toBe('00:00');
    expect(formatClock(65)).toBe('01:05');
    expect(formatClock(3665)).toBe('1:01:05');
  });

  it('formats time of day accurately', () => {
    expect(formatTimeOfDay(0)).toBe('--:--:--');
    expect(formatTimeOfDay(-5)).toBe('--:--:--');
    // Using a known unix timestamp
    const ts = 1700000000; // 2023-11-14T22:13:20Z
    expect(formatTimeOfDay(ts)).toMatch(/\d{2}:\d{2}:\d{2}/);
  });

  it('reads actual seekable bounds from HTMLVideoElement', () => {
    expect(readActualSeekableBounds(null)).toBeNull();

    const videoWithoutSeekable = { seekable: { length: 0 } } as any;
    expect(readActualSeekableBounds(videoWithoutSeekable)).toBeNull();

    const videoWithSeekable = {
      seekable: {
        length: 1,
        start: () => 10,
        end: () => 100,
      },
    } as any;
    expect(readActualSeekableBounds(videoWithSeekable)).toEqual({ start: 10, end: 100 });
  });

  it('computes VOD window duration and handles seeking within bounds', () => {
    const videoElement = {
      currentTime: 30,
      duration: 1800,
      paused: false,
      readyState: 4,
      seekable: { length: 1, start: () => 0, end: () => 1800 },
      play: vi.fn().mockResolvedValue(undefined),
    };
    const videoRef = { current: videoElement as any };
    const userPauseIntentRef = { current: false };

    const { result } = renderHook(() =>
      useDvrTimelineController({
        videoRef,
        playbackMode: 'VOD',
        canSeek: true,
        durationSeconds: 1800,
        userPauseIntentRef,
      })
    );

    expect(result.current.windowDuration).toBe(1800);
    expect(result.current.hasSeekWindow).toBe(true);
    expect(result.current.isLiveMode).toBe(false);

    act(() => {
      result.current.seekTo(500);
    });

    expect(videoElement.currentTime).toBe(500);
  });

  it('computes Live DVR window, detects live edge, and supports Go-Live seeking', () => {
    const videoElement = {
      currentTime: 95,
      duration: 100,
      paused: false,
      readyState: 4,
      seekable: { length: 1, start: () => 0, end: () => 100 },
      play: vi.fn().mockResolvedValue(undefined),
    };
    const videoRef = { current: videoElement as any };
    const userPauseIntentRef = { current: false };

    const { result } = renderHook(() =>
      useDvrTimelineController({
        videoRef,
        playbackMode: 'LIVE',
        canSeek: true,
        userPauseIntentRef,
        liveSeekWindow: {
          start: 0,
          end: 100,
          liveEdge: 100,
        },
      })
    );

    act(() => {
      result.current.refreshSeekableState();
    });

    expect(result.current.isLiveMode).toBe(true);
    expect(result.current.canSeekLiveWindow).toBe(true);
    expect(result.current.hasLiveDvrWindow).toBe(true);
    expect(result.current.isAtLiveEdge).toBe(false); // currentTime 95 vs liveEdge 100 is > 2s gap

    // Seek to live edge (target = liveEdge - safetyGap = 100 - 6 = 94)
    act(() => {
      result.current.seekToLiveEdge();
    });

    expect(videoElement.currentTime).toBe(94);
  });
});
