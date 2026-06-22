import { useEffect, useRef, useState } from 'react';

// useDelayedFlag returns true only after `active` has stayed continuously true for
// delayMs; going inactive resets it to false immediately. Used to keep the centered
// "preparing" spinner card from flashing for work that finishes faster than delayMs
// (e.g. a sub-second resume re-prepare). It gates ONLY the card — never a cover — so
// a delay can never expose a black frame. Mirrors the existing useBufferingOverlay.
export function useDelayedFlag(active: boolean, delayMs: number): boolean {
  const [shown, setShown] = useState(false);
  const timerRef = useRef<number | null>(null);

  useEffect(() => {
    if (timerRef.current !== null) {
      window.clearTimeout(timerRef.current);
      timerRef.current = null;
    }
    if (!active) {
      setShown(false);
      return;
    }
    timerRef.current = window.setTimeout(() => {
      timerRef.current = null;
      setShown(true);
    }, delayMs);
    return () => {
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current);
        timerRef.current = null;
      }
    };
  }, [active, delayMs]);

  return shown;
}
