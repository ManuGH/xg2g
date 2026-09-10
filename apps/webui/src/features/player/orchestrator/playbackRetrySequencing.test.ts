// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  createPlaybackController,
  type PlaybackController,
  type PlaybackRetryTarget,
} from './playbackController';
import type { LiveSessionTransport } from './liveSessionTransport';
import type { PlaybackCommand, PlaybackDomainState } from './playbackTypes';
import { createRecoveryLadderState } from './recoveryLadder';
import { buildPlaybackFailure } from './playbackMachine';

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

interface Deferred<T> {
  promise: Promise<T>;
  resolve: (value: T) => void;
  reject: (err: unknown) => void;
}

function defer<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  let reject!: (err: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
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

describe('PlaybackController: Retry Sequencing (Recovery Step 2)', () => {
  let transport: LiveSessionTransport;
  let executedCommands: PlaybackCommand[];
  let controller: PlaybackController;

  beforeEach(() => {
    vi.useFakeTimers();
    transport = createMockTransport();
    executedCommands = [];
  });

  afterEach(() => {
    controller?.dispose();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  const liveTarget: PlaybackRetryTarget = {
    kind: 'live',
    serviceRef: '1:0:19:283D:3FB:1:C00000:0:0:0:',
    explicitProfile: 'hd',
  };

  const vodTarget: PlaybackRetryTarget = {
    kind: 'vod',
    recordingId: 'rec-456',
    explicitProfile: 'direct',
  };

  const srcTarget: PlaybackRetryTarget = {
    kind: 'src',
    srcUrl: 'https://example.com/hls/test.m3u8',
  };

  describe('Scenario 1: Ordinary retry sequencing', () => {
    it('executes stop before restart, dispatches captured target, allocates a new epoch, and reports restarted', async () => {
      const simulateStart = true;
      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          executedCommands.push(cmd);
          if (cmd.type === 'command.playback.start' && simulateStart) {
            // Emulate startStream adapter behavior
            const epoch = controller.allocatePlaybackEpoch();
            controller.beginPlaybackAttempt(epoch, 'LIVE', 'starting');
          }
        },
      });

      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      const retryPromise = controller.retry(liveTarget);
      expect(controller.isRetryInFlight()).toBe(true);

      const result = await retryPromise;
      expect(result.status).toBe('restarted');
      if (result.status === 'restarted') {
        expect(result.epoch).toBeGreaterThan(1);
      }
      expect(controller.isRetryInFlight()).toBe(false);

      // Verify commands: intent.stop.requested was processed, followed by command.playback.start
      const startCmd = executedCommands.find((c) => c.type === 'command.playback.start') as Extract<
        PlaybackCommand,
        { type: 'command.playback.start' }
      >;
      expect(startCmd).toBeDefined();
      expect(startCmd.kind).toBe('live');
      expect(startCmd.serviceRef).toBe(liveTarget.serviceRef);
      expect(startCmd.explicitProfile).toBe(liveTarget.explicitProfile);
    });
  });

  describe('Scenario 2: Deferred teardown', () => {
    it('does not restart before stop teardown completes and restarts cleanly after completion', async () => {
      const stopTeardownDeferred = defer<void>();

      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          executedCommands.push(cmd);
          if (cmd.type === 'command.playback.stop') {
            return stopTeardownDeferred.promise;
          }
          if (cmd.type === 'command.playback.start') {
            const epoch = controller.allocatePlaybackEpoch();
            controller.beginPlaybackAttempt(epoch, 'LIVE', 'starting');
          }
        },
      });

      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      let settled = false;
      const retryPromise = controller.retry(liveTarget).then((r) => {
        settled = true;
        return r;
      });

      expect(controller.isRetryInFlight()).toBe(true);

      // Verify no start command has been emitted yet while stop teardown is pending
      await Promise.resolve(); // flush microtasks
      expect(executedCommands.some((c) => c.type === 'command.playback.start')).toBe(false);
      expect(settled).toBe(false);

      // Resolve stop teardown
      stopTeardownDeferred.resolve();
      const result = await retryPromise;

      expect(result.status).toBe('restarted');
      expect(executedCommands.some((c) => c.type === 'command.playback.start')).toBe(true);
      expect(controller.isRetryInFlight()).toBe(false);
    });
  });

  describe('Scenario 3: Duplicate retry coalescing', () => {
    it('returns the identical public promise for concurrent retries with matching target and issues exactly 1 restart', async () => {
      const stopTeardownDeferred = defer<void>();

      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          executedCommands.push(cmd);
          if (cmd.type === 'command.playback.stop') {
            return stopTeardownDeferred.promise;
          }
          if (cmd.type === 'command.playback.start') {
            const epoch = controller.allocatePlaybackEpoch();
            controller.beginPlaybackAttempt(epoch, 'LIVE', 'starting');
          }
        },
      });

      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      const p1 = controller.retry(liveTarget);
      const p2 = controller.retry(liveTarget);

      // Promise identity must be identical for coalesced caller
      expect(p1).toBe(p2);

      stopTeardownDeferred.resolve();
      const [res1, res2] = await Promise.all([p1, p2]);

      expect(res1).toEqual(res2);
      expect(res1.status).toBe('restarted');

      const startCommands = executedCommands.filter((c) => c.type === 'command.playback.start');
      expect(startCommands.length).toBe(1);
    });
  });

  describe('Scenario 4: Immediate public promise return and prompt cancellation', () => {
    it('settles public retry promise promptly upon cancellation while stop is still pending, and resolving stop later produces zero starts', async () => {
      const stopTeardownDeferred = defer<void>();

      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          executedCommands.push(cmd);
          if (cmd.type === 'command.playback.stop') {
            return stopTeardownDeferred.promise;
          }
          if (cmd.type === 'command.playback.start') {
            const epoch = controller.allocatePlaybackEpoch();
            controller.beginPlaybackAttempt(epoch, 'LIVE', 'starting');
          }
        },
      });

      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      const retryPromise = controller.retry(liveTarget);
      expect(controller.isRetryInFlight()).toBe(true);

      // User calls stop while teardown is pending
      const stopPromise = controller.stop('user_stop');

      // The public retry promise MUST settle promptly without waiting for stopTeardownDeferred!
      const retryResult = await retryPromise;
      expect(retryResult.status).toBe('cancelled');
      if (retryResult.status === 'cancelled') {
        expect(retryResult.reason).toBe('user_stop');
      }
      expect(controller.isRetryInFlight()).toBe(false);

      // Teardown promise is still unresolved at this moment
      expect(executedCommands.some((c) => c.type === 'command.playback.start')).toBe(false);

      // Now resolve the deferred stop teardown
      stopTeardownDeferred.resolve();
      await stopPromise;

      // Ensure that late completion of stop never delivered a start command!
      const startCommands = executedCommands.filter((c) => c.type === 'command.playback.start');
      expect(startCommands.length).toBe(0);
    });
  });

  describe('Scenario 5: Explicit user stop and coalesced stop cancellation', () => {
    it('cancels pending retry even when controller.stop coalesces with in-flight stop promise', async () => {
      const stopDeferred = defer<void>();

      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          executedCommands.push(cmd);
          if (cmd.type === 'command.playback.stop') {
            return stopDeferred.promise;
          }
          if (cmd.type === 'command.playback.start') {
            const epoch = controller.allocatePlaybackEpoch();
            controller.beginPlaybackAttempt(epoch, 'LIVE', 'starting');
          }
        },
      });

      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      const retryPromise = controller.retry(liveTarget);

      // User stops: because stop is already in flight, stop() coalesces with the in-flight stop promise
      const userStopPromise = controller.stop('user_stop');

      // Retry promise must be promptly cancelled
      const retryResult = await retryPromise;
      expect(retryResult).toEqual({ status: 'cancelled', reason: 'user_stop' });

      // Late resolution of the stop promise
      stopDeferred.resolve();
      await userStopPromise;

      // Zero starts must occur
      expect(executedCommands.filter((c) => c.type === 'command.playback.start')).toHaveLength(0);
    });
  });

  describe('Scenario 6: Synchronous nested public stop from command executor', () => {
    it.each([false, true])('coalesces synchronous nested public stop calls with exactly one teardown command (returnNestedPromise: %s)', async (returnNestedPromise) => {
      let stopCommands = 0;
      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          executedCommands.push(cmd);
          if (cmd.type === 'command.playback.stop') {
            stopCommands += 1;
            const nested = controller.stop('user_stop');
            if (returnNestedPromise) return nested;
          }
          if (cmd.type === 'command.playback.start') {
            const epoch = controller.allocatePlaybackEpoch();
            controller.beginPlaybackAttempt(epoch, 'LIVE', 'starting');
          }
        },
      });

      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      const retryResult = await controller.retry(liveTarget);

      expect(retryResult).toEqual({ status: 'cancelled', reason: 'user_stop' });
      let settled = false;
      void controller.stop('user_stop').then(() => { settled = true; });
      for (let i = 0; i < 30; i += 1) await Promise.resolve();
      expect(stopCommands).toBe(1);
      expect(settled).toBe(true);
      expect(executedCommands.filter((c) => c.type === 'command.playback.stop')).toHaveLength(1);
      expect(executedCommands.filter((c) => c.type === 'command.playback.start')).toHaveLength(0);
    });
  });

  describe('Scenario 7: Source A retry superseded by Source B', () => {
    it('cancels A promptly with superseded when B is requested, and late completion of A cannot affect B', async () => {
      const stopTeardownDeferred = defer<void>();

      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          executedCommands.push(cmd);
          if (cmd.type === 'command.playback.stop') {
            return stopTeardownDeferred.promise;
          }
          if (cmd.type === 'command.playback.start') {
            const epoch = controller.allocatePlaybackEpoch();
            controller.beginPlaybackAttempt(epoch, 'VOD', 'starting');
          }
        },
      });

      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      const pA = controller.retry(liveTarget);
      // New retry for VOD target while A teardown is pending
      const pB = controller.retry(vodTarget);

      // pA must resolve promptly with superseded
      const resA = await pA;
      expect(resA).toEqual({ status: 'cancelled', reason: 'superseded' });

      // pB is now active
      expect(controller.isRetryInFlight()).toBe(true);

      // Now release teardown
      stopTeardownDeferred.resolve();
      const resB = await pB;

      expect(resB.status).toBe('restarted');

      // The only start command emitted must be for B (VOD)
      const startCmds = executedCommands.filter((c) => c.type === 'command.playback.start') as Array<
        Extract<PlaybackCommand, { type: 'command.playback.start' }>
      >;
      expect(startCmds).toHaveLength(1);
      const startCmd = startCmds[0]!;
      expect(startCmd.kind).toBe('vod');
      expect(startCmd.recordingId).toBe('rec-456');
    });
  });

  describe('Scenario 8: Terminal auth during retry (owner-aware)', () => {
    it('cancels active retry with terminal_auth when failure matches or exceeds initial epoch', async () => {
      const stopDeferred = defer<void>();

      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          executedCommands.push(cmd);
          if (cmd.type === 'command.playback.stop') {
            return stopDeferred.promise;
          }
        },
      });

      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      const retryPromise = controller.retry(liveTarget);

      // Dispatch terminal auth failure for the active retry epoch (initial epoch 1)
      controller.dispatch({
        type: 'normative.playback.failure.raised',
        epoch: 1,
        failure: buildPlaybackFailure(
          { code: 'SESSION_FORBIDDEN', status: 403, message: 'Forbidden' } as any,
          'native-host',
          { class: 'auth', terminal: true },
        ),
      });

      const result = await retryPromise;
      expect(result).toEqual({ status: 'cancelled', reason: 'terminal_auth' });

      stopDeferred.resolve();
      await Promise.resolve();

      expect(executedCommands.filter((c) => c.type === 'command.playback.start')).toHaveLength(0);
    });

    it('does NOT cancel active retry when terminal auth failure is from an older stale epoch', async () => {
      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          executedCommands.push(cmd);
          if (cmd.type === 'command.playback.start') {
            const epoch = controller.allocatePlaybackEpoch();
            controller.beginPlaybackAttempt(epoch, 'LIVE', 'starting');
          }
        },
      });

      // Advance to epoch 5
      controller.allocatePlaybackEpoch(); // 2
      controller.allocatePlaybackEpoch(); // 3
      controller.allocatePlaybackEpoch(); // 4
      controller.allocatePlaybackEpoch(); // 5
      controller.beginPlaybackAttempt(5, 'LIVE', 'playing', true);

      // Retry begins at initialEpoch 5
      const retryPromise = controller.retry(liveTarget);

      // Stale auth failure from epoch 2 (an old abandoned session)
      controller.dispatch({
        type: 'normative.playback.failure.raised',
        epoch: 2,
        failure: buildPlaybackFailure(
          { code: 'SESSION_FORBIDDEN', status: 403, message: 'Stale Forbidden' } as any,
          'native-host',
          { class: 'auth', terminal: true },
        ),
      });

      // Active retry must NOT be cancelled by the old epoch failure!
      const result = await retryPromise;
      expect(result.status).toBe('restarted');
    });
  });

  describe('Scenario 9: Dispose and reactivation', () => {
    it('cancels pending retry with disposed and clean reactivation is unaffected', async () => {
      const stopDeferred = defer<void>();

      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          executedCommands.push(cmd);
          if (cmd.type === 'command.playback.stop') {
            return stopDeferred.promise;
          }
          if (cmd.type === 'command.playback.start') {
            const epoch = controller.allocatePlaybackEpoch();
            controller.beginPlaybackAttempt(epoch, 'LIVE', 'starting');
          }
        },
      });

      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      const retryPromise = controller.retry(liveTarget);
      controller.dispose();

      const result = await retryPromise;
      expect(result).toEqual({ status: 'cancelled', reason: 'disposed' });

      // Calling retry on a disposed controller returns disposed immediately
      const postDisposeRetry = await controller.retry(liveTarget);
      expect(postDisposeRetry).toEqual({ status: 'cancelled', reason: 'disposed' });

      // Reactivate
      controller.activate();
      stopDeferred.resolve();
      const freshRetry = await controller.retry(liveTarget);
      expect(freshRetry.status).toBe('restarted');
    });
  });

  describe('Scenario 10: Missing target validation', () => {
    it('returns cancelled missing_target without stopping or starting when target has empty source', async () => {
      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          executedCommands.push(cmd);
        },
      });

      const emptyTarget: PlaybackRetryTarget = {
        kind: 'live',
        serviceRef: '   ',
      };

      const result = await controller.retry(emptyTarget);
      expect(result).toEqual({ status: 'cancelled', reason: 'missing_target' });
      expect(executedCommands).toHaveLength(0);
      expect(controller.isRetryInFlight()).toBe(false);
    });

    it('validates and handles empty VOD and src targets', async () => {
      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          executedCommands.push(cmd);
        },
      });

      const resVod = await controller.retry({ kind: 'vod', recordingId: '' });
      expect(resVod).toEqual({ status: 'cancelled', reason: 'missing_target' });

      const resSrc = await controller.retry({ kind: 'src', srcUrl: '   ' });
      expect(resSrc).toEqual({ status: 'cancelled', reason: 'missing_target' });

      expect(executedCommands).toHaveLength(0);
    });
  });

  describe('Scenario 11: Source and profile matrix', () => {
    it('preserves target identities for Live, VOD, and src with explicitProfile', async () => {
      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          executedCommands.push(cmd);
          if (cmd.type === 'command.playback.start') {
            const epoch = controller.allocatePlaybackEpoch();
            controller.beginPlaybackAttempt(epoch, cmd.kind === 'vod' ? 'VOD' : 'LIVE', 'starting');
          }
        },
      });

      const getLatestStartCmd = () => {
        const starts = executedCommands.filter((c) => c.type === 'command.playback.start') as Array<
          Extract<PlaybackCommand, { type: 'command.playback.start' }>
        >;
        return starts[starts.length - 1]!;
      };

      // 1. Live target
      const resLive = await controller.retry(liveTarget);
      expect(resLive.status).toBe('restarted');
      let lastStart = getLatestStartCmd();
      expect(lastStart.kind).toBe('live');
      expect(lastStart.serviceRef).toBe(liveTarget.serviceRef);
      expect(lastStart.explicitProfile).toBe('hd');

      // 2. VOD target
      const resVod = await controller.retry(vodTarget);
      expect(resVod.status).toBe('restarted');
      lastStart = getLatestStartCmd();
      expect(lastStart.kind).toBe('vod');
      expect(lastStart.recordingId).toBe('rec-456');
      expect(lastStart.explicitProfile).toBe('direct');

      // 3. Src target
      const resSrc = await controller.retry(srcTarget);
      expect(resSrc.status).toBe('restarted');
      lastStart = getLatestStartCmd();
      expect(lastStart.kind).toBe('src');
      expect(lastStart.srcUrl).toBe(srcTarget.srcUrl);
    });
  });

  describe('Scenario 12: Absent executor or start failure', () => {
    it('reports cancelled with error if no command executor is present to start playback', async () => {
      controller = createPlaybackController({
        transport,
        createInitialState,
        // No executeCommand provided!
      });

      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      const result = await controller.retry(liveTarget);
      expect(result).toEqual({ status: 'cancelled', reason: 'error' });
      expect(controller.isRetryInFlight()).toBe(false);
    });

    it('reports cancelled with error if executor throws synchronously', async () => {
      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          if (cmd.type === 'command.playback.start') {
            throw new Error('Immediate start failure');
          }
        },
      });

      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      const result = await controller.retry(liveTarget);
      expect(result).toEqual({ status: 'cancelled', reason: 'error' });
      expect(controller.isRetryInFlight()).toBe(false);
    });

    it('cancels retry as superseded if external epoch allocation occurs while stopping', async () => {
      const stopDeferred = defer<void>();

      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          if (cmd.type === 'command.playback.stop') {
            return stopDeferred.promise;
          }
        },
      });

      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);

      const retryPromise = controller.retry(liveTarget);

      // An external source or user starts a different attempt while retry is stopping
      controller.allocatePlaybackEpoch();

      const result = await retryPromise;
      expect(result).toEqual({ status: 'cancelled', reason: 'superseded' });

      stopDeferred.resolve();
    });
  });

  describe('Scenario 17: Asynchronous start promise rejection', () => {
    it('observes rejection of current start promise without unhandled rejection and clears retry', async () => {
      const startDeferred = defer<void>();
      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          executedCommands.push(cmd);
          if (cmd.type === 'command.playback.start') {
            const epoch = controller.allocatePlaybackEpoch();
            controller.beginPlaybackAttempt(epoch, 'LIVE', 'starting');
            return startDeferred.promise;
          }
        },
      });

      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);
      const result = await controller.retry(liveTarget);
      expect(result.status).toBe('restarted');
      expect(controller.isRetryInFlight()).toBe(true);

      startDeferred.reject(new Error('simulated async start rejection'));
      await startDeferred.promise.catch(() => {});
      for (let i = 0; i < 5; i += 1) await Promise.resolve();

      expect(controller.isRetryInFlight()).toBe(false);
    });

    it('handles late rejection after cancellation without affecting state or unhandled errors', async () => {
      const startDeferred = defer<void>();
      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          executedCommands.push(cmd);
          if (cmd.type === 'command.playback.start') {
            const epoch = controller.allocatePlaybackEpoch();
            controller.beginPlaybackAttempt(epoch, 'LIVE', 'starting');
            return startDeferred.promise;
          }
        },
      });

      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);
      const result = await controller.retry(liveTarget);
      expect(result.status).toBe('restarted');
      expect(controller.isRetryInFlight()).toBe(true);

      // User calls stop while start is pending
      await controller.stop('user_stop');
      expect(controller.isRetryInFlight()).toBe(false);

      // Late rejection arrives after cancellation
      startDeferred.reject(new Error('simulated late rejection after stop'));
      await startDeferred.promise.catch(() => {});
      for (let i = 0; i < 5; i += 1) await Promise.resolve();

      expect(controller.isRetryInFlight()).toBe(false);
    });

    it('does not mutate replacement retry when previous start promise rejects late', async () => {
      const startDeferred1 = defer<void>();
      const startDeferred2 = defer<void>();
      let startCount = 0;
      controller = createPlaybackController({
        transport,
        createInitialState,
        executeCommand: (cmd) => {
          executedCommands.push(cmd);
          if (cmd.type === 'command.playback.start') {
            startCount += 1;
            const epoch = controller.allocatePlaybackEpoch();
            controller.beginPlaybackAttempt(epoch, 'LIVE', 'starting');
            return startCount === 1 ? startDeferred1.promise : startDeferred2.promise;
          }
        },
      });

      controller.beginPlaybackAttempt(1, 'LIVE', 'playing', true);
      // First retry
      const result1 = await controller.retry(liveTarget);
      expect(result1.status).toBe('restarted');
      expect(controller.isRetryInFlight()).toBe(true);

      // Replacement retry with different target (supersedes first retry)
      const vodTarget: PlaybackRetryTarget = { kind: 'vod', recordingId: 'rec-replace' };
      const result2 = await controller.retry(vodTarget);
      expect(result2.status).toBe('restarted');
      expect(controller.isRetryInFlight()).toBe(true);

      // Previous start promise rejects late
      startDeferred1.reject(new Error('simulated late rejection of replaced retry'));
      await startDeferred1.promise.catch(() => {});
      for (let i = 0; i < 5; i += 1) await Promise.resolve();

      // Replacement retry must STILL be in flight!
      expect(controller.isRetryInFlight()).toBe(true);

      // Complete replacement
      startDeferred2.resolve();
      await startDeferred2.promise;
      for (let i = 0; i < 5; i += 1) await Promise.resolve();
      expect(controller.isRetryInFlight()).toBe(false);
    });
  });
});
