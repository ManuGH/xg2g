import { useCallback, useEffect, useRef, useState } from 'react';
import type { MutableRefObject, RefObject } from 'react';
import type { PlayerStatus, VideoElementRef } from '../../../types/v3-player';
import {
  NATIVE_VIDEO_REVEAL_REBUFFER,
  NATIVE_VIDEO_REVEAL_STARTUP,
  NATIVE_VIDEO_REBUFFER_VEIL_MS,
  NATIVE_VIDEO_UNVEIL_AFTER_PLAYING_MS,
} from './nativePlaybackHelpers';

interface UseNativeVideoRevealArgs {
  isNativeEngine: boolean;
  status: PlayerStatus;
  videoRef: RefObject<VideoElementRef>;
  getBufferedAheadSeconds: () => number;
  nativeVideoRevealTimerRef: MutableRefObject<number | null>;
  nativeVideoVeilRevealTimerRef: MutableRefObject<number | null>;
  nativeVideoVeilClearTimerRef: MutableRefObject<number | null>;
  clearNativeVideoRevealTimer: () => void;
  clearNativeVideoVeilTimers: () => void;
}

export interface NativeVideoReveal {
  showNativeVideo: boolean;
  showNativeVideoVeil: boolean;
  nativeVeilResumeArmed: boolean;
  resetNativeVideoState: () => void;
}

