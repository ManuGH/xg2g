import { useCallback, useEffect, useMemo, useState } from 'react';
import type { Dispatch, MutableRefObject, RefObject, SetStateAction } from 'react';
import type { VideoElementRef } from '../../types/v3-player';
import { debugWarn } from '../../utils/logging';

export function formatClock(value: number): string {
  if (!Number.isFinite(value) || value < 0) return '--:--';
  const totalSeconds = Math.floor(value);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const pad = (n: number) => n.toString().padStart(2, '0');
  return hours > 0 ? `${hours}:${pad(minutes)}:${pad(seconds)}` : `${pad(minutes)}:${pad(seconds)}`;
}

export function formatTimeOfDay(unixSeconds: number): string {
  if (!unixSeconds || unixSeconds <= 0) return '--:--:--';
  const date = new Date(unixSeconds * 1000);
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
}

export function readActualSeekableBounds(video: VideoElementRef | null): { start: number; end: number } | null {
  if (!video) {
    return null;
  }
  try {
    if (!video.seekable || video.seekable.length === 0) {
      return null;
    }
    const start = video.seekable.start(0);
    const end = video.seekable.end(video.seekable.length - 1);
    if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) {
      return null;
    }
    return { start, end };
  } catch {
    return null;
  }
}

export interface LiveSeekWindowHint {
  start: number;
  end: number;
  liveEdge: number | null;
  capturedAtMs?: number;
}

export interface UseDvrTimelineControllerProps {
  videoRef: RefObject<VideoElementRef>;
  playbackMode: 'LIVE' | 'VOD' | 'UNKNOWN';
  canSeek: boolean;
  durationSeconds?: number | null;
  startUnix?: number | null;
  anchorStartSec?: number;
  onSeekOffset?: (targetSeconds: number) => void;
  liveSeekWindow?: LiveSeekWindowHint | null;
  userPauseIntentRef: MutableRefObject<boolean>;
  liveEdgeSeekSafetyGapSeconds?: number;
}

export interface UseDvrTimelineControllerResult {
  currentPlaybackTime: number;
  seekableStart: number;
  seekableEnd: number;
  windowDuration: number;
  relativePosition: number;
  canSeekLiveWindow: boolean;
  canRunSeekCommand: boolean;
  hasSeekWindow: boolean;
  hasLiveDvrWindow: boolean;
  isLiveMode: boolean;
  isAtLiveEdge: boolean;
  startTimeDisplay: string;
  endTimeDisplay: string;
  currentTimeDisplay: string;
  behindLiveSeconds: number;
  formatClock: (value: number) => string;
  formatTimeOfDay: (unixSeconds: number) => string;
  seekTo: (targetSeconds: number) => void;
  seekBy: (deltaSeconds: number) => void;
  seekToLiveEdge: () => void;
  seekWhenReady: (target: number) => void;
  readSeekableBounds: (video: VideoElementRef | null) => { start: number; end: number };
  refreshSeekableState: () => void;
  resetDvrTimeline: () => void;
  setCurrentPlaybackTime: Dispatch<SetStateAction<number>>;
  setSeekableStart: Dispatch<SetStateAction<number>>;
  setSeekableEnd: Dispatch<SetStateAction<number>>;
  normalizedLiveSeekWindow: { start: number; end: number; liveEdge: number } | null;
}

