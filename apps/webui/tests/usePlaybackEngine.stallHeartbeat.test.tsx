import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

// A minimal hls.js stand-in: records handlers so the test can raise events,
// and reports a fixed live latency. Hoisted because vi.mock is.
const { FakeHls, hlsInstances } = vi.hoisted(() => {
  type Handler = (event: string, data: unknown) => void;
  const instances: Array<InstanceType<typeof Fake>> = [];

  class Fake {
    static isSupported = () => true;
    static Events: Record<string, string> = {};
    static ErrorTypes: Record<string, string> = {};
    static ErrorDetails: Record<string, string> = {};

    handlers = new Map<string, Handler[]>();
    levels: unknown[] = [];
    currentLevel = -1;
    audioTracks: unknown[] = [];
    latency = 4.5;
    media: HTMLMediaElement | null = null;

    constructor() {
      instances.push(this);
    }

    on(event: string, handler: Handler) {
      const list = this.handlers.get(event) ?? [];
      list.push(handler);
      this.handlers.set(event, list);
    }

    once(event: string, handler: Handler) {
      this.on(event, handler);
    }

    off() {}
    loadSource() {}
    attachMedia(media: HTMLMediaElement) {
      this.media = media;
    }
    detachMedia() {}
    startLoad() {}
    stopLoad() {}
    recoverMediaError() {}
    swapAudioCodec() {}
    destroy() {}

    emit(event: string, data: unknown) {
      for (const handler of this.handlers.get(event) ?? []) {
        handler(event, data);
      }
    }
  }

  return { FakeHls: Fake, hlsInstances: instances };
});

vi.mock('../src/features/player/lib/hlsRuntime', async () => {
  const actual = await vi.importActual<typeof import('hls.js')>('hls.js');
  FakeHls.Events = actual.Events as unknown as Record<string, string>;
  FakeHls.ErrorTypes = actual.ErrorTypes as unknown as Record<string, string>;
  FakeHls.ErrorDetails = actual.ErrorDetails as unknown as Record<string, string>;
  return { default: FakeHls };
});

import { usePlaybackEngine } from '../src/features/player/usePlaybackEngine';

const HEARTBEAT_CODE = 243;
const HLS_NONFATAL_WARNING_CODE = 105;

function makeProps(video: HTMLVideoElement, reportError: ReturnType<typeof vi.fn>) {
  return {
    videoRef: { current: video },
    hlsRef: { current: null },
    sessionIdRef: { current: 'sess-stall' },
    isTeardownRef: { current: false },
    lastDecodedRef: { current: 0 },
    playbackEpochRef: { current: 1 },
    t: ((key: string) => key) as any,
    reportError,
    waitForSessionReady: vi.fn().mockResolvedValue({} as any),
    shouldPreferNativeHls: vi.fn(() => false),
    setStats: vi.fn(),
    setStatus: vi.fn(),
    clearPlaybackFailure: vi.fn(),
    reportPlaybackFailure: vi.fn(),
  } as any;
}

function nonFatal(details: string) {
  return { fatal: false, type: 'mediaError', details };
}

describe('usePlaybackEngine render heartbeat stall accounting', () => {
  beforeEach(() => {
    hlsInstances.length = 0;
    vi.useFakeTimers({
      toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date', 'performance'],
    });
    vi.spyOn(HTMLMediaElement.prototype, 'load').mockImplementation(() => {});
    vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {});
    vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue(undefined);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('reports every stall and hole in the heartbeat while the warning itself stays deduped', async () => {
    const video = document.createElement('video');
    const reportError = vi.fn().mockResolvedValue(undefined);
    const { result } = renderHook(() => usePlaybackEngine(makeProps(video, reportError)));

    await act(async () => {
      result.current.playHls('http://example.test/live.m3u8', 'hlsjs');
    });
    const hls = hlsInstances.at(-1)!;
    expect(hls).toBeDefined();

    await act(async () => {
      video.dispatchEvent(new Event('playing')); // starts the heartbeat for the session
      // The startup reveal hold ignores 'waiting' while the picture is still veiled;
      // stalls count once the stream is on screen.
      vi.advanceTimersByTime(2_000);
    });

    await act(async () => {
      hls.emit(FakeHls.Events.ERROR, nonFatal('bufferSeekOverHole'));
      hls.emit(FakeHls.Events.ERROR, nonFatal('bufferSeekOverHole'));
      hls.emit(FakeHls.Events.ERROR, nonFatal('bufferSeekOverHole'));
      hls.emit(FakeHls.Events.ERROR, nonFatal('bufferNudgeOnStall'));
    });

    await act(async () => {
      video.dispatchEvent(new Event('waiting'));
      vi.advanceTimersByTime(1500);
      video.dispatchEvent(new Event('playing'));
    });

    await act(async () => {
      vi.advanceTimersByTime(30_000);
    });

    const beats = reportError.mock.calls.filter((call) => call[1] === HEARTBEAT_CODE).map((call) => String(call[2]));
    expect(beats.length).toBeGreaterThan(0);
    const beat = beats.at(-1)!;
    expect(beat).toContain('stage=heartbeat');
    expect(beat).toContain('stalls=1');
    expect(beat).toContain('stall_ms=1500');
    expect(beat).toContain('holes=3');
    expect(beat).toContain('nudges=1');
    expect(beat).toContain('lat=4.50');
    expect(beat).toMatch(/ranges=\d+/);

    // The server log still gets the non-fatal warning once, not four times.
    const nonFatalWarnings = reportError.mock.calls.filter((call) => call[1] === HLS_NONFATAL_WARNING_CODE);
    expect(nonFatalWarnings).toHaveLength(1);
  });

  it('starts the counts from zero for the next session', async () => {
    const video = document.createElement('video');
    const reportError = vi.fn().mockResolvedValue(undefined);
    const props = makeProps(video, reportError);
    const { result } = renderHook(() => usePlaybackEngine(props));

    await act(async () => {
      result.current.playHls('http://example.test/a.m3u8', 'hlsjs');
    });
    await act(async () => {
      video.dispatchEvent(new Event('playing'));
      hlsInstances.at(-1)!.emit(FakeHls.Events.ERROR, nonFatal('bufferSeekOverHole'));
    });

    props.sessionIdRef.current = 'sess-next';
    await act(async () => {
      result.current.playHls('http://example.test/b.m3u8', 'hlsjs');
    });
    await act(async () => {
      video.dispatchEvent(new Event('playing'));
      vi.advanceTimersByTime(30_000);
    });

    const beat = reportError.mock.calls
      .filter((call) => call[1] === HEARTBEAT_CODE)
      .map((call) => String(call[2]))
      .at(-1)!;
    expect(beat).toContain('holes=0');
    expect(beat).toContain('stalls=0');
  });
});
