// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createPlaybackController } from './playbackController';
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
        data: {},
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
      } satisfies SessionReadyResult),

      postStopIntent: vi.fn().mockImplementation(async ({ sessionId }) => {
        stopCalls.push({ sessionId });
      }),

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
        waitForReady: vi.fn().mockImplementation(async ({ sessionId }) => ({ sessionId, playbackUrl: '/hls/index.m3u8' })),
      });
      (t.waitForReady as any).mockReturnValueOnce(d.promise);
      const c = createPlaybackController({ transport: t, createInitialState: createMockDomainState });
      void c.startLive({ serviceRef: 'A' });
      await vi.advanceTimersByTimeAsync(0);
      await c.startLive({ serviceRef: 'B' });
      d.resolve({ sessionId: 'session-default-1', playbackUrl: '/hls/index.m3u8' });
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
  });
});
