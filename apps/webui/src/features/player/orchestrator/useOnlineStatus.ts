import { useEffect, useState } from 'react';

// Tracks browser connectivity via navigator.onLine + the window online/offline
// events. The orchestrator uses the offline->online edge to auto-reconnect
// playback (see decideOnlineRecovery) instead of waiting for the user to switch
// tabs or hit Retry. Defaults to online when the API is unavailable so it never
// false-negatives playback on a healthy connection.
export function useOnlineStatus(): boolean {
  const [isOnline, setIsOnline] = useState<boolean>(() =>
    typeof navigator === 'undefined' || typeof navigator.onLine !== 'boolean'
      ? true
      : navigator.onLine,
  );

  useEffect(() => {
    if (typeof window === 'undefined') {
      return;
    }
    const handleOnline = () => setIsOnline(true);
    const handleOffline = () => setIsOnline(false);

    window.addEventListener('online', handleOnline);
    window.addEventListener('offline', handleOffline);

    return () => {
      window.removeEventListener('online', handleOnline);
      window.removeEventListener('offline', handleOffline);
    };
  }, []);

  return isOnline;
}
