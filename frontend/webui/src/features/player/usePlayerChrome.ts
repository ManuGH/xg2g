import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { Dispatch, MutableRefObject, RefObject, SetStateAction } from 'react';
import type { HlsInstanceRef, PlayerStats, PlayerStatus, VideoElementRef } from '../../types/v3-player';
import { debugLog, debugWarn } from '../../utils/logging';
import { hasTouchInput } from './utils/playerHelpers';

type PlaybackMode = 'LIVE' | 'VOD' | 'UNKNOWN';
type ForceNativeFn = (videoEl?: VideoElementRef) => boolean;
type DesktopFullscreenFn = (videoEl?: VideoElementRef) => boolean;

interface UsePlayerChromeProps {
  autoStart?: boolean;
  containerRef: RefObject<HTMLDivElement | null>;
  videoRef: RefObject<VideoElementRef>;
  hlsRef: MutableRefObject<HlsInstanceRef>;
  userPauseIntentRef: MutableRefObject<boolean>;
  lastDecodedRef: MutableRefObject<number>;
  playbackMode: PlaybackMode;
  durationSeconds: number | null;
  canSeek: boolean;
  startUnix: number | null;
  setStatus: Dispatch<SetStateAction<PlayerStatus>>;
  allowNativeFullscreen: boolean;
  shouldForceNativeMobileHls: ForceNativeFn;
  canUseDesktopWebKitFullscreen: DesktopFullscreenFn;
}

interface PlayerChromeController {
  showStats: boolean;
  currentPlaybackTime: number;
  seekableStart: number;
  seekableEnd: number;
  isPip: boolean;
  canTogglePiP: boolean;
  isFullscreen: boolean;
  canToggleFullscreen: boolean;
  isPlaying: boolean;
  isIdle: boolean;
  volume: number;
  isMuted: boolean;
  canToggleMute: boolean;
  canAdjustVolume: boolean;
  stats: PlayerStats;
  setStats: Dispatch<SetStateAction<PlayerStats>>;
  windowDuration: number;
  relativePosition: number;
  hasSeekWindow: boolean;
  isLiveMode: boolean;
  isAtLiveEdge: boolean;
  showDvrModeButton: boolean;
  startTimeDisplay: string;
  endTimeDisplay: string;
  formatClock: (value: number) => string;
  seekTo: (targetSeconds: number) => void;
  seekBy: (deltaSeconds: number) => void;
  seekWhenReady: (target: number) => void;
  togglePlayPause: () => void;
  toggleFullscreen: () => Promise<void>;
  enterDVRMode: () => void;
  togglePiP: () => Promise<void>;
  toggleMute: () => void;
  handleVolumeChange: (newVolume: number) => void;
  applyAutoplayMute: () => void;
  toggleStats: () => void;
  resetChromeState: () => void;
}

const initialStats: PlayerStats = {
  bandwidth: 0,
  resolution: '-',
  fps: 0,
  droppedFrames: 0,
  buffer: 0,
  bufferHealth: 0,
  latency: null,
  levelIndex: -1
};

