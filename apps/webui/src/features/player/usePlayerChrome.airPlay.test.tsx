import { StrictMode, useLayoutEffect, useRef } from 'react';
import { fireEvent, render } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { HlsInstanceRef, SafariVideoElement } from '../../types/v3-player';
import { usePlayerChrome } from './usePlayerChrome';

let video: SafariVideoElement;
let latest: ReturnType<typeof usePlayerChrome>;
function Child() {
  useLayoutEffect(() => { video.webkitCurrentPlaybackTargetIsWireless = true; }, []);
  return null;
}
function Harness({ onChange, child = false }: { onChange: (wireless: boolean) => void; child?: boolean }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef(video);
  const hlsRef = useRef<HlsInstanceRef>(null);
  const userPauseIntentRef = useRef(false);
  const lastDecodedRef = useRef(0);
  latest = usePlayerChrome({ autoStart: false, containerRef, videoRef, hlsRef, userPauseIntentRef, lastDecodedRef,
    playbackMode: 'LIVE', durationSeconds: null, canSeek: false, startUnix: null, setStatus: vi.fn(),
    allowNativeFullscreen: false, shouldForceNativeMobileHls: () => false, canUseDesktopWebKitFullscreen: () => false,
    onAirPlayWirelessChange: onChange });
  return <div ref={containerRef}>{child && <Child />}</div>;
}
afterEach(() => vi.restoreAllMocks());
describe.each([false, true])('AirPlay route listeners (StrictMode=%s)', (strict) => {
  function tree(onChange: (wireless: boolean) => void, child = false) {
    const ui = <Harness onChange={onChange} child={child} />;
    return strict ? <StrictMode>{ui}</StrictMode> : ui;
  }
  function setup(wireless = false) {
    video = document.createElement('video') as SafariVideoElement;
    video.webkitShowPlaybackTargetPicker = vi.fn();
    video.webkitCurrentPlaybackTargetIsWireless = wireless;
  }
  it('synchronizes an already wireless element at mount', () => {
    setup(true);
    const callback = vi.fn();
    render(tree(callback));
    expect(latest.isAirPlayActive).toBe(true);
    expect(callback).toHaveBeenCalledWith(true);
  });
  it('uses the latest callback without resubscribing and removes listeners on unmount', () => {
    setup();
    const add = vi.spyOn(video, 'addEventListener');
    const first = vi.fn();
    const second = vi.fn();
    const mounted = render(tree(first));
    const wirelessSubscriptions = () => add.mock.calls.filter(([name]) => name === 'webkitcurrentplaybacktargetiswirelesschanged').length;
    const subscriptions = wirelessSubscriptions();
    mounted.rerender(tree(second));
    expect(wirelessSubscriptions()).toBe(subscriptions);
    video.webkitCurrentPlaybackTargetIsWireless = true;
    fireEvent(video, new Event('webkitcurrentplaybacktargetiswirelesschanged'));
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledExactlyOnceWith(true);
    expect(latest.isAirPlayActive).toBe(true);
    mounted.unmount();
    fireEvent(video, new Event('webkitcurrentplaybacktargetiswirelesschanged'));
    expect(second).toHaveBeenCalledTimes(1);
  });
  it('observes route state set in a child layout effect before parent passive effects', () => {
    setup();
    const callback = vi.fn();
    render(tree(callback, true));
    expect(latest.isAirPlayActive).toBe(true);
    expect(callback).toHaveBeenCalledWith(true);
  });
});
