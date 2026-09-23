// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  createPlaybackController,
  type PlaybackController,
  type StartContinuationOutcome,
} from './playbackController';
import { createRecoveryLadderState } from './recoveryLadder';
import { buildPlaybackFailure } from './playbackMachine';
import type { LiveSessionTransport } from './liveSessionTransport';
import type { PlaybackCommand, PlaybackDomainState } from './playbackTypes';

function createInitialState(): PlaybackDomainState {
  return {
    epoch: { playback: 1, session: 0 },
    traceId: '-',
    status: 'idle',
    playbackMode: 'UNKNOWN',
    vodStreamMode: null,
    activeHlsEngine: null,
    durationSeconds: null,
    canSeek: false,
    startUnix: null,
    sessionPhase: 'idle',
    mediaPhase: 'idle',
    contract: null,
    failure: null,
    lastAdvisory: null,
    explicitProfilePinned: false,
    hasSessionIntent: false,
    recovery: createRecoveryLadderState(),
    leaseExpiresAt: null,
    connectionLost: false,
  };
}

function createMockTransport(): LiveSessionTransport {
  return {
    fetchStreamInfo: vi.fn().mockResolvedValue({
      status: 200,
      data: { mode: 'direct_stream', playbackDecisionToken: 'tok-1' },
      headers: new Headers(),
    }),
    postStartIntent: vi.fn().mockResolvedValue({
      status: 200,
      data: { sessionId: 's-mock-1' },
      headers: new Headers(),
    }),
    waitForReady: vi.fn().mockResolvedValue({
      sessionId: 's-mock-1',
      playbackUrl: 'http://example.test/stream.m3u8',
      heartbeatIntervalSeconds: 5,
      leaseExpiresAt: '2026-09-09T12:00:00Z',
    }),
    postStopIntent: vi.fn().mockResolvedValue(undefined),
    postHeartbeat: vi.fn().mockResolvedValue({
      status: 200,
      data: { acknowledged: true, sessionId: 's-mock-1', leaseExpiresAt: '2026-09-09T12:05:00Z' },
      headers: new Headers(),
    }),
    fetchSessionSnapshot: vi.fn().mockResolvedValue({
      status: 200,
      data: { sessionId: 's-mock-1', state: 'READY' },
      headers: new Headers(),
    }),
  };
}

