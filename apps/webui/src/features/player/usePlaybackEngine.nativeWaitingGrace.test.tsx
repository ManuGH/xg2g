import { StrictMode } from 'react';
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { TFunction } from 'i18next';
import { NATIVE_UNMUTE_WAITING_WINDOW_MS, NATIVE_WAITING_GRACE_MS, usePlaybackEngine } from './usePlaybackEngine';

vi.mock('./lib/hlsRuntime', () => ({ default: { isSupported: () => false } }));

const media = { currentTime: 100, readyState: 4, paused: false, bufferedAhead: 3 };

function setup() {
  const video = document.createElement('video');
  Object.defineProperties(video, {
    paused: { configurable: true, get: () => media.paused },
    readyState: { configurable: true, get: () => media.readyState },
    currentTime: { configurable: true, get: () => media.currentTime, set: (v: number) => { media.currentTime = v; } },
    currentSrc: { configurable: true, get: () => video.src },
    buffered: {
      configurable: true,
      get: () => ({
        length: 1,
        start: () => Math.max(0, media.currentTime - 10),
        end: () => media.currentTime + media.bufferedAhead,
      }),
    },
  });
  const setStatus = vi.fn();
  const props = {
    videoRef: { current: video },
    hlsRef: { current: null },
    sessionIdRef: { current: 'session-a' },
    isTeardownRef: { current: false },
    lastDecodedRef: { current: 0 },
    playbackEpochRef: { current: 0 },
    t: ((key: string) => key) as TFunction,
    reportError: vi.fn().mockResolvedValue(undefined),
    waitForSessionReady: vi.fn().mockResolvedValue({ playbackUrl: '/live.m3u8' }),
    shouldPreferNativeHls: () => true,
    setStats: vi.fn(),
    setStatus,
    clearPlaybackFailure: vi.fn(),
    reportPlaybackFailure: vi.fn(),
  };
  return { video, props, setStatus };
}

const bufferingCalls = (setStatus: ReturnType<typeof vi.fn>) =>
  setStatus.mock.calls.filter(([value]) => value === 'buffering').length;

async function tick(ms: number) {
  await act(async () => { await vi.advanceTimersByTimeAsync(ms); });
}

function emit(video: HTMLVideoElement, event: string) {
  act(() => { video.dispatchEvent(new Event(event)); });
}

describe.each([false, true])('native waiting grace (StrictMode=%s)', (strict) => {
  beforeEach(() => {
    vi.useFakeTimers();
    Object.assign(media, { currentTime: 100, readyState: 4, paused: false, bufferedAhead: 3 });
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue();
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {});
  });
  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });
  const wrapper = strict ? StrictMode : undefined;

  async function startNative() {
    const ctx = setup();
    const hook = renderHook(() => usePlaybackEngine(ctx.props), { wrapper });
    await act(async () => { hook.result.current.playHls('/live.m3u8', 'native'); });
    ctx.setStatus.mockClear();
    return { ...ctx, hook };
  }

  function stallWithHealthyBuffer(video: HTMLVideoElement) {
    media.readyState = 2;
    emit(video, 'waiting');
  }

  it('holds the frame when playback resumes within the grace window', async () => {
    const { video, setStatus } = await startNative();
    stallWithHealthyBuffer(video);
    await tick(200);
    media.readyState = 4;
    emit(video, 'playing');
    await tick(NATIVE_WAITING_GRACE_MS * 2);
    expect(bufferingCalls(setStatus)).toBe(0);
  });

  it('treats an advancing playhead as resumed even without a playing event', async () => {
    const { video, setStatus } = await startNative();
    stallWithHealthyBuffer(video);
    await tick(300);
    media.currentTime += 0.3;
    await tick(NATIVE_WAITING_GRACE_MS);
    expect(bufferingCalls(setStatus)).toBe(0);
  });

  it('enters buffering once the waiting outlasts the grace window', async () => {
    const { video, setStatus } = await startNative();
    stallWithHealthyBuffer(video);
    await tick(NATIVE_WAITING_GRACE_MS - 1);
    expect(bufferingCalls(setStatus)).toBe(0);
    await tick(1);
    expect(bufferingCalls(setStatus)).toBe(1);
  });

  it('anchors the deadline to the first waiting instead of extending it', async () => {
    const { video, setStatus } = await startNative();
    stallWithHealthyBuffer(video);
    await tick(NATIVE_WAITING_GRACE_MS - 100);
    emit(video, 'waiting');
    await tick(100);
    expect(bufferingCalls(setStatus)).toBe(1);
  });

  it('enters buffering immediately when the buffer is starved', async () => {
    const { video, setStatus } = await startNative();
    media.bufferedAhead = 0.2;
    stallWithHealthyBuffer(video);
    expect(bufferingCalls(setStatus)).toBe(1);
  });

  it('holds the frame through a starved-looking waiting right after an unmute', async () => {
    const { video, setStatus } = await startNative();
    video.muted = true;
    emit(video, 'volumechange');
    await tick(1000);
    video.muted = false;
    emit(video, 'volumechange');
    // Separate audio rendition re-enabled: the A/V intersection reads empty.
    media.bufferedAhead = 0;
    stallWithHealthyBuffer(video);
    await tick(300);
    media.readyState = 4;
    emit(video, 'playing');
    await tick(NATIVE_WAITING_GRACE_MS * 2);
    expect(bufferingCalls(setStatus)).toBe(0);
  });

  it('still enters buffering when the post-unmute waiting outlasts the grace window', async () => {
    const { video, setStatus } = await startNative();
    video.muted = true;
    emit(video, 'volumechange');
    video.muted = false;
    emit(video, 'volumechange');
    media.bufferedAhead = 0;
    stallWithHealthyBuffer(video);
    await tick(NATIVE_WAITING_GRACE_MS);
    expect(bufferingCalls(setStatus)).toBe(1);
  });

  it('treats an unmute outside the window like any starved waiting', async () => {
    const { video, setStatus } = await startNative();
    video.muted = true;
    emit(video, 'volumechange');
    video.muted = false;
    emit(video, 'volumechange');
    await tick(NATIVE_UNMUTE_WAITING_WINDOW_MS + 100);
    media.bufferedAhead = 0;
    stallWithHealthyBuffer(video);
    expect(bufferingCalls(setStatus)).toBe(1);
  });

  it('still reports the waiting warning while holding the frame', async () => {
    const { video, props } = await startNative();
    stallWithHealthyBuffer(video);
    await tick(10);
    expect(props.reportError).toHaveBeenCalledWith('warning', 101, 'waiting', expect.anything());
  });

  it.each(['unmount', 'pause', 'seeking', 'session', 'source', 'teardown'] as const)(
    'does not enter buffering late after %s during the grace window',
    async (change) => {
      const { video, props, setStatus, hook } = await startNative();
      stallWithHealthyBuffer(video);
      await tick(200);
      await act(async () => {
        if (change === 'unmount') hook.unmount();
        if (change === 'pause') { media.paused = true; video.dispatchEvent(new Event('pause')); }
        if (change === 'seeking') video.dispatchEvent(new Event('seeking'));
        if (change === 'session') props.sessionIdRef.current = 'session-b';
        if (change === 'source') video.src = 'http://test.local/next.m3u8';
        if (change === 'teardown') props.isTeardownRef.current = true;
      });
      setStatus.mockClear();
      await tick(NATIVE_WAITING_GRACE_MS * 2);
      expect(bufferingCalls(setStatus)).toBe(0);
    },
  );
});
