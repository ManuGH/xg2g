import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { TFunction } from 'i18next';
import { usePlaybackEngine } from './usePlaybackEngine';

vi.mock('./lib/hlsRuntime', () => ({ default: { isSupported: () => false } }));

function setup() {
  const video = document.createElement('video');
  const assigned: string[] = [];
  const proto = Object.getOwnPropertyDescriptor(HTMLMediaElement.prototype, 'src')!;
  Object.defineProperty(video, 'src', {
    configurable: true,
    get: () => proto.get!.call(video),
    set: (value: string) => { assigned.push(value); proto.set!.call(video, value); },
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
  return { video, assigned, props };
}

describe('reattachNativeSource', () => {
  beforeEach(() => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue();
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {});
  });
  afterEach(() => vi.restoreAllMocks());

  it('re-assigns the current native source on the same session', async () => {
    const { assigned, props } = setup();
    const { result } = renderHook(() => usePlaybackEngine(props));
    await act(async () => { result.current.playHls('http://x/live.m3u8', 'native'); });
    expect(assigned).toEqual(['http://x/live.m3u8']);

    let reattached = false;
    await act(async () => { reattached = result.current.reattachNativeSource(); });

    expect(reattached).toBe(true);
    expect(assigned).toEqual(['http://x/live.m3u8', 'http://x/live.m3u8']);
    expect(props.setStatus).toHaveBeenCalledWith('buffering');
    expect(props.waitForSessionReady).not.toHaveBeenCalled();
  });

  it('declines without a native source or after teardown', async () => {
    const { assigned, props } = setup();
    const { result } = renderHook(() => usePlaybackEngine(props));
    expect(result.current.reattachNativeSource()).toBe(false);

    await act(async () => { result.current.playHls('http://x/live.m3u8', 'native'); });
    props.isTeardownRef.current = true;
    expect(result.current.reattachNativeSource()).toBe(false);
    expect(assigned).toEqual(['http://x/live.m3u8']);
  });
});
