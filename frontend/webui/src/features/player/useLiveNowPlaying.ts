import { useEffect, useRef, useState } from 'react';
import { postServicesNowNext } from '../../client-ts';
import { getStoredToken } from '../../utils/tokenStorage';
import { debugWarn } from '../../utils/logging';

export interface LiveNowPlaying {
  title: string | null;
  desc: string | null;
}

const EMPTY: LiveNowPlaying = { title: null, desc: null };

// Safety poll cap: even if the now/next end time is far away (or missing),
// re-check at least this often so a mid-programme EPG correction is picked up.
const SAFETY_POLL_MS = 5 * 60 * 1000;
// Re-fetch a few seconds AFTER the current programme is due to end, so the
// backend has rolled now/next over to the new show before we ask again.
const REFRESH_BUFFER_MS = 4000;
const MIN_REFRESH_MS = 1000;

/**
 * useLiveNowPlaying resolves the currently-airing programme (title + short
 * synopsis) for a live service and keeps it current: it re-fetches when the
 * running programme is due to end, so the player header automatically switches
 * to the next show instead of sticking on the old title. Disabled (returns
 * empty) for non-live playback, where the title is fixed.
 */
export function useLiveNowPlaying(serviceRef: string | null, enabled: boolean): LiveNowPlaying {
  const [nowPlaying, setNowPlaying] = useState<LiveNowPlaying>(EMPTY);
  const timerRef = useRef<number | null>(null);

  useEffect(() => {
    if (!enabled || !serviceRef) {
      setNowPlaying(EMPTY);
      return;
    }

    let cancelled = false;
    const clearTimer = () => {
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current);
        timerRef.current = null;
      }
    };
    const scheduleNext = (delayMs: number) => {
      clearTimer();
      const clamped = Math.max(MIN_REFRESH_MS, Math.min(delayMs, SAFETY_POLL_MS));
      timerRef.current = window.setTimeout(() => {
        void fetchNow();
      }, clamped);
    };

    const fetchNow = async () => {
      try {
        const authToken = getStoredToken().trim();
        const result = await postServicesNowNext({
          headers: { ...(authToken ? { Authorization: `Bearer ${authToken}` } : {}) },
          body: { services: [serviceRef] },
        });
        if (cancelled) return;
        const now = result.data?.items?.[0]?.now;
        setNowPlaying({
          title: now?.title?.trim() || null,
          desc: now?.desc?.trim() || null,
        });
        const endMs = now?.end ? now.end * 1000 : 0;
        const untilEnd = endMs > 0 ? endMs - Date.now() + REFRESH_BUFFER_MS : 0;
        scheduleNext(untilEnd > 0 ? untilEnd : SAFETY_POLL_MS);
      } catch (err) {
        if (cancelled) return;
        debugWarn('now/next fetch failed', err);
        scheduleNext(SAFETY_POLL_MS);
      }
    };

    void fetchNow();
    return () => {
      cancelled = true;
      clearTimer();
    };
  }, [serviceRef, enabled]);

  return nowPlaying;
}
