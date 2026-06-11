import { useEffect, useRef, useState } from 'react';
import type { PlayerStatus } from '../../../types/v3-player';

const BUFFERING_OVERLAY_DELAY_MS = 325;

// Debounced flag for the transient "wait" overlay. It covers both `buffering`
// and `recovering`: a stall/decode reattach (the dominant HEVC failure path)
// transits `recovering` for several seconds, and that state otherwise renders
// NO overlay — leaving the user staring at a frozen/black frame with no
// feedback. The 325ms debounce avoids flashing the overlay on a sub-second
// in-place recovery.
//
// The timer lifecycle is managed with a dedicated unmount-only effect so that
// transitioning between waiting states (buffering <-> recovering) does NOT
// reset the debounce timer prematurely.
export function useBufferingOverlay(status: PlayerStatus): boolean {
  const [showBufferingOverlay, setShowBufferingOverlay] = useState(false);
  const bufferingOverlayTimerRef = useRef<number | null>(null);

  // Dedicated unmount-only effect to clear the timer on unmount.
  useEffect(() => {
    return () => {
      if (bufferingOverlayTimerRef.current !== null) {
        window.clearTimeout(bufferingOverlayTimerRef.current);
      }
    };
  }, []);

  useEffect(() => {
    const isWaiting = status === 'buffering' || status === 'recovering';

    if (!isWaiting) {
      if (bufferingOverlayTimerRef.current !== null) {
        window.clearTimeout(bufferingOverlayTimerRef.current);
        bufferingOverlayTimerRef.current = null;
      }
      setShowBufferingOverlay(false);
      return;
    }

    // If we are already showing the overlay or a timer is already running, do nothing.
    if (showBufferingOverlay || bufferingOverlayTimerRef.current !== null) {
      return;
    }

    bufferingOverlayTimerRef.current = window.setTimeout(() => {
      bufferingOverlayTimerRef.current = null;
      setShowBufferingOverlay(true);
    }, BUFFERING_OVERLAY_DELAY_MS);
  }, [status, showBufferingOverlay]);

  return showBufferingOverlay;
}
