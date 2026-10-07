import { StrictMode, useEffect } from 'react';
import { act, render, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { TFunction } from 'i18next';
import { usePlaybackEngine } from './usePlaybackEngine';
import { HLS_STARTUP_POLICY } from './playbackEnginePolicy';

vi.mock('./lib/hlsRuntime', () => ({ default: { isSupported: () => false } }));

function setup() {
  const video = document.createElement('video');
  Object.defineProperties(video, {
    paused: { configurable: true, value: false },
    readyState: { configurable: true, value: 2 },
    currentSrc: { configurable: true, get: () => video.src },
  });
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
    setStatus: vi.fn(),
    clearPlaybackFailure: vi.fn(),
    reportPlaybackFailure: vi.fn(),
  };
  return { video, props };
}

async function tick(ms: number) {
  await act(async () => { await vi.advanceTimersByTimeAsync(ms); });
}

function emit(video: HTMLVideoElement, event: string) {
  act(() => { video.dispatchEvent(new Event(event)); });
}

describe.each([false, true])('native startup recovery (StrictMode=%s)', (strict) => {
  beforeEach(() => {
    vi.useFakeTimers();
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

  it('preserves a cold source across waiting and rerenders, then recovers at a fixed deadline', async () => {
    const { video, props } = setup();
    const hook = renderHook((p) => usePlaybackEngine(p), { initialProps: props, wrapper });
    await act(async () => { hook.result.current.playHls('/live.m3u8', 'native'); });
    emit(video, 'waiting');
    await tick(3000);
    expect(props.waitForSessionReady).not.toHaveBeenCalled();
    expect(video.pause).not.toHaveBeenCalled();
    expect(video.src).toContain('/live.m3u8');
    hook.rerender({ ...props, setStatus: vi.fn(), reportError: vi.fn().mockResolvedValue(undefined) });
    emit(video, 'stalled');
    await tick(HLS_STARTUP_POLICY.liveTimeoutMs - 3001);
    expect(props.waitForSessionReady).not.toHaveBeenCalled();
    await tick(851);
    expect(props.waitForSessionReady).toHaveBeenCalledTimes(1);
    expect(video.pause).toHaveBeenCalledTimes(1);
  });

  it('cancels startup recovery on playing and retains fast recovery after warmup', async () => {
    const { video, props } = setup();
    const hook = renderHook(() => usePlaybackEngine(props), { wrapper });
    await act(async () => { hook.result.current.playHls('/live.m3u8', 'native'); });
    emit(video, 'waiting');
    await tick(3000);
    emit(video, 'playing');
    await tick(HLS_STARTUP_POLICY.liveTimeoutMs);
    expect(props.waitForSessionReady).not.toHaveBeenCalled();
    emit(video, 'stalled');
    await tick(3350);
    expect(props.waitForSessionReady).toHaveBeenCalledTimes(1);
  });

  it.each(['unmount', 'reset', 'session', 'source', 'direct'] as const)('does not recover a stale attachment after %s', async (replacement) => {
    const { video, props } = setup();
    const hook = renderHook(() => usePlaybackEngine(props), { wrapper });
    await act(async () => { hook.result.current.playHls('/live.m3u8', 'native'); });
    emit(video, 'waiting');
    await tick(1000);
    await act(async () => {
      if (replacement === 'unmount') hook.unmount();
      if (replacement === 'reset') hook.result.current.resetPlaybackEngine();
      if (replacement === 'session') props.sessionIdRef.current = 'session-b';
      if (replacement === 'source') hook.result.current.playHls('/next.m3u8', 'native');
      if (replacement === 'direct') hook.result.current.playDirectMp4('/movie.mp4');
    });
    await tick(HLS_STARTUP_POLICY.liveTimeoutMs + 1000);
    expect(props.waitForSessionReady).not.toHaveBeenCalled();
  });

  it('keeps actual media-error recovery immediate during startup', async () => {
    const { video, props } = setup();
    const hook = renderHook(() => usePlaybackEngine(props), { wrapper });
    await act(async () => { hook.result.current.playHls('/live.m3u8', 'native'); });
    Object.defineProperty(video, 'error', { value: { code: 3, message: 'decode failed' } });
    emit(video, 'error');
    await tick(850);
    expect(props.waitForSessionReady).toHaveBeenCalledTimes(1);
  });

  it('protects startup invoked by a parent effect', async () => {
    const { video, props } = setup();
    let start: (() => void) | undefined;
    function Child() {
      const engine = usePlaybackEngine(props);
      start = () => engine.playHls('/live.m3u8', 'native');
      return null;
    }
    function Parent() {
      useEffect(() => { start?.(); }, []);
      return <Child />;
    }
    await act(async () => { render(<Parent />, { wrapper }); });
    emit(video, 'waiting');
    await tick(4000);
    expect(props.waitForSessionReady).not.toHaveBeenCalled();
  });
});
