import { useCallback, useEffect, useRef } from 'react';
import { debugLog, debugWarn } from '../../../utils/logging';
import { timeoutSignal } from '../utils/requestTimeout';
import { NETWORK_STARVATION_CODES } from './recoveryLadder';

// The last net under the two connectivity edges the player already watches.
//
// foregroundResume covers hidden->visible, decideOnlineRecovery covers
// offline->online. Neither fires in the case that actually strands a mobile
// viewer: the app is in the foreground, the screen is on, and the link is dead
// without the browser saying so. `navigator.onLine` reports interface state,
// not reachability, and stays true through a tunnel — so when a start attempt
// fails mid-outage the player lands in 'error' and nothing ever takes it out
// again. The user sees a frozen picture and a Retry button.
//
// This watchdog closes that hole from the other side: while playback is parked
// on a non-terminal error, poll a cheap endpoint until the server answers, then
// hand one recovery attempt back to the orchestrator.

const FIRST_PROBE_DELAY_MS = 5_000;
const MAX_PROBE_DELAY_MS = 30_000;
const PROBE_TIMEOUT_MS = 5_000;

/**
 * Automatic recoveries granted per viewing intent. Bounded because the probe
 * only proves the server is reachable, not that playback will now succeed — a
 * failure with an unrelated cause would otherwise retry forever.
 */
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

export interface UseNetworkRecoveryWatchdogOptions {
  apiBase: string;
  /** Playback is parked on a failure that regained connectivity could fix. */
  active: boolean;
  /**
   * Identifies the current viewing intent (channel or recording). Changing it
   * returns the recovery budget — a new thing to watch is a fresh start.
   */
  intentKey: string;
  /** Playback is healthy; the budget is returned. */
  healthy: boolean;
  onReachable: () => void;
}

export function useNetworkRecoveryWatchdog({
  apiBase,
  active,
  intentKey,
  healthy,
  onReachable,
}: UseNetworkRecoveryWatchdogOptions): void {
  const recoveriesRef = useRef(0);
  const onReachableRef = useRef(onReachable);
  onReachableRef.current = onReachable;

  const probe = useCallback(async (): Promise<boolean> => {
    try {
      const response = await fetch(`${apiBase}/system/healthz`, {
        method: 'HEAD',
        cache: 'no-store',
        credentials: 'same-origin',
        signal: timeoutSignal(PROBE_TIMEOUT_MS),
      });
      return response.ok || response.status === 204;
    } catch {
      return false;
    }
  }, [apiBase]);

  useEffect(() => {
    recoveriesRef.current = 0;
  }, [intentKey]);

  useEffect(() => {
    if (healthy) {
      recoveriesRef.current = 0;
    }
  }, [healthy]);

  useEffect(() => {
    if (!active) {
      return;
    }
    if (recoveriesRef.current >= MAX_AUTOMATIC_NETWORK_RECOVERIES) {
      debugWarn('[V3Player][Watchdog] Automatic network recoveries exhausted');
      return;
    }

    let cancelled = false;
    let timerId: number | null = null;
    let attempt = 0;

    const tick = async () => {
      if (cancelled) {
        return;
      }
      const reachable = await probe();
      if (cancelled) {
        return;
      }
      if (reachable) {
        recoveriesRef.current += 1;
        debugLog('[V3Player][Watchdog] Server reachable again, recovering', {
          attempt: recoveriesRef.current,
        });
        onReachableRef.current();
        return;
      }
      attempt += 1;
      timerId = window.setTimeout(
        () => {
          timerId = null;
          void tick();
        },
        Math.min(FIRST_PROBE_DELAY_MS * 2 ** attempt, MAX_PROBE_DELAY_MS),
      );
    };

    // The first probe waits: a failure whose cause is not connectivity would
    // otherwise be retried the instant it surfaces, and the delay is what keeps
    // the bounded budget from being spent in one burst.
    timerId = window.setTimeout(() => {
      timerId = null;
      void tick();
    }, FIRST_PROBE_DELAY_MS);

    return () => {
      cancelled = true;
      if (timerId !== null) {
        window.clearTimeout(timerId);
      }
    };
  }, [active, probe]);
}
