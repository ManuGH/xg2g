import { useEffect, useRef, useState } from 'react';
import type { PlayerStatus } from '../../../types/v3-player';

const BUFFERING_OVERLAY_DELAY_MS = 325;

// Debounced flag for the transient "wait" overlay. It covers both `buffering`
// and `recovering`: a stall/decode reattach (the dominant HEVC failure path)
// transits `recovering` for several seconds, and that state otherwise renders
// NO overlay — leaving the user staring at a frozen/black frame with no
// feedback. The 325ms debounce avoids flashing the overlay on a sub-second
// in-place recovery.
export function useBufferingOverlay(status: PlayerStatus): boolean {
  const [showBufferingOverlay, setShowBufferingOverlay] = useState(false);
  const bufferingOverlayTimerRef = useRef<number | null>(null);

  useEffect(() => {
    if (bufferingOverlayTimerRef.current !== null) {
      window.clearTimeout(bufferingOverlayTimerRef.current);
      bufferingOverlayTimerRef.current = null;
    }

    if (status !== 'buffering' && status !== 'recovering') {
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
