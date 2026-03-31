import { useEffect } from 'react';
import type { RefObject } from 'react';
import { requestHostInputFocus, resolveHostEnvironment } from '../lib/hostBridge';

interface UseTvInitialFocusOptions<T extends HTMLElement> {
  enabled: boolean;
  targetRef: RefObject<T | null>;
}

export function useTvInitialFocus<T extends HTMLElement>({
  enabled,
  targetRef,
}: UseTvInitialFocusOptions<T>): void {
  useEffect(() => {
    if (!enabled) {
      return;
    }

    const isTvHost = resolveHostEnvironment().isTv;

    if (isTvHost) {
      requestHostInputFocus();
    }
    targetRef.current?.focus();

    const frame = window.requestAnimationFrame(() => {
      if (isTvHost) {
        requestHostInputFocus();
      }
      targetRef.current?.focus();
    });

    return () => window.cancelAnimationFrame(frame);
  }, [enabled, targetRef]);
}
