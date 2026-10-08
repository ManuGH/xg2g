import { StrictMode, useEffect, useRef } from 'react';
import { act, render, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  usePlaybackEngine,
  PLAYBACK_INFO_CODE_HLSJS_RENDER_HEARTBEAT,
} from './usePlaybackEngine';
import { HLS_STARTUP_POLICY } from './playbackEnginePolicy';

const { MockHls, hlsInstances } = vi.hoisted(() => {
  const instances: any[] = [];

  class MockHls {
    handlers = new Map<string, Array<(...args: any[]) => void>>();
    destroy = vi.fn(() => {
      this.handlers.clear();
    });
    recoverMediaError = vi.fn();
    loadSource = vi.fn();
    attachMedia = vi.fn();
    startLoad = vi.fn();
    currentLevel = -1;
    startLevel = -1;
    audioTrack = -1;
    levels: any[] = [];

    on(event: string, handler: (...args: any[]) => void) {
      const list = this.handlers.get(event) ?? [];
      list.push(handler);
      this.handlers.set(event, list);
    }

    emit(event: string, data?: any) {
      const list = this.handlers.get(event) ?? [];
      for (const h of list) {
        h(event, data);
      }
    }

    constructor() {
      instances.push(this);
    }

    static isSupported = vi.fn().mockReturnValue(true);
    static Events = {
      LEVEL_SWITCHED: 'hlsLevelSwitched',
      MANIFEST_PARSED: 'hlsManifestParsed',
      BUFFER_APPENDED: 'hlsBufferAppended',
      BUFFER_CODECS: 'hlsBufferCodecs',
      LEVEL_LOADED: 'hlsLevelLoaded',
      FRAG_LOADING: 'hlsFragLoading',
      FRAG_LOADED: 'hlsFragLoaded',
      AUDIO_TRACKS_UPDATED: 'hlsAudioTracksUpdated',
      AUDIO_TRACK_SWITCHING: 'hlsAudioTrackSwitching',
      AUDIO_TRACK_SWITCHED: 'hlsAudioTrackSwitched',
      ERROR: 'hlsError',
    };
    static ErrorTypes = {
      NETWORK_ERROR: 'networkError',
      MEDIA_ERROR: 'mediaError',
    };
  }

  return { MockHls, hlsInstances: instances };
});

vi.mock('./lib/hlsRuntime', () => {
  return {
    default: MockHls,
  };
});

function setMockBuffered(
  video: HTMLVideoElement,
  ranges: Array<{ start: number; end: number }>,
) {
  Object.defineProperty(video, 'buffered', {
    configurable: true,
    get: () => ({
      length: ranges.length,
      start: (i: number) => ranges[i]?.start ?? 0,
      end: (i: number) => ranges[i]?.end ?? 0,
    }),
  });
}

function makeEngineProps(
  video: HTMLVideoElement,
  setStatus: ReturnType<typeof vi.fn> = vi.fn(),
  options: Record<string, any> = {},
) {
  return {
    videoRef: { current: video },
    hlsRef: { current: null },
    sessionIdRef: { current: 'sess-test-cushion-1' },
    isTeardownRef: { current: false },
    lastDecodedRef: { current: 0 },
    playbackEpochRef: { current: 0 },
    t: ((key: string) => key) as any,
    reportError: vi.fn().mockResolvedValue(undefined),
    waitForSessionReady: vi.fn().mockResolvedValue({} as any),
    shouldPreferNativeHls: vi.fn(() => false),
    setStats: vi.fn(),
    setStatus,
    clearPlaybackFailure: vi.fn(),
    reportPlaybackFailure: vi.fn(),
    ...options,
  } as any;
}

