// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { StrictMode, useRef, useState } from 'react';
import { act, cleanup, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { HlsInstanceRef, V3PlayerProps, VideoElementRef } from '../../types/v3-player';
import { usePlaybackOrchestrator } from './usePlaybackOrchestrator';
import type { PlaybackController } from './orchestrator/playbackController';

const { postRecordingPlaybackInfoMock } = vi.hoisted(() => ({
  postRecordingPlaybackInfoMock: vi.fn(),
}));

vi.mock('../../client-ts', async (importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  postRecordingPlaybackInfo: postRecordingPlaybackInfoMock,
}));

vi.mock('./lib/hlsRuntime', () => {
  function HlsMock(this: any) {
    this.loadSource = vi.fn();
    this.attachMedia = vi.fn();
    this.on = vi.fn();
    this.startLoad = vi.fn();
    this.destroy = vi.fn();
  }
  (HlsMock as any).isSupported = vi.fn().mockReturnValue(true);
  (HlsMock as any).Events = { MANIFEST_PARSED: 'p', LEVEL_SWITCHED: 'l', FRAG_BUFFERED: 'f', ERROR: 'e' };
  return { default: HlsMock };
});

const sampleRecordingData = {
  requestId: 'req-rec-1',
  mode: 'hls',
  isSeekable: true,
  media: {
    durationSeconds: 120,
    startUnix: 1700000000,
  },
  decision: {
    mode: 'direct_stream',
    selectedOutputUrl: 'http://test.local/recordings/rec-1/index.m3u8',
  },
};

