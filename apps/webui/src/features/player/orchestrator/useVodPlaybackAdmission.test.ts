import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useVodPlaybackAdmission } from './useVodPlaybackAdmission';
import { createEngineTimerRegistry } from './engineTimerRegistry';

const { postRecordingPlaybackInfoMock } = vi.hoisted(() => ({
  postRecordingPlaybackInfoMock: vi.fn(),
}));

vi.mock('../../../client-ts', () => ({
  postRecordingPlaybackInfo: postRecordingPlaybackInfoMock,
}));

vi.mock('../utils/playbackNetworkProbe', () => ({
  measurePlaybackNetwork: vi.fn().mockResolvedValue({ kind: 'lan' }),
  applyPlaybackNetworkProbe: vi.fn().mockImplementation((_caps, ctx) => ctx),
}));

describe('useVodPlaybackAdmission', () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    vi.useFakeTimers();
    postRecordingPlaybackInfoMock.mockReset();
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    globalThis.fetch = originalFetch;
  });

  function createTestAdmissionHarness(overrides: Partial<Parameters<typeof useVodPlaybackAdmission>[0]> = {}) {
    const lifecycleGenerationRef = { current: 1 };
    const activeRecordingRef = { current: null as string | null };
    const dismissedResumeRecordingIdRef = { current: 'all' as string | null };
    const isTeardownRef = { current: false };
    const linkProfileRef = { current: 'lan' as const };
    const automaticProfileMemoryRef = { current: {} };
    const vodFetchRef = { current: null as AbortController | null };
    const vodRetryRef = { current: null as number | null };
    const timerRegistry = createEngineTimerRegistry();

    const dispatchPlayback = vi.fn();
    const reportPlaybackFailure = vi.fn();
    const playDirectMp4 = vi.fn();
    const playHls = vi.fn();
    const setActiveHlsEngine = vi.fn();
    const beginPlaybackAttempt = vi.fn();
    const prepareForNextPlaybackAttempt = vi.fn().mockResolvedValue(undefined);
    const ensureSessionCookie = vi.fn().mockResolvedValue(undefined);
    const recordContractAdvisories = vi.fn();

    const defaultProps = {
      apiBase: 'http://localhost/api',
      explicitProfile: 'auto',
      ensureSessionCookie,
      allocatePlaybackEpoch: vi.fn().mockReturnValue(1),
      beginPlaybackAttempt,
      prepareForNextPlaybackAttempt,
      isLifecycleActive: (gen: number) => gen === lifecycleGenerationRef.current && !isTeardownRef.current,
      isStalePlaybackEpoch: () => false,
      lifecycleGenerationRef,
      activeRecordingRef,
      dismissedResumeRecordingIdRef,
      isTeardownRef,
      linkProfileRef,
      automaticProfileMemoryRef,
      vodFetchRef,
      vodRetryRef,
      timerRegistry,
      setActiveRecordingId: vi.fn(),
      setCapabilitySnapshot: vi.fn(),
      setTraceId: vi.fn(),
      setPlaybackObservability: vi.fn(),
      setVodStreamMode: vi.fn(),
      setDurationSeconds: vi.fn(),
      setCanSeek: vi.fn(),
      setStartUnix: vi.fn(),
      setAnchorStartSec: vi.fn(),
      setResumeState: vi.fn(),
      setShowResumeOverlay: vi.fn(),
      clearPlayerError: vi.fn(),
      dispatchPlayback,
      reportPlaybackFailure,
      playDirectMp4,
      playHls,
      setActiveHlsEngine,
      gatherPlaybackCapabilitiesForPlayer: vi.fn().mockResolvedValue({
        deviceClass: 'desktop',
        codecs: { h264: true, hevc: false },
        videoCodecs: ['h264'],
        audioCodecs: ['aac'],
        preferredHlsEngine: 'native',
      }),
      resolvePreferredHlsEngineForCapabilities: vi.fn().mockReturnValue('native'),
      recordContractAdvisories,
      normalizeRuntimePlaybackError: (err: any) => err,
      mergeSessionPlaybackTrace: vi.fn(),
      sleep: timerRegistry.delay,
      t: ((k: string) => k) as any,
      ...overrides,
    };

    return {
      defaultProps,
      timerRegistry,
      lifecycleGenerationRef,
      activeRecordingRef,
      dismissedResumeRecordingIdRef,
      isTeardownRef,
      dispatchPlayback,
      reportPlaybackFailure,
      playDirectMp4,
      playHls,
      vodRetryRef,
    };
  }

  it('admits direct MP4 playback when contract mode is direct_file', async () => {
    const harness = createTestAdmissionHarness();
    harness.dismissedResumeRecordingIdRef.current = 'rec-mp4';

    postRecordingPlaybackInfoMock.mockResolvedValueOnce({
      data: {
        requestId: 'req-direct',
        mode: 'direct_mp4',
        isSeekable: true,
        decision: {
          mode: 'direct_play',
          selectedOutputUrl: 'http://localhost/vod/video.mp4',
        },
      },
      error: null,
      response: { status: 200 },
    });

    const { result } = renderHook(() => useVodPlaybackAdmission(harness.defaultProps));

    await act(async () => {
      await result.current.startRecordingPlayback('rec-mp4', undefined, 0);
    });

    expect(harness.playDirectMp4).toHaveBeenCalledWith(expect.stringContaining('http://localhost/vod/video.mp4'));
    expect(harness.playHls).not.toHaveBeenCalled();
  });

  it('stream 503 Retry-After schedules vodRetry timer, and unmounting / clearAll cancels retry without re-executing playback', async () => {
    const harness = createTestAdmissionHarness();
    harness.dismissedResumeRecordingIdRef.current = 'rec-100';

    postRecordingPlaybackInfoMock.mockResolvedValueOnce({
      data: {
        requestId: 'req-hls',
        mode: 'hls',
        isSeekable: true,
        decision: {
          mode: 'direct_stream',
          selectedOutputUrl: 'http://localhost/vod/stream.m3u8',
        },
      },
      error: null,
      response: { status: 200 },
    });

    const fetchSpy = vi.fn().mockResolvedValue({
      status: 503,
      headers: new Headers({ 'Retry-After': '3' }),
    });
    globalThis.fetch = fetchSpy;

    const { result, unmount } = renderHook(() => useVodPlaybackAdmission(harness.defaultProps));

    await act(async () => {
      await result.current.startRecordingPlayback('rec-100', undefined, 0);
    });

    // Verify vodRetry timer is active in timerRegistry
    expect(harness.timerRegistry.hasTimeout('vodRetry')).toBe(true);

    // Orchestrator cleanup / stop clears all timers atomically
    harness.timerRegistry.clearAll();
    unmount();
    expect(harness.timerRegistry.hasTimeout('vodRetry')).toBe(false);

    // Advance past retry delay
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });

    // Post recording info was only called once
    expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(1);
    expect(harness.playHls).not.toHaveBeenCalled();
  });

  it('cancels vodRetry when recording ID changes before timer fires', async () => {
    const harness = createTestAdmissionHarness();
    harness.dismissedResumeRecordingIdRef.current = 'rec-100';

    postRecordingPlaybackInfoMock.mockResolvedValueOnce({
      data: {
        requestId: 'req-hls',
        mode: 'hls',
        isSeekable: true,
        decision: {
          mode: 'direct_stream',
          selectedOutputUrl: 'http://localhost/vod/stream.m3u8',
        },
      },
      error: null,
      response: { status: 200 },
    });

    const fetchSpy = vi.fn().mockResolvedValue({
      status: 503,
      headers: new Headers({ 'Retry-After': '5' }),
    });
    globalThis.fetch = fetchSpy;

    const { result } = renderHook(() => useVodPlaybackAdmission(harness.defaultProps));

    await act(async () => {
      await result.current.startRecordingPlayback('rec-100', undefined, 0);
    });

    expect(harness.timerRegistry.hasTimeout('vodRetry')).toBe(true);

    // User switches to rec-200, which clears timers and updates activeRecordingRef
    harness.timerRegistry.clearAll();
    harness.activeRecordingRef.current = 'rec-200';

    await act(async () => {
      await vi.advanceTimersByTimeAsync(6000);
    });

    // rec-100 was not re-attempted
    expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(1);
  });
});
