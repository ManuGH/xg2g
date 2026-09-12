// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createPlaybackController, parseSessionId } from './playbackController';
import type {
  LiveSessionTransport,
  SessionReadyResult,
  StartIntentResult,
  StreamInfoResult,
} from './liveSessionTransport';
import type { PlaybackDomainState } from './playbackTypes';
import { createRecoveryLadderState } from './recoveryLadder';

function createMockDomainState(): PlaybackDomainState {
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

function accepted(sessionId: string): StartIntentResult {
  return {
    status: 202,
    headers: new Headers(),
    data: { sessionId },
  };
}

describe('PlaybackController - Deterministic Race & Adoption Tests', () => {
  let stopCalls: Array<{ sessionId: string }> = [];

  beforeEach(() => {
    stopCalls = [];
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  function createMockTransport(overrides?: Partial<LiveSessionTransport>): LiveSessionTransport {
    return {
      fetchStreamInfo: vi.fn().mockResolvedValue({
        status: 200,
        data: {
          mode: 'direct_stream',
          playbackDecisionToken: 'token-default',
          decision: { mode: 'direct_stream', playbackDecisionToken: 'token-default' },
        },
        headers: new Headers(),
      } satisfies StreamInfoResult),

      postStartIntent: vi.fn().mockResolvedValue({
        status: 200,
        data: { sessionId: 'session-default-1' },
        headers: new Headers(),
      } satisfies StartIntentResult),

      waitForReady: vi.fn().mockResolvedValue({
        sessionId: 'session-default-1',
        playbackUrl: 'http://localhost/stream.m3u8',
        heartbeatIntervalSeconds: 5,
        leaseExpiresAt: '2026-09-07T22:00:00Z',
      } satisfies SessionReadyResult),

      postStopIntent: vi.fn().mockImplementation(async ({ sessionId }) => {
        stopCalls.push({ sessionId });
      }),

      postHeartbeat: vi.fn().mockImplementation(async ({ sessionId }) => ({
        status: 200,
        data: {
          acknowledged: true,
          sessionId,
          leaseExpiresAt: '2026-09-07T22:00:30Z',
        },
        headers: new Headers(),
      })),

      fetchSessionSnapshot: vi.fn().mockImplementation(async ({ sessionId }) => ({
        status: 200,
        data: {
          sessionId,
          state: 'READY',
        },
        headers: new Headers(),
      })),

      ...overrides,
    };
  }

  it('Race Test 1: B returns S after 4 seconds -> S is adopted cleanly by B, 0 stops issued', async () => {
    const aStartDeferred = defer<StartIntentResult>();
    const bStartDeferred = defer<StartIntentResult>();

    let postStartCount = 0;
    const transport = createMockTransport({
      postStartIntent: vi.fn().mockImplementation(async () => {
        postStartCount++;
        if (postStartCount === 1) {
          return aStartDeferred.promise;
        }
        return bStartDeferred.promise;
      }),
    });

    const controller = createPlaybackController({
      transport,
      createInitialState: createMockDomainState,
    });

    // 1. Start A
    const aPromise = controller.startLive({ serviceRef: 'srv_1' });

    // Let A preflight resolve and submit start intent
    await vi.advanceTimersByTimeAsync(10);

    // 2. Start B (supersedes A)
    const bPromise = controller.startLive({ serviceRef: 'srv_1' });

    // A settles promptly with cancelled
    const aResult = await aPromise;
    expect(aResult).toEqual({ status: 'cancelled', reason: 'superseded' });

    // Let B preflight resolve and submit start intent
    await vi.advanceTimersByTimeAsync(10);

    expect(controller.getInFlightStartsCount()).toBe(2); // A and B both in flight

    // 3. At t=0, A's response returns with session S
    aStartDeferred.resolve({
      status: 200,
      data: { sessionId: 'session-S' },
      headers: new Headers(),
    });
    await vi.advanceTimersByTimeAsync(10);

    // A should NOT stop session-S because B is in flight!
    expect(stopCalls).toHaveLength(0);
    expect(controller.getPendingAdoptionCandidatesCount()).toBe(1);

    // 4. Advance time by 4 seconds
    await vi.advanceTimersByTimeAsync(4_000);

    // B is still within settlement timeout (10s), so S is still held
    expect(stopCalls).toHaveLength(0);

    // 5. At t=4s, B returns session S
    bStartDeferred.resolve({
      status: 200,
      data: { sessionId: 'session-S' },
      headers: new Headers(),
    });
    await vi.advanceTimersByTimeAsync(10);

    const bResult = await bPromise;
    expect(bResult.status).toBe('ready');
    if (bResult.status === 'ready') {
      expect(bResult.sessionId).toBe('session-S');
    }

    // Zero stop requests must have been sent!
    expect(stopCalls).toHaveLength(0);
    expect(controller.getActiveSessionId()).toBe('session-S');
    expect(controller.getPendingAdoptionCandidatesCount()).toBe(0);
  });

  it('Race Test 2A: B returns just before settlement deadline -> adopts cleanly', async () => {
    const aStartDeferred = defer<StartIntentResult>();
    const bStartDeferred = defer<StartIntentResult>();

    let postStartCount = 0;
    const transport = createMockTransport({
      postStartIntent: vi.fn().mockImplementation(async () => {
        postStartCount++;
        return postStartCount === 1 ? aStartDeferred.promise : bStartDeferred.promise;
      }),
    });

    const controller = createPlaybackController({
      transport,
      createInitialState: createMockDomainState,
      startSettlementTimeoutMs: 10_000,
    });

    void controller.startLive({ serviceRef: 'srv_1' });
    await vi.advanceTimersByTimeAsync(10);

    const bPromise = controller.startLive({ serviceRef: 'srv_1' });
    await vi.advanceTimersByTimeAsync(10);

    // A returns session-S
    aStartDeferred.resolve({
      status: 200,
      data: { sessionId: 'session-S' },
      headers: new Headers(),
    });
    await vi.advanceTimersByTimeAsync(10);

    // Advance 9.9 seconds (just before 10s deadline)
    await vi.advanceTimersByTimeAsync(9_900);
    expect(stopCalls).toHaveLength(0);

    // B returns session-S at 9.9s
    bStartDeferred.resolve({
      status: 200,
      data: { sessionId: 'session-S' },
      headers: new Headers(),
    });
    await vi.advanceTimersByTimeAsync(10);

    const bResult = await bPromise;
    expect(bResult.status).toBe('ready');
    expect(stopCalls).toHaveLength(0);
  });

  it('Race Test 2B: Settlement deadline expires -> B is ineligible, S is stopped, late B cannot activate S', async () => {
    const aStartDeferred = defer<StartIntentResult>();
    const bStartDeferred = defer<StartIntentResult>();

    let postStartCount = 0;
    const transport = createMockTransport({
      postStartIntent: vi.fn().mockImplementation(async () => {
        postStartCount++;
        return postStartCount === 1 ? aStartDeferred.promise : bStartDeferred.promise;
      }),
    });

    const controller = createPlaybackController({
      transport,
      createInitialState: createMockDomainState,
      startSettlementTimeoutMs: 10_000,
    });

    void controller.startLive({ serviceRef: 'srv_1' });
    await vi.advanceTimersByTimeAsync(10);

    void controller.startLive({ serviceRef: 'srv_1' });
    await vi.advanceTimersByTimeAsync(10);

    // A returns session-S
    aStartDeferred.resolve({
      status: 200,
      data: { sessionId: 'session-S' },
      headers: new Headers(),
    });
    await vi.advanceTimersByTimeAsync(10);
    expect(stopCalls).toHaveLength(0);

    // Advance past the 10s deadline
    await vi.advanceTimersByTimeAsync(10_001);

    // B is now ineligible, candidate S must be stopped!
    expect(stopCalls).toHaveLength(1);
    expect(stopCalls[0]!.sessionId).toBe('session-S');
    expect(controller.getStoppingSessionIds().has('session-S')).toBe(true);

    // At t=11s, B returns session-S late
    bStartDeferred.resolve({
      status: 200,
      data: { sessionId: 'session-S' },
      headers: new Headers(),
    });
    await vi.advanceTimersByTimeAsync(10);

    // S must NOT be active!
    expect(controller.getActiveSessionId()).toBeNull();
  });

  it('Race Test 3: B returns after stop submission for S -> B cannot adopt S', async () => {
    const aStartDeferred = defer<StartIntentResult>();
    const bStartDeferred = defer<StartIntentResult>();

    let postStartCount = 0;
    const transport = createMockTransport({
      postStartIntent: vi.fn().mockImplementation(async () => {
        postStartCount++;
        return postStartCount === 1 ? aStartDeferred.promise : bStartDeferred.promise;
      }),
      // Simulated slow stop response
      postStopIntent: vi.fn().mockImplementation(async ({ sessionId }) => {
        stopCalls.push({ sessionId });
        await new Promise((r) => setTimeout(r, 2_000));
      }),
    });

    const controller = createPlaybackController({
      transport,
      createInitialState: createMockDomainState,
    });

    // Start A
    void controller.startLive({ serviceRef: 'srv_1' });
    await vi.advanceTimersByTimeAsync(10);

    // User stops playback before B even starts!
    await controller.stop();

    // Now A returns session-S (no other starts in flight)
    aStartDeferred.resolve({
      status: 200,
      data: { sessionId: 'session-S' },
      headers: new Headers(),
    });
    await vi.advanceTimersByTimeAsync(10);

    // S was submitted for stop and is in stoppingSessionIds
    expect(stopCalls).toHaveLength(1);
    expect(stopCalls[0]!.sessionId).toBe('session-S');
    expect(controller.getStoppingSessionIds().has('session-S')).toBe(true);

    // Now user starts B, and server replays session-S
    const bPromise = controller.startLive({ serviceRef: 'srv_1' });
    const bAssertion = expect(bPromise).rejects.toThrow(/already revoked or stopping/i);
    await vi.advanceTimersByTimeAsync(10);

    bStartDeferred.resolve({
      status: 200,
      data: { sessionId: 'session-S' },
      headers: new Headers(),
    });
    await vi.advanceTimersByTimeAsync(10);

    // B must reject / fail because S is stopping!
    await bAssertion;
    expect(controller.getActiveSessionId()).toBeNull();
  });

  it('Race Test 4: Later C replays S during an ambiguous/in-flight stop -> C cannot adopt S', async () => {
    const transport = createMockTransport({
      postStartIntent: vi.fn().mockResolvedValue({
        status: 200,
        data: { sessionId: 'session-S' },
        headers: new Headers(),
      }),
    });

    const controller = createPlaybackController({
      transport,
      createInitialState: createMockDomainState,
    });

    // Start A
    await controller.startLive({ serviceRef: 'srv_1' });
    expect(controller.getActiveSessionId()).toBe('session-S');

    // Stop A -> session-S is stopping
    await controller.stop();
    expect(controller.getStoppingSessionIds().has('session-S')).toBe(true);

    // Now C starts and backend replays session-S
    const cPromise = controller.startLive({ serviceRef: 'srv_1' });
    const cAssertion = expect(cPromise).rejects.toThrow(/already revoked or stopping/i);
    await vi.advanceTimersByTimeAsync(10);

    await cAssertion;
    expect(controller.getActiveSessionId()).toBeNull();
  });

  it('Race Test 5A: Response order B before A -> 0 stops, active session stays protected', async () => {
    const aStartDeferred = defer<StartIntentResult>();
    const bStartDeferred = defer<StartIntentResult>();

    let postStartCount = 0;
    const transport = createMockTransport({
      postStartIntent: vi.fn().mockImplementation(async () => {
        postStartCount++;
        return postStartCount === 1 ? aStartDeferred.promise : bStartDeferred.promise;
      }),
    });

    const controller = createPlaybackController({
      transport,
      createInitialState: createMockDomainState,
    });

    void controller.startLive({ serviceRef: 'srv_1' });
    await vi.advanceTimersByTimeAsync(10);

    const bPromise = controller.startLive({ serviceRef: 'srv_1' });
    await vi.advanceTimersByTimeAsync(10);

    // B returns session-S first!
    bStartDeferred.resolve({
      status: 200,
      data: { sessionId: 'session-S' },
      headers: new Headers(),
    });
    await vi.advanceTimersByTimeAsync(10);

    const bResult = await bPromise;
    expect(bResult.status).toBe('ready');
    expect(controller.getActiveSessionId()).toBe('session-S');

    // Now A returns session-S late!
    aStartDeferred.resolve({
      status: 200,
      data: { sessionId: 'session-S' },
      headers: new Headers(),
    });
    await vi.advanceTimersByTimeAsync(10);

    // A sees S === activeSessionId, so 0 stops sent!
    expect(stopCalls).toHaveLength(0);
    expect(controller.getActiveSessionId()).toBe('session-S');
  });

  it('Race Test 6: B fails -> candidate S is released and stopped', async () => {
    const aStartDeferred = defer<StartIntentResult>();
    const bStartDeferred = defer<StartIntentResult>();

    let postStartCount = 0;
    const transport = createMockTransport({
      postStartIntent: vi.fn().mockImplementation(async () => {
        postStartCount++;
        return postStartCount === 1 ? aStartDeferred.promise : bStartDeferred.promise;
      }),
    });

    const controller = createPlaybackController({
      transport,
      createInitialState: createMockDomainState,
    });

    void controller.startLive({ serviceRef: 'srv_1' });
    await vi.advanceTimersByTimeAsync(10);

    const bPromise = controller.startLive({ serviceRef: 'srv_1' });
    const bAssertion = expect(bPromise).rejects.toThrow('Network error on B');
    await vi.advanceTimersByTimeAsync(10);

    // A returns session-S
    aStartDeferred.resolve({
      status: 200,
      data: { sessionId: 'session-S' },
      headers: new Headers(),
    });
    await vi.advanceTimersByTimeAsync(10);
    expect(stopCalls).toHaveLength(0);

    // B fails in network
    bStartDeferred.reject(new Error('Network error on B'));
    await vi.advanceTimersByTimeAsync(10);

    await bAssertion;

    // S must now be stopped!
    expect(stopCalls).toHaveLength(1);
    expect(stopCalls[0]!.sessionId).toBe('session-S');
  });

  it('Repeated stop and dispose: idempotent and leak-free', async () => {
    const transport = createMockTransport({
      postStartIntent: vi.fn().mockResolvedValue({
        status: 200,
        data: { sessionId: 'session-123' },
        headers: new Headers(),
      }),
    });

    const controller = createPlaybackController({
      transport,
      createInitialState: createMockDomainState,
    });

    await controller.startLive({ serviceRef: 'srv_1' });
    expect(controller.getActiveSessionId()).toBe('session-123');

    // First stop
    await controller.stop();
    expect(stopCalls).toHaveLength(1);
    expect(controller.getActiveSessionId()).toBeNull();

    // Second stop (idempotent)
    await controller.stop();
    expect(stopCalls).toHaveLength(1); // No duplicate stop intent

    // Dispose
    controller.dispose();
    expect(stopCalls).toHaveLength(1);
    expect(controller.getInFlightStartsCount()).toBe(0);
    expect(controller.getPendingAdoptionCandidatesCount()).toBe(0);
  });

  it('Single epoch authority across modes (Live -> VOD -> Live)', () => {
    const controller = createPlaybackController({
      transport: createMockTransport(),
      createInitialState: createMockDomainState,
    });

    expect(controller.getEpoch()).toBe(1);

    const epochLive1 = controller.allocatePlaybackEpoch();
    expect(epochLive1).toBe(2);

    const epochVod = controller.allocatePlaybackEpoch();
    expect(epochVod).toBe(3);

    const epochLive2 = controller.allocatePlaybackEpoch();
    expect(epochLive2).toBe(4);

    expect(controller.isStalePlaybackEpoch(epochLive1)).toBe(true);
    expect(controller.isStalePlaybackEpoch(epochVod)).toBe(true);
    expect(controller.isStalePlaybackEpoch(epochLive2)).toBe(false);
  });

  it('Race Test 5B: A returns S_A, B returns S_B -> S_A is stopped, S_B stays active', async () => {
    const aStartDeferred = defer<StartIntentResult>();
    const bStartDeferred = defer<StartIntentResult>();

    let postStartCount = 0;
    const transport = createMockTransport({
      postStartIntent: vi.fn().mockImplementation(async () => {
        postStartCount++;
        return postStartCount === 1 ? aStartDeferred.promise : bStartDeferred.promise;
      }),
    });

    const controller = createPlaybackController({
      transport,
      createInitialState: createMockDomainState,
    });

    void controller.startLive({ serviceRef: 'srv_1' });
    await vi.advanceTimersByTimeAsync(10);

    const bPromise = controller.startLive({ serviceRef: 'srv_2' });
    await vi.advanceTimersByTimeAsync(10);

    // A returns session-A
    aStartDeferred.resolve({
      status: 200,
      data: { sessionId: 'session-A' },
      headers: new Headers(),
    });
    await vi.advanceTimersByTimeAsync(10);
    // session-A held while B is in flight
    expect(stopCalls).toHaveLength(0);

    // B returns session-B (different!)
    bStartDeferred.resolve({
      status: 200,
      data: { sessionId: 'session-B' },
      headers: new Headers(),
    });
    await vi.advanceTimersByTimeAsync(10);

    const bResult = await bPromise;
    expect(bResult.status).toBe('ready');
    expect(controller.getActiveSessionId()).toBe('session-B');

    // session-A was not adopted, so it must be stopped!
    expect(stopCalls).toHaveLength(1);
    expect(stopCalls[0]!.sessionId).toBe('session-A');
  });

  it('Prompt user stop during preflight: cancels immediately and never posts start intent', async () => {
    const preflightDeferred = defer<StreamInfoResult>();
    const postStartIntent = vi.fn();

    const transport = createMockTransport({
      fetchStreamInfo: vi.fn().mockReturnValue(preflightDeferred.promise),
      postStartIntent,
    });

    const controller = createPlaybackController({
      transport,
      createInitialState: createMockDomainState,
    });

    const startPromise = controller.startLive({ serviceRef: 'srv_1' });

    // User calls stop while preflight is still pending
    await controller.stop('user_stop');

    const result = await startPromise;
    expect(result).toEqual({ status: 'cancelled', reason: 'user_stop' });

    // Now let preflight finish
    preflightDeferred.resolve({
      status: 200,
      data: {},
      headers: new Headers(),
    });
    await vi.advanceTimersByTimeAsync(50);

    // postStartIntent must NEVER have been called!
    expect(postStartIntent).not.toHaveBeenCalled();
    expect(stopCalls).toHaveLength(0);
  });

  it('409 lease conflict retry sleep is abortable: cancellation during sleep halts immediately', async () => {
    let postStartCount = 0;
    const transport = createMockTransport({
      postStartIntent: vi.fn().mockImplementation(async () => {
        postStartCount++;
        const headers = new Headers();
        headers.set('Retry-After', '2');
        return {
          status: 409,
          data: {},
          headers,
        };
      }),
    });

    const controller = createPlaybackController({
      transport,
      createInitialState: createMockDomainState,
    });

    const startPromise = controller.startLive({ serviceRef: 'srv_1' });
    await vi.advanceTimersByTimeAsync(20);

    // postStartCount should be 1 (first 409 response received, now in sleep)
    expect(postStartCount).toBe(1);

    // Now cancel during sleep
    await controller.stop();

    const result = await startPromise;
    expect(result).toEqual({ status: 'cancelled', reason: 'user_stop' });

    // Advance time past the 2s retry delay
    await vi.advanceTimersByTimeAsync(3_000);

    // No second postStartIntent should be issued!
    expect(postStartCount).toBe(1);
  });

  it('Rapid channel zapping A -> B -> C: only C activates, obsolete sessions are stopped', async () => {
    const aDeferred = defer<StartIntentResult>();
    const bDeferred = defer<StartIntentResult>();
    const cDeferred = defer<StartIntentResult>();

    let postStartCount = 0;
    const transport = createMockTransport({
      postStartIntent: vi.fn().mockImplementation(async () => {
        postStartCount++;
        if (postStartCount === 1) return aDeferred.promise;
        if (postStartCount === 2) return bDeferred.promise;
        return cDeferred.promise;
      }),
    });

    const controller = createPlaybackController({
      transport,
      createInitialState: createMockDomainState,
    });

    // Start A
    const aPromise = controller.startLive({ serviceRef: 'srv_A' });
    await vi.advanceTimersByTimeAsync(10);

    // Zap to B
    const bPromise = controller.startLive({ serviceRef: 'srv_B' });
    await vi.advanceTimersByTimeAsync(10);

    // Zap to C
    const cPromise = controller.startLive({ serviceRef: 'srv_C' });
    await vi.advanceTimersByTimeAsync(10);

    // Both A and B settle promptly as cancelled
    expect(await aPromise).toEqual({ status: 'cancelled', reason: 'superseded' });
    expect(await bPromise).toEqual({ status: 'cancelled', reason: 'superseded' });

    // A returns session-A
    aDeferred.resolve({ status: 200, data: { sessionId: 'session-A' }, headers: new Headers() });
    await vi.advanceTimersByTimeAsync(10);

    // B returns session-B
    bDeferred.resolve({ status: 200, data: { sessionId: 'session-B' }, headers: new Headers() });
    await vi.advanceTimersByTimeAsync(10);

    // C returns session-C
    cDeferred.resolve({ status: 200, data: { sessionId: 'session-C' }, headers: new Headers() });
    await vi.advanceTimersByTimeAsync(10);

    const cResult = await cPromise;
    expect(cResult.status).toBe('ready');
    expect(controller.getActiveSessionId()).toBe('session-C');

    // Both obsolete sessions session-A and session-B must have stop intents issued!
    const stoppedIds = stopCalls.map((c) => c.sessionId);
    expect(stoppedIds).toContain('session-A');
    expect(stoppedIds).toContain('session-B');
    expect(stoppedIds).not.toContain('session-C');
  });

  describe('Codex Review Invariant & Regression Contracts', () => {
    function accepted(sessionId: string) {
      return { status: 200, data: { sessionId }, headers: new Headers() };
    }

    it('settles the public start promise at its deadline', async () => {
      const t = createMockTransport({
        postStartIntent: vi.fn().mockReturnValue(defer().promise),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      let settled = false;
      void c.startLive({ serviceRef: 'A' }).then(() => { settled = true; }, () => { settled = true; });
      await vi.advanceTimersByTimeAsync(10_001);
      expect(settled).toBe(true);
    });

    it('keeps a submitted intent independent from local cancellation', async () => {
      const t = createMockTransport({
        postStartIntent: vi.fn().mockReturnValue(defer().promise),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      void c.startLive({ serviceRef: 'A' });
      await vi.advanceTimersByTimeAsync(0);
      const signal = (t.postStartIntent as any).mock.calls[0]![0].signal;
      await c.stop();
      expect(signal.aborted).toBe(false);
    });

    it('does not adopt a late accepted response after dispose', async () => {
      const d = defer<StartIntentResult>();
      const t = createMockTransport({
        postStartIntent: vi.fn().mockReturnValue(d.promise),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      void c.startLive({ serviceRef: 'A' });
      await vi.advanceTimersByTimeAsync(0);
      c.dispose();
      d.resolve(accepted('S'));
      await vi.advanceTimersByTimeAsync(0);
      expect(c.getActiveSessionId()).toBeNull();
      expect(t.waitForReady).not.toHaveBeenCalled();
      expect(t.postStopIntent).toHaveBeenCalled();
    });

    it('cleans an already ready previous session when switching channels', async () => {
      const t = createMockTransport();
      (t.postStartIntent as any)
        .mockResolvedValueOnce(accepted('A'))
        .mockResolvedValueOnce(accepted('B'));
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      await c.startLive({ serviceRef: 'A' });
      await c.startLive({ serviceRef: 'B' });
      expect(t.postStopIntent).toHaveBeenCalledWith(expect.objectContaining({ sessionId: 'A' }));
    });

    it('does not stop the adopted session when obsolete readiness completes', async () => {
      const d = defer<SessionReadyResult>();
      const t = createMockTransport({
        waitForReady: vi.fn().mockImplementation(async ({ sessionId }) => ({
          sessionId,
          playbackUrl: '/hls/index.m3u8',
          heartbeatIntervalSeconds: 5,
          leaseExpiresAt: '2026-09-07T22:00:00Z',
        })),
      });
      (t.waitForReady as any).mockReturnValueOnce(d.promise);
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      void c.startLive({ serviceRef: 'A' });
      await vi.advanceTimersByTimeAsync(0);
      await c.startLive({ serviceRef: 'B' });
      d.resolve({
        sessionId: 'session-default-1',
        playbackUrl: '/hls/index.m3u8',
        heartbeatIntervalSeconds: 5,
        leaseExpiresAt: '2026-09-07T22:00:00Z',
      });
      await vi.advanceTimersByTimeAsync(0);
      expect(t.postStopIntent).not.toHaveBeenCalled();
      expect(c.getActiveSessionId()).toBe('session-default-1');
    });

    it('waits for the remote response or stop budget before stop settles', async () => {
      const d = defer<void>();
      const t = createMockTransport({
        postStopIntent: vi.fn().mockReturnValue(d.promise),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      await c.startLive({ serviceRef: 'A' });
      let settled = false;
      void c.stop().then(() => { settled = true; });
      await vi.advanceTimersByTimeAsync(0);
      expect(settled).toBe(false);
    });

    it('settles a pending readiness start when disposed', async () => {
      const t = createMockTransport({
        waitForReady: vi.fn().mockReturnValue(defer().promise),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      let result: any = null;
      void c.startLive({ serviceRef: 'A' }).then((r) => { result = r; });
      await vi.advanceTimersByTimeAsync(0);
      c.dispose();
      await vi.advanceTimersByTimeAsync(0);
      expect(result).toEqual({ status: 'cancelled', reason: 'user_stop' });
    });

    it('stops B while the previous stop of A is still pending', async () => {
      const d = defer<void>();
      const t = createMockTransport({
        postStartIntent: vi
          .fn()
          .mockResolvedValueOnce(accepted('A'))
          .mockResolvedValueOnce(accepted('B')),
        postStopIntent: vi.fn().mockResolvedValue(undefined).mockReturnValueOnce(d.promise),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      await c.startLive({ serviceRef: 'A' });
      void c.stop();
      await c.startLive({ serviceRef: 'B' });
      void c.stop();
      await vi.advanceTimersByTimeAsync(0);
      expect(c.getActiveSessionId()).toBeNull();
      expect(t.postStopIntent).toHaveBeenCalledWith(expect.objectContaining({ sessionId: 'B' }));
    });

    it('does not stop adopted S when obsolete readiness rejects', async () => {
      const d = defer<SessionReadyResult>();
      const t = createMockTransport({
        postStartIntent: vi.fn().mockResolvedValue(accepted('S')),
        waitForReady: vi.fn().mockImplementation(async ({ sessionId }) => ({
          sessionId,
          playbackUrl: '/hls/index.m3u8',
          heartbeatIntervalSeconds: 5,
          leaseExpiresAt: '2026-09-07T22:00:00Z',
        })),
      });
      (t.waitForReady as any).mockReturnValueOnce(d.promise);
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      void c.startLive({ serviceRef: 'A' });
      await vi.advanceTimersByTimeAsync(0);
      await c.startLive({ serviceRef: 'B' });
      d.reject(new DOMException('Aborted', 'AbortError'));
      await vi.advanceTimersByTimeAsync(0);
      expect(t.postStopIntent).not.toHaveBeenCalled();
      expect(c.getActiveSessionId()).toBe('S');
    });

    it('does not stop B after a timed-out A returns the same S late', async () => {
      const d = defer<StartIntentResult>();
      const t = createMockTransport({
        postStartIntent: vi.fn().mockReturnValueOnce(d.promise).mockResolvedValue(accepted('S')),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      void c.startLive({ serviceRef: 'A' });
      await vi.advanceTimersByTimeAsync(10_001);
      await c.startLive({ serviceRef: 'B' });
      d.resolve(accepted('S'));
      await vi.advanceTimersByTimeAsync(0);
      expect(t.postStopIntent).not.toHaveBeenCalled();
    });

    it('does not age out S while a newer eligible C may still adopt it', async () => {
      const a = defer<StartIntentResult>();
      const b = defer<StartIntentResult>();
      const cc = defer<StartIntentResult>();
      const t = createMockTransport({
        postStartIntent: vi
          .fn()
          .mockReturnValueOnce(a.promise)
          .mockReturnValueOnce(b.promise)
          .mockReturnValueOnce(cc.promise),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      void c.startLive({ serviceRef: 'A' });
      await vi.advanceTimersByTimeAsync(0);
      void c.startLive({ serviceRef: 'B' });
      await vi.advanceTimersByTimeAsync(0);
      a.resolve(accepted('S'));
      await vi.advanceTimersByTimeAsync(9_000);
      void c.startLive({ serviceRef: 'C' });
      await vi.advanceTimersByTimeAsync(1_001);
      expect(t.postStopIntent).not.toHaveBeenCalled();
    });

    it('rejects denied preflight without submitting a start intent', async () => {
      const t = createMockTransport({
        fetchStreamInfo: vi.fn().mockResolvedValue({
          status: 200,
          data: { mode: 'deny', decision: { mode: 'deny' } },
          headers: new Headers(),
        } satisfies StreamInfoResult),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      await c.startLive({ serviceRef: 'A' }).catch(() => {});
      expect(t.postStartIntent).not.toHaveBeenCalled();
    });

    it('uses the normalized live playback mode in the start request', async () => {
      const t = createMockTransport({
        fetchStreamInfo: vi.fn().mockResolvedValue({
          status: 200,
          data: { mode: 'direct_stream', playbackDecisionToken: 'token', decision: { mode: 'direct_stream' } },
          headers: new Headers(),
        } satisfies StreamInfoResult),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      await c.startLive({ serviceRef: 'A', capabilities: { preferredHlsEngine: 'hlsjs' } as any });
      expect((t.postStartIntent as any).mock.calls[0]![0].body.params.playback_mode).toBe('hlsjs');
    });

    it('holds late S from expired A while eligible B is still awaiting its reply', async () => {
      const a = defer<StartIntentResult>();
      const b = defer<StartIntentResult>();
      const t = createMockTransport({
        postStartIntent: vi
          .fn()
          .mockReturnValueOnce(a.promise)
          .mockReturnValueOnce(b.promise),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      void c.startLive({ serviceRef: 'same' });
      await vi.advanceTimersByTimeAsync(10_001);
      void c.startLive({ serviceRef: 'same' }).catch(() => {});
      await vi.advanceTimersByTimeAsync(0);
      a.resolve(accepted('S'));
      await vi.advanceTimersByTimeAsync(0);
      expect(t.postStopIntent).not.toHaveBeenCalled();
    });

    it.each([null, {}, { requestId: 'broken-response' }])(
      'rejects malformed preflight %j before any start intent',
      async (data) => {
        const t = createMockTransport({
          fetchStreamInfo: vi.fn().mockResolvedValue({ status: 200, data, headers: new Headers() } as any),
        });
        const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
        await c.startLive({ serviceRef: 'A' }).catch(() => {});
        expect(t.postStartIntent).not.toHaveBeenCalled();
      },
    );

    it('does not repeat a completed stop when obsolete readiness later completes', async () => {
      const d = defer<SessionReadyResult>();
      const t = createMockTransport({
        waitForReady: vi.fn().mockReturnValueOnce(d.promise),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      void c.startLive({ serviceRef: 'A' });
      await vi.advanceTimersByTimeAsync(0);
      await c.stop();
      expect(t.postStopIntent).toHaveBeenCalledTimes(1);
      d.resolve({ sessionId: 'S', playbackUrl: '/hls/index.m3u8' });
      await vi.advanceTimersByTimeAsync(0);
      expect(t.postStopIntent).toHaveBeenCalledTimes(1);
    });

    it('retires timed-out protocol entries even if transport never settles', async () => {
      const t = createMockTransport({
        postStartIntent: vi.fn().mockReturnValue(defer().promise),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      void c.startLive({ serviceRef: 'A' });
      await vi.advanceTimersByTimeAsync(10_001);
      expect(c.getInFlightStartsCount()).toBe(0);
    });

    function conflict(seconds?: string) {
      const headers = new Headers();
      if (seconds !== undefined) {
        headers.set('Retry-After', seconds);
      }
      return { status: 409, data: {}, headers };
    }

    it('honors the server Retry-After before submitting the next start', async () => {
      const t = createMockTransport({
        postStartIntent: vi
          .fn()
          .mockResolvedValueOnce(conflict('5'))
          .mockResolvedValueOnce(accepted('S')),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      void c.startLive({ serviceRef: 'A' }).catch(() => {});
      await vi.advanceTimersByTimeAsync(2_001);
      expect(t.postStartIntent).toHaveBeenCalledTimes(1);
      await vi.advanceTimersByTimeAsync(3_000);
      expect(t.postStartIntent).toHaveBeenCalledTimes(2);
    });

    it('allows recovery after two conflicts within the existing retry budget', async () => {
      const t = createMockTransport({
        postStartIntent: vi
          .fn()
          .mockResolvedValueOnce(conflict('1'))
          .mockResolvedValueOnce(conflict('1'))
          .mockResolvedValueOnce(accepted('S')),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      let result: any = null;
      void c.startLive({ serviceRef: 'A' }).then(
        (r) => { result = r; },
        (e) => { result = e; },
      );
      await vi.advanceTimersByTimeAsync(4_001);
      expect(t.postStartIntent).toHaveBeenCalledTimes(3);
      expect(result?.status).toBe('ready');
    });

    it('keeps an already submitted retry independent of local cancellation', async () => {
      const d = defer<StartIntentResult>();
      const t = createMockTransport({
        postStartIntent: vi
          .fn()
          .mockResolvedValueOnce(conflict('1'))
          .mockReturnValueOnce(d.promise),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      void c.startLive({ serviceRef: 'A' }).catch(() => {});
      await vi.advanceTimersByTimeAsync(2_001);
      expect(t.postStartIntent).toHaveBeenCalledTimes(2);
      const retrySignal = (t.postStartIntent as any).mock.calls[1]![0].signal;
      await c.stop();
      expect(retrySignal.aborted).toBe(false);
    });

    it('falls back to 1s when Retry-After is absent or invalid', async () => {
      const t = createMockTransport({
        postStartIntent: vi
          .fn()
          .mockResolvedValueOnce(conflict())
          .mockResolvedValueOnce(accepted('S')),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      void c.startLive({ serviceRef: 'A' }).catch(() => {});
      await vi.advanceTimersByTimeAsync(500);
      expect(t.postStartIntent).toHaveBeenCalledTimes(1);
      await vi.advanceTimersByTimeAsync(600);
      expect(t.postStartIntent).toHaveBeenCalledTimes(2);
    });

    it('caps Retry-After at 5s when server requests longer delay', async () => {
      const t = createMockTransport({
        postStartIntent: vi
          .fn()
          .mockResolvedValueOnce(conflict('10'))
          .mockResolvedValueOnce(accepted('S')),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      void c.startLive({ serviceRef: 'A' }).catch(() => {});
      await vi.advanceTimersByTimeAsync(4_900);
      expect(t.postStartIntent).toHaveBeenCalledTimes(1);
      await vi.advanceTimersByTimeAsync(200);
      expect(t.postStartIntent).toHaveBeenCalledTimes(2);
    });

    it('exhausts retries and rejects when receiving 409 past the retry limit', async () => {
      const t = createMockTransport({
        postStartIntent: vi
          .fn()
          .mockResolvedValue(conflict('1')),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      let error: any = null;
      void c.startLive({ serviceRef: 'A' }).catch((e) => { error = e; });
      await vi.advanceTimersByTimeAsync(5_000);
      expect(t.postStartIntent).toHaveBeenCalledTimes(4);
      expect(error).toBeInstanceOf(Error);
      expect(error.message).toMatch(/409/);
    });

    it('allows two valid five-second lease waits before a successful third request', async () => {
      const t = createMockTransport({
        postStartIntent: vi
          .fn()
          .mockResolvedValueOnce(conflict('5'))
          .mockResolvedValueOnce(conflict('5'))
          .mockResolvedValueOnce(accepted('S')),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      let result: any = null;
      void c.startLive({ serviceRef: 'A' }).then((r) => { result = r; }, (e) => { result = e; });
      await vi.advanceTimersByTimeAsync(10_001);
      expect(t.postStartIntent).toHaveBeenCalledTimes(3);
      expect(result?.status).toBe('ready');
    });

    it('allows three capped five-second lease waits before a successful fourth request', async () => {
      const t = createMockTransport({
        postStartIntent: vi
          .fn()
          .mockResolvedValueOnce(conflict('5'))
          .mockResolvedValueOnce(conflict('5'))
          .mockResolvedValueOnce(conflict('5'))
          .mockResolvedValueOnce(accepted('S')),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      let result: any = null;
      void c.startLive({ serviceRef: 'A' }).then((r) => { result = r; }, (e) => { result = e; });
      await vi.advanceTimersByTimeAsync(15_001);
      expect(t.postStartIntent).toHaveBeenCalledTimes(4);
      expect(result?.status).toBe('ready');
    });

    it('accommodates slower preflight combined with conflict retries', async () => {
      const preflightDeferred = defer<any>();
      const t = createMockTransport({
        fetchStreamInfo: vi.fn().mockReturnValue(preflightDeferred.promise),
        postStartIntent: vi
          .fn()
          .mockResolvedValueOnce(conflict('5'))
          .mockResolvedValueOnce(accepted('S')),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      let result: any = null;
      void c.startLive({ serviceRef: 'A' }).then((r) => { result = r; }, (e) => { result = e; });
      await vi.advanceTimersByTimeAsync(4_000);
      preflightDeferred.resolve({
        status: 200,
        data: {
          mode: 'direct_stream',
          playbackDecisionToken: 'token',
          decision: { mode: 'direct_stream' },
        },
        headers: new Headers(),
      });
      await vi.advanceTimersByTimeAsync(5_001);
      expect(t.postStartIntent).toHaveBeenCalledTimes(2);
      expect(result?.status).toBe('ready');
    });

    it('reports defined timeout cancellation outcome on overall budget exhaustion rather than superseded', async () => {
      const t = createMockTransport({
        postStartIntent: vi
          .fn()
          .mockResolvedValue(conflict('5')),
      });
      const c = createPlaybackController({
        transport: t,
        createInitialState: createMockDomainState,
        startSettlementTimeoutMs: 12_000,
      });
      let result: any = null;
      void c.startLive({ serviceRef: 'A' }).then((r) => { result = r; }, (e) => { result = e; });
      await vi.advanceTimersByTimeAsync(12_001);
      expect(result).toEqual({ status: 'cancelled', reason: 'timeout' });
      expect(c.getInFlightStartsCount()).toBe(0);
    });

    it('handles near-deadline submitted reply: stops orphan session when no eligible start remains', async () => {
      const preflightDeferred = defer<any>();
      const postDeferred = defer<StartIntentResult>();
      const t = createMockTransport({
        fetchStreamInfo: vi.fn().mockReturnValue(preflightDeferred.promise),
        postStartIntent: vi.fn().mockReturnValue(postDeferred.promise),
      });
      const c = createPlaybackController({
        transport: t,
        createInitialState: createMockDomainState,
        startSettlementTimeoutMs: 10_000,
      });
      let result: any = null;
      void c.startLive({ serviceRef: 'A' }).then((r) => { result = r; }, (e) => { result = e; });

      // Preflight takes 8 seconds
      await vi.advanceTimersByTimeAsync(8_000);
      preflightDeferred.resolve({
        status: 200,
        data: {
          mode: 'direct_stream',
          playbackDecisionToken: 'token',
          decision: { mode: 'direct_stream' },
        },
        headers: new Headers(),
      });
      await vi.advanceTimersByTimeAsync(0);

      // POST is submitted at t = 8s
      expect(t.postStartIntent).toHaveBeenCalledTimes(1);

      // Overall budget (10s) expires at t = 10_001ms
      await vi.advanceTimersByTimeAsync(2_001);
      expect(result).toEqual({ status: 'cancelled', reason: 'timeout' });
      expect(c.getInFlightStartsCount()).toBe(0);

      // Near-deadline reply returns at t = 11s (3s after POST submitted)
      postDeferred.resolve(accepted('S_ORPHAN'));
      await vi.advanceTimersByTimeAsync(0);

      expect(c.getActiveSessionId()).toBeNull();
      expect(t.postStopIntent).toHaveBeenCalledWith(expect.objectContaining({ sessionId: 'S_ORPHAN' }));
    });

    it('handles near-deadline submitted reply: holds candidate for adoption by subsequent eligible attempt', async () => {
      const aPreflightDeferred = defer<any>();
      const aDeferred = defer<StartIntentResult>();
      const bDeferred = defer<StartIntentResult>();
      let postCallCount = 0;
      const t = createMockTransport({
        fetchStreamInfo: vi.fn().mockImplementation(async ({ serviceRef }) => {
          if (serviceRef === 'A') {
            return aPreflightDeferred.promise;
          }
          return {
            status: 200,
            data: {
              mode: 'direct_stream',
              playbackDecisionToken: 'token-b',
              decision: { mode: 'direct_stream' },
            },
            headers: new Headers(),
          };
        }),
        postStartIntent: vi.fn().mockImplementation(async () => {
          postCallCount++;
          return postCallCount === 1 ? aDeferred.promise : bDeferred.promise;
        }),
      });
      const c = createPlaybackController({
        transport: t,
        createInitialState: createMockDomainState,
        startSettlementTimeoutMs: 10_000,
      });
      let aResult: any = null;
      void c.startLive({ serviceRef: 'A' }).then((r) => { aResult = r; }, (e) => { aResult = e; });

      // Preflight takes 8 seconds
      await vi.advanceTimersByTimeAsync(8_000);
      aPreflightDeferred.resolve({
        status: 200,
        data: {
          mode: 'direct_stream',
          playbackDecisionToken: 'token-a',
          decision: { mode: 'direct_stream' },
        },
        headers: new Headers(),
      });
      await vi.advanceTimersByTimeAsync(0);

      // Attempt A submits POST at 8s
      expect(t.postStartIntent).toHaveBeenCalledTimes(1);

      // Overall budget expires at t = 10_001ms
      await vi.advanceTimersByTimeAsync(2_001);
      expect(aResult).toEqual({ status: 'cancelled', reason: 'timeout' });
      expect(c.getInFlightStartsCount()).toBe(0);

      // Now start attempt B at 10.5s (fresh budget)
      let bResult: any = null;
      void c.startLive({ serviceRef: 'B' }).then((r) => { bResult = r; }, (e) => { bResult = e; });
      await vi.advanceTimersByTimeAsync(500);

      // Attempt A's late response returns S at 11s (3s after POST submitted)
      aDeferred.resolve(accepted('S_ADOPT'));
      await vi.advanceTimersByTimeAsync(0);

      // Must NOT stop S_ADOPT because eligible B is in flight
      expect(t.postStopIntent).not.toHaveBeenCalled();
      expect(c.getPendingAdoptionCandidatesCount()).toBe(1);

      // Attempt B returns the same session S_ADOPT
      bDeferred.resolve(accepted('S_ADOPT'));
      await vi.advanceTimersByTimeAsync(0);

      expect(bResult?.status).toBe('ready');
      expect(c.getActiveSessionId()).toBe('S_ADOPT');
      expect(c.getPendingAdoptionCandidatesCount()).toBe(0);
      expect(t.postStopIntent).not.toHaveBeenCalled();
    });

    it('cleans an accepted response that arrives after the individual POST timeout', async () => {
      const d = defer<StartIntentResult>();
      const t = createMockTransport({
        postStartIntent: vi.fn().mockReturnValueOnce(d.promise),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      let result: any = null;
      void c.startLive({ serviceRef: 'A' }).then((r) => { result = r; }, (e) => { result = e; });
      await vi.advanceTimersByTimeAsync(10_001);
      expect(result).toEqual({ status: 'cancelled', reason: 'timeout' });
      expect(c.getInFlightStartsCount()).toBe(0);
      d.resolve(accepted('late-S'));
      await vi.advanceTimersByTimeAsync(0);
      expect(t.postStopIntent).toHaveBeenCalledWith(expect.objectContaining({ sessionId: 'late-S' }));
      expect(t.waitForReady).not.toHaveBeenCalled();
    });

    it('holds a response arriving after individual POST timeout for a newer eligible adopter', async () => {
      const aDeferred = defer<StartIntentResult>();
      const bDeferred = defer<StartIntentResult>();
      let callCount = 0;
      const t = createMockTransport({
        postStartIntent: vi.fn().mockImplementation(async () => {
          callCount++;
          return callCount === 1 ? aDeferred.promise : bDeferred.promise;
        }),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      let aResult: any = null;
      void c.startLive({ serviceRef: 'A' }).then((r) => { aResult = r; }, (e) => { aResult = e; });
      await vi.advanceTimersByTimeAsync(10_001);
      expect(aResult).toEqual({ status: 'cancelled', reason: 'timeout' });
      expect(c.getInFlightStartsCount()).toBe(0);

      // Start attempt B
      let bResult: any = null;
      void c.startLive({ serviceRef: 'B' }).then((r) => { bResult = r; }, (e) => { bResult = e; });
      await vi.advanceTimersByTimeAsync(0);

      // Attempt A's raw POST returns late-S after A's individual timeout
      aDeferred.resolve(accepted('late-S'));
      await vi.advanceTimersByTimeAsync(0);

      // Must be held as candidate for eligible B
      expect(t.postStopIntent).not.toHaveBeenCalled();
      expect(c.getPendingAdoptionCandidatesCount()).toBe(1);

      // Attempt B returns the same session late-S
      bDeferred.resolve(accepted('late-S'));
      await vi.advanceTimersByTimeAsync(0);

      expect(bResult?.status).toBe('ready');
      expect(c.getActiveSessionId()).toBe('late-S');
      expect(c.getPendingAdoptionCandidatesCount()).toBe(0);
      expect(t.postStopIntent).not.toHaveBeenCalled();
    });

    function trackUnhandledRejections(): { unhandled: unknown[]; cleanup: () => void } {
      const unhandled: unknown[] = [];
      const onUnhandled = (reason: unknown) => unhandled.push(reason);
      const nodeProcess = (globalThis as unknown as { process?: { on: (event: string, cb: (...args: unknown[]) => void) => void; off: (event: string, cb: (...args: unknown[]) => void) => void } }).process;
      if (nodeProcess?.on) {
        nodeProcess.on('unhandledRejection', onUnhandled);
      }
      return {
        unhandled,
        cleanup: () => {
          if (nodeProcess?.off) {
            nodeProcess.off('unhandledRejection', onUnhandled);
          }
        },
      };
    }

    it('handles timely malformed session response without unhandled rejection and rejects public promise', async () => {
      const t = createMockTransport({
        postStartIntent: vi.fn().mockResolvedValue({
          status: 200,
          data: { sessionId: 42 },
          headers: new Headers(),
        }),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      const { unhandled, cleanup } = trackUnhandledRejections();
      try {
        await expect(c.startLive({ serviceRef: 'A' })).rejects.toThrow(
          'Start failed with status 200 or missing sessionId',
        );
        await vi.advanceTimersByTimeAsync(0);
        expect(unhandled).toEqual([]);
        expect(t.postStopIntent).not.toHaveBeenCalled();
      } finally {
        cleanup();
      }
    });

    it('handles timely whitespace-only session response without unhandled rejection and rejects public promise', async () => {
      const t = createMockTransport({
        postStartIntent: vi.fn().mockResolvedValue({
          status: 200,
          data: { sessionId: '   ' },
          headers: new Headers(),
        }),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      const { unhandled, cleanup } = trackUnhandledRejections();
      try {
        await expect(c.startLive({ serviceRef: 'A' })).rejects.toThrow(
          'Start failed with status 200 or missing sessionId',
        );
        await vi.advanceTimersByTimeAsync(0);
        expect(unhandled).toEqual([]);
        expect(t.postStopIntent).not.toHaveBeenCalled();
      } finally {
        cleanup();
      }
    });

    it('handles late malformed session response after individual timeout without unhandled rejection', async () => {
      const d = defer<StartIntentResult>();
      const t = createMockTransport({
        postStartIntent: vi.fn().mockReturnValueOnce(d.promise),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      const { unhandled, cleanup } = trackUnhandledRejections();
      try {
        let result: any = null;
        void c.startLive({ serviceRef: 'A' }).then((r) => { result = r; }, (e) => { result = e; });
        await vi.advanceTimersByTimeAsync(10_001);
        expect(result).toEqual({ status: 'cancelled', reason: 'timeout' });
        expect(c.getInFlightStartsCount()).toBe(0);

        d.resolve({ status: 200, data: { sessionId: 42 }, headers: new Headers() } as any);
        await vi.advanceTimersByTimeAsync(0);
        expect(unhandled).toEqual([]);
        expect(t.postStopIntent).not.toHaveBeenCalled();
      } finally {
        cleanup();
      }
    });

    it('handles late whitespace-only session response after individual timeout without unhandled rejection', async () => {
      const d = defer<StartIntentResult>();
      const t = createMockTransport({
        postStartIntent: vi.fn().mockReturnValueOnce(d.promise),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      const { unhandled, cleanup } = trackUnhandledRejections();
      try {
        let result: any = null;
        void c.startLive({ serviceRef: 'A' }).then((r) => { result = r; }, (e) => { result = e; });
        await vi.advanceTimersByTimeAsync(10_001);
        expect(result).toEqual({ status: 'cancelled', reason: 'timeout' });
        expect(c.getInFlightStartsCount()).toBe(0);

        d.resolve({ status: 200, data: { sessionId: '   ' }, headers: new Headers() } as any);
        await vi.advanceTimersByTimeAsync(0);
        expect(unhandled).toEqual([]);
        expect(t.postStopIntent).not.toHaveBeenCalled();
      } finally {
        cleanup();
      }
    });

    it('timely valid response establishes foreground ownership without premature obsolete handling', async () => {
      const t = createMockTransport({
        postStartIntent: vi.fn().mockResolvedValue(accepted('timely-S')),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      const res = await c.startLive({ serviceRef: 'A' });
      expect(res.status).toBe('ready');
      expect(c.getActiveSessionId()).toBe('timely-S');
      expect(c.getPendingAdoptionCandidatesCount()).toBe(0);
      expect(t.postStopIntent).not.toHaveBeenCalled();
    });

    it('yields reason: timeout when transport rejects with AbortError on stalled response body (never missing sessionId)', async () => {
      const t = createMockTransport({
        postStartIntent: vi.fn().mockImplementation(async ({ signal }: { signal?: AbortSignal }) => {
          return new Promise<StartIntentResult>((_, reject) => {
            const onAbort = () => reject(new DOMException('Aborted', 'AbortError'));
            signal?.addEventListener('abort', onAbort, { once: true });
          });
        }),
      });

      const c = createPlaybackController({
        transport: t,
        createInitialState: createMockDomainState,
        httpRequestTimeoutMs: 100,
      });

      const startPromise = c.startLive({ serviceRef: 'A' });

      // Advance past httpRequestTimeoutMs so signal aborts
      await vi.advanceTimersByTimeAsync(150);

      const res = await startPromise;
      expect(res.status).toBe('cancelled');
      expect((res as any).reason).toBe('timeout');
      expect(c.getActiveSessionId()).toBeNull();
    });

    it('reaps session and rejects if ready live session lacks valid heartbeat interval', async () => {
      const t = createMockTransport({
        postStartIntent: vi.fn().mockResolvedValue(accepted('s-invalid-lease')),
        waitForReady: vi.fn().mockResolvedValue({
          sessionId: 's-invalid-lease',
          playbackUrl: 'http://localhost/stream.m3u8',
          leaseExpiresAt: '2026-09-07T22:00:00Z',
          // missing heartbeatIntervalSeconds
        }),
      });

      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });

      await expect(c.startLive({ serviceRef: 'A' })).rejects.toThrow(
        /readiness contract violation: invalid lease metadata/,
      );

      expect(c.getActiveSessionId()).toBeNull();
      expect(t.postStopIntent).toHaveBeenCalledWith(
        expect.objectContaining({ sessionId: 's-invalid-lease' }),
      );
      expect(t.postStopIntent).toHaveBeenCalledTimes(1);
    });

    it('reaps session and rejects if ready LIVE session has heartbeatIntervalSeconds=5 but leaseExpiresAt is missing', async () => {
      const t = createMockTransport({
        postStartIntent: vi.fn().mockResolvedValue(accepted('s-no-lease')),
        waitForReady: vi.fn().mockResolvedValue({
          sessionId: 's-no-lease',
          playbackUrl: 'http://localhost/stream.m3u8',
          heartbeatIntervalSeconds: 5,
          // missing leaseExpiresAt
        }),
      });

      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });

      await expect(c.startLive({ serviceRef: 'A' })).rejects.toThrow(
        /readiness contract violation: invalid lease metadata/,
      );

      expect(c.getActiveSessionId()).toBeNull();
      expect(t.postStopIntent).toHaveBeenCalledWith(
        expect.objectContaining({ sessionId: 's-no-lease' }),
      );
      expect(t.postStopIntent).toHaveBeenCalledTimes(1);
    });

    it('reaps session and rejects if ready session from startLive returns unexpected mode RECORDING', async () => {
      const t = createMockTransport({
        postStartIntent: vi.fn().mockResolvedValue(accepted('s-recording')),
        waitForReady: vi.fn().mockResolvedValue({
          sessionId: 's-recording',
          mode: 'RECORDING',
          playbackUrl: 'http://localhost/stream.m3u8',
          heartbeatIntervalSeconds: 5,
          leaseExpiresAt: '2026-09-07T22:00:00Z',
        }),
      });

      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });

      await expect(c.startLive({ serviceRef: 'A' })).rejects.toThrow(
        /returned unexpected mode RECORDING/,
      );

      expect(c.getActiveSessionId()).toBeNull();
      expect(t.postStopIntent).toHaveBeenCalledWith(
        expect.objectContaining({ sessionId: 's-recording' }),
      );
      expect(t.postStopIntent).toHaveBeenCalledTimes(1);
    });
  });

  describe('Merged integration and mode-switch ownership regressions', () => {
    it('invalidates the epoch of preparation when stop precedes startLive', async () => {
      const t = createMockTransport();
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      try {
        const epoch = c.allocatePlaybackEpoch();
        c.beginPlaybackAttempt(epoch, 'LIVE', 'starting');
        await c.stop();
        expect(c.getState().status).toBe('stopped');
        const result = await c.startLive({ epoch, serviceRef: 'A' });
        expect(result.status).toBe('cancelled');
        expect(t.postStartIntent).not.toHaveBeenCalled();
      } finally {
        c.dispose();
      }
    });

    it('allows startLive without an explicit epoch to reach ready after stop', async () => {
      const t = createMockTransport({
        postStartIntent: vi.fn()
          .mockResolvedValueOnce(accepted('s-A'))
          .mockResolvedValueOnce(accepted('s-B')),
        waitForReady: vi.fn().mockImplementation(async ({ sessionId }) => ({
          sessionId,
          playbackUrl: `http://test/${sessionId}.m3u8`,
          heartbeatIntervalSeconds: 5,
          leaseExpiresAt: '2026-09-09T22:00:00Z',
        })),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      try {
        expect((await c.startLive({ serviceRef: 'A' })).status).toBe('ready');
        expect(c.getActiveSessionId()).toBe('s-A');
        await c.stop();
        expect(c.getState().status).toBe('stopped');
        expect(c.getActiveSessionId()).toBeNull();
        const res = await c.startLive({ serviceRef: 'B' });
        expect(res.status).toBe('ready');
        expect(c.getActiveSessionId()).toBe('s-B');
      } finally {
        c.dispose();
      }
    });

    it('allows fresh allocated epoch to reach ready after stop', async () => {
      const t = createMockTransport({
        postStartIntent: vi.fn().mockResolvedValue(accepted('s-fresh')),
        waitForReady: vi.fn().mockImplementation(async ({ sessionId }) => ({
          sessionId,
          playbackUrl: `http://test/${sessionId}.m3u8`,
          heartbeatIntervalSeconds: 5,
          leaseExpiresAt: '2026-09-09T22:00:00Z',
        })),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      try {
        await c.stop();
        const freshEpoch = c.allocatePlaybackEpoch();
        const res = await c.startLive({ epoch: freshEpoch, serviceRef: 'A' });
        expect(res.status).toBe('ready');
        expect(c.getActiveSessionId()).toBe('s-fresh');
      } finally {
        c.dispose();
      }
    });

    it('deduplicates concurrent stop calls without duplicating remote cleanup', async () => {
      const d = defer<void>();
      const t = createMockTransport({
        postStopIntent: vi.fn().mockImplementation(() => d.promise),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      try {
        await c.startLive({ serviceRef: 'A' });
        const p1 = c.stop();
        const p2 = c.stop();
        expect(p1).toBe(p2);
        d.resolve();
        await Promise.all([p1, p2]);
        expect(t.postStopIntent).toHaveBeenCalledTimes(1);
      } finally {
        c.dispose();
      }
    });

    it('handles independent sessions when stopping pending A, starting B, and stopping B without overwrite', async () => {
      const dA = defer<SessionReadyResult>();
      const t = createMockTransport({
        postStartIntent: vi.fn()
          .mockResolvedValueOnce(accepted('s-A'))
          .mockResolvedValueOnce(accepted('s-B')),
        waitForReady: vi.fn()
          .mockReturnValueOnce(dA.promise)
          .mockImplementation(async ({ sessionId }) => ({
            sessionId,
            playbackUrl: `http://test/${sessionId}.m3u8`,
            heartbeatIntervalSeconds: 5,
            leaseExpiresAt: '2026-09-09T22:00:00Z',
          })),
        postStopIntent: vi.fn().mockResolvedValue(undefined),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      try {
        // Start A and stop while waiting for ready
        void c.startLive({ serviceRef: 'A' });
        await vi.advanceTimersByTimeAsync(0);
        void c.stop();

        // Start B and let it become ready
        const startBPromise = c.startLive({ serviceRef: 'B' });
        await vi.advanceTimersByTimeAsync(0);
        const resB = await startBPromise;
        expect(resB.status).toBe('ready');
        expect(c.getActiveSessionId()).toBe('s-B');

        // Resolve A late - B must not be overwritten
        dA.resolve({
          sessionId: 's-A',
          playbackUrl: 'http://test/s-A.m3u8',
          heartbeatIntervalSeconds: 5,
          leaseExpiresAt: '2026-09-09T22:00:00Z',
        });
        await vi.advanceTimersByTimeAsync(0);
        expect(c.getActiveSessionId()).toBe('s-B');

        // Stop B
        await c.stop();
        expect(c.getActiveSessionId()).toBeNull();
        expect(t.postStopIntent).toHaveBeenCalledWith(expect.objectContaining({ sessionId: 's-B' }));
      } finally {
        c.dispose();
      }
    });

    it('retires the ready Live session when switching to VOD', async () => {
      const t = createMockTransport();
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      try {
        expect((await c.startLive({ serviceRef: 'A' })).status).toBe('ready');
        const epoch = c.allocatePlaybackEpoch();
        c.beginPlaybackAttempt(epoch, 'VOD', 'starting');
        await Promise.resolve();
        expect(t.postStopIntent).toHaveBeenCalledTimes(1);
        expect(c.getActiveSessionId()).toBeNull();
      } finally {
        c.dispose();
      }
    });

    it('retires the ready Live session when switching to Direct src (hasSessionIntent=false)', async () => {
      const t = createMockTransport();
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      try {
        expect((await c.startLive({ serviceRef: 'A' })).status).toBe('ready');
        const epoch = c.allocatePlaybackEpoch();
        c.beginPlaybackAttempt(epoch, 'LIVE', 'buffering', false);
        await Promise.resolve();
        expect(t.postStopIntent).toHaveBeenCalledTimes(1);
        expect(c.getActiveSessionId()).toBeNull();
      } finally {
        c.dispose();
      }
    });

    it('retires the ready Live session when switching to Native playback (hasSessionIntent=false)', async () => {
      const t = createMockTransport();
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      try {
        expect((await c.startLive({ serviceRef: 'A' })).status).toBe('ready');
        const epoch = c.allocatePlaybackEpoch();
        c.beginPlaybackAttempt(epoch, 'LIVE', 'starting', false);
        await Promise.resolve();
        expect(t.postStopIntent).toHaveBeenCalledTimes(1);
        expect(c.getActiveSessionId()).toBeNull();
      } finally {
        c.dispose();
      }
    });

    it('compensates late replies when switching to non-Live mode during pending Live start', async () => {
      const d = defer<StartIntentResult>();
      const t = createMockTransport({
        postStartIntent: vi.fn().mockImplementation(() => d.promise),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      try {
        void c.startLive({ serviceRef: 'A' });
        await vi.advanceTimersByTimeAsync(0);

        // Switch to VOD
        const epoch = c.allocatePlaybackEpoch();
        c.beginPlaybackAttempt(epoch, 'VOD', 'starting');

        // Late response arrives for A
        d.resolve(accepted('s-late-A'));
        await vi.advanceTimersByTimeAsync(0);

        expect(t.postStopIntent).toHaveBeenCalledWith(
          expect.objectContaining({ sessionId: 's-late-A' }),
        );
        expect(c.getActiveSessionId()).toBeNull();
      } finally {
        c.dispose();
      }
    });

    it('fences stale non-Live begin from stopping a newer ready Live session', async () => {
      const t = createMockTransport({
        postStartIntent: vi.fn()
          .mockResolvedValueOnce(accepted('s-live-1'))
          .mockResolvedValueOnce(accepted('s-live-2')),
        waitForReady: vi.fn().mockImplementation(async ({ sessionId }) => ({
          sessionId,
          playbackUrl: `http://test/${sessionId}.m3u8`,
          heartbeatIntervalSeconds: 5,
          leaseExpiresAt: '2026-09-09T22:00:00Z',
        })),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      try {
        // Live 1 allocated at epoch 1
        const staleEpoch = c.allocatePlaybackEpoch();

        // Live 2 started and reaches ready at epoch 2
        const res2 = await c.startLive({ serviceRef: 'B' });
        expect(res2.status).toBe('ready');
        expect(c.getActiveSessionId()).toBe('s-live-1');

        // Stale VOD begin attempt from epoch 1 arrives
        c.beginPlaybackAttempt(staleEpoch, 'VOD', 'starting');
        await Promise.resolve();

        // Must NOT stop s-live-1!
        expect(t.postStopIntent).not.toHaveBeenCalled();
        expect(c.getActiveSessionId()).toBe('s-live-1');
      } finally {
        c.dispose();
      }
    });

    it('protects active Live session during Live-to-Live channel replacement until ready', async () => {
      const d = defer<SessionReadyResult>();
      const t = createMockTransport({
        postStartIntent: vi.fn()
          .mockResolvedValueOnce(accepted('s-A'))
          .mockResolvedValueOnce(accepted('s-B')),
        waitForReady: vi.fn()
          .mockImplementationOnce(async () => ({
            sessionId: 's-A',
            playbackUrl: 'http://test/s-A.m3u8',
            heartbeatIntervalSeconds: 5,
            leaseExpiresAt: '2026-09-09T22:00:00Z',
          }))
          .mockReturnValueOnce(d.promise),
      });
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      try {
        await c.startLive({ serviceRef: 'A' });
        expect(c.getActiveSessionId()).toBe('s-A');

        // Start B (Live-to-Live)
        const epochB = c.allocatePlaybackEpoch();
        c.beginPlaybackAttempt(epochB, 'LIVE', 'starting', true);
        await Promise.resolve();

        // s-A must NOT be stopped yet!
        expect(t.postStopIntent).not.toHaveBeenCalled();
        expect(c.getActiveSessionId()).toBe('s-A');

        // Now startLive for B runs and waitForReady resolves
        const startBPromise = c.startLive({ epoch: epochB, serviceRef: 'B' });
        await vi.advanceTimersByTimeAsync(0);
        d.resolve({
          sessionId: 's-B',
          playbackUrl: 'http://test/s-B.m3u8',
          heartbeatIntervalSeconds: 5,
          leaseExpiresAt: '2026-09-09T22:00:00Z',
        });
        const resB = await startBPromise;
        expect(resB.status).toBe('ready');

        // Now s-B is adopted, and s-A is stopped!
        expect(c.getActiveSessionId()).toBe('s-B');
        expect(t.postStopIntent).toHaveBeenCalledWith(expect.objectContaining({ sessionId: 's-A' }));
        expect(t.postStopIntent).toHaveBeenCalledTimes(1);
      } finally {
        c.dispose();
      }
    });
  });

  describe('PlaybackController - Heartbeat & Lease Supervision Integration', () => {
    it('supervises active session A while attempt B is in-flight, transferring to B upon ready', async () => {
      const bStartDeferred = defer<StartIntentResult>();
      const heartbeatCalls: string[] = [];

      const transport = createMockTransport({
        waitForReady: vi.fn().mockImplementation(async ({ sessionId }) => ({
          sessionId,
          playbackUrl: `http://localhost/${sessionId}.m3u8`,
          heartbeatIntervalSeconds: sessionId === 's-B' ? 10 : 5,
          leaseExpiresAt: sessionId === 's-B' ? '2026-09-09T13:00:00Z' : '2026-09-09T12:00:00Z',
        })),
        postStartIntent: vi.fn().mockImplementation(async (params) => {
          const serviceRef = (params?.body as any)?.serviceRef;
          if (serviceRef === 'channel-B') {
            return bStartDeferred.promise;
          }
          return accepted('s-A');
        }),
        postHeartbeat: vi.fn().mockImplementation(async ({ sessionId }) => {
          heartbeatCalls.push(sessionId);
          return {
            status: 200,
            data: {
              acknowledged: true,
              sessionId,
              leaseExpiresAt: '2026-09-09T12:05:00Z',
            },
            headers: new Headers(),
          };
        }),
      });

      const controller = createPlaybackController({
        getTransport: () => transport,
        createInitialState: createMockDomainState,
      });

      try {
        // 1. Start Live A -> reaches ready
        const resA = await controller.startLive({ serviceRef: 'channel-A' });
        expect(resA.status).toBe('ready');
        expect(controller.getActiveSessionId()).toBe('s-A');
        expect(controller.getHeartbeatSessionId()).toBe('s-A');
        expect(controller.isHeartbeatSupervising()).toBe(true);
        expect(controller.getState().leaseExpiresAt).toBe('2026-09-09T12:00:00Z');

        // 2. Start attempt B (Live-to-Live)
        const epochB = controller.allocatePlaybackEpoch();
        controller.beginPlaybackAttempt(epochB, 'LIVE', 'starting', true);

        const startBPromise = controller.startLive({ epoch: epochB, serviceRef: 'channel-B' });
        await vi.advanceTimersByTimeAsync(0);

        // s-A must still be active and supervised while B is starting
        expect(controller.getActiveSessionId()).toBe('s-A');
        expect(controller.getHeartbeatSessionId()).toBe('s-A');
        expect(controller.isHeartbeatSupervising()).toBe(true);

        // 3. Advance timer by 5s -> s-A must receive its heartbeat!
        await vi.advanceTimersByTimeAsync(5000);
        expect(heartbeatCalls).toContain('s-A');
        expect(controller.getState().leaseExpiresAt).toBe('2026-09-09T12:05:00Z');

        // 4. Accept attempt B
        bStartDeferred.resolve(accepted('s-B'));
        const resB = await startBPromise;
        expect(resB.status).toBe('ready');

        // 5. Supervision transferred to s-B and s-A was stopped
        expect(controller.getActiveSessionId()).toBe('s-B');
        expect(controller.getHeartbeatSessionId()).toBe('s-B');
        expect(controller.getState().leaseExpiresAt).toBe('2026-09-09T13:00:00Z');
        expect(transport.postStopIntent).toHaveBeenCalledWith(expect.objectContaining({ sessionId: 's-A' }));

        // 6. Advance timer by 10s -> s-B receives its heartbeat
        await vi.advanceTimersByTimeAsync(10000);
        expect(heartbeatCalls).toContain('s-B');
      } finally {
        controller.dispose();
      }
    });

    it('keeps session A active and supervised if attempt B fails before acceptance', async () => {
      const heartbeatCalls: string[] = [];
      let postStartAttempts = 0;

      const transport = createMockTransport({
        waitForReady: vi.fn().mockResolvedValue({
          sessionId: 's-A',
          playbackUrl: 'http://localhost/s-A.m3u8',
          heartbeatIntervalSeconds: 5,
          leaseExpiresAt: '2026-09-09T12:00:00Z',
        }),
        postStartIntent: vi.fn().mockImplementation(async () => {
          postStartAttempts++;
          if (postStartAttempts > 1) {
            throw new Error('503 Service Unavailable');
          }
          return accepted('s-A');
        }),
        postHeartbeat: vi.fn().mockImplementation(async ({ sessionId }) => {
          heartbeatCalls.push(sessionId);
          return {
            status: 200,
            data: {
              acknowledged: true,
              sessionId,
              leaseExpiresAt: '2026-09-09T12:05:00Z',
            },
            headers: new Headers(),
          };
        }),
      });

      const controller = createPlaybackController({
        getTransport: () => transport,
        createInitialState: createMockDomainState,
      });

      try {
        await controller.startLive({ serviceRef: 'channel-A' });
        expect(controller.getActiveSessionId()).toBe('s-A');
        expect(controller.getHeartbeatSessionId()).toBe('s-A');

        // Start B which will fail at postStartIntent
        const epochB = controller.allocatePlaybackEpoch();
        controller.beginPlaybackAttempt(epochB, 'LIVE', 'starting', true);

        const startBPromise = controller.startLive({ epoch: epochB, serviceRef: 'channel-B' });
        const rejectExpectation = expect(startBPromise).rejects.toThrow('503 Service Unavailable');
        await vi.advanceTimersByTimeAsync(0);
        await rejectExpectation;

        // Session A was retained, its heartbeat is still supervising
        expect(controller.getActiveSessionId()).toBe('s-A');
        expect(controller.getHeartbeatSessionId()).toBe('s-A');
        expect(controller.isHeartbeatSupervising()).toBe(true);

        // Advance timer -> session A receives heartbeat
        await vi.advanceTimersByTimeAsync(5000);
        expect(heartbeatCalls).toContain('s-A');
      } finally {
        controller.dispose();
      }
    });

    it('stops heartbeat supervision cleanly on stop() while timer is waiting', async () => {
      const heartbeatCalls: string[] = [];
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockImplementation(async ({ sessionId }) => {
          heartbeatCalls.push(sessionId);
          return {
            status: 200,
            data: { acknowledged: true, sessionId, leaseExpiresAt: '2026-09-09T12:05:00Z' },
            headers: new Headers(),
          };
        }),
      });

      const controller = createPlaybackController({
        getTransport: () => transport,
        createInitialState: createMockDomainState,
      });

      try {
        await controller.startLive({ serviceRef: 'channel-A' });
        expect(controller.isHeartbeatSupervising()).toBe(true);

        // Stop controller while waiting for next beat
        await controller.stop('user_stop');
        expect(controller.isHeartbeatSupervising()).toBe(false);
        expect(controller.getHeartbeatSessionId()).toBeNull();
        expect(controller.getState().leaseExpiresAt).toBeNull();

        // Advance timers by 10s: no heartbeats should fire
        await vi.advanceTimersByTimeAsync(10000);
        expect(heartbeatCalls).toHaveLength(0);
      } finally {
        controller.dispose();
      }
    });

    it('ignores late completion of in-flight heartbeat request if stop() occurs', async () => {
      const beatDeferred = defer<any>();
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockReturnValue(beatDeferred.promise),
      });

      const controller = createPlaybackController({
        getTransport: () => transport,
        createInitialState: createMockDomainState,
      });

      try {
        await controller.startLive({ serviceRef: 'channel-A' });
        // Advance timer by 5s to trigger in-flight heartbeat
        await vi.advanceTimersByTimeAsync(5000);
        expect(transport.postHeartbeat).toHaveBeenCalledTimes(1);

        // While request is in-flight, user stops
        const stopPromise = controller.stop('user_stop');
        await vi.advanceTimersByTimeAsync(0);
        await stopPromise;

        // Late response arrives from server
        beatDeferred.resolve({
          status: 200,
          data: { acknowledged: true, sessionId: 'session-default-1', leaseExpiresAt: '2026-09-09T12:10:00Z' },
          headers: new Headers(),
        });
        await vi.advanceTimersByTimeAsync(0);

        // State remains stopped, leaseExpiresAt is not revived
        expect(controller.getState().status).toBe('stopped');
        expect(controller.getState().leaseExpiresAt).toBeNull();
        expect(controller.isHeartbeatSupervising()).toBe(false);
      } finally {
        controller.dispose();
      }
    });

    it('stops heartbeat supervision synchronously upon mode switch to VOD', async () => {
      const transport = createMockTransport();
      const controller = createPlaybackController({
        getTransport: () => transport,
        createInitialState: createMockDomainState,
      });

      try {
        await controller.startLive({ serviceRef: 'channel-A' });
        expect(controller.isHeartbeatSupervising()).toBe(true);
        expect(controller.getActiveSessionId()).toBe('session-default-1');

        // Mode switch away from LIVE
        const epochVod = controller.allocatePlaybackEpoch();
        controller.beginPlaybackAttempt(epochVod, 'VOD', 'starting', false);

        expect(controller.isHeartbeatSupervising()).toBe(false);
        expect(controller.getHeartbeatSessionId()).toBeNull();
        expect(controller.getState().leaseExpiresAt).toBeNull();
        expect(controller.getState().connectionLost).toBe(false);
      } finally {
        controller.dispose();
      }
    });

    it('restarts heartbeat supervision cleanly on Start -> Stop -> Start', async () => {
      const heartbeatCalls: string[] = [];
      let postCallNum = 0;
      let callNum = 0;
      const transport = createMockTransport({
        postStartIntent: vi.fn().mockImplementation(async () => {
          postCallNum++;
          return accepted(`session-${postCallNum}`);
        }),
        waitForReady: vi.fn().mockImplementation(async () => {
          callNum++;
          return {
            sessionId: `session-${callNum}`,
            playbackUrl: `http://localhost/s${callNum}.m3u8`,
            heartbeatIntervalSeconds: 5,
            leaseExpiresAt: '2026-09-09T12:00:00Z',
          };
        }),
        postHeartbeat: vi.fn().mockImplementation(async ({ sessionId }) => {
          heartbeatCalls.push(sessionId);
          return {
            status: 200,
            data: { acknowledged: true, sessionId, leaseExpiresAt: '2026-09-09T12:05:00Z' },
            headers: new Headers(),
          };
        }),
      });

      const controller = createPlaybackController({
        getTransport: () => transport,
        createInitialState: createMockDomainState,
      });

      try {
        // Start 1
        await controller.startLive({ serviceRef: 'channel-1' });
        expect(controller.getHeartbeatSessionId()).toBe('session-1');

        // Stop
        await controller.stop('user_stop');
        expect(controller.isHeartbeatSupervising()).toBe(false);

        // Start 2
        const epoch2 = controller.allocatePlaybackEpoch();
        controller.beginPlaybackAttempt(epoch2, 'LIVE', 'starting', true);
        await controller.startLive({ epoch: epoch2, serviceRef: 'channel-2' });

        expect(controller.getHeartbeatSessionId()).toBe('session-2');
        expect(controller.isHeartbeatSupervising()).toBe(true);

        // Advance 5s -> session-2 receives heartbeat
        await vi.advanceTimersByTimeAsync(5000);
        expect(heartbeatCalls).toEqual(['session-2']);
      } finally {
        controller.dispose();
      }
    });

    it('handles reachability failures with 5s retry and sets connectionLost after 2 failures, clearing upon recovery', async () => {
      let fail = true;
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockImplementation(async ({ sessionId }) => {
          if (fail) {
            return {
              status: 502,
              data: null,
              headers: new Headers(),
            };
          }
          return {
            status: 200,
            data: { acknowledged: true, sessionId, leaseExpiresAt: '2026-09-09T12:30:00Z' },
            headers: new Headers(),
          };
        }),
      });

      const controller = createPlaybackController({
        getTransport: () => transport,
        createInitialState: createMockDomainState,
      });

      try {
        await controller.startLive({ serviceRef: 'channel-A' });
        expect(controller.getState().connectionLost).toBe(false);

        // Advance 5s -> failure 1 (retry scheduled after 5s, connectionLost remains false)
        await vi.advanceTimersByTimeAsync(5000);
        expect(controller.getState().connectionLost).toBe(false);

        // Advance 5s (retry delay) -> failure 2 (triggers connectionLost = true)
        await vi.advanceTimersByTimeAsync(5000);
        expect(controller.getState().connectionLost).toBe(true);

        // Recovery: turn off failure, advance 5s
        fail = false;
        await vi.advanceTimersByTimeAsync(5000);
        expect(controller.getState().connectionLost).toBe(false);
        expect(controller.getState().leaseExpiresAt).toBe('2026-09-09T12:30:00Z');
      } finally {
        controller.dispose();
      }
    });

    it('pauses media and dispatches failure event on terminal auth error (401)', async () => {
      const commands: any[] = [];
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 401,
          data: null,
          headers: new Headers(),
        }),
      });

      const controller = createPlaybackController({
        getTransport: () => transport,
        createInitialState: createMockDomainState,
        executeCommand: (cmd) => commands.push(cmd),
      });

      try {
        await controller.startLive({ serviceRef: 'channel-A' });
        await vi.advanceTimersByTimeAsync(5000);

        expect(controller.getState().status).toBe('error');
        expect(controller.getState().failure?.code).toBe('SESSION_UNAUTHORIZED');
        expect(controller.isHeartbeatSupervising()).toBe(false);
        expect(commands).toContainEqual({ type: 'command.media.pause' });
      } finally {
        controller.dispose();
      }
    });

    it('treats invalid contract payload (synthetic 502) as INVALID_HEARTBEAT_CONTRACT and pauses media', async () => {
      const commands: any[] = [];
      const transport = createMockTransport({
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 200,
          data: { acknowledged: false }, // missing leaseExpiresAt & wrong sessionId
          headers: new Headers(),
        }),
      });

      const controller = createPlaybackController({
        getTransport: () => transport,
        createInitialState: createMockDomainState,
        executeCommand: (cmd) => commands.push(cmd),
      });

      try {
        await controller.startLive({ serviceRef: 'channel-A' });
        await vi.advanceTimersByTimeAsync(5000);

        // Recovery ladder escalates recoverable session failures to 'recovering'
        expect(controller.getState().status).toBe('recovering');
        expect(controller.isHeartbeatSupervising()).toBe(false);
        expect(commands).toContainEqual({ type: 'command.media.pause' });
      } finally {
        controller.dispose();
      }
    });

    it('reports retained-session auth failure after a newer attempt has begun', async () => {
      const commands: any[] = [];
      const expiry = '2026-09-09T12:10:00Z';
      const transport = createMockTransport({
        postStartIntent: vi.fn().mockResolvedValue({ status: 202, data: { sessionId: 'A' }, headers: new Headers() }),
        waitForReady: vi.fn().mockResolvedValue({
          sessionId: 'A',
          playbackUrl: 'https://example.test/live.m3u8',
          heartbeatIntervalSeconds: 5,
          leaseExpiresAt: expiry,
        }),
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 401,
          data: {},
          headers: new Headers(),
        }),
      });

      const controller = createPlaybackController({
        transport,
        createInitialState: createMockDomainState,
        executeCommand: (cmd) => commands.push(cmd),
      });

      try {
        await controller.startLive({ serviceRef: 'channel-A' });
        const epochB = controller.allocatePlaybackEpoch();
        controller.beginPlaybackAttempt(epochB, 'LIVE', 'starting', true);

        await vi.advanceTimersByTimeAsync(5000);
        expect(controller.getActiveSessionId()).toBe('A');
        expect(commands).toContainEqual({ type: 'command.media.pause' });
        expect(controller.getState().failure?.code).toBe('SESSION_UNAUTHORIZED');
        expect(controller.getState().status).toBe('error');
      } finally {
        controller.dispose();
      }
    });

    it('handles retained-session recoverable 404/410 failure during in-flight new attempt without cancelling new attempt', async () => {
      const expiry = '2026-09-09T12:10:00Z';
      let resolveStartB!: (val: any) => void;
      let attemptBReady!: (val: any) => void;
      const transport = createMockTransport({
        postStartIntent: vi.fn()
          .mockResolvedValueOnce({ status: 202, data: { sessionId: 'A' }, headers: new Headers() })
          .mockImplementationOnce(() => new Promise((resolve) => {
            resolveStartB = () => resolve({ status: 202, data: { sessionId: 'B' }, headers: new Headers() });
          })),
        waitForReady: vi.fn()
          .mockResolvedValueOnce({
            sessionId: 'A',
            playbackUrl: 'https://example.test/live-A.m3u8',
            heartbeatIntervalSeconds: 5,
            leaseExpiresAt: expiry,
          })
          .mockImplementationOnce(() => new Promise((resolve) => {
            attemptBReady = () => resolve({
              sessionId: 'B',
              playbackUrl: 'https://example.test/live-B.m3u8',
              heartbeatIntervalSeconds: 5,
              leaseExpiresAt: expiry,
            });
          })),
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 404,
          data: {},
          headers: new Headers(),
        }),
      });

      const controller = createPlaybackController({
        transport,
        createInitialState: createMockDomainState,
      });

      try {
        await controller.startLive({ serviceRef: 'channel-A' });
        expect(controller.getActiveSessionId()).toBe('A');
        expect(controller.isHeartbeatSupervising()).toBe(true);

        // Start attempt B (in-flight)
        const epochB = controller.allocatePlaybackEpoch();
        controller.beginPlaybackAttempt(epochB, 'LIVE', 'starting', true);
        const startBPromise = controller.startLive({ epoch: epochB, serviceRef: 'channel-B' });

        // Session A's heartbeat returns 404 while B is in flight
        await vi.advanceTimersByTimeAsync(5000);

        // Session A's supervision should stop, but attempt B is NOT cancelled
        expect(controller.isHeartbeatSupervising()).toBe(false);

        // Attempt B resolves start intent then ready
        resolveStartB({});
        await vi.advanceTimersByTimeAsync(10);
        attemptBReady({});
        const resB = await startBPromise;

        expect(controller.getActiveSessionId()).toBe('B');
        expect(controller.isHeartbeatSupervising()).toBe(true);
        expect(resB.status).toBe('ready');
        expect(controller.getState().sessionPhase).toBe('ready');
      } finally {
        controller.dispose();
      }
    });

    it('adopts same session ID cleanly without duplicate timers or leaking prior supervision', async () => {
      const expiry = '2026-09-09T12:10:00Z';
      const transport = createMockTransport({
        postStartIntent: vi.fn().mockResolvedValue({ status: 202, data: { sessionId: 'A' }, headers: new Headers() }),
        waitForReady: vi.fn().mockResolvedValue({
          sessionId: 'A',
          playbackUrl: 'https://example.test/live.m3u8',
          heartbeatIntervalSeconds: 5,
          leaseExpiresAt: expiry,
        }),
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 200,
          data: { acknowledged: true, sessionId: 'A', leaseExpiresAt: expiry },
          headers: new Headers(),
        }),
      });

      const controller = createPlaybackController({
        transport,
        createInitialState: createMockDomainState,
      });

      try {
        await controller.startLive({ serviceRef: 'channel-A' });
        expect(controller.getActiveSessionId()).toBe('A');
        expect(controller.isHeartbeatSupervising()).toBe(true);

        // Re-request same channel -> adopts session 'A'
        await controller.startLive({ serviceRef: 'channel-A' });
        expect(controller.getActiveSessionId()).toBe('A');
        expect(controller.isHeartbeatSupervising()).toBe(true);

        // Advance 5s -> heartbeat should be called exactly once for this period (not twice)
        await vi.advanceTimersByTimeAsync(5000);
        expect(transport.postHeartbeat).toHaveBeenCalledTimes(1);
      } finally {
        controller.dispose();
      }
    });

    it('does not switch existing session traffic when updated transport has a different apiBase', async () => {
      const expiry = '2026-09-09T12:10:00Z';
      const transport1 = createMockTransport({
        apiBase: 'https://srv1.example.test/api/v3',
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 200,
          data: { acknowledged: true, sessionId: 'A', leaseExpiresAt: expiry },
          headers: new Headers(),
        }),
      });
      const transport2 = createMockTransport({
        apiBase: 'https://srv2.example.test/api/v3',
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 200,
          data: { acknowledged: true, sessionId: 'A', leaseExpiresAt: expiry },
          headers: new Headers(),
        }),
      });

      const controller = createPlaybackController({
        transport: transport1,
        createInitialState: createMockDomainState,
      });

      try {
        await controller.startLive({ serviceRef: 'channel-A' });

        // Update transport with different apiBase
        controller.updateTransport(transport2);

        // Advance 5s -> heartbeat should continue using transport1 because apiBase changed
        await vi.advanceTimersByTimeAsync(5000);
        expect(transport1.postHeartbeat).toHaveBeenCalledTimes(1);
        expect(transport2.postHeartbeat).not.toHaveBeenCalled();
      } finally {
        controller.dispose();
      }
    });

    it('settles the new start promise when retained-session auth cancels its preflight', async () => {
      const t = createMockTransport({
        postHeartbeat: vi.fn().mockResolvedValue({ status: 401, data: {}, headers: new Headers() }),
      });
      const controller = createPlaybackController({
        transport: t,
        createInitialState: createMockDomainState,
      });
      try {
        await controller.startLive({ serviceRef: 'channel-A' });
        vi.mocked(t.fetchStreamInfo).mockImplementationOnce(() => new Promise(() => {}));
        let settled = false;
        void controller.startLive({ serviceRef: 'channel-B' }).then(
          () => { settled = true; }, () => { settled = true; },
        );
        await vi.advanceTimersByTimeAsync(5000);
        expect(controller.getState().failure?.code).toBe('SESSION_UNAUTHORIZED');
        await vi.advanceTimersByTimeAsync(30000);
        expect(settled).toBe(true);
      } finally {
        controller.dispose();
      }
    });

    it('invalidates already allocated preparation after retained-session terminal auth failure', async () => {
      const t = createMockTransport({
        postHeartbeat: vi.fn().mockResolvedValue({ status: 403, data: {}, headers: new Headers() }),
      });
      const controller = createPlaybackController({
        transport: t,
        createInitialState: createMockDomainState,
      });
      try {
        await controller.startLive({ serviceRef: 'channel-A' });
        const epochB = controller.allocatePlaybackEpoch();
        controller.beginPlaybackAttempt(epochB, 'LIVE', 'starting', true);
        await vi.advanceTimersByTimeAsync(5000);
        expect(controller.getState().failure?.code).toBe('SESSION_FORBIDDEN');
        const result = await controller.startLive({ epoch: epochB, serviceRef: 'channel-B' });
        expect(result.status).toBe('cancelled');
        expect(t.postStartIntent).toHaveBeenCalledTimes(1);
      } finally {
        controller.dispose();
      }
    });

    it('uses a same-endpoint transport refresh that occurs while readiness is pending', async () => {
      const expiry = '2026-09-09T12:10:00Z';
      let resolveReady!: (val: any) => void;
      const readyPromise = new Promise<any>((r) => { resolveReady = r; });
      const oldTransport = createMockTransport({
        waitForReady: vi.fn().mockReturnValue(readyPromise),
      });
      const updatedTransport = createMockTransport();
      const controller = createPlaybackController({
        transport: oldTransport,
        createInitialState: createMockDomainState,
      });
      try {
        const startup = controller.startLive({ serviceRef: 'channel-A' });
        await vi.advanceTimersByTimeAsync(0);
        expect(oldTransport.waitForReady).toHaveBeenCalledTimes(1);
        controller.updateTransport(updatedTransport);
        resolveReady({
          sessionId: 'A',
          playbackUrl: 'https://example.test/live.m3u8',
          heartbeatIntervalSeconds: 5,
          leaseExpiresAt: expiry,
        });
        await startup;
        await vi.advanceTimersByTimeAsync(5000);
        expect(updatedTransport.postHeartbeat).toHaveBeenCalledTimes(1);
        expect(oldTransport.postHeartbeat).not.toHaveBeenCalled();
      } finally {
        controller.dispose();
      }
    });

    it('retires a retained session after invalid heartbeat contract while another start is pending', async () => {
      const expiry = '2026-09-09T12:10:00Z';
      let resolveStartB!: (val: any) => void;
      const startBPromise = new Promise<any>((r) => { resolveStartB = r; });
      const t = createMockTransport({
        postStartIntent: vi.fn()
          .mockResolvedValueOnce({ status: 202, data: { sessionId: 'A' }, headers: new Headers() })
          .mockReturnValueOnce(startBPromise),
        waitForReady: vi.fn().mockImplementation(async ({ sessionId }) => ({
          sessionId,
          playbackUrl: 'https://example.test/live.m3u8',
          heartbeatIntervalSeconds: 5,
          leaseExpiresAt: expiry,
        })),
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 200,
          data: { acknowledged: false, sessionId: 'A' },
          headers: new Headers(),
        }),
      });
      const controller = createPlaybackController({
        transport: t,
        createInitialState: createMockDomainState,
      });
      try {
        await controller.startLive({ serviceRef: 'channel-A' });
        const startupB = controller.startLive({ serviceRef: 'channel-B' });
        await vi.advanceTimersByTimeAsync(5000);
        resolveStartB({ status: 202, data: { sessionId: 'B' }, headers: new Headers() });
        await startupB;
        await controller.stop();
        expect(vi.mocked(t.postStopIntent).mock.calls.some(([p]) => p.sessionId === 'A')).toBe(true);
      } finally {
        controller.dispose();
      }
    });

    it('preserves each pending attempt endpoint when refreshing the retained session credentials', async () => {
      const expiry = '2026-09-09T12:10:00Z';
      const makeTransport = (apiBase = 'https://one.example.test/api/v3', sessionId = 'A'): LiveSessionTransport => ({
        apiBase,
        fetchStreamInfo: vi.fn().mockResolvedValue({
          status: 200,
          data: {
            mode: 'direct_stream', playbackDecisionToken: 'fixture',
            decision: { mode: 'direct_stream', playbackDecisionToken: 'fixture' },
          },
          headers: new Headers(),
        }),
        postStartIntent: vi.fn().mockResolvedValue({
          status: 200,
          data: { sessionId },
          headers: new Headers(),
        }),
        waitForReady: vi.fn().mockResolvedValue({
          sessionId,
          playbackUrl: 'https://example.test/live.m3u8',
          heartbeatIntervalSeconds: 5,
          leaseExpiresAt: expiry,
        }),
        postStopIntent: vi.fn().mockResolvedValue(undefined),
        postHeartbeat: vi.fn().mockResolvedValue({
          status: 200,
          data: { acknowledged: true, sessionId, leaseExpiresAt: expiry },
          headers: new Headers(),
        }),
      });

      const endpointOne = makeTransport('https://one.example.test/api/v3', 'A');
      const endpointTwo = makeTransport('https://two.example.test/api/v3', 'B');
      const refreshedOne = makeTransport('https://one.example.test/api/v3', 'A');

      let resolvePreflight!: (val: any) => void;
      const pendingPreflight = new Promise<any>((r) => { resolvePreflight = r; });
      vi.mocked(endpointTwo.fetchStreamInfo).mockReturnValue(pendingPreflight);

      const controller = createPlaybackController({
        transport: endpointOne,
        createInitialState: createMockDomainState,
      });

      try {
        await controller.startLive({ serviceRef: 'channel-A' });
        controller.updateTransport(endpointTwo);
        const startupB = controller.startLive({ serviceRef: 'channel-B' });
        await vi.advanceTimersByTimeAsync(0);
        expect(endpointTwo.fetchStreamInfo).toHaveBeenCalledTimes(1);

        // Session A is retained on endpoint one; B was initiated on endpoint two.
        controller.updateTransport(refreshedOne);
        resolvePreflight({
          status: 200,
          data: {
            mode: 'direct_stream', playbackDecisionToken: 'fixture',
            decision: { mode: 'direct_stream', playbackDecisionToken: 'fixture' },
          },
          headers: new Headers(),
        });
        await startupB;
        expect(
          vi.mocked(endpointTwo.postStartIntent).mock.calls.length +
          vi.mocked(refreshedOne.postStartIntent).mock.calls.length,
        ).toBe(1);
        expect(endpointTwo.postStartIntent).toHaveBeenCalledTimes(1);
        expect(refreshedOne.postStartIntent).not.toHaveBeenCalled();
      } finally {
        controller.dispose();
      }
    });
  });
});

describe('parseSessionId', () => {
  it('parses valid non-empty string and trims it', () => {
    expect(parseSessionId({ sessionId: 'session-123' })).toBe('session-123');
    expect(parseSessionId({ sessionId: '  session-abc  ' })).toBe('session-abc');
  });

  it('returns null for empty string or whitespace-only string', () => {
    expect(parseSessionId({ sessionId: '' })).toBeNull();
    expect(parseSessionId({ sessionId: '   ' })).toBeNull();
  });

  it('returns null for non-string values or missing property', () => {
    expect(parseSessionId(null)).toBeNull();
    expect(parseSessionId(undefined)).toBeNull();
    expect(parseSessionId(123)).toBeNull();
    expect(parseSessionId('string')).toBeNull();
    expect(parseSessionId(true)).toBeNull();
    expect(parseSessionId({})).toBeNull();
    expect(parseSessionId({ sessionId: 42 })).toBeNull();
    expect(parseSessionId({ sessionId: null })).toBeNull();
    expect(parseSessionId({ sessionId: undefined })).toBeNull();
    expect(parseSessionId({ sessionId: {} })).toBeNull();
  });
});
