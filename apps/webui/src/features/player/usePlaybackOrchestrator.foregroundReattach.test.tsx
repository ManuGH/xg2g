// Native-HLS (iOS) foreground recovery after a long background (screen lock).
// Live native playback, user unmutes, WebKit reports a short 'waiting' while
// several seconds are still buffered, then 'playing' again. Before the fix the
// waiting masked the picture at once and the veil's temporary mute revoked the
// user's unmute; if every unmute costs WebKit a waiting, the veil re-triggered
// itself.
import { StrictMode, useRef } from 'react';
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { HlsInstanceRef, V3PlayerProps, VideoElementRef } from '../../types/v3-player';
import { LOCK_PAUSE_SETTLE_MS, usePlaybackOrchestrator } from './usePlaybackOrchestrator';

vi.mock('./lib/hlsRuntime', () => {
  function HlsMock(this: any) {
    this.loadSource = vi.fn();
    this.attachMedia = vi.fn();
    this.on = vi.fn();
    this.startLoad = vi.fn();
    this.destroy = vi.fn();
  }
  (HlsMock as any).isSupported = vi.fn().mockReturnValue(false);
  (HlsMock as any).Events = { MANIFEST_PARSED: 'p', LEVEL_SWITCHED: 'l', FRAG_BUFFERED: 'f', ERROR: 'e' };
  return { default: HlsMock };
});

// Mutable media state the fake <video> reports.
const media = {
  currentTime: 100,
  readyState: 4,
  paused: true,
  bufferedAhead: 3,
};

function installMediaState(el: HTMLVideoElement) {
  Object.defineProperties(el, {
    currentTime: { configurable: true, get: () => media.currentTime, set: (v: number) => { media.currentTime = v; } },
    readyState: { configurable: true, get: () => media.readyState },
    paused: { configurable: true, get: () => media.paused },
    buffered: {
      configurable: true,
      get: () => ({
        length: 1,
        start: () => Math.max(0, media.currentTime - 10),
        end: () => media.currentTime + media.bufferedAhead,
      }),
    },
  });
}

