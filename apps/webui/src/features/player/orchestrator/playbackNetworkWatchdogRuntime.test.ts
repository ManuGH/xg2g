// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import {
  createPlaybackNetworkWatchdogRuntime,
  MAX_AUTOMATIC_NETWORK_RECOVERIES,
  type PlaybackNetworkWatchdogRuntime,
  type PlaybackNetworkWatchdogRuntimeOptions,
} from './playbackNetworkWatchdogRuntime';
import type { PlayerStatus } from '../../../types/v3-player';
import type { PlaybackFailure, PlaybackRetryTarget } from './playbackTypes';

describe('PlaybackNetworkWatchdogRuntime', () => {
  let status: PlayerStatus = 'idle';
  let failure: PlaybackFailure | null = null;
  let epoch = 1;
  let disposed = false;
  let retryInFlight = false;
  let targetContext: PlaybackRetryTarget | null = { kind: 'live', serviceRef: '1:0:1:TEST' };
  const recoveredTargets: PlaybackRetryTarget[] = [];

  function makeWatchableFailure(overrides: Partial<PlaybackFailure> = {}): PlaybackFailure {
    return {
      class: 'media',
      code: 'PLAYBACK_FAILURE',
      message: 'Network error',
      retryable: true,
      recoverable: true,
      terminal: false,
      userVisible: true,
      status: null,
      source: 'adapter',
      policyImpact: 'none',
      messageKey: null,
      appError: null,
      telemetryContext: null,
      telemetryReason: null,
      ...overrides,
    };
  }

  function createRuntime(
    overrides: Partial<PlaybackNetworkWatchdogRuntimeOptions> = {},
  ): PlaybackNetworkWatchdogRuntime {
    return createPlaybackNetworkWatchdogRuntime({
      getDomainStatus: () => status,
      getFailure: () => failure,
      getPlaybackEpoch: () => epoch,
      isDisposed: () => disposed,
      isRetryInFlight: () => retryInFlight,
      onRecover: (target) => {
        recoveredTargets.push(target);
      },
      getTargetContext: () => targetContext,
      ...overrides,
    });
  }

  beforeEach(() => {
    vi.useFakeTimers();
    status = 'idle';
    failure = null;
    epoch = 1;
    disposed = false;
    retryInFlight = false;
    targetContext = { kind: 'live', serviceRef: '1:0:1:TEST' };
    recoveredTargets.length = 0;
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  describe('Timing and backoff progression', () => {
    it('waits FIRST_PROBE_DELAY_MS (5s) before the first probe; does not probe immediately', async () => {
      const probe = vi.fn().mockResolvedValue(true);
      const runtime = createRuntime();

      status = 'error';
      failure = makeWatchableFailure();

      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe });
      runtime.onDomainStateChanged();

      expect(runtime.getState().active).toBe(true);
      expect(runtime.getState().timerPending).toBe(true);
      expect(probe).not.toHaveBeenCalled();

      // Advance 4,999 ms - still not called
      await vi.advanceTimersByTimeAsync(4_999);
      expect(probe).not.toHaveBeenCalled();

      // Advance remaining 1 ms - probe fires
      await vi.advanceTimersByTimeAsync(1);
      expect(probe).toHaveBeenCalledTimes(1);
      expect(recoveredTargets).toEqual([{ kind: 'live', serviceRef: '1:0:1:TEST' }]);
      expect(runtime.getState().recoveries).toBe(1);
      expect(runtime.getState().loopEnded).toBe(true);
      expect(runtime.getState().timerPending).toBe(false);
    });

    it('backs off exponentially when server is unreachable: 5s, 10s, 20s, capped at 30s', async () => {
      const probe = vi.fn().mockResolvedValue(false);
      const runtime = createRuntime();

      status = 'error';
      failure = makeWatchableFailure();

      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe });
      runtime.onDomainStateChanged();

      // Tick 0: 5,000 ms
      await vi.advanceTimersByTimeAsync(5_000);
      expect(probe).toHaveBeenCalledTimes(1);
      expect(runtime.getState().attempt).toBe(1);

      // Tick 1: 5,000 * 2^1 = 10,000 ms
      await vi.advanceTimersByTimeAsync(9_999);
      expect(probe).toHaveBeenCalledTimes(1);
      await vi.advanceTimersByTimeAsync(1);
      expect(probe).toHaveBeenCalledTimes(2);
      expect(runtime.getState().attempt).toBe(2);

      // Tick 2: 5,000 * 2^2 = 20,000 ms
      await vi.advanceTimersByTimeAsync(19_999);
      expect(probe).toHaveBeenCalledTimes(2);
      await vi.advanceTimersByTimeAsync(1);
      expect(probe).toHaveBeenCalledTimes(3);
      expect(runtime.getState().attempt).toBe(3);

      // Tick 3: 5,000 * 2^3 = 40,000 -> capped at MAX_PROBE_DELAY_MS (30,000 ms)
      await vi.advanceTimersByTimeAsync(29_999);
      expect(probe).toHaveBeenCalledTimes(3);
      await vi.advanceTimersByTimeAsync(1);
      expect(probe).toHaveBeenCalledTimes(4);
      expect(runtime.getState().attempt).toBe(4);

      // Tick 4: stays capped at 30,000 ms
      await vi.advanceTimersByTimeAsync(30_000);
      expect(probe).toHaveBeenCalledTimes(5);
      expect(runtime.getState().attempt).toBe(5);
    });

    it('treats probe network rejection / exception as unreachable and continues backoff', async () => {
      const probe = vi.fn().mockRejectedValue(new Error('Network error'));
      const runtime = createRuntime();

      status = 'error';
      failure = makeWatchableFailure();

      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe });
      runtime.onDomainStateChanged();

      await vi.advanceTimersByTimeAsync(5_000);
      expect(probe).toHaveBeenCalledTimes(1);
      expect(runtime.getState().attempt).toBe(1);
      expect(runtime.getState().timerPending).toBe(true);
      expect(recoveredTargets).toHaveLength(0);
    });
  });

  describe('Cancellation and in-flight guards', () => {
    it('disposed runtime cancels pending timer and ignores in-flight probe', async () => {
      let probeResolve!: (val: boolean) => void;
      const probe = vi.fn().mockImplementation(() => new Promise<boolean>((res) => { probeResolve = res; }));
      const runtime = createRuntime();

      status = 'error';
      failure = makeWatchableFailure();

      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe });
      runtime.onDomainStateChanged();

      // Advance to tick 0 so probe is in flight
      vi.advanceTimersByTime(5_000);
      expect(probe).toHaveBeenCalledTimes(1);
      expect(runtime.getState().probeInFlight).toBe(true);

      // Dispose while probe is in flight
      runtime.dispose();
      expect(runtime.getState().active).toBe(false);

      // Probe resolves after dispose
      probeResolve(true);
      await Promise.resolve();

      expect(recoveredTargets).toHaveLength(0);
      expect(runtime.getState().recoveries).toBe(0);
    });

    it('ignores in-flight probe result if domain status leaves error before probe resolves', async () => {
      let probeResolve!: (val: boolean) => void;
      const probe = vi.fn().mockImplementation(() => new Promise<boolean>((res) => { probeResolve = res; }));
      const runtime = createRuntime();

      status = 'error';
      failure = makeWatchableFailure();

      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe });
      runtime.onDomainStateChanged();

      vi.advanceTimersByTime(5_000);
      expect(probe).toHaveBeenCalledTimes(1);

      // Status leaves error (e.g. user manually stopped or started)
      status = 'starting';
      runtime.onDomainStateChanged();
      expect(runtime.getState().active).toBe(false);

      // Late probe resolution
      probeResolve(true);
      await Promise.resolve();

      expect(recoveredTargets).toHaveLength(0);
      expect(runtime.getState().recoveries).toBe(0);
    });

    it('suppresses duplicate onRecover call if isRetryInFlight is already true', async () => {
      const probe = vi.fn().mockResolvedValue(true);
      retryInFlight = true;
      const runtime = createRuntime();

      status = 'error';
      failure = makeWatchableFailure();

      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe });
      runtime.onDomainStateChanged();

      await vi.advanceTimersByTimeAsync(5_000);
      expect(probe).toHaveBeenCalledTimes(1);
      // recoveries is still recorded, but onRecover was not dispatched
      expect(runtime.getState().recoveries).toBe(1);
      expect(recoveredTargets).toHaveLength(0);
    });
  });

  describe('Budget enforcement and reset', () => {
    it('enforces maximum automatic recoveries budget and warns on exhaustion', async () => {
      const probe = vi.fn().mockResolvedValue(true);
      const runtime = createRuntime();

      // Exhaust 3 recoveries
      for (let i = 1; i <= MAX_AUTOMATIC_NETWORK_RECOVERIES; i++) {
        status = 'error';
        failure = makeWatchableFailure();
        runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe });
        runtime.onDomainStateChanged();

        await vi.advanceTimersByTimeAsync(5_000);
        expect(runtime.getState().recoveries).toBe(i);

        // Transition out of error as if recovery started
        status = 'stopped';
        runtime.onDomainStateChanged();
      }

      expect(runtime.getState().recoveries).toBe(3);

      // 4th activation attempt: status enters error again
      status = 'error';
      failure = makeWatchableFailure();
      runtime.onDomainStateChanged();

      // Loop is refused: active may be true, but timerPending is false and probe not called
      expect(runtime.getState().active).toBe(true);
      expect(runtime.getState().timerPending).toBe(false);
      await vi.advanceTimersByTimeAsync(10_000);
      expect(probe).toHaveBeenCalledTimes(3);
    });

    it('resets recoveries budget when domain status reaches playing', async () => {
      const probe = vi.fn().mockResolvedValue(true);
      const runtime = createRuntime();

      status = 'error';
      failure = makeWatchableFailure();
      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe });
      runtime.onDomainStateChanged();

      await vi.advanceTimersByTimeAsync(5_000);
      expect(runtime.getState().recoveries).toBe(1);

      // Playback becomes healthy
      status = 'playing';
      runtime.onDomainStateChanged();
      expect(runtime.getState().recoveries).toBe(0);
      expect(runtime.getState().active).toBe(false);
    });

    it('resets recoveries budget when intentKey changes', async () => {
      const probe = vi.fn().mockResolvedValue(true);
      const runtime = createRuntime();

      status = 'error';
      failure = makeWatchableFailure();
      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe });
      runtime.onDomainStateChanged();

      await vi.advanceTimersByTimeAsync(5_000);
      expect(runtime.getState().recoveries).toBe(1);

      // User changes viewing intent (new channel/recording)
      runtime.setContext({ platformEligible: true, intentKey: 'channel-2', probe });
      expect(runtime.getState().recoveries).toBe(0);
    });
  });

  describe('Condition C1: Probe identity is part of loop key', () => {
    it('restarts loop (fresh 5s delay, attempt=0, budget preserved) when probe identity changes while active', async () => {
      const probe1 = vi.fn().mockResolvedValue(false);
      const probe2 = vi.fn().mockResolvedValue(true);
      const runtime = createRuntime();

      status = 'error';
      failure = makeWatchableFailure();

      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe: probe1 });
      runtime.onDomainStateChanged();

      // Tick 0 fails with probe1
      await vi.advanceTimersByTimeAsync(5_000);
      expect(probe1).toHaveBeenCalledTimes(1);
      expect(runtime.getState().attempt).toBe(1);

      // Advance 4,000 ms towards tick 1 (which needs 10,000 ms)
      await vi.advanceTimersByTimeAsync(4_000);
      expect(probe1).toHaveBeenCalledTimes(1);

      // Now probe changes while active!
      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe: probe2 });

      // C1 contract: fresh 5s delay, attempt = 0, budget kept
      expect(runtime.getState().attempt).toBe(0);
      expect(runtime.getState().timerPending).toBe(true);

      // Old timer is cancelled: advance 6,000 ms (which would have fired old 10s timer) -> probe1 not called
      await vi.advanceTimersByTimeAsync(4_000);
      expect(probe1).toHaveBeenCalledTimes(1);
      expect(probe2).not.toHaveBeenCalled();

      // Advance remaining 1,000 ms -> probe2 fires at exactly 5,000 ms after context change
      await vi.advanceTimersByTimeAsync(1_000);
      expect(probe2).toHaveBeenCalledTimes(1);
      expect(runtime.getState().recoveries).toBe(1);
    });

    it('is a no-op when publishing the exact same probe identity while active', async () => {
      const probe = vi.fn().mockResolvedValue(false);
      const runtime = createRuntime();

      status = 'error';
      failure = makeWatchableFailure();

      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe });
      runtime.onDomainStateChanged();

      // Advance 3,000 ms
      await vi.advanceTimersByTimeAsync(3_000);
      expect(probe).not.toHaveBeenCalled();

      // Republish exact same probe identity (e.g. from unrelated render)
      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe });

      // Should NOT reset the timer: should fire in 2,000 ms, not 5,000 ms
      await vi.advanceTimersByTimeAsync(2_000);
      expect(probe).toHaveBeenCalledTimes(1);
    });

    it('does nothing when probe identity changes while inactive', () => {
      const probe1 = vi.fn();
      const probe2 = vi.fn();
      const runtime = createRuntime();

      status = 'idle';
      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe: probe1 });
      expect(runtime.getState().active).toBe(false);
      expect(runtime.getState().timerPending).toBe(false);

      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe: probe2 });
      expect(runtime.getState().active).toBe(false);
      expect(runtime.getState().timerPending).toBe(false);
      expect(probe2).not.toHaveBeenCalled();
    });

    it('cancels loop if probe becomes null or undefined while active', async () => {
      const probe = vi.fn().mockResolvedValue(false);
      const runtime = createRuntime();

      status = 'error';
      failure = makeWatchableFailure();

      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe });
      runtime.onDomainStateChanged();
      expect(runtime.getState().timerPending).toBe(true);

      // Probe removed
      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe: undefined });
      expect(runtime.getState().timerPending).toBe(false);

      await vi.advanceTimersByTimeAsync(10_000);
      expect(probe).not.toHaveBeenCalled();
    });
  });

  describe('Condition C2: Edge-triggered activation on active', () => {
    it('triggers on false -> true edge and cancels on true -> false edge', () => {
      const probe = vi.fn().mockResolvedValue(true);
      const runtime = createRuntime();

      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe });

      // false -> true: starts loop
      status = 'error';
      failure = makeWatchableFailure();
      runtime.onDomainStateChanged();
      expect(runtime.getState().active).toBe(true);
      expect(runtime.getState().timerPending).toBe(true);

      // true -> false: cancels loop
      status = 'starting';
      runtime.onDomainStateChanged();
      expect(runtime.getState().active).toBe(false);
      expect(runtime.getState().timerPending).toBe(false);
    });

    it('sub-state 1: true -> true while loop running does NOT restart, does NOT reset attempt or cancel timer', async () => {
      const probe = vi.fn().mockResolvedValue(false);
      const runtime = createRuntime();

      status = 'error';
      failure = makeWatchableFailure({ code: 'FIRST_FAILURE' });
      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe });
      runtime.onDomainStateChanged();

      // Tick 0 fails
      await vi.advanceTimersByTimeAsync(5_000);
      expect(probe).toHaveBeenCalledTimes(1);
      expect(runtime.getState().attempt).toBe(1);

      // Advance 4,000 ms towards tick 1 (delay 10,000 ms)
      await vi.advanceTimersByTimeAsync(4_000);

      // Another watchable error dispatched while still in error: true -> true
      failure = makeWatchableFailure({ code: 'SECOND_FAILURE' });
      runtime.onDomainStateChanged();

      // C2 contract: must not restart, must not reset attempt, timer must fire in 6,000 ms
      expect(runtime.getState().attempt).toBe(1);

      await vi.advanceTimersByTimeAsync(5_999);
      expect(probe).toHaveBeenCalledTimes(1);

      await vi.advanceTimersByTimeAsync(1);
      expect(probe).toHaveBeenCalledTimes(2);
      expect(runtime.getState().attempt).toBe(2);
    });

    it('sub-state 2: true -> true after loop ended after success does NOT revive the loop', async () => {
      const probe = vi.fn().mockResolvedValue(true);
      const runtime = createRuntime();

      status = 'error';
      failure = makeWatchableFailure();
      runtime.setContext({ platformEligible: true, intentKey: 'channel-1', probe });
      runtime.onDomainStateChanged();

      // Probe succeeds, loop ends
      await vi.advanceTimersByTimeAsync(5_000);
      expect(probe).toHaveBeenCalledTimes(1);
      expect(runtime.getState().loopEnded).toBe(true);
      expect(runtime.getState().timerPending).toBe(false);

      // Re-dispatch error while status is still error: true -> true
      failure = makeWatchableFailure({ code: 'ANOTHER_WATCHABLE_FAILURE' });
      runtime.onDomainStateChanged();

      // C2 contract: must not revive ended loop
      expect(runtime.getState().loopEnded).toBe(true);
      expect(runtime.getState().timerPending).toBe(false);

      await vi.advanceTimersByTimeAsync(10_000);
      expect(probe).toHaveBeenCalledTimes(1);
    });
  });

  describe('Target context precedence', () => {
    it('uses targetContext returned by options.getTargetContext()', async () => {
      const probe = vi.fn().mockResolvedValue(true);
      const customTarget: PlaybackRetryTarget = {
        kind: 'vod',
        recordingId: 'rec-456',
        explicitProfile: 'high',
      };
      targetContext = customTarget;

      const runtime = createRuntime();
      status = 'error';
      failure = makeWatchableFailure();
      runtime.setContext({ platformEligible: true, intentKey: 'rec-456', probe });
      runtime.onDomainStateChanged();

      await vi.advanceTimersByTimeAsync(5_000);
      expect(recoveredTargets).toEqual([customTarget]);
    });
  });
});
