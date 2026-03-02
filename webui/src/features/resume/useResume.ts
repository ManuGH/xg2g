import { useEffect, useRef, useCallback } from 'react';
import { saveResume } from './api';
import { debugWarn, formatError } from '../../utils/logging';

const SAVE_INTERVAL_MS = 10000;
const JUMP_THRESHOLD = 30;

interface UseResumeProps {
  recordingId?: string;
  duration?: number | null;
  videoElement: HTMLVideoElement | null;
  isPlaying: boolean;
  isSeekable?: boolean;
}

export function useResume({ recordingId, duration, videoElement, isPlaying, isSeekable = true }: UseResumeProps) {
  const lastSavedTime = useRef<number>(0);
  const saveTimerRef = useRef<number | null>(null);
  const finishedRef = useRef(false);

  // Reset finished flag when recordingId changes
  useEffect(() => {
    finishedRef.current = false;
  }, [recordingId]);

  const save = useCallback(async (forceFinished: boolean = false) => {
    if (!recordingId || !videoElement) return;
    if (!isSeekable) return;

    // Prevent overwriting finished state unless forced
    if (finishedRef.current && !forceFinished) return;

    const currentTime = videoElement.currentTime;
    const durationSec = duration && duration > 0 ? duration : 0;

    // Safety check: don't save 0 if we haven't started (unless forceFinished)
    if (currentTime < 1 && !forceFinished) return;

    const isFinished = forceFinished || (durationSec > 0 && currentTime >= durationSec - 5); // 5s threshold for "watched"

    if (isFinished) {
      finishedRef.current = true;
    }

    try {
      await saveResume(recordingId, {
        position: currentTime,
        total: durationSec > 0 ? durationSec : undefined,
        finished: isFinished
      });
      lastSavedTime.current = currentTime;
    } catch (err) {
      debugWarn('[useResume] Failed to save resume state', formatError(err));
    }
  }, [recordingId, videoElement, duration, isSeekable]);

  // Periodic Save
  useEffect(() => {
    if (!isPlaying || !recordingId || !isSeekable) {
      if (saveTimerRef.current) {
        window.clearInterval(saveTimerRef.current);
        saveTimerRef.current = null;
      }
      return;
    }

    saveTimerRef.current = window.setInterval(() => {
      save();
    }, SAVE_INTERVAL_MS);

    return () => {
      if (saveTimerRef.current) {
        window.clearInterval(saveTimerRef.current);
        saveTimerRef.current = null;
      }
    };
  }, [isPlaying, recordingId, isSeekable, save]);

  // Event Listeners (Pause, Ended, Seeking)
  useEffect(() => {
    if (!videoElement || !recordingId || !isSeekable) return;

    const handlePause = () => save();
    const handleEnded = () => save(true);
    const handleSeeked = () => {
      if (Math.abs(videoElement.currentTime - lastSavedTime.current) > JUMP_THRESHOLD) {
        save();
      }
    };
    const handleVisibilityChange = () => {
      if (document.visibilityState === 'hidden') save();
    };
    const handleBeforeUnload = () => save();

    videoElement.addEventListener('pause', handlePause);
    videoElement.addEventListener('ended', handleEnded);
    videoElement.addEventListener('seeked', handleSeeked);
    document.addEventListener('visibilitychange', handleVisibilityChange);
    window.addEventListener('beforeunload', handleBeforeUnload);

    return () => {
      videoElement.removeEventListener('pause', handlePause);
      videoElement.removeEventListener('ended', handleEnded);
      videoElement.removeEventListener('seeked', handleSeeked);
      document.removeEventListener('visibilitychange', handleVisibilityChange);
      window.removeEventListener('beforeunload', handleBeforeUnload);
    };
  }, [videoElement, recordingId, isSeekable, save]);
}
