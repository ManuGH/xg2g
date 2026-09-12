// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { useCallback, useInsertionEffect, useLayoutEffect } from 'react';
import type { PlaybackController } from './playbackController';
import { timeoutSignal } from '../utils/requestTimeout';
import { PROBE_TIMEOUT_MS } from './playbackNetworkWatchdogRuntime';

export {
  shouldWatchForNetworkRecovery,
  FIRST_PROBE_DELAY_MS,
  MAX_PROBE_DELAY_MS,
  PROBE_TIMEOUT_MS,
  MAX_AUTOMATIC_NETWORK_RECOVERIES,
} from './playbackNetworkWatchdogRuntime';

export interface UseNetworkRecoveryWatchdogOptions {
  controller: PlaybackController;
  apiBase: string;
  isTv: boolean;
  intentKey: string;
}

/**
 * Thin React adapter hook that publishes connectivity probing capabilities
 * and viewing intent to the PlaybackController. The lifecycle, timing,
 * backoff, and recovery budget are owned by PlaybackNetworkWatchdogRuntime.
 */
export function useNetworkRecoveryWatchdog({
  controller,
  apiBase,
  isTv,
  intentKey,
}: UseNetworkRecoveryWatchdogOptions): void {
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

  useInsertionEffect(() => {
    controller.setNetworkWatchdogContext({
      platformEligible: !isTv,
      intentKey,
      probe,
    });
  });

  useLayoutEffect(() => {
    controller.setNetworkWatchdogContext({
      platformEligible: !isTv,
      intentKey,
      probe,
    });
  });
}
