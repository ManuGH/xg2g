import { useEffect, useRef, useState } from 'react';

export function useStartupElapsed(active: boolean): number {
  const [startupElapsedSeconds, setStartupElapsedSeconds] = useState(0);
  const startupStartedAtRef = useRef<number | null>(null);

  useEffect(() => {
    if (!active) {
      startupStartedAtRef.current = null;
      setStartupElapsedSeconds(0);
      return;
    }

    if (startupStartedAtRef.current === null) {
      startupStartedAtRef.current = Date.now();
    }

    const updateElapsed = () => {
      const startedAt = startupStartedAtRef.current;
      if (startedAt === null) {
        setStartupElapsedSeconds(0);
        return;
      }
      setStartupElapsedSeconds(Math.max(0, Math.floor((Date.now() - startedAt) / 1000)));
    };

    updateElapsed();
    const timer = window.setInterval(updateElapsed, 1000);
    return () => window.clearInterval(timer);
  }, [active]);

  return startupElapsedSeconds;
}