export function usePlayerChrome({
  autoStart,
  containerRef,
  videoRef,
  hlsRef,
  userPauseIntentRef,
  lastDecodedRef,
  playbackMode,
  durationSeconds,
  canSeek,
  startUnix,
  setStatus,
  allowNativeFullscreen,
  shouldForceNativeMobileHls,
  canUseDesktopWebKitFullscreen
}: UsePlayerChromeProps): PlayerChromeController {
  const [showStats, setShowStats] = useState(false);
  const [currentPlaybackTime, setCurrentPlaybackTime] = useState(0);
  const [seekableStart, setSeekableStart] = useState(0);
  const [seekableEnd, setSeekableEnd] = useState(0);
  const [isPip, setIsPip] = useState(false);
  const [canTogglePiP, setCanTogglePiP] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [canToggleFullscreen, setCanToggleFullscreen] = useState(false);
  const [isPlaying, setIsPlaying] = useState(false);
  const [isIdle, setIsIdle] = useState(false);
  const [volume, setVolume] = useState(1);
  const [isMuted, setIsMuted] = useState(false);
  const [canToggleMute, setCanToggleMute] = useState(true);
  const [canAdjustVolume, setCanAdjustVolume] = useState(true);
  const [stats, setStats] = useState<PlayerStats>(initialStats);
  const lastNonZeroVolumeRef = useRef<number>(1);
  const idleTimerRef = useRef<number | null>(null);
  const isTouchDevice = useMemo(() => hasTouchInput(), []);

  const formatClock = useCallback((value: number): string => {
    if (!Number.isFinite(value) || value < 0) return '--:--';
    const totalSeconds = Math.floor(value);
    const hours = Math.floor(totalSeconds / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);
    const seconds = totalSeconds % 60;
    const pad = (n: number) => n.toString().padStart(2, '0');
    return hours > 0 ? `${hours}:${pad(minutes)}:${pad(seconds)}` : `${pad(minutes)}:${pad(seconds)}`;
  }, []);

  const formatTimeOfDay = useCallback((unixSeconds: number): string => {
    if (!unixSeconds || unixSeconds <= 0) return '--:--:--';
    const date = new Date(unixSeconds * 1000);
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
  }, []);

  const refreshSeekableState = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;

    let start = 0;
    let end = 0;
    if (playbackMode === 'VOD' && durationSeconds && durationSeconds > 0) {
      end = durationSeconds;
    } else if (video.seekable && video.seekable.length > 0) {
      start = video.seekable.start(0);
      end = video.seekable.end(video.seekable.length - 1);
    } else if (durationSeconds && durationSeconds > 0) {
      end = durationSeconds;
    }

    setSeekableStart(start);
    setSeekableEnd(end);
    setCurrentPlaybackTime(video.currentTime);
  }, [durationSeconds, playbackMode, videoRef]);

  const seekTo = useCallback((targetSeconds: number) => {
    const video = videoRef.current;
    if (!video || !Number.isFinite(targetSeconds)) return;

    let clamped = Math.max(0, targetSeconds);
    if (seekableEnd > seekableStart) {
      clamped = Math.min(Math.max(targetSeconds, seekableStart), seekableEnd);
    }
    video.currentTime = clamped;
  }, [seekableEnd, seekableStart, videoRef]);

  const seekBy = useCallback((deltaSeconds: number) => {
    const video = videoRef.current;
    if (!video) return;
    seekTo(video.currentTime + deltaSeconds);
  }, [seekTo, videoRef]);

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

  const togglePlayPause = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;

    if (video.paused) {
      userPauseIntentRef.current = false;
      video.play().catch((err) => debugWarn('Play failed', err));
      return;
    }

    userPauseIntentRef.current = true;
    video.pause();
  }, [userPauseIntentRef, videoRef]);

  const toggleFullscreen = useCallback(async () => {
    const video = videoRef.current;
    const container = containerRef.current;

    if (!document.fullscreenElement) {
      if (allowNativeFullscreen && video && canUseDesktopWebKitFullscreen(video)) {
        try {
          video.controls = true;
          video.webkitEnterFullscreen?.();
          return;
        } catch (err) {
          debugWarn('WebKit fullscreen failed', err);
        }
      }

      if (container?.requestFullscreen) {
        try {
          await container.requestFullscreen();
          return;
        } catch (err) {
          debugWarn('Container fullscreen failed', err);
        }
      }

      if (allowNativeFullscreen && video?.webkitEnterFullscreen) {
        video.controls = true;
        video.webkitEnterFullscreen();
        return;
      }

      try {
        await container?.requestFullscreen?.();
      } catch (err) {
        debugWarn('Fullscreen failed', err);
      }
      return;
    }

    await document.exitFullscreen();
  }, [allowNativeFullscreen, canUseDesktopWebKitFullscreen, containerRef, videoRef]);

  const enterDVRMode = useCallback(() => {
    const video = videoRef.current;
    if (allowNativeFullscreen && video && video.webkitEnterFullscreen && shouldForceNativeMobileHls(video)) {
      video.controls = true;
      video.webkitEnterFullscreen();
      return;
    }
    void toggleFullscreen();
  }, [allowNativeFullscreen, shouldForceNativeMobileHls, toggleFullscreen, videoRef]);

  const togglePiP = useCallback(async () => {
    const video = videoRef.current;
    if (!video || !document.pictureInPictureEnabled || typeof video.requestPictureInPicture !== 'function') return;
    try {
      if (document.pictureInPictureElement) {
        await document.exitPictureInPicture();
        setIsPip(false);
      } else {
        await video.requestPictureInPicture();
        setIsPip(true);
      }
    } catch (err) {
      debugWarn('PiP failed', err);
    }
  }, [videoRef]);

  const toggleMute = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;

    if (!video.muted) {
      if (video.volume > 0) {
        lastNonZeroVolumeRef.current = video.volume;
      }
      video.muted = true;
      setIsMuted(true);
      return;
    }

    const restoreVolume = lastNonZeroVolumeRef.current > 0 ? lastNonZeroVolumeRef.current : video.volume;
    if (restoreVolume > 0 && video.volume !== restoreVolume) {
      video.volume = restoreVolume;
      setVolume(restoreVolume);
    }
    video.muted = false;
    setIsMuted(false);
  }, [videoRef]);

  const handleVolumeChange = useCallback((newVolume: number) => {
    const video = videoRef.current;
    if (!video) return;
    video.volume = newVolume;
    setVolume(newVolume);
    if (newVolume > 0) {
      lastNonZeroVolumeRef.current = newVolume;
    }
    const shouldMute = newVolume === 0;
    video.muted = shouldMute;
    setIsMuted(shouldMute);
  }, [videoRef]);

  const applyAutoplayMute = useCallback(() => {
    if (!autoStart) return;
    const video = videoRef.current;
    if (!video) return;
    video.muted = true;
    setIsMuted(true);
  }, [autoStart, videoRef]);

  const toggleStats = useCallback(() => {
    setShowStats((prev) => !prev);
  }, []);

  const resetChromeState = useCallback(() => {
    setSeekableStart(0);
    setSeekableEnd(0);
    setCurrentPlaybackTime(0);
  }, []);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      const isInput = ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName) || target.isContentEditable;
      if (isInput) return;

      switch (e.key.toLowerCase()) {
        case 'f':
          void toggleFullscreen();
          break;
        case 'm':
          e.preventDefault();
          toggleMute();
          break;
        case ' ':
        case 'k':
          e.preventDefault();
          togglePlayPause();
          break;
        case 'i':
          toggleStats();
          break;
        case 'p':
          void togglePiP();
          break;
        case 'arrowleft':
          seekBy(-15);
          break;
        case 'arrowright':
          seekBy(15);
          break;
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [seekBy, toggleFullscreen, toggleMute, togglePiP, togglePlayPause, toggleStats]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    const handleTimeUpdate = () => refreshSeekableState();

    video.addEventListener('timeupdate', handleTimeUpdate);
    video.addEventListener('loadedmetadata', handleTimeUpdate);
    video.addEventListener('durationchange', handleTimeUpdate);
    video.addEventListener('progress', handleTimeUpdate);
    video.addEventListener('seeking', handleTimeUpdate);

    refreshSeekableState();

    return () => {
      video.removeEventListener('timeupdate', handleTimeUpdate);
      video.removeEventListener('loadedmetadata', handleTimeUpdate);
      video.removeEventListener('durationchange', handleTimeUpdate);
      video.removeEventListener('progress', handleTimeUpdate);
      video.removeEventListener('seeking', handleTimeUpdate);
    };
  }, [refreshSeekableState, videoRef]);

  useEffect(() => {
    if (!showStats) return;

    const interval = window.setInterval(() => {
      const video = videoRef.current;
      if (!video) return;

      let dropped = 0;
      let decoded = lastDecodedRef.current;

      interface WebkitVideoElement extends HTMLVideoElement {
        webkitDroppedFrameCount?: number;
        webkitDecodedFrameCount?: number;
      }

      if (video.getVideoPlaybackQuality) {
        const quality = video.getVideoPlaybackQuality();
        dropped = quality.droppedVideoFrames;
        decoded = quality.totalVideoFrames;
      } else if ('webkitDroppedFrameCount' in video) {
        dropped = (video as WebkitVideoElement).webkitDroppedFrameCount || 0;
        decoded = (video as WebkitVideoElement).webkitDecodedFrameCount || lastDecodedRef.current;
      }

      const currentFps = Math.max(0, decoded - lastDecodedRef.current);
      lastDecodedRef.current = decoded;

      let bufferHealth = 0;
      if (video.buffered.length > 0) {
        for (let i = 0; i < video.buffered.length; i++) {
          const start = video.buffered.start(i);
          const end = video.buffered.end(i);
          if (video.currentTime >= start && video.currentTime <= end) {
            bufferHealth = end - video.currentTime;
            break;
          }
        }
        if (bufferHealth === 0 && video.buffered.length > 0) {
          const lastEnd = video.buffered.end(video.buffered.length - 1);
          if (lastEnd > video.currentTime) {
            bufferHealth = lastEnd - video.currentTime;
          }
        }
      }
      bufferHealth = Math.max(0, bufferHealth);

      let latency: number | null = null;
      const isLive = playbackMode === 'LIVE';

      if (isLive && hlsRef.current) {
        if (hlsRef.current.latency !== undefined && hlsRef.current.latency !== null) {
          latency = hlsRef.current.latency;
        } else if (hlsRef.current.liveSyncPosition) {
          latency = hlsRef.current.liveSyncPosition - video.currentTime;
        }
        if (latency !== null) latency = Math.max(0, latency);
      }

      setStats((prev) => {
        let resolution = prev.resolution;
        let fps = prev.fps;
        let bandwidth = prev.bandwidth;

        if (video.videoWidth && video.videoHeight) {
          const videoResolution = `${video.videoWidth}x${video.videoHeight}`;
          if (prev.resolution === '-' || prev.resolution === 'Original (Direct)' || prev.resolution !== videoResolution) {
            resolution = videoResolution;
          }
        }

        if (!hlsRef.current && video.src) {
          fps = currentFps;
        } else if (hlsRef.current) {
          if (currentFps > 0) {
            fps = currentFps;
          } else if (prev.fps === 0 && hlsRef.current.levels && hlsRef.current.currentLevel >= 0) {
            const level = hlsRef.current.levels[hlsRef.current.currentLevel];
            if (level && level.frameRate) fps = level.frameRate;
          }

          if (bandwidth === 0 && hlsRef.current.levels) {
            const idx = hlsRef.current.currentLevel === -1 ? 0 : hlsRef.current.currentLevel;
            const level = hlsRef.current.levels[idx];
            if (level && level.bitrate) {
              bandwidth = Math.round(level.bitrate / 1024);
              if (resolution === '-') {
                resolution = level.width ? `${level.width}x${level.height}` : '-';
              }
            }
          }
        }

        return {
          ...prev,
          resolution,
          fps,
          bandwidth,
          droppedFrames: dropped,
          bufferHealth: parseFloat(bufferHealth.toFixed(1)),
          latency: latency !== null ? parseFloat(latency.toFixed(2)) : null
        };
      });

      setStatus((prev) => {
        if (video.readyState >= 3 && !video.paused && (prev === 'buffering' || prev === 'starting' || prev === 'priming')) {
          debugLog(`[V3Player] Monitor: readyState=${video.readyState}, forcing PLAYING`);
          return 'playing';
        }
        if (video.readyState >= 3 && video.paused && (prev === 'buffering' || prev === 'starting')) {
          debugLog(`[V3Player] Monitor: readyState=${video.readyState} (paused), forcing READY`);
          return 'ready';
        }
        return prev;
      });
    }, 1000);

    return () => window.clearInterval(interval);
  }, [hlsRef, lastDecodedRef, playbackMode, setStatus, showStats, videoRef]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    const handlePlay = () => setIsPlaying(true);
    const handlePause = () => setIsPlaying(false);

    video.addEventListener('play', handlePlay);
    video.addEventListener('pause', handlePause);
    setIsPlaying(!video.paused);

    return () => {
      video.removeEventListener('play', handlePlay);
      video.removeEventListener('pause', handlePause);
    };
  }, [videoRef]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    setVolume(video.volume);
    setIsMuted(video.muted);
    if (video.volume > 0) {
      lastNonZeroVolumeRef.current = video.volume;
    }
  }, [videoRef]);

  useEffect(() => {
    const video = videoRef.current;
    const container = containerRef.current;
    const nativeMobileHls = allowNativeFullscreen && shouldForceNativeMobileHls(video);
    const pipAvailable =
      typeof document !== 'undefined' &&
      !!document.pictureInPictureEnabled &&
      !!video &&
      typeof video.requestPictureInPicture === 'function';
    const fullscreenAvailable =
      (allowNativeFullscreen && !!video?.webkitEnterFullscreen) ||
      !!container?.requestFullscreen ||
      (typeof document !== 'undefined' && document.fullscreenEnabled === true);
    // Native mobile WebKit uses the device buttons for loudness; keep mute
    // available but hide the ineffective browser volume slider there.
    const volumeAvailable = !nativeMobileHls;

    setCanTogglePiP(pipAvailable);
    setCanToggleFullscreen(fullscreenAvailable);
    setCanToggleMute(!!video);
    setCanAdjustVolume(volumeAvailable);
  }, [allowNativeFullscreen, containerRef, shouldForceNativeMobileHls, videoRef]);

  useEffect(() => {
    const onFsChange = () => setIsFullscreen(!!document.fullscreenElement);
    const onPipChange = () => setIsPip(!!document.pictureInPictureElement);

    const video = videoRef.current;
    const supportsWebkitFullscreen = allowNativeFullscreen && !!video?.webkitEnterFullscreen;

    const onWebkitBeginFullscreen = () => {
      setIsFullscreen(true);
    };
    const onWebkitEndFullscreen = () => {
      setIsFullscreen(false);
      if (video) {
        video.controls = false;
      }
    };

    document.addEventListener('fullscreenchange', onFsChange);
    if (video) {
      video.addEventListener('enterpictureinpicture', onPipChange);
      video.addEventListener('leavepictureinpicture', onPipChange);

      if (supportsWebkitFullscreen) {
        video.addEventListener('webkitbeginfullscreen', onWebkitBeginFullscreen);
        video.addEventListener('webkitendfullscreen', onWebkitEndFullscreen);
      }
    }

    return () => {
      document.removeEventListener('fullscreenchange', onFsChange);
      if (video) {
        video.removeEventListener('enterpictureinpicture', onPipChange);
        video.removeEventListener('leavepictureinpicture', onPipChange);

        if (supportsWebkitFullscreen) {
          video.removeEventListener('webkitbeginfullscreen', onWebkitBeginFullscreen);
          video.removeEventListener('webkitendfullscreen', onWebkitEndFullscreen);
        }
      }
    };
  }, [allowNativeFullscreen, videoRef]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    if (isTouchDevice) {
      setIsIdle(false);
      return;
    }

    const resetIdle = () => {
      setIsIdle(false);
      if (idleTimerRef.current) window.clearTimeout(idleTimerRef.current);
      idleTimerRef.current = window.setTimeout(() => setIsIdle(true), 3000);
    };

    resetIdle();

    const onMove = () => resetIdle();
    const onClick = () => resetIdle();
    const onKey = () => resetIdle();

    container.addEventListener('mousemove', onMove);
    container.addEventListener('click', onClick);
    container.addEventListener('keydown', onKey);
    container.addEventListener('touchstart', onClick);

    return () => {
      if (idleTimerRef.current) window.clearTimeout(idleTimerRef.current);
      container.removeEventListener('mousemove', onMove);
      container.removeEventListener('click', onClick);
      container.removeEventListener('keydown', onKey);
      container.removeEventListener('touchstart', onClick);
    };
  }, [containerRef, isTouchDevice]);

  const windowDuration = useMemo(() => Math.max(0, seekableEnd - seekableStart), [seekableEnd, seekableStart]);
  const relativePosition = useMemo(
    () => Math.min(windowDuration, Math.max(0, currentPlaybackTime - seekableStart)),
    [currentPlaybackTime, seekableStart, windowDuration]
  );
  const hasSeekWindow = canSeek && windowDuration > 0;
  const isLiveMode = playbackMode === 'LIVE';
  const isAtLiveEdge = isLiveMode && windowDuration > 0 && Math.abs(seekableEnd - currentPlaybackTime) < 2;
  const showDvrModeButton = allowNativeFullscreen && shouldForceNativeMobileHls(videoRef.current);

  const startTimeDisplay = startUnix
    ? formatTimeOfDay(startUnix + relativePosition)
    : formatClock(relativePosition);

  const endTimeDisplay = startUnix
    ? formatTimeOfDay(startUnix + windowDuration)
    : formatClock(windowDuration);

  return {
    showStats,
    currentPlaybackTime,
    seekableStart,
    seekableEnd,
    isPip,
    canTogglePiP,
    isFullscreen,
    canToggleFullscreen,
    isPlaying,
    isIdle,
    volume,
    isMuted,
    canToggleMute,
    canAdjustVolume,
    stats,
    setStats,
    windowDuration,
    relativePosition,
    hasSeekWindow,
    isLiveMode,
    isAtLiveEdge,
    showDvrModeButton,
    startTimeDisplay,
    endTimeDisplay,
    formatClock,
    seekTo,
    seekBy,
    seekWhenReady,
    togglePlayPause,
    toggleFullscreen,
    enterDVRMode,
    togglePiP,
    toggleMute,
    handleVolumeChange,
    applyAutoplayMute,
    toggleStats,
    resetChromeState
  };
}