describe.each([false, true])('native HLS foreground reattach (StrictMode=%s)', (strict) => {
  let now = 1_000;
  let visibility: DocumentVisibilityState = 'visible';
  let touchPoints = 0;
  let srcAssignments: string[] = [];

  let latest!: ReturnType<typeof usePlaybackOrchestrator>;
  let video!: HTMLVideoElement;

  function Harness({ props }: { props: V3PlayerProps }) {
    const containerRef = useRef<HTMLDivElement>(null);
    const videoRef = useRef<VideoElementRef>(null);
    const hlsRef = useRef<HlsInstanceRef | null>(null);
    const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);
    latest = usePlaybackOrchestrator(props, { containerRef, videoRef, hlsRef, resumePrimaryActionRef });
    return (
      <div ref={containerRef}>
        <video
          ref={(el) => {
            if (el && el !== video) {
              video = el;
              installMediaState(el);
              const proto = Object.getOwnPropertyDescriptor(HTMLMediaElement.prototype, 'src')!;
              Object.defineProperty(el, 'src', {
                configurable: true,
                get: () => proto.get!.call(el),
                set: (value: string) => { srcAssignments.push(value); proto.set!.call(el, value); },
              });
              el.load = vi.fn();
              el.pause = vi.fn().mockImplementation(() => { media.paused = true; });
              el.play = vi.fn().mockImplementation(() => {
                media.paused = false;
                return Promise.resolve();
              });
            }
            videoRef.current = el;
          }}
        />
      </div>
    );
  }

  // Advance fake time while the playhead moves in real time (when not stalled).
  async function play(ms: number, { advancing = true } = {}) {
    const step = 50;
    for (let t = 0; t < ms; t += step) {
      await act(async () => {
        if (advancing && !media.paused) {
          media.currentTime += step / 1000;
          video.dispatchEvent(new Event('timeupdate'));
        }
        await vi.advanceTimersByTimeAsync(step);
      });
    }
  }

  function emit(event: string) {
    act(() => { video.dispatchEvent(new Event(event)); });
  }

  async function startLivePlayback() {
    render(
      <Harness props={{ autoStart: false, sRef: '1:0:1:UNMUTE', apiBase: 'http://test.local' } as unknown as V3PlayerProps} />,
      { wrapper: strict ? StrictMode : undefined },
    );
    await act(async () => {
      await latest.actions.startStream('1:0:1:UNMUTE');
      await vi.advanceTimersByTimeAsync(300);
    });
    await act(async () => {
      video.dispatchEvent(new Event('loadedmetadata'));
      await vi.advanceTimersByTimeAsync(50);
    });
    emit('playing');
    await play(5000);
  }

  beforeEach(() => {
    vi.useFakeTimers();
    now = 1_000;
    visibility = 'visible';
    srcAssignments = [];
    // Wall clock only: performance.now keeps ticking normally here, like an
    // iOS device whose monotonic clock paused during the lock.
    vi.spyOn(Date, 'now').mockImplementation(() => now);
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => visibility });
    touchPoints = 0;
    Object.defineProperty(navigator, 'maxTouchPoints', { configurable: true, get: () => touchPoints });
    Object.defineProperty(document, 'pictureInPictureElement', { configurable: true, value: null, writable: true });
    media.currentTime = 100;
    media.readyState = 4;
    media.paused = true;
    media.bufferedAhead = 3;
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
      const u = String(url);
      const ok = (body: unknown, status = 200) =>
        Promise.resolve({ ok: status < 300, status, headers: new Headers(), json: async () => body, text: async () => JSON.stringify(body) });
      if (u.includes('/live/stream-info')) {
        return ok({ mode: 'direct_stream', playbackDecisionToken: 'tok', decision: { mode: 'direct_stream', playbackDecisionToken: 'tok' } });
      }
      if (u.includes('/intents')) {
        return ok({ sessionId: 'sess-unmute' }, 202);
      }
      if (u.includes('/heartbeat')) {
        return ok({ acknowledged: true, sessionId: 'sess-unmute', leaseExpiresAt: '2026-10-07T23:00:00Z' });
      }
      if (u.includes('/sessions/')) {
        return ok({ state: 'READY', sessionId: 'sess-unmute', playbackUrl: 'http://test.local/live.m3u8', leaseExpiresAt: '2026-10-07T23:00:00Z', heartbeatIntervalSeconds: 15 });
      }
      return ok({});
    }));
  });

  afterEach(() => {
    vi.clearAllTimers();
    cleanup();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  async function background(ms: number) {
    await act(async () => {
      visibility = 'hidden';
      document.dispatchEvent(new Event('visibilitychange'));
      await vi.advanceTimersByTimeAsync(50);
    });
    now += ms;
    await act(async () => {
      visibility = 'visible';
      document.dispatchEvent(new Event('visibilitychange'));
      await vi.advanceTimersByTimeAsync(50);
    });
  }

  it('rejoins the live edge by re-attaching the source after a long background', async () => {
    await startLivePlayback();
    const before = srcAssignments.length;
    expect(before).toBeGreaterThan(0);

    await background(25_000);

    expect(srcAssignments.length).toBe(before + 1);
    expect(srcAssignments[srcAssignments.length - 1]).toBe(srcAssignments[before - 1]);
  });

  it('keeps the plain resume after a momentary hide', async () => {
    await startLivePlayback();
    const before = srcAssignments.length;

    await background(1_000);

    expect(srcAssignments.length).toBe(before);
  });

  it('does not touch a stream the user paused before locking', async () => {
    await startLivePlayback();
    act(() => { latest.actions.togglePlayPause(); });
    media.paused = true;
    act(() => { video.dispatchEvent(new Event('pause')); });
    const before = srcAssignments.length;

    await background(25_000);

    expect(srcAssignments.length).toBe(before);
  });

  it('continues at the DVR position the viewer left instead of the live edge', async () => {
    await startLivePlayback();
    media.currentTime = 321;
    const before = srcAssignments.length;

    await background(25_000);
    expect(srcAssignments.length).toBe(before + 1);

    media.currentTime = 400; // the fresh attachment starts near the live edge
    await act(async () => {
      video.dispatchEvent(new Event('loadedmetadata'));
      await vi.advanceTimersByTimeAsync(50);
    });

    expect(media.currentTime).toBe(321);
  });

  // Screen lock: WebKit pauses the video, then the page hides.
  async function lock() {
    await act(async () => {
      media.paused = true;
      video.dispatchEvent(new Event('pause'));
      visibility = 'hidden';
      document.dispatchEvent(new Event('visibilitychange'));
      await vi.advanceTimersByTimeAsync(50);
    });
  }

  async function unlock() {
    await act(async () => {
      visibility = 'visible';
      document.dispatchEvent(new Event('visibilitychange'));
      await vi.advanceTimersByTimeAsync(50);
    });
  }

  // The page sleeps: no events, only the wall clock moves.
  function sleep(ms: number) {
    now += ms;
  }

  // Lock-screen play button: playback resumes while the page stays hidden.
  async function resumeWhileLocked() {
    await act(async () => {
      media.paused = false;
      video.dispatchEvent(new Event('play'));
      video.dispatchEvent(new Event('playing'));
      await vi.advanceTimersByTimeAsync(10);
    });
  }

  async function playWhileLocked(ms: number) {
    const step = 250;
    for (let t = 0; t < ms; t += step) {
      await act(async () => {
        now += step;
        media.currentTime += step / 1000;
        video.dispatchEvent(new Event('timeupdate'));
        await vi.advanceTimersByTimeAsync(step);
      });
    }
  }

  async function finishReattach() {
    media.currentTime = 900; // the fresh attachment starts near the live edge
    await act(async () => {
      video.dispatchEvent(new Event('loadedmetadata'));
      await vi.advanceTimersByTimeAsync(50);
    });
  }

  it('continues where lock-screen playback got to', async () => {
    await startLivePlayback();
    media.currentTime = 321;
    await lock();
    sleep(20_000);
    await resumeWhileLocked();
    await playWhileLocked(30_000);
    const before = srcAssignments.length;

    await unlock();
    expect(srcAssignments.length).toBe(before + 1);
    await finishReattach();

    expect(media.currentTime).toBeCloseTo(351, 1);
  });

  it('keeps the position of a lock-screen pause', async () => {
    await startLivePlayback();
    media.currentTime = 321;
    await lock();
    await resumeWhileLocked();
    await playWhileLocked(10_000);
    await act(async () => {
      media.paused = true;
      video.dispatchEvent(new Event('pause'));
      await vi.advanceTimersByTimeAsync(10);
    });
    sleep(60_000);

    await unlock();
    await finishReattach();

    expect(media.currentTime).toBeCloseTo(331, 1);
  });

  it('ignores the live-edge jump of a player resumed on unlock', async () => {
    await startLivePlayback();
    media.currentTime = 321;
    await lock();
    sleep(300_000);
    // WebKit restores playback before the page reports visible; the cold
    // player lands at the live edge.
    await resumeWhileLocked();
    await act(async () => {
      now += 500;
      media.currentTime = 640;
      video.dispatchEvent(new Event('timeupdate'));
      await vi.advanceTimersByTimeAsync(10);
    });

    await unlock();
    await finishReattach();

    expect(media.currentTime).toBe(321);
  });

  it('ignores a restart at the window start', async () => {
    await startLivePlayback();
    media.currentTime = 321;
    await lock();
    await resumeWhileLocked();
    await act(async () => {
      now += 500;
      media.currentTime = 2;
      video.dispatchEvent(new Event('timeupdate'));
      await vi.advanceTimersByTimeAsync(10);
    });
    sleep(10_000);

    await unlock();
    await finishReattach();

    expect(media.currentTime).toBe(321);
  });

  it('re-anchors on a seek from the lock-screen scrubber', async () => {
    await startLivePlayback();
    media.currentTime = 321;
    await lock();
    await act(async () => {
      media.currentTime = 120;
      video.dispatchEvent(new Event('seeked'));
      await vi.advanceTimersByTimeAsync(10);
    });
    sleep(10_000);

    await unlock();
    await finishReattach();

    expect(media.currentTime).toBe(120);
  });

  it('stops following once the page is visible again', async () => {
    await startLivePlayback();
    media.currentTime = 321;
    await lock();
    sleep(10_000);
    // Visible before React has re-rendered: progress after this point belongs
    // to the foreground and must not move the mark.
    visibility = 'visible';
    await resumeWhileLocked();
    await playWhileLocked(5_000);
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'));
      await vi.advanceTimersByTimeAsync(50);
    });
    await finishReattach();

    expect(media.currentTime).toBe(321);
  });

  type FrameCallback = (now: number, metadata: { mediaTime?: number }) => void;

  // requestVideoFrameCallback on the fake element; present() delivers a frame.
  function installFrameCallbacks() {
    let pending: FrameCallback | null = null;
    Object.assign(video, {
      requestVideoFrameCallback: vi.fn((cb: FrameCallback) => {
        pending = cb;
        return 1;
      }),
      cancelVideoFrameCallback: vi.fn(() => {
        pending = null;
      }),
    });
    return (mediaTime: number) => {
      const cb = pending;
      pending = null;
      act(() => { cb?.(0, { mediaTime }); });
    };
  }

  async function listenOnLockScreen() {
    await startLivePlayback();
    media.currentTime = 321;
    await lock();
    await resumeWhileLocked();
    await playWhileLocked(30_000);
  }

  it('keeps lock-screen playback running when its picture comes back', async () => {
    await listenOnLockScreen();
    const present = installFrameCallbacks();
    const before = srcAssignments.length;

    await unlock();
    present(351);
    present(351.04);
    await play(5_000);

    expect(srcAssignments.length).toBe(before);
    expect(media.paused).toBe(false);
  });

  it('re-attaches where playback is when the picture does not come back', async () => {
    await listenOnLockScreen();
    installFrameCallbacks();
    const before = srcAssignments.length;

    await unlock();
    await play(2_400);
    expect(srcAssignments.length).toBe(before);
    await play(200);
    expect(srcAssignments.length).toBe(before + 1);
    await finishReattach();

    // 351 at unlock plus the 2.5 s the check waited while audio played on.
    expect(media.currentTime).toBeCloseTo(353.5, 1);
  });

  it('does not re-attach after the viewer paused during the check', async () => {
    await listenOnLockScreen();
    installFrameCallbacks();
    const before = srcAssignments.length;

    await unlock();
    act(() => { latest.actions.togglePlayPause(); });
    await play(5_000);

    expect(srcAssignments.length).toBe(before);
  });

  it('drops the check when the page hides again', async () => {
    await listenOnLockScreen();
    installFrameCallbacks();
    const before = srcAssignments.length;

    await unlock();
    await act(async () => {
      visibility = 'hidden';
      document.dispatchEvent(new Event('visibilitychange'));
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });

    expect(srcAssignments.length).toBe(before);
  });

  it('drops the check on unmount', async () => {
    await listenOnLockScreen();
    installFrameCallbacks();
    const before = srcAssignments.length;

    await unlock();
    cleanup();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5_000);
    });

    expect(srcAssignments.length).toBe(before);
  });

  // Touch WebKit with native HLS, i.e. an iPhone or iPad.
  function asIPhone() {
    touchPoints = 5;
    Object.assign(video, { webkitEnterFullscreen: vi.fn() });
  }

  // WebKit skipped its lock pause: the page hides while the stream plays on.
  async function hideWhilePlaying() {
    await act(async () => {
      visibility = 'hidden';
      document.dispatchEvent(new Event('visibilitychange'));
    });
  }

  async function wait(ms: number) {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(ms);
    });
  }

  const pauseCalls = () => (video.pause as ReturnType<typeof vi.fn>).mock.calls.length;

  it('pauses a stream WebKit left playing under the lock', async () => {
    await startLivePlayback();
    asIPhone();
    media.currentTime = 321;
    const before = pauseCalls();

    await hideWhilePlaying();
    await wait(LOCK_PAUSE_SETTLE_MS - 1);
    expect(pauseCalls()).toBe(before);
    await wait(1);
    expect(pauseCalls()).toBe(before + 1);
    expect(media.paused).toBe(true);

    // Not the viewer's pause: the return re-attaches at the pause point.
    const sources = srcAssignments.length;
    sleep(30_000);
    await unlock();
    expect(srcAssignments.length).toBe(sources + 1);
    await finishReattach();
    expect(media.currentTime).toBe(321);
  });

  it('leaves a stream WebKit already paused alone', async () => {
    await startLivePlayback();
    asIPhone();
    const before = pauseCalls();

    await lock();
    await wait(LOCK_PAUSE_SETTLE_MS * 3);

    expect(pauseCalls()).toBe(before);
  });

  it.each([
    ['picture-in-picture', () => { (document as { pictureInPictureElement: Element | null }).pictureInPictureElement = video; }],
    ['webkit picture-in-picture', () => { Object.assign(video, { webkitPresentationMode: 'picture-in-picture' }); }],
    ['AirPlay', () => { Object.assign(video, { webkitCurrentPlaybackTargetIsWireless: true }); }],
  ])('keeps playing in %s', async (_name, enable) => {
    await startLivePlayback();
    asIPhone();
    enable();
    const before = pauseCalls();

    await hideWhilePlaying();
    await wait(LOCK_PAUSE_SETTLE_MS * 3);

    expect(pauseCalls()).toBe(before);
  });

  it('does not pause when the page is visible again in time', async () => {
    await startLivePlayback();
    asIPhone();
    const before = pauseCalls();

    await hideWhilePlaying();
    await wait(LOCK_PAUSE_SETTLE_MS / 2);
    await unlock();
    await wait(LOCK_PAUSE_SETTLE_MS * 3);

    expect(pauseCalls()).toBe(before);
  });

  it('does not pause on a desktop without touch input', async () => {
    await startLivePlayback();
    Object.assign(video, { webkitEnterFullscreen: vi.fn() });
    const before = pauseCalls();

    await hideWhilePlaying();
    await wait(LOCK_PAUSE_SETTLE_MS * 3);

    expect(pauseCalls()).toBe(before);
  });

  it('lets a lock-screen play after the lock pause run on', async () => {
    await startLivePlayback();
    asIPhone();
    await hideWhilePlaying();
    await wait(LOCK_PAUSE_SETTLE_MS);
    expect(media.paused).toBe(true);
    const before = pauseCalls();

    await resumeWhileLocked();
    await playWhileLocked(10_000);

    expect(pauseCalls()).toBe(before);
    expect(media.paused).toBe(false);
  });

  it('does not pause a stream that was paused and resumed meanwhile', async () => {
    await startLivePlayback();
    asIPhone();
    await hideWhilePlaying();
    // Lock-screen pause, then play, both before the settle time is over.
    await act(async () => {
      media.paused = true;
      video.dispatchEvent(new Event('pause'));
      await vi.advanceTimersByTimeAsync(LOCK_PAUSE_SETTLE_MS / 4);
    });
    await resumeWhileLocked();
    const before = pauseCalls();

    await playWhileLocked(LOCK_PAUSE_SETTLE_MS * 3);

    expect(pauseCalls()).toBe(before);
    expect(media.paused).toBe(false);
  });

  it('does not pause after unmount', async () => {
    await startLivePlayback();
    asIPhone();
    const before = pauseCalls();

    await hideWhilePlaying();
    cleanup();
    await wait(LOCK_PAUSE_SETTLE_MS * 3);

    expect(pauseCalls()).toBe(before);
  });
});