export function useNativeVideoReveal({
  isNativeEngine,
  status,
  videoRef,
  getBufferedAheadSeconds,
  nativeVideoRevealTimerRef,
  nativeVideoVeilRevealTimerRef,
  nativeVideoVeilClearTimerRef,
  clearNativeVideoRevealTimer,
  clearNativeVideoVeilTimers,
}: UseNativeVideoRevealArgs): NativeVideoReveal {
  const [showNativeVideo, setShowNativeVideo] = useState(true);
  const [showNativeVideoVeil, setShowNativeVideoVeil] = useState(false);
  const [nativeVeilResumeArmed, setNativeVeilResumeArmed] = useState(false);
  const nativeVideoShownRef = useRef(false);
  const nativeVideoHoldPositionRef = useRef<number | null>(null);

  const resetNativeVideoState = useCallback(() => {
    nativeVideoShownRef.current = false;
    nativeVideoHoldPositionRef.current = null;
    clearNativeVideoVeilTimers();
    setShowNativeVideo(true);
    setShowNativeVideoVeil(false);
    setNativeVeilResumeArmed(false);
  }, [clearNativeVideoVeilTimers]);

  useEffect(() => {
    if (!isNativeEngine) {
      clearNativeVideoRevealTimer();
      clearNativeVideoVeilTimers();
      nativeVideoShownRef.current = false;
      nativeVideoHoldPositionRef.current = null;
      setShowNativeVideo(true);
      setShowNativeVideoVeil(false);
      setNativeVeilResumeArmed(false);
      return;
    }

    if (status === 'starting' || status === 'priming' || status === 'building' || status === 'buffering' || status === 'recovering') {
      clearNativeVideoRevealTimer();
      clearNativeVideoVeilTimers();
      if (showNativeVideo) {
        nativeVideoHoldPositionRef.current = videoRef.current?.currentTime ?? null;
      }
      setShowNativeVideo(false);
      setShowNativeVideoVeil(true);
      setNativeVeilResumeArmed(false);
      return;
    }

    if (status === 'idle' || status === 'error' || status === 'stopped') {
      clearNativeVideoRevealTimer();
      clearNativeVideoVeilTimers();
      nativeVideoShownRef.current = false;
      nativeVideoHoldPositionRef.current = null;
      setShowNativeVideo(true);
      setShowNativeVideoVeil(false);
      setNativeVeilResumeArmed(false);
      return;
    }

    if (status === 'paused') {
      clearNativeVideoRevealTimer();
      clearNativeVideoVeilTimers();
      nativeVideoHoldPositionRef.current = null;
      setShowNativeVideo(true);
      setShowNativeVideoVeil(false);
      setNativeVeilResumeArmed(false);
      return;
    }

    if (showNativeVideo) {
      return;
    }

    const revealThresholds = nativeVideoShownRef.current
      ? NATIVE_VIDEO_REVEAL_REBUFFER
      : NATIVE_VIDEO_REVEAL_STARTUP;

    const waitForStablePlayback = () => {
      const video = videoRef.current;
      if (!video) {
        nativeVideoRevealTimerRef.current = window.setTimeout(waitForStablePlayback, revealThresholds.retryMs);
        return;
      }

      const bufferAheadSeconds = getBufferedAheadSeconds();
      const holdPosition = nativeVideoHoldPositionRef.current;
      const playbackAdvancedEnough =
        holdPosition === null || !Number.isFinite(holdPosition)
          ? true
          : Math.max(0, video.currentTime - holdPosition) >= revealThresholds.minAdvanceSeconds;
      const playbackResumeSatisfied = revealThresholds.requirePlaybackResume
        ? !video.paused
        : (status === 'ready' || !video.paused);
      const readyForReveal =
        video.readyState >= 3 &&
        playbackResumeSatisfied &&
        playbackAdvancedEnough &&
        (video.readyState >= 4 || bufferAheadSeconds >= revealThresholds.minBufferSeconds);

      if (readyForReveal) {
        nativeVideoRevealTimerRef.current = null;
        const isRebufferReveal = nativeVideoShownRef.current;
        nativeVideoShownRef.current = true;
        nativeVideoHoldPositionRef.current = null;
        setShowNativeVideo(true);
        clearNativeVideoVeilTimers();
        if (isRebufferReveal) {
          setShowNativeVideoVeil(true);
          setNativeVeilResumeArmed(false);
          nativeVideoVeilRevealTimerRef.current = window.setTimeout(() => {
            nativeVideoVeilRevealTimerRef.current = null;
            setNativeVeilResumeArmed(true);
          }, NATIVE_VIDEO_REBUFFER_VEIL_MS);
        } else {
          setShowNativeVideoVeil(true);
          setNativeVeilResumeArmed(true);
        }
        return;
      }

      nativeVideoRevealTimerRef.current = window.setTimeout(waitForStablePlayback, revealThresholds.retryMs);
    };

    clearNativeVideoRevealTimer();
    nativeVideoRevealTimerRef.current = window.setTimeout(waitForStablePlayback, revealThresholds.stableMs);

    return () => {
      clearNativeVideoRevealTimer();
      clearNativeVideoVeilTimers();
    };
  }, [
    clearNativeVideoRevealTimer,
    clearNativeVideoVeilTimers,
    getBufferedAheadSeconds,
    isNativeEngine,
    nativeVideoRevealTimerRef,
    nativeVideoVeilRevealTimerRef,
    showNativeVideo,
    status,
    videoRef,
  ]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) {
      return;
    }

    if (!(isNativeEngine && showNativeVideoVeil && showNativeVideo && nativeVeilResumeArmed)) {
      return;
    }

    const releaseVeil = () => {
      clearNativeVideoVeilTimers();
      nativeVideoVeilClearTimerRef.current = window.setTimeout(() => {
        nativeVideoVeilClearTimerRef.current = null;
        setShowNativeVideoVeil(false);
        setNativeVeilResumeArmed(false);
      }, NATIVE_VIDEO_UNVEIL_AFTER_PLAYING_MS);
    };

    const handlePlaying = () => {
      releaseVeil();
    };
    const handleProgress = () => {
      if (!video.paused && video.readyState >= 3) {
        releaseVeil();
      }
    };

    if (!video.paused && video.readyState >= 3) {
      releaseVeil();
      return;
    }

    video.addEventListener('playing', handlePlaying, { once: true });
    video.addEventListener('timeupdate', handleProgress);
    video.addEventListener('canplay', handleProgress);

    return () => {
      video.removeEventListener('playing', handlePlaying);
      video.removeEventListener('timeupdate', handleProgress);
      video.removeEventListener('canplay', handleProgress);
    };
  }, [
    clearNativeVideoVeilTimers,
    isNativeEngine,
    nativeVeilResumeArmed,
    nativeVideoVeilClearTimerRef,
    showNativeVideo,
    showNativeVideoVeil,
    videoRef,
  ]);

  return {
    showNativeVideo,
    showNativeVideoVeil,
    nativeVeilResumeArmed,
    resetNativeVideoState,
  };
}