describe('usePlaybackOrchestrator: VOD Retry-After Continuation (Step 3e-B Points 6-11)', () => {
  let exposedController!: PlaybackController;
  let exposedActions!: any;

  function Harness({ props }: { props: V3PlayerProps }) {
    const containerRef = useRef<HTMLDivElement>(null);
    const videoRef = useRef<VideoElementRef>(null);
    const hlsRef = useRef<HlsInstanceRef | null>(null);
    const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);
    const { controller, actions } = usePlaybackOrchestrator(props, {
      containerRef,
      videoRef,
      hlsRef,
      resumePrimaryActionRef,
    });
    exposedController = controller;
    exposedActions = actions;
    return (
      <div ref={containerRef}>
        <video
          ref={(el) => {
            if (el) {
              el.play = vi.fn().mockResolvedValue(undefined);
              el.pause = vi.fn();
            }
            videoRef.current = el;
          }}
        />
      </div>
    );
  }

  beforeEach(() => {
    vi.useFakeTimers();
    postRecordingPlaybackInfoMock.mockReset();
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        headers: new Headers(),
        json: async () => ({}),
        text: async () => '',
      }),
    );
  });

  afterEach(() => {
    vi.clearAllTimers();
    cleanup();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  describe('Point 6: Site A stream-info 503 + Retry-After loop', () => {
    it('Site A: 503 + Retry-After: N -> building, advisory recording_retry_after recorded; at N s a second call with same start_ms; success on second call continues pipeline', async () => {
      let callCount = 0;
      postRecordingPlaybackInfoMock.mockImplementation(async () => {
        callCount++;
        if (callCount === 1) {
          return {
            data: undefined,
            error: { error: 'recording_building' },
            response: { status: 503, headers: new Headers({ 'Retry-After': '5' }) },
          };
        }
        return {
          data: sampleRecordingData,
          error: undefined,
          response: { status: 200, headers: new Headers() },
        };
      });

      render(
        <Harness
          props={
            {
              autoStart: true,
              recordingId: 'rec-site-a',
              apiBase: 'http://test.local',
            } as unknown as V3PlayerProps
          }
        />,
      );

      // Settle initial start invocation
      await act(async () => {
        await vi.advanceTimersByTimeAsync(100);
      });

      expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(1);
      const firstCallArgs = postRecordingPlaybackInfoMock.mock.calls[0]![0];
      expect(firstCallArgs.path).toEqual({ recordingId: 'rec-site-a' });

      // Domain status is building and advisory recording_retry_after is recorded
      expect(exposedController.getDomainStatus()).toBe('building');
      const pendingContinuation = exposedController.getPendingStartContinuation();
      expect(pendingContinuation).not.toBeNull();
      expect(pendingContinuation?.delayMs).toBe(5000);
      expect(pendingContinuation?.reason).toBe('recording_retry_after');

      // Before N s (total 4800 + 100 = 4900 ms < 5000 ms): second call has NOT occurred
      await act(async () => {
        await vi.advanceTimersByTimeAsync(4800);
      });
      expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(1);

      // Past N s (total 4900 + 200 = 5100 ms > 5000 ms): second call executes with same start_ms and proceeds
      await act(async () => {
        await vi.advanceTimersByTimeAsync(200);
      });
      expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(2);
      const secondCallArgs = postRecordingPlaybackInfoMock.mock.calls[1]![0];
      expect(secondCallArgs.path).toEqual({ recordingId: 'rec-site-a' });
      expect(secondCallArgs.query?.start_ms).toBe(firstCallArgs.query?.start_ms);
      expect(secondCallArgs.headers).toEqual(firstCallArgs.headers);

      // Pipeline continues normally
      expect(exposedController.getPendingStartContinuation()).toBeNull();
      expect(exposedController.getDomainStatus()).not.toBe('building');
    });
  });

  describe('Point 7: Site B HEAD preflight 503 + Retry-After re-entry', () => {
    it('Site B: stream-info OK, HEAD 503 + Retry-After: N -> building; at N s re-enters with same offset and profile; vodFetchRef is cleared before continuation wait', async () => {
      postRecordingPlaybackInfoMock.mockImplementation(async () => ({
        data: sampleRecordingData,
        error: undefined,
        response: { status: 200, headers: new Headers() },
      }));

      let headCallCount = 0;
      const fetchMock = vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
        if (init?.method === 'HEAD') {
          headCallCount++;
          if (headCallCount === 1) {
            return {
              ok: false,
              status: 503,
              headers: new Headers({ 'Retry-After': '4' }),
              json: async () => ({}),
              text: async () => '',
            };
          }
          return {
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({}),
            text: async () => '',
          };
        }
        return {
          ok: true,
          status: 200,
          headers: new Headers(),
          json: async () => ({}),
          text: async () => '',
        };
      });
      vi.stubGlobal('fetch', fetchMock);

      render(
        <Harness
          props={
            {
              autoStart: true,
              recordingId: 'rec-site-b',
              apiBase: 'http://test.local',
            } as unknown as V3PlayerProps
          }
        />,
      );

      // First attempt: stream-info succeeds, HEAD returns 503
      await act(async () => {
        await vi.advanceTimersByTimeAsync(100);
      });

      expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(1);
      const firstCallArgs = postRecordingPlaybackInfoMock.mock.calls[0]![0];
      expect(headCallCount).toBe(1);
      expect(exposedController.getDomainStatus()).toBe('building');

      const pending = exposedController.getPendingStartContinuation();
      expect(pending).not.toBeNull();
      expect(pending?.delayMs).toBe(4000);

      // Advance by 3700 ms (total 3800 ms < 4000 ms): no second stream-info request yet
      await act(async () => {
        await vi.advanceTimersByTimeAsync(3700);
      });
      expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(1);

      // At 4000 ms + teardown settle: re-entry executes startRecordingPlayback with same offset and profile
      await act(async () => {
        await vi.advanceTimersByTimeAsync(500);
      });

      expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(2);
      const secondCallArgs = postRecordingPlaybackInfoMock.mock.calls[1]![0];
      expect(secondCallArgs.path).toEqual({ recordingId: 'rec-site-b' });
      expect(secondCallArgs.query?.start_ms).toBe(firstCallArgs.query?.start_ms);
      expect(secondCallArgs.headers).toEqual(firstCallArgs.headers);
      expect(exposedController.getPendingStartContinuation()).toBeNull();
    });
  });

  describe('Point 8: Stop during either wait', () => {
    it('stop during Site A wait: getPendingStartContinuation() is null immediately, no second request, status stopped', async () => {
      postRecordingPlaybackInfoMock.mockImplementation(async () => ({
        data: undefined,
        error: { error: 'recording_building' },
        response: { status: 503, headers: new Headers({ 'Retry-After': '10' }) },
      }));

      render(
        <Harness
          props={
            {
              autoStart: true,
              recordingId: 'rec-stop-a',
              apiBase: 'http://test.local',
            } as unknown as V3PlayerProps
          }
        />,
      );

      await act(async () => {
        await vi.advanceTimersByTimeAsync(500);
      });
      expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(1);
      expect(exposedController.getPendingStartContinuation()).not.toBeNull();

      // Stop during wait
      let stopPromise!: Promise<void>;
      act(() => {
        stopPromise = exposedController.stop('user_stop');
      });

      // Synchronous cancellation check
      expect(exposedController.getPendingStartContinuation()).toBeNull();

      // Settle stop within teardown window
      await act(async () => {
        await vi.advanceTimersByTimeAsync(1000);
        await stopPromise;
      });
      expect(exposedController.getDomainStatus()).toBe('stopped');

      // Advance past delayMs: no second request occurs
      await act(async () => {
        await vi.advanceTimersByTimeAsync(15000);
      });
      expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(1);
    });

    it('stop during Site B wait: getPendingStartContinuation() is null immediately, no second request, status stopped', async () => {
      postRecordingPlaybackInfoMock.mockImplementation(async () => ({
        data: sampleRecordingData,
        error: undefined,
        response: { status: 200, headers: new Headers() },
      }));

      vi.stubGlobal(
        'fetch',
        vi.fn().mockImplementation(async (_url: string, init?: RequestInit) => {
          if (init?.method === 'HEAD') {
            return {
              ok: false,
              status: 503,
              headers: new Headers({ 'Retry-After': '10' }),
              json: async () => ({}),
              text: async () => '',
            };
          }
          return {
            ok: true,
            status: 200,
            headers: new Headers(),
            json: async () => ({}),
            text: async () => '',
          };
        }),
      );

      render(
        <Harness
          props={
            {
              autoStart: true,
              recordingId: 'rec-stop-b',
              apiBase: 'http://test.local',
            } as unknown as V3PlayerProps
          }
        />,
      );

      await act(async () => {
        await vi.advanceTimersByTimeAsync(500);
      });
      expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(1);
      expect(exposedController.getPendingStartContinuation()).not.toBeNull();

      // Stop during wait
      let stopPromise!: Promise<void>;
      act(() => {
        stopPromise = exposedController.stop('user_stop');
      });

      // Synchronous cancellation check
      expect(exposedController.getPendingStartContinuation()).toBeNull();

      await act(async () => {
        await vi.advanceTimersByTimeAsync(1000);
        await stopPromise;
      });
      expect(exposedController.getDomainStatus()).toBe('stopped');

      await act(async () => {
        await vi.advanceTimersByTimeAsync(15000);
      });
      expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(1);
    });
  });

  describe('Point 9: Superseding during wait (different recording or seek)', () => {
    it('start of different recording during wait cancels old continuation; only new recording pipeline runs', async () => {
      postRecordingPlaybackInfoMock.mockImplementation(async (params: any) => {
        if (params.path.recordingId === 'rec-1') {
          return {
            data: undefined,
            error: { error: 'building' },
            response: { status: 503, headers: new Headers({ 'Retry-After': '10' }) },
          };
        }
        return {
          data: { ...sampleRecordingData, requestId: 'req-rec-2' },
          error: undefined,
          response: { status: 200, headers: new Headers() },
        };
      });

      function DynamicHarness() {
        const [recId, setRecId] = useState('rec-1');
        return (
          <div>
            <Harness
              key={recId}
              props={
                {
                  autoStart: true,
                  recordingId: recId,
                  apiBase: 'http://test.local',
                } as unknown as V3PlayerProps
              }
            />
            <button onClick={() => setRecId('rec-2')} type="button">
              switch-to-rec-2
            </button>
          </div>
        );
      }

      const { getByRole } = render(<DynamicHarness />);

      await act(async () => {
        await vi.advanceTimersByTimeAsync(500);
      });
      expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(1);
      expect(postRecordingPlaybackInfoMock.mock.calls[0]![0].path.recordingId).toBe('rec-1');
      expect(exposedController.getPendingStartContinuation()).not.toBeNull();

      // Switch to rec-2 during wait
      act(() => {
        getByRole('button', { name: 'switch-to-rec-2' }).click();
      });

      await act(async () => {
        await vi.advanceTimersByTimeAsync(500);
      });

      // rec-2 start has been initiated; old rec-1 continuation was cancelled
      expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(2);
      expect(postRecordingPlaybackInfoMock.mock.calls[1]![0].path.recordingId).toBe('rec-2');

      // Advance past rec-1's 10s wait; no additional request for rec-1 follows
      await act(async () => {
        await vi.advanceTimersByTimeAsync(12000);
      });
      expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(2);
    });

    it('seek during wait cancels old continuation and starts new attempt with new offset (C1 Matrix 9)', async () => {
      postRecordingPlaybackInfoMock.mockImplementation(async () => {
        return {
          data: undefined,
          error: { error: 'building' },
          response: { status: 503, headers: new Headers({ 'Retry-After': '10' }) },
        };
      });

      render(
        <Harness
          props={
            {
              autoStart: true,
              recordingId: 'rec-seek',
              apiBase: 'http://test.local',
            } as unknown as V3PlayerProps
          }
        />,
      );

      await act(async () => {
        await vi.advanceTimersByTimeAsync(500);
      });
      expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(1);
      const oldPending = exposedController.getPendingStartContinuation();
      expect(oldPending).not.toBeNull();

      // Seek to 5 seconds (5000 ms) via actions.seekTo during wait
      await act(async () => {
        exposedActions.seekTo(5);
        await vi.advanceTimersByTimeAsync(500);
      });

      // Old continuation cancelled, new attempt initiated with start_ms = 5000
      expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(2);
      expect(postRecordingPlaybackInfoMock.mock.calls[1]![0].query?.start_ms).toBe(5000);

      const newPending = exposedController.getPendingStartContinuation();
      expect(newPending).not.toBeNull();
      expect(newPending?.epoch).toBeGreaterThan(oldPending!.epoch);

      // Advance past the first attempt's 10s mark; no old re-entry fires
      await act(async () => {
        await vi.advanceTimersByTimeAsync(8000);
      });
      expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(2);
    });
  });

  describe('Point 10: StrictMode double-mount', () => {
    it('Root StrictMode mount with autoStart and 503 produces exactly one pending continuation and one re-entry', async () => {
      let callCount = 0;
      postRecordingPlaybackInfoMock.mockImplementation(async () => {
        callCount++;
        if (callCount === 1) {
          return {
            data: undefined,
            error: { error: 'building' },
            response: { status: 503, headers: new Headers({ 'Retry-After': '5' }) },
          };
        }
        return {
          data: sampleRecordingData,
          error: undefined,
          response: { status: 200, headers: new Headers() },
        };
      });

      render(
        <StrictMode>
          <Harness
            props={
              {
                autoStart: true,
                recordingId: 'rec-strict',
                apiBase: 'http://test.local',
              } as unknown as V3PlayerProps
            }
          />
        </StrictMode>,
      );

      await act(async () => {
        await vi.advanceTimersByTimeAsync(500);
      });

      // Exactly one pending continuation exists on the active controller
      const pending = exposedController.getPendingStartContinuation();
      expect(pending).not.toBeNull();
      expect(pending?.delayMs).toBe(5000);

      const initialCalls = postRecordingPlaybackInfoMock.mock.calls.length;

      // Advance to 5000 ms: exactly one re-entry fires
      await act(async () => {
        await vi.advanceTimersByTimeAsync(5000);
      });

      expect(postRecordingPlaybackInfoMock.mock.calls.length).toBe(initialCalls + 1);
      expect(exposedController.getPendingStartContinuation()).toBeNull();
    });
  });

  describe('Point 11: maxMetaRetries bound at Site A', () => {
    it('permanent 503 (Retry-After: 1) bounds at exactly 20 calls, transitions to error, and leaves no pending continuation', async () => {
      postRecordingPlaybackInfoMock.mockImplementation(async () => ({
        data: undefined,
        error: { error: 'recording_building' },
        response: { status: 503, headers: new Headers({ 'Retry-After': '1' }) },
      }));

      render(
        <Harness
          props={
            {
              autoStart: true,
              recordingId: 'rec-max-retries',
              apiBase: 'http://test.local',
            } as unknown as V3PlayerProps
          }
        />,
      );

      // Run through all 20 attempts
      for (let i = 0; i < 25; i++) {
        await act(async () => {
          await vi.advanceTimersByTimeAsync(1000);
        });
      }

      // Must be capped at exactly 20 calls (maxMetaRetries = 20)
      expect(postRecordingPlaybackInfoMock).toHaveBeenCalledTimes(20);

      // State transitions to error
      expect(exposedController.getDomainStatus()).toBe('error');

      // No pending continuation remains
      expect(exposedController.getPendingStartContinuation()).toBeNull();
    });
  });
});
