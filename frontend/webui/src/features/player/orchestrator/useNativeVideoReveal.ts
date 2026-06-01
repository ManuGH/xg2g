import { useCallback, useEffect, useRef, useState } from 'react';
import type { MutableRefObject, RefObject } from 'react';
import type { PlayerStatus, VideoElementRef } from '../../../types/v3-player';
import {
  NATIVE_VIDEO_REVEAL_REBUFFER,
  NATIVE_VIDEO_REVEAL_STARTUP,
  NATIVE_VIDEO_REBUFFER_VEIL_MS,
  NATIVE_VIDEO_UNVEIL_AFTER_PLAYING_MS,
  NATIVE_VIDEO_WATCHDOG_INTERVAL_MS,
  shouldForceRevealNativeVideo,
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

  // Ground-truth reveal watchdog. Independent of the status FSM (deliberately
  // NOT depending on `status`, so a buffering<->playing oscillation cannot keep
  // restarting the sampler): while the native video is hidden, poll the element
  // itself and reveal as soon as it is demonstrably decoding frames. This is the
  // safety net for the pause->resume->black case where the FSM stays pinned at
  // 'buffering' even though the <video> is playing. It only reveals on real
  // forward progress, so a genuine rebuffer keeps the veil up.
  useEffect(() => {
    if (!isNativeEngine || showNativeVideo) {
      return;
    }
    const video = videoRef.current;
    if (!video) {
      return;
    }

    let lastSampledTime = video.currentTime;
    const intervalId = window.setInterval(() => {
      const current = videoRef.current;
      if (!current) {
        return;
      }
      const advancedSeconds = current.currentTime - lastSampledTime;
      lastSampledTime = current.currentTime;
      if (
        shouldForceRevealNativeVideo({
          paused: current.paused,
          readyState: current.readyState,
          advancedSeconds,
        })
      ) {
        window.clearInterval(intervalId);
        nativeVideoShownRef.current = true;
        nativeVideoHoldPositionRef.current = null;
        clearNativeVideoRevealTimer();
        clearNativeVideoVeilTimers();
        setShowNativeVideo(true);
        setShowNativeVideoVeil(false);
        setNativeVeilResumeArmed(false);
      }
    }, NATIVE_VIDEO_WATCHDOG_INTERVAL_MS);

    return () => {
      window.clearInterval(intervalId);
    };
  }, [
    clearNativeVideoRevealTimer,
    clearNativeVideoVeilTimers,
    isNativeEngine,
    showNativeVideo,
    videoRef,
  ]);

  return {
    showNativeVideo,
    showNativeVideoVeil,
    nativeVeilResumeArmed,
    resetNativeVideoState,
  };
}
