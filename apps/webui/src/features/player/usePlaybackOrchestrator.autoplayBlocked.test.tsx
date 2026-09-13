// Regression for the pull-request-959 review finding: with the in-player overlay (onClose set), an
// autoplay rejection must clear the startup spinner and expose the playback chrome.
// Startup 'ready' before playback (the flicker fix) must still count as startup.
import { useRef } from 'react';
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { HlsInstanceRef, V3PlayerProps, VideoElementRef } from '../../types/v3-player';
import { usePlaybackOrchestrator } from './usePlaybackOrchestrator';

// Force the native engine so playback goes through the loadedmetadata → play() path.
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

describe('autoplay rejection under the in-player overlay', () => {
  let latest!: ReturnType<typeof usePlaybackOrchestrator>;
  let video!: HTMLVideoElement;
  let playCalls = 0;

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
            if (el) {
              video = el;
              el.load = vi.fn();
              el.pause = vi.fn();
              el.play = vi.fn().mockImplementation(() => {
                playCalls += 1;
                return Promise.reject(Object.assign(new Error('blocked'), { name: 'NotAllowedError' }));
              });
            }
            videoRef.current = el;
          }}
        />
      </div>
    );
  }

  beforeEach(() => {
    vi.useFakeTimers();
    playCalls = 0;
    // jsdom reports no HLS support; claim native HLS so the engine decision picks 'native'.
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    let sessionCounter = 0;
    vi.stubGlobal('fetch', vi.fn().mockImplementation((url: string) => {
      const u = String(url);
      const ok = (body: unknown, status = 200) =>
        Promise.resolve({ ok: status < 300, status, headers: new Headers(), json: async () => body, text: async () => JSON.stringify(body) });
      if (u.includes('/live/stream-info')) {
        return ok({ mode: 'direct_stream', playbackDecisionToken: 'tok', decision: { mode: 'direct_stream', playbackDecisionToken: 'tok' } });
      }
      if (u.includes('/intents')) {
        sessionCounter += 1;
        return ok({ sessionId: `sess-autoplay-${sessionCounter}` }, 202);
      }
      if (u.includes('/heartbeat')) {
        return ok({ acknowledged: true, leaseExpiresAt: '2026-09-13T23:00:00Z' });
      }
      if (u.includes('/sessions/')) {
        return ok({ state: 'READY', sessionId: `sess-autoplay-${sessionCounter}`, playbackUrl: 'http://test.local/live.m3u8', leaseExpiresAt: '2026-09-13T23:00:00Z', heartbeatIntervalSeconds: 15 });
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

  it('clears the startup spinner and shows the playback chrome once autoplay is rejected (muted retry included)', async () => {
    render(
      <Harness
        props={{ autoStart: false, sRef: '1:0:1:AUTOPLAY', apiBase: 'http://test.local', onClose: () => {} } as unknown as V3PlayerProps}
      />,
    );

    await act(async () => {
      await latest.actions.startStream('1:0:1:AUTOPLAY');
      await vi.advanceTimersByTimeAsync(300);
    });

    // Startup: 'ready' before any play attempt still counts as startup (the PR-959 flicker fix).
    expect(latest.playbackState.status).toBe('ready');
    expect(playCalls).toBe(0);
    expect(latest.viewState.showPlaybackChrome).toBe(false);

    // Native path: metadata arrives, unmuted play is rejected, muted retry is rejected too.
    await act(async () => {
      video.dispatchEvent(new Event('loadedmetadata'));
      await vi.advanceTimersByTimeAsync(50);
    });

    expect(playCalls).toBe(2);
    expect(video.muted).toBe(true);
    expect(latest.playbackState.status).toBe('ready');
    // The resting 'ready' after a rejected autoplay must expose the controls under the overlay.
    expect(latest.viewState.showPlaybackChrome).toBe(true);
  });
});
