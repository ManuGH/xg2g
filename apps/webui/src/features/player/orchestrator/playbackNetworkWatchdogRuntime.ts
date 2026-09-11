// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { debugLog, debugWarn } from '../../../utils/logging';
import type { PlayerStatus } from '../../../types/v3-player';
import type { PlaybackFailure, PlaybackRetryTarget } from './playbackTypes';
import { NETWORK_STARVATION_CODES } from './recoveryLadder';

export const FIRST_PROBE_DELAY_MS = 5_000;
export const MAX_PROBE_DELAY_MS = 30_000;
export const PROBE_TIMEOUT_MS = 5_000;
export const MAX_AUTOMATIC_NETWORK_RECOVERIES = 3;

/**
 * Whether waiting for connectivity could plausibly fix this failure.
 *
 * The discriminator is whether the server answered at all. If it did — a 410
 * for a deleted recording, a packager failure, any HTTP status — then
 * connectivity is not the problem and polling until the server is reachable
 * would just retry a fault that is not going to clear. Only a request that
 * never arrived, or a stream that starved, is worth waiting out.
 */
export function shouldWatchForNetworkRecovery(failure: {
  class: string;
  code: string;
  terminal: boolean;
  status: number | null;
} | null): boolean {
  if (!failure || failure.terminal || failure.class === 'auth') {
    return false;
  }
  return failure.status == null || NETWORK_STARVATION_CODES.has(failure.code);
}

export interface PlaybackNetworkWatchdogRuntimeOptions {
  getDomainStatus: () => PlayerStatus;
  getFailure: () => PlaybackFailure | null;
  getPlaybackEpoch: () => number;
  isDisposed: () => boolean;
  isRetryInFlight?: () => boolean;
  onRecover: (target: PlaybackRetryTarget) => Promise<unknown> | void;
  getTargetContext: () => PlaybackRetryTarget | null;
}

export interface PlaybackNetworkWatchdogState {
  active: boolean;
  attempt: number;
  recoveries: number;
  timerPending: boolean;
  probeInFlight: boolean;
  loopEnded: boolean;
}

export interface PlaybackNetworkWatchdogRuntime {
  setContext(params: {
    platformEligible: boolean;
    intentKey: string;
    probe?: () => Promise<boolean>;
  }): void;
  onDomainStateChanged(): void;
  activate(): void;
  dispose(): void;
  getState(): PlaybackNetworkWatchdogState;
}

export function createPlaybackNetworkWatchdogRuntime(
  options: PlaybackNetworkWatchdogRuntimeOptions,
): PlaybackNetworkWatchdogRuntime {
  let platformEligible = true;
  let intentKey = '';
  let currentProbe: (() => Promise<boolean>) | null = null;
  let recoveries = 0;
  let active = false;
  let loopRunning = false;
  let loopEnded = false;
  let attempt = 0;
  let timerId: ReturnType<typeof setTimeout> | null = null;
  let probeInFlight = false;
  let generation = 0;
  let isDisposed = false;

  function computeActive(): boolean {
    if (isDisposed || options.isDisposed()) return false;
    if (!platformEligible) return false;
    if (options.getDomainStatus() !== 'error') return false;
    const failure = options.getFailure();
    return shouldWatchForNetworkRecovery(failure);
  }

  function cancelLoop(): void {
    loopRunning = false;
    generation++;
    if (timerId !== null) {
      clearTimeout(timerId);
      timerId = null;
    }
  }

  function scheduleNextTick(delayMs: number, gen: number): void {
    timerId = setTimeout(() => {
      timerId = null;
      void executeTick(gen);
    }, delayMs);
  }

  async function executeTick(gen: number): Promise<void> {
    if (gen !== generation || !loopRunning || !active || isDisposed || options.isDisposed()) {
      return;
    }
    if (!currentProbe) {
      return;
    }

    probeInFlight = true;
    let reachable: boolean;
    try {
      reachable = await currentProbe();
    } catch {
      reachable = false;
    } finally {
      probeInFlight = false;
    }

    if (gen !== generation || !loopRunning || !active || isDisposed || options.isDisposed()) {
      return;
    }

    if (reachable) {
      recoveries += 1;
      loopRunning = false;
      loopEnded = true;
      debugLog('[V3Player][Watchdog] Server reachable again, recovering', {
        attempt: recoveries,
      });

      if (options.isRetryInFlight?.()) {
        return;
      }

      const target = options.getTargetContext();
      if (target) {
        void options.onRecover(target);
      }
      return;
    }

    // Unreachable: advance attempt and schedule next backoff tick
    attempt += 1;
    const nextDelay = Math.min(FIRST_PROBE_DELAY_MS * 2 ** attempt, MAX_PROBE_DELAY_MS);
    scheduleNextTick(nextDelay, gen);
  }

  function startLoop(): void {
    cancelLoop();
    loopRunning = true;
    loopEnded = false;
    attempt = 0;

    if (recoveries >= MAX_AUTOMATIC_NETWORK_RECOVERIES) {
      debugWarn('[V3Player][Watchdog] Automatic network recoveries exhausted');
      loopRunning = false;
      return;
    }

    if (!currentProbe) {
      return;
    }

    const currentGen = generation;
    scheduleNextTick(FIRST_PROBE_DELAY_MS, currentGen);
  }

  function onDomainStateChanged(): void {
    if (isDisposed || options.isDisposed()) return;

    if (options.getDomainStatus() === 'playing') {
      recoveries = 0;
    }

    const newActive = computeActive();

    // Condition C2: Edge-triggered activation
    if (!active && newActive) {
      active = true;
      startLoop();
    } else if (active && !newActive) {
      active = false;
      cancelLoop();
    }
    // true -> true: no restart, no attempt reset, no revival of ended loop
    // false -> false: no-op
  }

  function setContext(params: {
    platformEligible: boolean;
    intentKey: string;
    probe?: () => Promise<boolean>;
  }): void {
    if (isDisposed || options.isDisposed()) return;

    const previousIntentKey = intentKey;
    intentKey = params.intentKey;
    if (intentKey !== previousIntentKey) {
      recoveries = 0;
    }

    platformEligible = params.platformEligible;

    const previousProbe = currentProbe;
    currentProbe = params.probe ?? null;
    const probeChanged = currentProbe !== previousProbe;

    const newActive = computeActive();

    // Condition C1: Probe identity is part of the loop key
    if (active && newActive && probeChanged) {
      if (currentProbe) {
        startLoop();
      } else {
        cancelLoop();
      }
      return;
    }

    if (!active && newActive) {
      active = true;
      startLoop();
    } else if (active && !newActive) {
      active = false;
      cancelLoop();
    }
  }

  function activate(): void {
    isDisposed = false;
    onDomainStateChanged();
  }

  function dispose(): void {
    isDisposed = true;
    active = false;
    cancelLoop();
    currentProbe = null;
  }

  return {
    setContext,
    onDomainStateChanged,
    activate,
    dispose,
    getState: (): PlaybackNetworkWatchdogState => ({
      active,
      attempt,
      recoveries,
      timerPending: timerId !== null,
      probeInFlight,
      loopEnded,
    }),
  };
}