describe('PlaybackController: Start Continuation / VOD Retry-After (Step 3e-B)', () => {
  let transport: LiveSessionTransport;
  let executedCommands: PlaybackCommand[];
  let controller: PlaybackController;

  beforeEach(() => {
    vi.useFakeTimers();
    transport = createMockTransport();
    executedCommands = [];
    controller = createPlaybackController({
      transport,
      createInitialState,
      executeCommand: (cmd) => {
        executedCommands.push(cmd);
      },
    });
  });

  afterEach(() => {
    controller?.dispose();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  describe('Point 1: Basic scheduling and timer resolution', () => {
    it('does not resolve before delayMs, resolves proceed exactly at delayMs, and tracks pending continuation state', async () => {
      const epoch = controller.getEpoch();
      let resolvedOutcome: StartContinuationOutcome | undefined;

      const continuationPromise = controller.scheduleStartContinuation({
        epoch,
        delayMs: 5000,
        reason: 'recording_retry_after',
      }).then((res) => {
        resolvedOutcome = res;
        return res;
      });

      // Pending state is visible immediately
      expect(controller.getPendingStartContinuation()).toEqual({
        epoch,
        delayMs: 5000,
        reason: 'recording_retry_after',
      });

      // Advance to 4999 ms: not resolved yet
      await vi.advanceTimersByTimeAsync(4999);
      expect(resolvedOutcome).toBeUndefined();
      expect(controller.getPendingStartContinuation()).not.toBeNull();

      // Advance by 1 ms to reach exactly 5000 ms: resolves 'proceed'
      await vi.advanceTimersByTimeAsync(1);
      const outcome = await continuationPromise;
      expect(outcome).toBe('proceed');
      expect(resolvedOutcome).toBe('proceed');

      // Pending state is cleared synchronously with resolution
      expect(controller.getPendingStartContinuation()).toBeNull();
    });
  });

  describe('Point 2: Cancellation via stop()', () => {
    it('stop() during the wait resolves cancelled before any timer advance and leaves pending null', async () => {
      const epoch = controller.getEpoch();
      let resolvedOutcome: StartContinuationOutcome | undefined;

      const continuationPromise = controller.scheduleStartContinuation({
        epoch,
        delayMs: 30000,
        reason: 'recording_retry_after',
      }).then((res) => {
        resolvedOutcome = res;
        return res;
      });

      expect(controller.getPendingStartContinuation()).not.toBeNull();

      // Trigger stop() synchronously
      const stopPromise = controller.stop('user_stop');

      // Pending continuation must be cleared immediately (synchronous with stop cause)
      expect(controller.getPendingStartContinuation()).toBeNull();

      // On microtask queue, the promise resolves to 'cancelled' without advancing any timer
      await Promise.resolve();
      expect(resolvedOutcome).toBe('cancelled');
      const outcome = await continuationPromise;
      expect(outcome).toBe('cancelled');

      // Advancing past delayMs produces nothing further
      await vi.advanceTimersByTimeAsync(35000);
      await stopPromise;
      expect(controller.getPendingStartContinuation()).toBeNull();
    });
  });

  describe('Point 3: Cancellation via dispose(), retry(), epoch advance, terminal auth, and explicit cancel', () => {
    it('cancels continuation immediately on dispose()', async () => {
      const epoch = controller.getEpoch();
      let resolvedOutcome: StartContinuationOutcome | undefined;

      const continuationPromise = controller.scheduleStartContinuation({
        epoch,
        delayMs: 10000,
        reason: 'recording_retry_after',
      }).then((res) => {
        resolvedOutcome = res;
        return res;
      });

      expect(controller.getPendingStartContinuation()).not.toBeNull();

      controller.dispose();

      expect(controller.getPendingStartContinuation()).toBeNull();
      await Promise.resolve();
      expect(resolvedOutcome).toBe('cancelled');
      expect(await continuationPromise).toBe('cancelled');

      await vi.advanceTimersByTimeAsync(15000);
    });

    it('cancels continuation immediately on retry()', async () => {
      const epoch = controller.getEpoch();
      let resolvedOutcome: StartContinuationOutcome | undefined;

      const continuationPromise = controller.scheduleStartContinuation({
        epoch,
        delayMs: 10000,
        reason: 'recording_retry_after',
      }).then((res) => {
        resolvedOutcome = res;
        return res;
      });

      expect(controller.getPendingStartContinuation()).not.toBeNull();

      // Initiate retry with a VOD target
      void controller.retry({ kind: 'vod', recordingId: 'rec-1' });

      expect(controller.getPendingStartContinuation()).toBeNull();
      await Promise.resolve();
      expect(resolvedOutcome).toBe('cancelled');
      expect(await continuationPromise).toBe('cancelled');
    });

    it('cancels continuation when playbackEpoch advances via allocatePlaybackEpoch() (C1)', async () => {
      const epoch = controller.getEpoch();
      let resolvedOutcome: StartContinuationOutcome | undefined;

      const continuationPromise = controller.scheduleStartContinuation({
        epoch,
        delayMs: 10000,
        reason: 'recording_retry_after',
      }).then((res) => {
        resolvedOutcome = res;
        return res;
      });

      expect(controller.getPendingStartContinuation()?.epoch).toBe(epoch);

      // Advance epoch to epoch + 1
      const newEpoch = controller.allocatePlaybackEpoch();
      expect(newEpoch).toBeGreaterThan(epoch);

      // Old continuation is cancelled synchronously with epoch advance
      expect(controller.getPendingStartContinuation()).toBeNull();
      await Promise.resolve();
      expect(resolvedOutcome).toBe('cancelled');
      expect(await continuationPromise).toBe('cancelled');
    });

    it('does not cancel the continuation when beginPlaybackAttempt() is called with an epoch the controller never allocated (epoch authority)', async () => {
      const epoch = controller.getEpoch();
      let resolvedOutcome: StartContinuationOutcome | undefined;

      const continuationPromise = controller.scheduleStartContinuation({
        epoch,
        delayMs: 10000,
        reason: 'recording_retry_after',
      }).then((res) => {
        resolvedOutcome = res;
        return res;
      });

      expect(controller.getPendingStartContinuation()?.epoch).toBe(epoch);

      // An unallocated higher epoch is stale by definition (isStalePlaybackEpoch) and must be ignored:
      // the controller epoch does not move and the continuation stays pending.
      controller.beginPlaybackAttempt(epoch + 5, 'VOD', 'buffering');

      expect(controller.getEpoch()).toBe(epoch);
      expect(controller.getPendingStartContinuation()?.epoch).toBe(epoch);
      await Promise.resolve();
      expect(resolvedOutcome).toBeUndefined();

      // The real advance happens through allocatePlaybackEpoch(); beginPlaybackAttempt then runs for that epoch.
      const allocated = controller.allocatePlaybackEpoch();
      controller.beginPlaybackAttempt(allocated, 'VOD', 'buffering');
      expect(controller.getPendingStartContinuation()).toBeNull();
      await Promise.resolve();
      expect(resolvedOutcome).toBe('cancelled');
      expect(await continuationPromise).toBe('cancelled');
    });

    it('cancels continuation on terminal auth failure (C3: epoch <= authEpoch and undefined cancels all)', async () => {
      const epoch = controller.getEpoch();
      let resolvedOutcome: StartContinuationOutcome | undefined;

      const continuationPromise = controller.scheduleStartContinuation({
        epoch,
        delayMs: 10000,
        reason: 'recording_retry_after',
      }).then((res) => {
        resolvedOutcome = res;
        return res;
      });

      expect(controller.getPendingStartContinuation()).not.toBeNull();

      // Dispatch terminal auth failure for this epoch
      controller.dispatch({
        type: 'normative.playback.failure.raised',
        epoch,
        status: 'error',
        failure: buildPlaybackFailure(
          { code: 'SESSION_FORBIDDEN', status: 403, title: 'Forbidden', retryable: false },
          'native-host',
          { class: 'auth', terminal: true },
        ),
      });

      expect(controller.getPendingStartContinuation()).toBeNull();
      await Promise.resolve();
      expect(resolvedOutcome).toBe('cancelled');
      expect(await continuationPromise).toBe('cancelled');
    });

    it('cancels continuation on terminal auth with undefined epoch (C3)', async () => {
      const epoch = controller.getEpoch();
      let resolvedOutcome: StartContinuationOutcome | undefined;

      const continuationPromise = controller.scheduleStartContinuation({
        epoch,
        delayMs: 10000,
        reason: 'recording_retry_after',
      }).then((res) => {
        resolvedOutcome = res;
        return res;
      });

      expect(controller.getPendingStartContinuation()).not.toBeNull();

      // Dispatch terminal auth failure with undefined epoch
      controller.dispatch({
        type: 'normative.playback.failure.raised',
        epoch: undefined,
        status: 'error',
        failure: buildPlaybackFailure(
          { code: 'SESSION_UNAUTHORIZED', status: 401, title: 'Unauthorized', retryable: false },
          'native-host',
          { class: 'auth', terminal: true },
        ),
      } as any);

      expect(controller.getPendingStartContinuation()).toBeNull();
      await Promise.resolve();
      expect(resolvedOutcome).toBe('cancelled');
      expect(await continuationPromise).toBe('cancelled');
    });

    it('cancels continuation via explicit cancelStartContinuation()', async () => {
      const epoch = controller.getEpoch();
      let resolvedOutcome: StartContinuationOutcome | undefined;

      const continuationPromise = controller.scheduleStartContinuation({
        epoch,
        delayMs: 10000,
        reason: 'recording_retry_after',
      }).then((res) => {
        resolvedOutcome = res;
        return res;
      });

      expect(controller.getPendingStartContinuation()).not.toBeNull();

      controller.cancelStartContinuation(epoch);

      expect(controller.getPendingStartContinuation()).toBeNull();
      await Promise.resolve();
      expect(resolvedOutcome).toBe('cancelled');
      expect(await continuationPromise).toBe('cancelled');
    });
  });

  describe('Point 4: Replacement and invalid scheduling', () => {
    it('rescheduling for the same epoch replaces the earlier continuation immediately with cancelled', async () => {
      const epoch = controller.getEpoch();
      let p1Outcome: StartContinuationOutcome | undefined;
      let p2Outcome: StartContinuationOutcome | undefined;

      const p1 = controller.scheduleStartContinuation({
        epoch,
        delayMs: 15000,
        reason: 'recording_retry_after',
      }).then((res) => {
        p1Outcome = res;
        return res;
      });

      expect(controller.getPendingStartContinuation()?.delayMs).toBe(15000);

      const p2 = controller.scheduleStartContinuation({
        epoch,
        delayMs: 5000,
        reason: 'recording_retry_after',
      }).then((res) => {
        p2Outcome = res;
        return res;
      });

      // p1 is cancelled immediately
      await Promise.resolve();
      expect(p1Outcome).toBe('cancelled');
      expect(await p1).toBe('cancelled');

      // p2 is the active pending continuation
      expect(controller.getPendingStartContinuation()?.delayMs).toBe(5000);
      expect(p2Outcome).toBeUndefined();

      // Advance to 5000 ms: p2 proceeds
      await vi.advanceTimersByTimeAsync(5000);
      expect(p2Outcome).toBe('proceed');
      expect(await p2).toBe('proceed');
      expect(controller.getPendingStartContinuation()).toBeNull();
    });

    it('C1 Invariant: schedule, advance epoch, schedule again -> only newer is pending, older cancelled synchronously, at most 1 continuation pending at all times', async () => {
      const epoch1 = controller.getEpoch();
      let p1Outcome: StartContinuationOutcome | undefined;

      const p1 = controller.scheduleStartContinuation({
        epoch: epoch1,
        delayMs: 20000,
        reason: 'recording_retry_after',
      }).then((res) => {
        p1Outcome = res;
        return res;
      });

      expect(controller.getPendingStartContinuation()?.epoch).toBe(epoch1);

      // Advance epoch
      const epoch2 = controller.allocatePlaybackEpoch();
      expect(controller.getPendingStartContinuation()).toBeNull();
      await Promise.resolve();
      expect(p1Outcome).toBe('cancelled');
      expect(await p1).toBe('cancelled');

      // Schedule for new epoch
      let p2Outcome: StartContinuationOutcome | undefined;
      const p2 = controller.scheduleStartContinuation({
        epoch: epoch2,
        delayMs: 10000,
        reason: 'recording_retry_after',
      }).then((res) => {
        p2Outcome = res;
        return res;
      });

      // Exactly one continuation pending, for epoch2
      expect(controller.getPendingStartContinuation()).toEqual({
        epoch: epoch2,
        delayMs: 10000,
        reason: 'recording_retry_after',
      });

      await vi.advanceTimersByTimeAsync(10000);
      expect(p2Outcome).toBe('proceed');
      expect(await p2).toBe('proceed');
      expect(controller.getPendingStartContinuation()).toBeNull();
    });

    it('scheduling on a stale or stopped epoch resolves cancelled immediately without setting a timer or touching current continuation (C3)', async () => {
      const currentEpoch = controller.getEpoch();
      controller.scheduleStartContinuation({
        epoch: currentEpoch,
        delayMs: 10000,
        reason: 'recording_retry_after',
      });

      expect(controller.getPendingStartContinuation()?.epoch).toBe(currentEpoch);

      // Schedule with stale epoch (e.g. currentEpoch - 1)
      const stalePromise = controller.scheduleStartContinuation({
        epoch: currentEpoch - 1,
        delayMs: 5000,
        reason: 'recording_retry_after',
      });

      const outcome = await stalePromise;
      expect(outcome).toBe('cancelled');

      // Current continuation for currentEpoch is completely untouched
      expect(controller.getPendingStartContinuation()?.epoch).toBe(currentEpoch);
      expect(controller.getPendingStartContinuation()?.delayMs).toBe(10000);
    });

    it('scheduling on a disposed controller resolves cancelled immediately without timer', async () => {
      const epoch = controller.getEpoch();
      controller.dispose();

      const p = controller.scheduleStartContinuation({
        epoch,
        delayMs: 5000,
        reason: 'recording_retry_after',
      });

      expect(await p).toBe('cancelled');
      expect(controller.getPendingStartContinuation()).toBeNull();
      expect(vi.getTimerCount()).toBe(0);
    });
  });

  describe('Point 5: Command drain separation', () => {
    it('continuation is never dispatched as a command or queued in command executor', async () => {
      const epoch = controller.getEpoch();
      const p = controller.scheduleStartContinuation({
        epoch,
        delayMs: 1000,
        reason: 'recording_retry_after',
      });

      // No commands executed for scheduling start continuation
      expect(executedCommands).toHaveLength(0);

      await vi.advanceTimersByTimeAsync(1000);
      expect(await p).toBe('proceed');

      expect(executedCommands).toHaveLength(0);
    });
  });
});
