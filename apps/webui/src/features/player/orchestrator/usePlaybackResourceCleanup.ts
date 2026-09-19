import { useCallback, useRef } from 'react';
import type { MutableRefObject } from 'react';

export interface PlaybackResourceCleanup {
  vodFetchRef: MutableRefObject<AbortController | null>;
  nativeVideoRevealTimerRef: MutableRefObject<number | null>;
  nativeVideoVeilRevealTimerRef: MutableRefObject<number | null>;
  nativeVideoVeilClearTimerRef: MutableRefObject<number | null>;
  clearVodFetch: () => void;
  clearNativeVideoVeilTimers: () => void;
  clearNativeVideoRevealTimer: () => void;
}

export function usePlaybackResourceCleanup(): PlaybackResourceCleanup {
  const vodFetchRef = useRef<AbortController | null>(null);
  const nativeVideoRevealTimerRef = useRef<number | null>(null);
  const nativeVideoVeilRevealTimerRef = useRef<number | null>(null);
  const nativeVideoVeilClearTimerRef = useRef<number | null>(null);

  const clearVodFetch = useCallback(() => {
    if (vodFetchRef.current) {
      vodFetchRef.current.abort();
      vodFetchRef.current = null;
    }
  }, []);

  const clearNativeVideoVeilTimers = useCallback(() => {
    if (nativeVideoVeilRevealTimerRef.current !== null) {
      window.clearTimeout(nativeVideoVeilRevealTimerRef.current);
      nativeVideoVeilRevealTimerRef.current = null;
    }
    if (nativeVideoVeilClearTimerRef.current !== null) {
      window.clearTimeout(nativeVideoVeilClearTimerRef.current);
      nativeVideoVeilClearTimerRef.current = null;
    }
  }, []);

  const clearNativeVideoRevealTimer = useCallback(() => {
    if (nativeVideoRevealTimerRef.current !== null) {
      window.clearTimeout(nativeVideoRevealTimerRef.current);
      nativeVideoRevealTimerRef.current = null;
    }
  }, []);

  return {
    vodFetchRef,
    nativeVideoRevealTimerRef,
    nativeVideoVeilRevealTimerRef,
    nativeVideoVeilClearTimerRef,
    clearVodFetch,
    clearNativeVideoVeilTimers,
    clearNativeVideoRevealTimer,
  };
}
