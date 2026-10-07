// Characterization of the native-HLS (iOS) "unmute -> black veil" path.
// Live native playback, user unmutes, WebKit reports a short 'waiting' while
// several seconds are still buffered, then 'playing' again.
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

describe('native HLS unmute veil (characterization)', () => {
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

  it('reaches steady native live playback without a mask', async () => {
    await startLivePlayback();
    expect(latest.playbackState.status).toBe('playing');
    expect(latest.viewState.showNativeBufferingMask).toBe(false);
  });

  it('today: a short waiting after unmute masks the picture and re-mutes the user until the veil drops', async () => {
    await startLivePlayback();
    video.muted = true;
    act(() => { latest.actions.toggleMute(); });
    expect(video.muted).toBe(false);

    // WebKit: readyState drops to HAVE_CURRENT_DATA while 3 s are still buffered.
    media.readyState = 2;
    emit('waiting');
    expect(latest.playbackState.status).toBe('buffering');
    expect(latest.viewState.showNativeBufferingMask).toBe(true);
    expect(video.muted).toBe(true);

    // Playback resumes 200 ms later.
    await play(200, { advancing: false });
    media.readyState = 4;
    emit('playing');

    let maskedMs = 0;
    let mutedWhileMaskedMs = 0;
    for (let t = 0; t < 6000 && latest.viewState.showNativeBufferingMask; t += 50) {
      if (video.muted) mutedWhileMaskedMs += 50;
      maskedMs += 50;
      await play(50);
    }
    // The reveal watchdog (not the 2300 ms rebuffer veil) ends the mask.
    expect(maskedMs).toBeLessThan(1000);
    expect(mutedWhileMaskedMs).toBe(maskedMs);
    expect(video.muted).toBe(false);
    console.info(`[characterization] mask after resume: ${maskedMs} ms`);
  });

  it('today: if every unmute costs WebKit a short waiting, the veil re-mutes and re-triggers itself', async () => {
    await startLivePlayback();

    // Hypothesis model of iOS: each muted->unmuted transition causes a 200 ms waiting.
    let mutedValue = false;
    let waitingCount = 0;
    Object.defineProperty(video, 'muted', {
      configurable: true,
      get: () => mutedValue,
      set: (next: boolean) => {
        const unmuting = mutedValue && !next;
        mutedValue = next;
        if (!unmuting) return;
        window.setTimeout(() => {
          waitingCount += 1;
          media.readyState = 2;
          video.dispatchEvent(new Event('waiting'));
          window.setTimeout(() => {
            media.readyState = 4;
            video.dispatchEvent(new Event('playing'));
          }, 200);
        }, 20);
      },
    });

    video.muted = true;
    act(() => { latest.actions.toggleMute(); });

    let maskedMs = 0;
    for (let t = 0; t < 5000; t += 50) {
      if (latest.viewState.showNativeBufferingMask) maskedMs += 50;
      await play(50);
    }
    console.info(`[characterization] unmute-waiting model: ${waitingCount} waiting cycles, masked ${maskedMs} ms of 5000`);
    // One user unmute turns into repeated veil cycles.
    expect(waitingCount).toBeGreaterThanOrEqual(3);
  });
});