export function useDvrTimelineController({
  videoRef,
  playbackMode,
  canSeek,
  durationSeconds,
  startUnix,
  anchorStartSec,
  onSeekOffset,
  liveSeekWindow,
  userPauseIntentRef,
  liveEdgeSeekSafetyGapSeconds = 6,
}: UseDvrTimelineControllerProps): UseDvrTimelineControllerResult {
  const [currentPlaybackTime, setCurrentPlaybackTime] = useState(0);
  const [seekableStart, setSeekableStart] = useState(0);
  const [seekableEnd, setSeekableEnd] = useState(0);
  const [liveWindowClockMs, setLiveWindowClockMs] = useState(() => Date.now());

  useEffect(() => {
    if (playbackMode !== 'LIVE' || !liveSeekWindow) {
      return;
    }
    setLiveWindowClockMs(Date.now());
    const timer = window.setInterval(() => {
      setLiveWindowClockMs(Date.now());
    }, 1000);
    return () => window.clearInterval(timer);
  }, [liveSeekWindow, playbackMode]);

  const normalizedLiveSeekWindow = useMemo(() => {
    if (playbackMode !== 'LIVE' || !liveSeekWindow) {
      return null;
    }
    const capturedAtMs = Number.isFinite(liveSeekWindow.capturedAtMs)
      ? Math.max(0, liveSeekWindow.capturedAtMs as number)
      : liveWindowClockMs;
    const elapsedSeconds = Math.max(0, (liveWindowClockMs - capturedAtMs) / 1000);
    const start = Number.isFinite(liveSeekWindow.start) ? Math.max(0, liveSeekWindow.start + elapsedSeconds) : 0;
    const end = Number.isFinite(liveSeekWindow.end) ? Math.max(start, liveSeekWindow.end + elapsedSeconds) : 0;
    const liveEdge = liveSeekWindow.liveEdge !== null && Number.isFinite(liveSeekWindow.liveEdge)
      ? Math.max(end, liveSeekWindow.liveEdge + elapsedSeconds)
      : end;
    return { start, end, liveEdge };
  }, [liveSeekWindow, liveWindowClockMs, playbackMode]);

  const readSeekableBounds = useCallback((video: VideoElementRef | null): { start: number; end: number } => {
    let start = 0;
    let end = 0;
    if (normalizedLiveSeekWindow) {
      start = normalizedLiveSeekWindow.start;
      end = normalizedLiveSeekWindow.end;
    } else if (playbackMode === 'VOD' && durationSeconds && durationSeconds > 0) {
      end = durationSeconds;
    } else if (video && video.seekable && video.seekable.length > 0) {
      start = video.seekable.start(0);
      end = video.seekable.end(video.seekable.length - 1);
    }
    return { start, end };
  }, [durationSeconds, normalizedLiveSeekWindow, playbackMode]);

  const refreshSeekableState = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;

    const { start, end } = readSeekableBounds(video);
    setSeekableStart(start);
    setSeekableEnd(end);
    setCurrentPlaybackTime(video.currentTime);
  }, [readSeekableBounds, videoRef]);

  const canSeekLiveWindow = playbackMode === 'LIVE' && seekableEnd > seekableStart;
  const canRunSeekCommand = canSeek || canSeekLiveWindow;

  const seekTo = useCallback((targetSeconds: number) => {
    const video = videoRef.current;
    if (!canRunSeekCommand) return;
    if (!video || !Number.isFinite(targetSeconds)) return;

    if (playbackMode === 'VOD') {
      const anchor = anchorStartSec ?? 0;
      const localTarget = targetSeconds - anchor;

      let localBufferedStart = 0;
      let localBufferedEnd = 0;
      try {
        if (video.seekable && video.seekable.length > 0) {
          localBufferedStart = video.seekable.start(0);
          localBufferedEnd = video.seekable.end(video.seekable.length - 1);
        }
      } catch {
        // ignore
      }

      const isWithinLocalWindow = localBufferedEnd > localBufferedStart &&
        localTarget >= localBufferedStart &&
        localTarget <= Math.max(localBufferedStart, localBufferedEnd - 0.5);

      if (isWithinLocalWindow) {
        video.currentTime = Math.max(0, localTarget);
      } else if (onSeekOffset) {
        onSeekOffset(targetSeconds);
        return;
      } else {
        video.currentTime = Math.max(0, localTarget);
      }
    } else {
      let clamped = Math.max(0, targetSeconds);
      if (seekableEnd > seekableStart) {
        clamped = Math.min(Math.max(targetSeconds, seekableStart), seekableEnd);
      }
      video.currentTime = clamped;
    }

    if (!userPauseIntentRef.current && video.paused) {
      const resume = () => {
        video.play().catch((err) => debugWarn('Live seek resume play failed', err));
      };
      if (video.readyState >= 1) {
        resume();
      } else {
        video.addEventListener('loadedmetadata', resume, { once: true });
      }
    }
  }, [anchorStartSec, canRunSeekCommand, onSeekOffset, playbackMode, seekableEnd, seekableStart, userPauseIntentRef, videoRef]);

  const seekBy = useCallback((deltaSeconds: number) => {
    const video = videoRef.current;
    if (!video) return;
    const currentAbs = (anchorStartSec ?? 0) + video.currentTime;
    seekTo(currentAbs + deltaSeconds);
  }, [anchorStartSec, seekTo, videoRef]);

  const seekToLiveEdge = useCallback(() => {
    const video = videoRef.current;
    if (!video || seekableEnd <= seekableStart) return;
    const target = Math.max(seekableStart, seekableEnd - liveEdgeSeekSafetyGapSeconds);
    seekTo(target);
    if (video.paused) {
      video.play().catch((err) => debugWarn('Go-live play failed', err));
    }
  }, [liveEdgeSeekSafetyGapSeconds, seekTo, seekableEnd, seekableStart, videoRef]);

  const seekWhenReady = useCallback((target: number) => {
    const video = videoRef.current;
    if (!video) return;

    const doSeek = () => {
      seekTo(target);
      video.play().catch((err) => debugWarn('Seek play failed', err));
    };

    if (video.readyState >= 1) {
      doSeek();
    } else {
      video.addEventListener('loadedmetadata', doSeek, { once: true });
    }
  }, [seekTo, videoRef]);

  const windowDuration = useMemo(() => {
    if (playbackMode === 'VOD' && durationSeconds && durationSeconds > 0) {
      return durationSeconds;
    }
    return Math.max(0, seekableEnd - seekableStart);
  }, [playbackMode, durationSeconds, seekableEnd, seekableStart]);

  const relativePosition = useMemo(() => {
    if (playbackMode === 'VOD') {
      const anchor = anchorStartSec ?? 0;
      const absolutePos = anchor + Math.max(0, currentPlaybackTime - seekableStart);
      return windowDuration > 0 ? Math.min(windowDuration, Math.max(0, absolutePos)) : absolutePos;
    }
    return Math.min(windowDuration, Math.max(0, currentPlaybackTime - seekableStart));
  }, [playbackMode, anchorStartSec, currentPlaybackTime, seekableStart, windowDuration]);

  const hasLiveDvrWindow = canSeekLiveWindow && windowDuration > 0;
  const seekEnabled = canRunSeekCommand;
  const hasSeekWindow = seekEnabled && windowDuration > 0;
  const isLiveMode = playbackMode === 'LIVE';
  const liveEdgePosition = normalizedLiveSeekWindow?.liveEdge ?? seekableEnd;
  const isAtLiveEdge = hasLiveDvrWindow && Math.abs(liveEdgePosition - currentPlaybackTime) < 2;

  const liveWindowStartPosition = normalizedLiveSeekWindow?.start ?? seekableStart;
  const liveWindowEndPosition = normalizedLiveSeekWindow?.liveEdge ?? seekableEnd;

  const startTimeDisplay = playbackMode === 'LIVE'
    ? startUnix
      ? formatTimeOfDay(startUnix + liveWindowStartPosition)
      : formatClock(liveWindowStartPosition)
    : startUnix
      ? formatTimeOfDay(startUnix)
      : formatClock(0);

  const endTimeDisplay = playbackMode === 'LIVE'
    ? startUnix
      ? formatTimeOfDay(startUnix + liveWindowEndPosition)
      : formatClock(liveWindowEndPosition)
    : startUnix
      ? formatTimeOfDay(startUnix + windowDuration)
      : formatClock(windowDuration);

  const playheadWindowPosition = seekableStart + relativePosition;
  const currentTimeDisplay = playbackMode === 'LIVE'
    ? startUnix
      ? formatTimeOfDay(startUnix + playheadWindowPosition)
      : formatClock(playheadWindowPosition)
    : formatClock(relativePosition);
  const behindLiveSeconds = isLiveMode
    ? Math.max(0, liveEdgePosition - playheadWindowPosition)
    : 0;

  useEffect(() => {
    if (
      typeof navigator === 'undefined' ||
      !('mediaSession' in navigator) ||
      typeof navigator.mediaSession.setPositionState !== 'function'
    ) {
      return;
    }

    const positionDuration = hasSeekWindow
      ? windowDuration
      : playbackMode === 'VOD' && durationSeconds && durationSeconds > 0
        ? durationSeconds
        : 0;
    if (!(positionDuration > 0)) {
      try {
        navigator.mediaSession.setPositionState?.(undefined);
      } catch (err) {
        debugWarn('Media session position reset failed', err);
      }
      return;
    }

    const position = hasSeekWindow ? relativePosition : currentPlaybackTime;
    try {
      navigator.mediaSession.setPositionState({
        duration: positionDuration,
        playbackRate: 1,
        position: Math.min(positionDuration, Math.max(0, position)),
      });
    } catch (err) {
      debugWarn('Media session position update failed', err);
    }
  }, [
    currentPlaybackTime,
    durationSeconds,
    hasSeekWindow,
    playbackMode,
    relativePosition,
    windowDuration,
  ]);

  const resetDvrTimeline = useCallback(() => {
    setSeekableStart(0);
    setSeekableEnd(0);
    setCurrentPlaybackTime(0);
  }, []);

  return {
    currentPlaybackTime,
    seekableStart,
    seekableEnd,
    windowDuration,
    relativePosition,
    canSeekLiveWindow,
    canRunSeekCommand,
    hasSeekWindow,
    hasLiveDvrWindow,
    isLiveMode,
    isAtLiveEdge,
    startTimeDisplay,
    endTimeDisplay,
    currentTimeDisplay,
    behindLiveSeconds,
    formatClock,
    formatTimeOfDay,
    seekTo,
    seekBy,
    seekToLiveEdge,
    seekWhenReady,
    readSeekableBounds,
    refreshSeekableState,
    resetDvrTimeline,
    setCurrentPlaybackTime,
    setSeekableStart,
    setSeekableEnd,
    normalizedLiveSeekWindow,
  };
}
