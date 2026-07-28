import { useCallback, useRef } from 'react';
import type { MutableRefObject } from 'react';

export interface PlaybackResourceCleanup {
  recordingTimeoutRef: MutableRefObject<number | null>;
  vodRetryRef: MutableRefObject<number | null>;
  vodFetchRef: MutableRefObject<AbortController | null>;
  nativeVideoRevealTimerRef: MutableRefObject<number | null>;
  nativeVideoVeilRevealTimerRef: MutableRefObject<number | null>;
  nativeVideoVeilClearTimerRef: MutableRefObject<number | null>;
  clearRecordingTimeout: () => void;
  clearVodRetry: () => void;
  clearVodFetch: () => void;
  clearNativeVideoVeilTimers: () => void;
  clearNativeVideoRevealTimer: () => void;
}

export function usePlaybackResourceCleanup(): PlaybackResourceCleanup {
  const recordingTimeoutRef = useRef<number | null>(null);
  const vodRetryRef = useRef<number | null>(null);
  const vodFetchRef = useRef<AbortController | null>(null);
  const nativeVideoRevealTimerRef = useRef<number | null>(null);
  const nativeVideoVeilRevealTimerRef = useRef<number | null>(null);
  const nativeVideoVeilClearTimerRef = useRef<number | null>(null);

  const clearRecordingTimeout = useCallback(() => {
    if (recordingTimeoutRef.current !== null) {
      window.clearTimeout(recordingTimeoutRef.current);
      recordingTimeoutRef.current = null;
    }
  }, []);

  const clearVodRetry = useCallback(() => {
    if (vodRetryRef.current !== null) {
      window.clearTimeout(vodRetryRef.current);
      vodRetryRef.current = null;
    }
    clearRecordingTimeout();
  }, [clearRecordingTimeout]);

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
    recordingTimeoutRef,
    vodRetryRef,
    vodFetchRef,
    nativeVideoRevealTimerRef,
    nativeVideoVeilRevealTimerRef,
    nativeVideoVeilClearTimerRef,
    clearRecordingTimeout,
    clearVodRetry,
    clearVodFetch,
    clearNativeVideoVeilTimers,
    clearNativeVideoRevealTimer,
  };
}
