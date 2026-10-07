// Native-HLS (iOS) foreground recovery after a long background (screen lock).
// Live native playback, user unmutes, WebKit reports a short 'waiting' while
// several seconds are still buffered, then 'playing' again. Before the fix the
// waiting masked the picture at once and the veil's temporary mute revoked the
// user's unmute; if every unmute costs WebKit a waiting, the veil re-triggered
// itself.
import { useRef } from 'react';
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { HlsInstanceRef, V3PlayerProps, VideoElementRef } from '../../types/v3-player';
import { usePlaybackOrchestrator } from './usePlaybackOrchestrator';

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

describe('native HLS foreground reattach', () => {
  let now = 1_000;
  let visibility: DocumentVisibilityState = 'visible';
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
    vi.spyOn(performance, 'now').mockImplementation(() => now);
    Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => visibility });
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
        return ok({ acknowledged: true, leaseExpiresAt: '2026-10-07T23:00:00Z' });
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

  it('keeps the plain resume after a short background', async () => {
    await startLivePlayback();
    const before = srcAssignments.length;

    await background(5_000);

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
});