describe('usePlaybackEngine live startup cushion and start gate', () => {
  let playSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    vi.useFakeTimers();
    hlsInstances.length = 0;
    playSpy = vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined as never);
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {});
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  describe('live playback start cushion', () => {
    it.each([false, true])('allows parent effects to start a child engine with cadence protection (StrictMode=%s)', (strict) => {
      const video = document.createElement('video');
      Object.defineProperty(video, 'readyState', { value: 4, configurable: true });
      setMockBuffered(video, [{ start: 0, end: 12 }]);
      const props = makeEngineProps(video);
      function Child({ start }: { start: { current: (() => void) | null } }) {
        const engine = usePlaybackEngine(props);
        useEffect(() => {
          start.current = () => engine.playHls('/api/v3/hls/child/live.m3u8');
          return () => { start.current = null; };
        }, [engine.playHls, start]);
        return null;
      }
      function Parent() {
        const start = useRef<(() => void) | null>(null);
        useEffect(() => { start.current?.(); }, []);
        return <Child start={start} />;
      }
      const { unmount } = render(strict ? <StrictMode><Parent /></StrictMode> : <Parent />);
      const hls = hlsInstances[hlsInstances.length - 1];
      act(() => {
        hls.emit(MockHls.Events.MANIFEST_PARSED, { levels: [], audioTracks: [] });
        hls.emit(MockHls.Events.LEVEL_LOADED, { details: { live: true, targetduration: 10 } });
        hls.emit(MockHls.Events.BUFFER_APPENDED);
      });
      expect(playSpy).not.toHaveBeenCalled();
      setMockBuffered(video, [{ start: 0, end: 20 }]);
      act(() => hls.emit(MockHls.Events.BUFFER_APPENDED));
      expect(playSpy).toHaveBeenCalledTimes(1);
      unmount();
      act(() => vi.advanceTimersByTime(60_000));
      expect(playSpy).toHaveBeenCalledTimes(1);
    });
    it.each([false, true])('retains two long segments before playing (StrictMode=%s)', (strict) => {
      const video = document.createElement('video');
      Object.defineProperty(video, 'readyState', { value: 4, configurable: true });
      setMockBuffered(video, [{ start: 0, end: 12 }]);
      const props = makeEngineProps(video);
      const { result, rerender, unmount } = renderHook(() => usePlaybackEngine(props), {
        wrapper: strict ? StrictMode : undefined,
      });
      act(() => result.current.playHls('/api/v3/hls/long/live.m3u8'));
      const hls = hlsInstances[hlsInstances.length - 1];
      act(() => {
        hls.emit(MockHls.Events.MANIFEST_PARSED, { levels: [], audioTracks: [] });
        hls.emit(MockHls.Events.LEVEL_LOADED, {
          details: { live: true, targetduration: 10, totalduration: 60, fragments: [{}] },
        });
        hls.emit(MockHls.Events.BUFFER_APPENDED);
      });
      expect(hls.targetLatency).toBe(30);
      expect(playSpy).not.toHaveBeenCalled();
      rerender();
      act(() => vi.advanceTimersByTime(HLS_STARTUP_POLICY.liveTimeoutMs));
      expect(playSpy).not.toHaveBeenCalled();
      setMockBuffered(video, [{ start: 0, end: 20 }]);
      act(() => hls.emit(MockHls.Events.BUFFER_APPENDED));
      expect(playSpy).toHaveBeenCalledTimes(1);
      unmount();
      act(() => vi.advanceTimersByTime(60_000));
      expect(playSpy).toHaveBeenCalledTimes(1);
    });

    it('does not count disconnected future ranges as playable startup reserve', () => {
      const video = document.createElement('video');
      Object.defineProperty(video, 'readyState', { value: 4, configurable: true });
      setMockBuffered(video, [{ start: 0, end: 2 }, { start: 10, end: 30 }]);
      const props = makeEngineProps(video);
      const { result } = renderHook(() => usePlaybackEngine(props));
      act(() => result.current.playHls('/api/v3/hls/gap/live.m3u8'));
      const hls = hlsInstances[hlsInstances.length - 1];
      act(() => hls.emit(MockHls.Events.BUFFER_APPENDED));
      expect(playSpy).not.toHaveBeenCalled();
    });

    it('keeps the long-cadence deadline finite through repeated playlist polls', () => {
      const video = document.createElement('video');
      Object.defineProperty(video, 'readyState', { value: 4, configurable: true });
      setMockBuffered(video, [{ start: 0, end: 8 }]);
      const props = makeEngineProps(video);
      const { result } = renderHook(() => usePlaybackEngine(props));
      act(() => result.current.playHls('/api/v3/hls/long/live.m3u8'));
      const hls = hlsInstances[hlsInstances.length - 1];
      act(() => hls.emit(MockHls.Events.MANIFEST_PARSED, { levels: [], audioTracks: [] }));
      for (let i = 0; i < 4; i++) {
        act(() => {
          hls.emit(MockHls.Events.LEVEL_LOADED, { details: { live: true, targetduration: 10 } });
          vi.advanceTimersByTime(7_000);
        });
        expect(playSpy).not.toHaveBeenCalled();
      }
      act(() => vi.advanceTimersByTime(2_000));
      expect(playSpy).toHaveBeenCalledTimes(1);
    });

    it.each([false, true])('ignores captured callbacks after executor replacement and unmount (StrictMode=%s)', (strict) => {
      const video = document.createElement('video');
      Object.defineProperty(video, 'readyState', { value: 4, configurable: true });
      setMockBuffered(video, [{ start: 0, end: 2 }]);
      const props = makeEngineProps(video);
      const { result, unmount } = renderHook(() => usePlaybackEngine(props), { wrapper: strict ? StrictMode : undefined });
      act(() => result.current.playHls('/api/v3/hls/first/live.m3u8'));
      const old = hlsInstances[hlsInstances.length - 1];
      const oldManifest = old.handlers.get(MockHls.Events.MANIFEST_PARSED)[0];
      const oldLevel = old.handlers.get(MockHls.Events.LEVEL_LOADED)[0];
      act(() => result.current.playHls('/api/v3/hls/second/live.m3u8'));
      const active = hlsInstances[hlsInstances.length - 1];
      act(() => {
        active.emit(MockHls.Events.MANIFEST_PARSED, { levels: [], audioTracks: [] });
        oldManifest('', { levels: [], audioTracks: [] });
        oldLevel('', { details: { live: true, targetduration: 10 } });
        vi.advanceTimersByTime(15_000);
      });
      expect(playSpy).toHaveBeenCalledTimes(1);
      const activeManifest = active.handlers.get(MockHls.Events.MANIFEST_PARSED)[0];
      unmount();
      act(() => {
        activeManifest('', { levels: [], audioTracks: [] });
        vi.advanceTimersByTime(60_000);
      });
      expect(playSpy).toHaveBeenCalledTimes(1);
      expect(vi.getTimerCount()).toBe(0);
    });

    it('keeps live start gate closed at 1..5.9s buffered, and opens when buffered reaches >= 6s', () => {
      const video = document.createElement('video');
      Object.defineProperty(video, 'readyState', { value: 4, configurable: true, writable: true });
      Object.defineProperty(video, 'currentTime', { value: 0, configurable: true, writable: true });
      setMockBuffered(video, [{ start: 0, end: 1.0 }]);

      const { result } = renderHook(() => usePlaybackEngine(makeEngineProps(video)));

      act(() => {
        result.current.playHls('/api/v3/hls/stream-test/live.m3u8');
      });

      const hls = hlsInstances[hlsInstances.length - 1];
      expect(hls).toBeDefined();

      act(() => {
        hls.emit(MockHls.Events.MANIFEST_PARSED, { levels: [{ frameRate: 50 }], audioTracks: [] });
      });

      act(() => {
        hls.emit(MockHls.Events.LEVEL_LOADED, {
          details: { live: true, totalduration: 60, fragments: [{}] },
        });
      });

      // At 1.0s buffered: gate stays closed (would fail if bufferTargetSeconds was 1)
      act(() => {
        hls.emit(MockHls.Events.BUFFER_APPENDED);
      });
      expect(playSpy).not.toHaveBeenCalled();

      // At 3.0s buffered: gate stays closed
      setMockBuffered(video, [{ start: 0, end: 3.0 }]);
      act(() => {
        hls.emit(MockHls.Events.BUFFER_APPENDED);
      });
      expect(playSpy).not.toHaveBeenCalled();

      // At 5.9s buffered: gate stays closed
      setMockBuffered(video, [{ start: 0, end: 5.9 }]);
      act(() => {
        hls.emit(MockHls.Events.BUFFER_APPENDED);
      });
      expect(playSpy).not.toHaveBeenCalled();

      // At 6.0s buffered: gate opens!
      setMockBuffered(video, [{ start: 0, end: 6.0 }]);
      act(() => {
        hls.emit(MockHls.Events.BUFFER_APPENDED);
      });
      expect(playSpy).toHaveBeenCalledTimes(1);
    });

    it('opens live start gate on 15s timeout with whatever is buffered', () => {
      const video = document.createElement('video');
      Object.defineProperty(video, 'readyState', { value: 4, configurable: true, writable: true });
      Object.defineProperty(video, 'currentTime', { value: 0, configurable: true, writable: true });
      setMockBuffered(video, [{ start: 0, end: 2.0 }]);

      const { result } = renderHook(() => usePlaybackEngine(makeEngineProps(video)));

      act(() => {
        result.current.playHls('/api/v3/hls/stream-test/live.m3u8');
      });

      const hls = hlsInstances[hlsInstances.length - 1];

      act(() => {
        hls.emit(MockHls.Events.MANIFEST_PARSED, { levels: [], audioTracks: [] });
      });

      act(() => {
        hls.emit(MockHls.Events.LEVEL_LOADED, {
          details: { live: true, totalduration: 60, fragments: [{}] },
        });
      });

      act(() => {
        hls.emit(MockHls.Events.BUFFER_APPENDED);
      });
      expect(playSpy).not.toHaveBeenCalled();

      // Advance by 14.9s: timeout should not have fired yet
      act(() => {
        vi.advanceTimersByTime(HLS_STARTUP_POLICY.liveTimeoutMs - 100);
      });
      expect(playSpy).not.toHaveBeenCalled();

      // Advance past 15s: timeout fires and opens gate
      act(() => {
        vi.advanceTimersByTime(200);
      });
      expect(playSpy).toHaveBeenCalledTimes(1);
    });

    it('starts immediately when joining a running live session already buffered >= 6s', () => {
      const video = document.createElement('video');
      Object.defineProperty(video, 'readyState', { value: 4, configurable: true, writable: true });
      Object.defineProperty(video, 'currentTime', { value: 0, configurable: true, writable: true });
      setMockBuffered(video, [{ start: 0, end: 8.5 }]);

      const { result } = renderHook(() => usePlaybackEngine(makeEngineProps(video)));

      act(() => {
        result.current.playHls('/api/v3/hls/stream-test/live.m3u8');
      });

      const hls = hlsInstances[hlsInstances.length - 1];

      act(() => {
        hls.emit(MockHls.Events.MANIFEST_PARSED, { levels: [], audioTracks: [] });
      });

      act(() => {
        hls.emit(MockHls.Events.LEVEL_LOADED, {
          details: { live: true, totalduration: 60, fragments: [{}] },
        });
      });

      expect(playSpy).toHaveBeenCalledTimes(1);
    });
  });

  describe('VOD playback start gate', () => {
    it('opens VOD start gate on LEVEL_LOADED with 0 s buffered', () => {
      const video = document.createElement('video');
      Object.defineProperty(video, 'readyState', { value: 4, configurable: true, writable: true });
      Object.defineProperty(video, 'currentTime', { value: 0, configurable: true, writable: true });
      setMockBuffered(video, []); // 0s buffered

      const { result } = renderHook(() => usePlaybackEngine(makeEngineProps(video)));

      act(() => {
        result.current.playHls('/api/v3/hls/stream-test/vod.m3u8');
      });

      const hls = hlsInstances[hlsInstances.length - 1];

      act(() => {
        hls.emit(MockHls.Events.MANIFEST_PARSED, { levels: [], audioTracks: [] });
      });
      expect(playSpy).not.toHaveBeenCalled();

      // LEVEL_LOADED with live === false must open gate immediately even with 0s buffered
      act(() => {
        hls.emit(MockHls.Events.LEVEL_LOADED, {
          details: { live: false, totalduration: 3600, fragments: [{}] },
        });
      });

      expect(playSpy).toHaveBeenCalledTimes(1);
    });
  });

  describe('lifecycle & concurrency guards', () => {
    it('clears timers and prevents play() if component unmounts before gate opens', () => {
      const video = document.createElement('video');
      Object.defineProperty(video, 'readyState', { value: 4, configurable: true, writable: true });
      Object.defineProperty(video, 'currentTime', { value: 0, configurable: true, writable: true });
      setMockBuffered(video, [{ start: 0, end: 2.0 }]);

      const { result, unmount } = renderHook(() => usePlaybackEngine(makeEngineProps(video)));

      act(() => {
        result.current.playHls('/api/v3/hls/stream-test/live.m3u8');
      });

      const hls = hlsInstances[hlsInstances.length - 1];

      act(() => {
        hls.emit(MockHls.Events.MANIFEST_PARSED, { levels: [], audioTracks: [] });
      });

      act(() => {
        hls.emit(MockHls.Events.LEVEL_LOADED, {
          details: { live: true, totalduration: 60, fragments: [{}] },
        });
      });

      expect(playSpy).not.toHaveBeenCalled();

      // Unmount before timeout expires
      unmount();

      // Advance timers past live timeout
      act(() => {
        vi.advanceTimersByTime(25_000);
      });

      // Emit buffer appended on stale hls instance
      setMockBuffered(video, [{ start: 0, end: 10.0 }]);
      act(() => {
        hls.emit(MockHls.Events.BUFFER_APPENDED);
      });

      expect(playSpy).not.toHaveBeenCalled();
    });

    it('ensures a stale replaced HLS instance never opens the gate', () => {
      const video = document.createElement('video');
      Object.defineProperty(video, 'readyState', { value: 4, configurable: true, writable: true });
      Object.defineProperty(video, 'currentTime', { value: 0, configurable: true, writable: true });
      setMockBuffered(video, [{ start: 0, end: 2.0 }]);

      const { result } = renderHook(() => usePlaybackEngine(makeEngineProps(video)));

      act(() => {
        result.current.playHls('/api/v3/hls/stream-test/session-1.m3u8');
      });
      const hls1 = hlsInstances[0];

      act(() => {
        hls1.emit(MockHls.Events.MANIFEST_PARSED, { levels: [], audioTracks: [] });
      });

      // Now switch to session 2 before session 1 finishes buffering
      act(() => {
        result.current.playHls('/api/v3/hls/stream-test/session-2.m3u8');
      });
      const hls2 = hlsInstances[1];
      expect(hls2).not.toBe(hls1);

      // Simulate stale hls1 events
      setMockBuffered(video, [{ start: 0, end: 8.0 }]);
      act(() => {
        hls1.emit(MockHls.Events.BUFFER_APPENDED);
      });
      expect(playSpy).not.toHaveBeenCalled();

      // Stale timeout expiration for hls1 must not open gate
      act(() => {
        vi.advanceTimersByTime(20_000);
      });
      expect(playSpy).not.toHaveBeenCalled();

      // Active instance hls2 buffers and opens gate
      act(() => {
        hls2.emit(MockHls.Events.MANIFEST_PARSED, { levels: [], audioTracks: [] });
      });
      act(() => {
        hls2.emit(MockHls.Events.BUFFER_APPENDED);
      });
      expect(playSpy).toHaveBeenCalledTimes(1);
    });

    it('ensures replay while stale instance A is waiting for readiness does not cancel instance B timeout', () => {
      const video = document.createElement('video');
      // A reaches target but video is not yet ready (readyState < 2)
      Object.defineProperty(video, 'readyState', { value: 1, configurable: true, writable: true });
      Object.defineProperty(video, 'currentTime', { value: 0, configurable: true, writable: true });
      setMockBuffered(video, [{ start: 0, end: 6.0 }]);

      const { result } = renderHook(() => usePlaybackEngine(makeEngineProps(video)));

      act(() => {
        result.current.playHls('/api/v3/hls/stream-test/session-A.m3u8');
      });
      const hlsA = hlsInstances[0];

      act(() => {
        hlsA.emit(MockHls.Events.MANIFEST_PARSED, { levels: [], audioTracks: [] });
      });

      // A reaches 6s buffer with readyState = 1 -> attaches onCanPlay & sets 200ms timer
      act(() => {
        hlsA.emit(MockHls.Events.BUFFER_APPENDED);
      });
      expect(playSpy).not.toHaveBeenCalled();

      // Within 200ms, replay / stream change to session B occurs
      setMockBuffered(video, [{ start: 0, end: 2.0 }]); // B has only 2s buffer (< 6s)
      act(() => {
        result.current.playHls('/api/v3/hls/stream-test/session-B.m3u8');
      });
      const hlsB = hlsInstances[1];
      expect(hlsB).not.toBe(hlsA);

      act(() => {
        hlsB.emit(MockHls.Events.MANIFEST_PARSED, { levels: [], audioTracks: [] });
      });

      // Video becomes ready -> canplay event fires on video
      act(() => {
        video.dispatchEvent(new Event('canplay'));
      });

      // At this point, A must NOT have cleared B's timeout or opened the gate.
      // B does NOT have 6s buffer, so B has not started yet.
      expect(playSpy).not.toHaveBeenCalled();

      // Advance timers by 15s (B's timeout). B must open on its 15s timeout!
      act(() => {
        vi.advanceTimersByTime(15_000);
      });

      expect(playSpy).toHaveBeenCalledTimes(1);
    });

    it('calls play() exactly once per attempt and preserves gate across re-renders in StrictMode', () => {
      const video = document.createElement('video');
      Object.defineProperty(video, 'readyState', { value: 4, configurable: true, writable: true });
      Object.defineProperty(video, 'currentTime', { value: 0, configurable: true, writable: true });
      setMockBuffered(video, [{ start: 0, end: 6.5 }]);

      const { result, rerender } = renderHook(() => usePlaybackEngine(makeEngineProps(video)), {
        wrapper: StrictMode,
      });

      act(() => {
        result.current.playHls('/api/v3/hls/stream-test/live.m3u8');
      });

      const hls = hlsInstances[hlsInstances.length - 1];

      act(() => {
        hls.emit(MockHls.Events.MANIFEST_PARSED, { levels: [], audioTracks: [] });
      });

      act(() => {
        hls.emit(MockHls.Events.BUFFER_APPENDED);
      });

      expect(playSpy).toHaveBeenCalledTimes(1);

      // Additional buffer appended events
      setMockBuffered(video, [{ start: 0, end: 12.0 }]);
      act(() => {
        hls.emit(MockHls.Events.BUFFER_APPENDED);
      });
      act(() => {
        hls.emit(MockHls.Events.BUFFER_APPENDED);
      });

      // Hook re-render
      rerender();

      // Timeout expiration
      act(() => {
        vi.advanceTimersByTime(25_000);
      });

      expect(playSpy).toHaveBeenCalledTimes(1);
    });
  });

  describe('telemetry heartbeat minBuf', () => {
    it('tracks interval minBuf across timeupdate events, resets per heartbeat, and reports interval minimum', () => {
      const video = document.createElement('video');
      Object.defineProperty(video, 'readyState', { value: 4, configurable: true, writable: true });
      Object.defineProperty(video, 'currentTime', { value: 0, configurable: true, writable: true });
      Object.defineProperty(video, 'paused', { value: false, configurable: true, writable: true });
      setMockBuffered(video, [{ start: 0, end: 6.0 }]);

      const reportError = vi.fn().mockResolvedValue(undefined);
      const { result } = renderHook(() =>
        usePlaybackEngine(makeEngineProps(video, vi.fn(), { reportError })),
      );

      act(() => {
        result.current.playHls('/api/v3/hls/stream-test/live.m3u8');
      });

      const hls = hlsInstances[hlsInstances.length - 1];

      act(() => {
        hls.emit(MockHls.Events.MANIFEST_PARSED, { levels: [], audioTracks: [] });
      });
      act(() => {
        hls.emit(MockHls.Events.BUFFER_APPENDED);
      });

      // Simulate video entering 'playing' state to start the render probe heartbeat
      act(() => {
        video.dispatchEvent(new Event('playing'));
      });

      // First heartbeat interval (30s): simulate buffer dips on timeupdate
      setMockBuffered(video, [{ start: 0, end: 5.5 }]);
      act(() => {
        video.dispatchEvent(new Event('timeupdate'));
      });

      setMockBuffered(video, [{ start: 0, end: 3.2 }]);
      act(() => {
        video.dispatchEvent(new Event('timeupdate'));
      });

      setMockBuffered(video, [{ start: 0, end: 4.8 }]);
      act(() => {
        video.dispatchEvent(new Event('timeupdate'));
      });

      // Advance by 30s to trigger the first heartbeat
      act(() => {
        vi.advanceTimersByTime(30_000);
      });

      const firstHeartbeatCalls = reportError.mock.calls.filter(
        (call) => call[1] === PLAYBACK_INFO_CODE_HLSJS_RENDER_HEARTBEAT,
      );
      expect(firstHeartbeatCalls.length).toBe(1);
      expect(firstHeartbeatCalls[0]?.[2]).toContain('minBuf=3.20');

      // Second heartbeat interval (next 30s): simulate buffer dips on timeupdate
      setMockBuffered(video, [{ start: 0, end: 5.5 }]);
      act(() => {
        video.dispatchEvent(new Event('timeupdate'));
      });

      setMockBuffered(video, [{ start: 0, end: 4.1 }]);
      act(() => {
        video.dispatchEvent(new Event('timeupdate'));
      });

      setMockBuffered(video, [{ start: 0, end: 4.6 }]);
      act(() => {
        video.dispatchEvent(new Event('timeupdate'));
      });

      // Advance by another 30s to trigger the second heartbeat
      act(() => {
        vi.advanceTimersByTime(30_000);
      });

      const allHeartbeatCalls = reportError.mock.calls.filter(
        (call) => call[1] === PLAYBACK_INFO_CODE_HLSJS_RENDER_HEARTBEAT,
      );
      expect(allHeartbeatCalls.length).toBe(2);
      expect(allHeartbeatCalls[1]?.[2]).toContain('minBuf=4.10');
      // Must not carry over 3.20 from the previous interval
      expect(allHeartbeatCalls[1]?.[2]).not.toContain('minBuf=3.20');
    });
  });
});
