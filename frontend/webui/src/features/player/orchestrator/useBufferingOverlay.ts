import { useEffect, useRef, useState } from 'react';
import type { PlayerStatus } from '../../../types/v3-player';

const BUFFERING_OVERLAY_DELAY_MS = 325;

export function useBufferingOverlay(status: PlayerStatus): boolean {
  const [showBufferingOverlay, setShowBufferingOverlay] = useState(false);
  const bufferingOverlayTimerRef = useRef<number | null>(null);

  useEffect(() => {
    if (bufferingOverlayTimerRef.current !== null) {
      window.clearTimeout(bufferingOverlayTimerRef.current);
      bufferingOverlayTimerRef.current = null;
    }

    if (status !== 'buffering') {
      setShowBufferingOverlay(false);
      return;
    }

    bufferingOverlayTimerRef.current = window.setTimeout(() => {
      bufferingOverlayTimerRef.current = null;
      setShowBufferingOverlay(true);
    }, BUFFERING_OVERLAY_DELAY_MS);

    return () => {
      if (bufferingOverlayTimerRef.current !== null) {
        window.clearTimeout(bufferingOverlayTimerRef.current);
        bufferingOverlayTimerRef.current = null;
      }
    };
  }, [status]);

  return showBufferingOverlay;
}
