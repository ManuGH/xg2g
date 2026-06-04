import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { Dispatch, MutableRefObject, RefObject, SetStateAction } from 'react';
import type { HlsInstanceRef, PlayerStats, PlayerStatus, SafariVideoElement, VideoElementRef } from '../../types/v3-player';
import { debugLog, debugWarn } from '../../utils/logging';
import { onHostMediaKey } from '../../lib/hostBridge';
import { hasTouchInput } from './utils/playerHelpers';

type PlaybackMode = 'LIVE' | 'VOD' | 'UNKNOWN';
type ForceNativeFn = (videoEl?: VideoElementRef) => boolean;
type DesktopFullscreenFn = (videoEl?: VideoElementRef) => boolean;

interface LiveSeekWindowHint {
  start: number;
  end: number;
  liveEdge: number | null;
  capturedAtMs?: number;
}

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
  liveSeekWindow?: LiveSeekWindowHint | null;
  setStatus: Dispatch<SetStateAction<PlayerStatus>>;
  allowNativeFullscreen: boolean;
  shouldForceNativeMobileHls: ForceNativeFn;
  canUseDesktopWebKitFullscreen: DesktopFullscreenFn;
  onNativeFullscreenExit?: (details: { currentTime: number | null; wasPaused: boolean }) => void;
  mediaTitle?: string | null;
  mediaSubtitle?: string | null;
  mediaArtworkUrl?: string | null;
}

interface PlayerChromeController {
  showStats: boolean;
  currentPlaybackTime: number;
  seekableStart: number;
  seekableEnd: number;
  supportsNativeFullscreen: boolean;
  canEnterNativeFullscreen: boolean;
  prefersDesktopNativeFullscreen: boolean;
  nativeFullscreenPending: boolean;
  isWebKitFullscreenActive: boolean;
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
  hasLiveDvrWindow: boolean;
  isLiveMode: boolean;
  isAtLiveEdge: boolean;
  showDvrModeButton: boolean;
  startTimeDisplay: string;
  endTimeDisplay: string;
  currentTimeDisplay: string;
  behindLiveSeconds: number;
  formatClock: (value: number) => string;
  seekTo: (targetSeconds: number) => void;
  seekToLiveEdge: () => void;
  seekBy: (deltaSeconds: number) => void;
  seekWhenReady: (target: number) => void;
  togglePlayPause: () => void;
  toggleFullscreen: () => Promise<void>;
  enterNativeFullscreen: () => boolean;
  primeNativeFullscreen: () => boolean;
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

const touchLiveDvrDefaultOffsetSeconds = 18;

// Seconds behind the live edge that the "LIVE" button targets. Seeking to the
// exact seekableEnd lands on the newest, not-yet-decodable boundary: Safari
// stalls there and currentTime stops advancing, which also blocks the
// timeupdate/watchdog reveal -> permanent black (device-confirmed 2026-06-01:
// "Bild schwarz wenn man auf Live klickt"). Landing a few seconds back puts the
// playhead inside already-buffered, decodable data.
const liveEdgeSeekSafetyGapSeconds = 6;

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
  liveSeekWindow,
  setStatus,
  allowNativeFullscreen,
  shouldForceNativeMobileHls,
  canUseDesktopWebKitFullscreen,
  onNativeFullscreenExit,
  mediaTitle,
  mediaSubtitle,
  mediaArtworkUrl,
}: UsePlayerChromeProps): PlayerChromeController {
  const [showStats, setShowStats] = useState(false);
  const [currentPlaybackTime, setCurrentPlaybackTime] = useState(0);
  const [seekableStart, setSeekableStart] = useState(0);
  const [seekableEnd, setSeekableEnd] = useState(0);
  const [isWebKitFullscreenActive, setIsWebKitFullscreenActive] = useState(false);
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
  const [liveWindowClockMs, setLiveWindowClockMs] = useState(() => Date.now());
  const [nativeFullscreenPending, setNativeFullscreenPending] = useState(false);
  const lastNonZeroVolumeRef = useRef<number>(1);
  const idleTimerRef = useRef<number | null>(null);
  const pendingNativeFullscreenRef = useRef(false);
  const appliedTouchDvrDefaultRef = useRef(false);
  const isTouchDevice = useMemo(() => hasTouchInput(), []);
  const idleDelayMs = isTouchDevice ? 2400 : 3000;

  const shouldUseTouchWebKitFullscreen = useCallback((videoEl?: VideoElementRef) => {
    if (!videoEl?.webkitEnterFullscreen) return false;
    return shouldForceNativeMobileHls(videoEl);
  }, [shouldForceNativeMobileHls]);

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
    if (end <= start) {
      return null;
    }
    return { start, end, liveEdge };
  }, [liveSeekWindow, liveWindowClockMs, playbackMode]);

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

  const readSeekableBounds = useCallback((video: SafariVideoElement) => {
    let start = 0;
    let end = 0;
    if (normalizedLiveSeekWindow) {
      start = normalizedLiveSeekWindow.start;
      end = normalizedLiveSeekWindow.end;
    } else if (playbackMode === 'VOD' && durationSeconds && durationSeconds > 0) {
      end = durationSeconds;
    } else if (video.seekable && video.seekable.length > 0) {
      start = video.seekable.start(0);
      end = video.seekable.end(video.seekable.length - 1);
    } else if (durationSeconds && durationSeconds > 0) {
      end = durationSeconds;
    }
    return { start, end };
  }, [durationSeconds, normalizedLiveSeekWindow, playbackMode]);

  const readActualSeekableBounds = useCallback((video: SafariVideoElement) => {
    try {
      if (!video.seekable || video.seekable.length <= 0) {
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
  }, []);

  const logNativeFullscreenProbe = useCallback((reason: string, video: SafariVideoElement) => {
    const { start, end } = readSeekableBounds(video);
    debugLog('[V3Player] Native fullscreen probe', {
      reason,
      playbackMode,
      canSeek,
      allowNativeFullscreen,
      supportsNativeFullscreen: typeof video.webkitEnterFullscreen === 'function',
      desktopWebKitEligible: canUseDesktopWebKitFullscreen(video),
      webkitDisplayingFullscreen: video.webkitDisplayingFullscreen === true,
      readyState: video.readyState,
      paused: video.paused,
      controls: video.controls,
      currentTime: video.currentTime,
      duration: Number.isFinite(video.duration) ? video.duration : null,
      seekableStart: start,
      seekableEnd: end,
      seekableWindow: Math.max(0, end - start),
      videoWidth: video.videoWidth || 0,
      videoHeight: video.videoHeight || 0,
    });
  }, [allowNativeFullscreen, canSeek, canUseDesktopWebKitFullscreen, playbackMode, readSeekableBounds]);

  const canEnterNativeFullscreenNow = useCallback((video: SafariVideoElement) => (
    video.readyState >= 1 ||
    (video.videoWidth > 0 && video.videoHeight > 0)
  ), []);

  const canEnterTouchNativeFullscreenNow = useCallback((video: SafariVideoElement) => {
    if (!canEnterNativeFullscreenNow(video)) {
      return false;
    }
    if (playbackMode !== 'LIVE') {
      return true;
    }

    const actualWindow = readActualSeekableBounds(video);
    return !!actualWindow && actualWindow.end - actualWindow.start >= 8;
  }, [canEnterNativeFullscreenNow, playbackMode, readActualSeekableBounds]);

  const requiresVerifiedDesktopLiveWindow = useCallback(() => (
    playbackMode === 'LIVE' &&
    !!normalizedLiveSeekWindow &&
    normalizedLiveSeekWindow.end - normalizedLiveSeekWindow.start >= 8
  ), [normalizedLiveSeekWindow, playbackMode]);

  const canEnterDesktopNativeFullscreenNow = useCallback((video: SafariVideoElement) => {
    if (!canEnterNativeFullscreenNow(video)) {
      return false;
    }
    if (!requiresVerifiedDesktopLiveWindow()) {
      return true;
    }

    const actualWindow = readActualSeekableBounds(video);
    return !!actualWindow && actualWindow.end - actualWindow.start >= 8;
  }, [canEnterNativeFullscreenNow, readActualSeekableBounds, requiresVerifiedDesktopLiveWindow]);

  const flushPendingNativeFullscreen = useCallback((reason: string) => {
    const video = videoRef.current;
    if (!pendingNativeFullscreenRef.current || !video?.webkitEnterFullscreen) {
      return false;
    }
    const useTouchFullscreen = shouldUseTouchWebKitFullscreen(video);
    const useDesktopFullscreen = !useTouchFullscreen && canUseDesktopWebKitFullscreen(video);
    if (!useTouchFullscreen && !useDesktopFullscreen) {
      pendingNativeFullscreenRef.current = false;
      setNativeFullscreenPending(false);
      return false;
    }
    const canEnterNow = useTouchFullscreen
      ? canEnterTouchNativeFullscreenNow(video)
      : canEnterDesktopNativeFullscreenNow(video);
    if (!canEnterNow) {
      return false;
    }

    try {
      logNativeFullscreenProbe(reason, video);
      video.controls = true;
      video.webkitEnterFullscreen();
      pendingNativeFullscreenRef.current = false;
      setNativeFullscreenPending(false);
      return true;
    } catch (err) {
      debugWarn('Pending WebKit fullscreen failed', err);
      return false;
    }
  }, [
    canEnterDesktopNativeFullscreenNow,
    canEnterTouchNativeFullscreenNow,
    canUseDesktopWebKitFullscreen,
    logNativeFullscreenProbe,
    shouldUseTouchWebKitFullscreen,
    videoRef,
  ]);

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

    let clamped = Math.max(0, targetSeconds);
    if (seekableEnd > seekableStart) {
      clamped = Math.min(Math.max(targetSeconds, seekableStart), seekableEnd);
    }
    video.currentTime = clamped;

    // Live/DVR seeks land on un-buffered (transcoded) or evicted data; Safari
    // leaves the element PAUSED after such a seek, so the picture freezes/blacks
    // and never resumes until a manual Play. Re-assert playback intent unless
    // the user deliberately paused. Mirrors seekWhenReady's readyState gate; the
    // `video.paused` guard makes this a no-op on in-buffer seeks that keep
    // playing, so it never fights the shipped isInMemorySeekTarget path.
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
  }, [canRunSeekCommand, seekableEnd, seekableStart, userPauseIntentRef, videoRef]);

  const seekBy = useCallback((deltaSeconds: number) => {
    const video = videoRef.current;
    if (!video) return;
    seekTo(video.currentTime + deltaSeconds);
  }, [seekTo, videoRef]);

  // "Go LIVE": never seek to the exact edge (stalls -> black). Target a safe
  // margin behind it, clamped into the seekable window, and resume playback.
  const seekToLiveEdge = useCallback(() => {
    const video = videoRef.current;
    if (!video || seekableEnd <= seekableStart) return;
    const target = Math.max(seekableStart, seekableEnd - liveEdgeSeekSafetyGapSeconds);
    seekTo(target);
    if (video.paused) {
      video.play().catch((err) => debugWarn('Go-live play failed', err));
    }
  }, [seekTo, seekableEnd, seekableStart, videoRef]);

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

  const clearAutoplayMuteIfNeeded = useCallback(() => {
    const video = videoRef.current;
    if (!video || !shouldForceNativeMobileHls(video) || !video.muted) {
      return;
    }
    video.muted = false;
    setIsMuted(false);
  }, [shouldForceNativeMobileHls, videoRef]);

  const play = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    if (!video.paused) return;

    clearAutoplayMuteIfNeeded();
    userPauseIntentRef.current = false;
    setStatus((current) => (current === 'paused' || current === 'ready' ? 'buffering' : current));
    video.play().catch((err) => debugWarn('Play failed', err));
  }, [clearAutoplayMuteIfNeeded, setStatus, userPauseIntentRef, videoRef]);

  const pause = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    if (video.paused) {
      setStatus('paused');
      return;
    }

    userPauseIntentRef.current = true;
    video.pause();
    setStatus('paused');
  }, [setStatus, userPauseIntentRef, videoRef]);

  const stop = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;

    userPauseIntentRef.current = true;
    video.pause();
    setStatus('paused');
  }, [setStatus, userPauseIntentRef, videoRef]);

  const togglePlayPause = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;

    if (video.paused) {
      play();
      return;
    }

    pause();
  }, [pause, play, videoRef]);

  const toggleFullscreen = useCallback(async () => {
    const video = videoRef.current;
    const container = containerRef.current;
    const useTouchWebKitFullscreen = shouldUseTouchWebKitFullscreen(video);
    const fullscreenElement = typeof document !== 'undefined' ? document.fullscreenElement : null;
    const ownsFullscreen = !!fullscreenElement && (
      fullscreenElement === document.documentElement ||
      fullscreenElement === container ||
      fullscreenElement === video
    );

    const requestWebKitFullscreen = (reason: string) => {
      if (!video?.webkitEnterFullscreen) {
        return false;
      }

      try {
        logNativeFullscreenProbe(reason, video);
        video.controls = true;
        video.webkitEnterFullscreen();
        return true;
      } catch (err) {
        debugWarn('WebKit fullscreen failed', err);
        return false;
      }
    };

    if (video?.webkitDisplayingFullscreen) {
      try {
        video.webkitExitFullscreen?.();
        return;
      } catch (err) {
        debugWarn('WebKit fullscreen exit failed', err);
      }
    }

    if (ownsFullscreen) {
      try {
        await document.exitFullscreen();
        return;
      } catch (err) {
        debugWarn('Fullscreen exit failed', err);
      }
    }

    if (fullscreenElement && !ownsFullscreen) {
      try {
        await document.exitFullscreen();
      } catch (err) {
        debugWarn('Fullscreen handoff exit failed', err);
      }
    }

    if (video && useTouchWebKitFullscreen) {
      pendingNativeFullscreenRef.current = true;
      if (!canEnterTouchNativeFullscreenNow(video)) {
        return;
      }
      if (requestWebKitFullscreen('touch-webkit-request')) {
        pendingNativeFullscreenRef.current = false;
        return;
      }
      return;
    }

    if (container?.requestFullscreen) {
      try {
        await container.requestFullscreen();
        return;
      } catch (err) {
        debugWarn('Container fullscreen failed', err);
      }
    }

    if (allowNativeFullscreen && requestWebKitFullscreen('webkit-request')) {
      return;
    }

    try {
      await container?.requestFullscreen?.();
    } catch (err) {
      debugWarn('Fullscreen failed', err);
    }
  }, [allowNativeFullscreen, canEnterTouchNativeFullscreenNow, containerRef, logNativeFullscreenProbe, shouldUseTouchWebKitFullscreen, videoRef]);

  const enterNativeFullscreen = useCallback((): boolean => {
    const video = videoRef.current;
    if (!allowNativeFullscreen || !video?.webkitEnterFullscreen || !canUseDesktopWebKitFullscreen(video)) {
      return false;
    }

    pendingNativeFullscreenRef.current = true;
    setNativeFullscreenPending(true);
    if (!canEnterDesktopNativeFullscreenNow(video)) {
      return true;
    }

    try {
      logNativeFullscreenProbe('explicit-native-request', video);
      video.controls = true;
      video.webkitEnterFullscreen();
      pendingNativeFullscreenRef.current = false;
      setNativeFullscreenPending(false);
      return true;
    } catch (err) {
      debugWarn('Explicit native fullscreen failed', err);
      return false;
    }
  }, [
    allowNativeFullscreen,
    canEnterDesktopNativeFullscreenNow,
    canUseDesktopWebKitFullscreen,
    logNativeFullscreenProbe,
    videoRef,
  ]);

  const primeNativeFullscreen = useCallback((): boolean => {
    const video = videoRef.current;
    if (!video?.webkitEnterFullscreen || !shouldUseTouchWebKitFullscreen(video)) {
      return false;
    }

    pendingNativeFullscreenRef.current = true;
    setNativeFullscreenPending(true);
    if (!canEnterTouchNativeFullscreenNow(video)) {
      return true;
    }

    try {
      logNativeFullscreenProbe('touch-start-handoff', video);
      video.controls = true;
      video.webkitEnterFullscreen();
      pendingNativeFullscreenRef.current = false;
      setNativeFullscreenPending(false);
      return true;
    } catch (err) {
      debugWarn('Primed native fullscreen failed', err);
      return false;
    }
  }, [canEnterTouchNativeFullscreenNow, logNativeFullscreenProbe, shouldUseTouchWebKitFullscreen, videoRef]);

  const enterDVRMode = useCallback(() => {
    const video = videoRef.current;
    if (allowNativeFullscreen && video && video.webkitEnterFullscreen && shouldForceNativeMobileHls(video)) {
      logNativeFullscreenProbe('dvr-native-request', video);
      video.controls = true;
      video.webkitEnterFullscreen();
      return;
    }
    void toggleFullscreen();
  }, [allowNativeFullscreen, logNativeFullscreenProbe, shouldForceNativeMobileHls, toggleFullscreen, videoRef]);

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
    setNativeFullscreenPending(false);
    pendingNativeFullscreenRef.current = false;
    appliedTouchDvrDefaultRef.current = false;
  }, []);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      const isInput = ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName) || target.isContentEditable;
      if (isInput) return;

      switch (e.key.toLowerCase()) {
        case 'f':
          e.preventDefault();
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

  useEffect(() => onHostMediaKey((action) => {
    switch (action) {
      case 'playPause':
        togglePlayPause();
        break;
      case 'play':
        play();
        break;
      case 'pause':
        pause();
        break;
      case 'seekBack':
        seekBy(-15);
        break;
      case 'seekForward':
        seekBy(15);
        break;
      case 'stop':
        stop();
        break;
    }
  }), [pause, play, seekBy, stop, togglePlayPause]);

  useEffect(() => {
    if (typeof navigator === 'undefined' || !('mediaSession' in navigator)) {
      return;
    }

    const mediaSession = navigator.mediaSession;
    const canMediaSeek = canRunSeekCommand;
    const setHandler = (
      action: MediaSessionAction,
      handler: MediaSessionActionHandler | null,
    ) => {
      try {
        mediaSession.setActionHandler(action, handler);
      } catch (err) {
        debugWarn(`Media session action "${action}" is not available`, err);
      }
    };

    setHandler('play', () => play());
    setHandler('pause', () => pause());
    setHandler('stop', () => stop());
    setHandler('seekbackward', canMediaSeek ? () => seekBy(-15) : null);
    setHandler('seekforward', canMediaSeek ? () => seekBy(15) : null);

    return () => {
      setHandler('play', null);
      setHandler('pause', null);
      setHandler('stop', null);
      setHandler('seekbackward', null);
      setHandler('seekforward', null);
    };
  }, [canRunSeekCommand, pause, play, seekBy, stop]);

  useEffect(() => {
    if (
      typeof navigator === 'undefined' ||
      !('mediaSession' in navigator) ||
      typeof MediaMetadata === 'undefined'
    ) {
      return;
    }

    const title = mediaTitle?.trim();
    const subtitle = mediaSubtitle?.trim();
    const artworkUrl = mediaArtworkUrl?.trim();
    if (!title) {
      return;
    }

    const artwork = artworkUrl ? [
      { src: artworkUrl, sizes: '512x512', type: 'image/png' },
      { src: artworkUrl, sizes: '256x256', type: 'image/png' },
      { src: artworkUrl, sizes: '192x192', type: 'image/png' },
      { src: artworkUrl, sizes: '128x128', type: 'image/png' },
    ] : undefined;

    try {
      navigator.mediaSession.metadata = new MediaMetadata({
        title,
        artist: subtitle || 'xg2g',
        album: 'xg2g',
        artwork,
      });
    } catch (err) {
      debugWarn('Media session metadata failed', err);
    }

    return () => {
      if ('mediaSession' in navigator && navigator.mediaSession.metadata?.title === title) {
        navigator.mediaSession.metadata = null;
      }
    };
  }, [mediaArtworkUrl, mediaSubtitle, mediaTitle]);

  useEffect(() => {
    if (typeof navigator === 'undefined' || !('mediaSession' in navigator)) {
      return;
    }

    navigator.mediaSession.playbackState = isPlaying ? 'playing' : 'paused';
    return () => {
      if ('mediaSession' in navigator) {
        navigator.mediaSession.playbackState = 'none';
      }
    };
  }, [isPlaying]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) return;

    const handleTimeUpdate = () => {
      refreshSeekableState();
      void flushPendingNativeFullscreen('touch-live-window-update');
    };
    const handleNativeFullscreenReady = () => {
      void flushPendingNativeFullscreen('deferred-touch-webkit-request');
    };

    video.addEventListener('timeupdate', handleTimeUpdate);
    video.addEventListener('loadedmetadata', handleTimeUpdate);
    video.addEventListener('durationchange', handleTimeUpdate);
    video.addEventListener('progress', handleTimeUpdate);
    video.addEventListener('seeking', handleTimeUpdate);
    video.addEventListener('loadedmetadata', handleNativeFullscreenReady);
    video.addEventListener('canplay', handleNativeFullscreenReady);
    video.addEventListener('playing', handleNativeFullscreenReady);

    refreshSeekableState();

    return () => {
      video.removeEventListener('timeupdate', handleTimeUpdate);
      video.removeEventListener('loadedmetadata', handleTimeUpdate);
      video.removeEventListener('durationchange', handleTimeUpdate);
      video.removeEventListener('progress', handleTimeUpdate);
      video.removeEventListener('seeking', handleTimeUpdate);
      video.removeEventListener('loadedmetadata', handleNativeFullscreenReady);
      video.removeEventListener('canplay', handleNativeFullscreenReady);
      video.removeEventListener('playing', handleNativeFullscreenReady);
    };
  }, [flushPendingNativeFullscreen, refreshSeekableState, videoRef]);

  useEffect(() => {
    void flushPendingNativeFullscreen('touch-live-window-hint');
  }, [flushPendingNativeFullscreen, normalizedLiveSeekWindow, playbackMode]);

  useEffect(() => {
    const video = videoRef.current;
    if (
      !video ||
      appliedTouchDvrDefaultRef.current ||
      !allowNativeFullscreen ||
      !normalizedLiveSeekWindow ||
      !shouldForceNativeMobileHls(video)
    ) {
      return;
    }

    const liveEdge = normalizedLiveSeekWindow.liveEdge ?? normalizedLiveSeekWindow.end;
    const windowStart = normalizedLiveSeekWindow.start;
    const windowSpan = Math.max(0, liveEdge - windowStart);
    const current = video.currentTime;

    if (!Number.isFinite(current) || current <= 0 || !Number.isFinite(liveEdge) || windowSpan < 8) {
      return;
    }

    if (current < liveEdge - 2) {
      appliedTouchDvrDefaultRef.current = true;
      return;
    }

    const desiredOffset = Math.min(
      touchLiveDvrDefaultOffsetSeconds,
      Math.max(8, Math.floor(windowSpan / 6)),
    );
    const target = Math.max(windowStart, liveEdge - desiredOffset);

    if (!(target < liveEdge - 1)) {
      appliedTouchDvrDefaultRef.current = true;
      return;
    }

    video.currentTime = target;
    setCurrentPlaybackTime(target);
    appliedTouchDvrDefaultRef.current = true;
  }, [
    allowNativeFullscreen,
    normalizedLiveSeekWindow,
    setCurrentPlaybackTime,
    shouldForceNativeMobileHls,
    videoRef,
  ]);

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
      shouldUseTouchWebKitFullscreen(video) ||
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
  }, [allowNativeFullscreen, containerRef, shouldForceNativeMobileHls, shouldUseTouchWebKitFullscreen, videoRef]);

  useEffect(() => {
    const onFsChange = () => {
      const fullscreenElement = document.fullscreenElement;
      const container = containerRef.current;
      const video = videoRef.current;
      setIsFullscreen(!!fullscreenElement && (
        fullscreenElement === document.documentElement ||
        fullscreenElement === container ||
        fullscreenElement === video
      ));
    };
    const onPipChange = () => setIsPip(!!document.pictureInPictureElement);

    const video = videoRef.current;
    const supportsWebkitFullscreen =
      !!video?.webkitEnterFullscreen &&
      (allowNativeFullscreen || shouldUseTouchWebKitFullscreen(video));

    const onWebkitBeginFullscreen = () => {
      setIsFullscreen(true);
      setIsWebKitFullscreenActive(true);
      pendingNativeFullscreenRef.current = false;
      setNativeFullscreenPending(false);
      if (video) {
        refreshSeekableState();
        logNativeFullscreenProbe('webkit-beginfullscreen', video);
      }
    };
    const onWebkitEndFullscreen = () => {
      setIsFullscreen(false);
      setIsWebKitFullscreenActive(false);
      setNativeFullscreenPending(false);
      if (video) {
        onNativeFullscreenExit?.({
          currentTime: Number.isFinite(video.currentTime) ? video.currentTime : null,
          wasPaused: video.paused,
        });
        refreshSeekableState();
        logNativeFullscreenProbe('webkit-endfullscreen', video);
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
  }, [allowNativeFullscreen, containerRef, logNativeFullscreenProbe, onNativeFullscreenExit, refreshSeekableState, shouldUseTouchWebKitFullscreen, videoRef]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const resetIdle = () => {
      setIsIdle(false);
      if (idleTimerRef.current) window.clearTimeout(idleTimerRef.current);
      idleTimerRef.current = window.setTimeout(() => setIsIdle(true), idleDelayMs);
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
  }, [containerRef, idleDelayMs]);

  const windowDuration = useMemo(() => Math.max(0, seekableEnd - seekableStart), [seekableEnd, seekableStart]);
  const relativePosition = useMemo(
    () => Math.min(windowDuration, Math.max(0, currentPlaybackTime - seekableStart)),
    [currentPlaybackTime, seekableStart, windowDuration]
  );
  const hasLiveDvrWindow = canSeekLiveWindow && windowDuration > 0;
  const seekEnabled = canRunSeekCommand;
  const hasSeekWindow = seekEnabled && windowDuration > 0;
  const isLiveMode = playbackMode === 'LIVE';
  const liveEdgePosition = normalizedLiveSeekWindow?.liveEdge ?? seekableEnd;
  const isAtLiveEdge = hasLiveDvrWindow && Math.abs(liveEdgePosition - currentPlaybackTime) < 2;
  const showDvrModeButton = hasLiveDvrWindow && allowNativeFullscreen && shouldForceNativeMobileHls(videoRef.current);
  const supportsNativeFullscreen = allowNativeFullscreen && typeof videoRef.current?.webkitEnterFullscreen === 'function';
  const canEnterNativeFullscreen = supportsNativeFullscreen && !isTouchDevice;
  const prefersDesktopNativeFullscreen = !!videoRef.current && allowNativeFullscreen && canUseDesktopWebKitFullscreen(videoRef.current);

  const liveWindowStartPosition = normalizedLiveSeekWindow?.start ?? seekableStart;
  const liveWindowEndPosition = normalizedLiveSeekWindow?.liveEdge ?? seekableEnd;

  const startTimeDisplay = playbackMode === 'LIVE'
    ? startUnix
      ? formatTimeOfDay(startUnix + liveWindowStartPosition)
      : formatClock(liveWindowStartPosition)
    : startUnix
      ? formatTimeOfDay(startUnix + relativePosition)
      : formatClock(relativePosition);

  const endTimeDisplay = playbackMode === 'LIVE'
    ? startUnix
      ? formatTimeOfDay(startUnix + liveWindowEndPosition)
      : formatClock(liveWindowEndPosition)
    : startUnix
      ? formatTimeOfDay(startUnix + windowDuration)
      : formatClock(windowDuration);

  // The playhead itself: wall-clock time-of-day (LIVE with an EPG anchor) or the
  // window-relative clock, plus how far behind the live edge we currently are. This
  // is what lets the timeline answer "which minute of the stream am I on", instead
  // of only labelling the window bounds. behindLiveSeconds is 0 for VOD and at the
  // live edge. playheadWindowPosition mirrors the slider thumb (seekableStart +
  // relativePosition) so the readout and the thumb never disagree.
  const playheadWindowPosition = seekableStart + relativePosition;
  const currentTimeDisplay = playbackMode === 'LIVE'
    ? startUnix
      ? formatTimeOfDay(startUnix + playheadWindowPosition)
      : formatClock(playheadWindowPosition)
    : formatClock(playheadWindowPosition);
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

  return {
    showStats,
    currentPlaybackTime,
    seekableStart,
    seekableEnd,
    supportsNativeFullscreen,
    canEnterNativeFullscreen,
    prefersDesktopNativeFullscreen,
    nativeFullscreenPending,
    isWebKitFullscreenActive,
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
    hasLiveDvrWindow,
    isLiveMode,
    isAtLiveEdge,
    showDvrModeButton,
    startTimeDisplay,
    endTimeDisplay,
    currentTimeDisplay,
    behindLiveSeconds,
    formatClock,
    seekTo,
    seekToLiveEdge,
    seekBy,
    seekWhenReady,
    togglePlayPause,
    toggleFullscreen,
    enterNativeFullscreen,
    primeNativeFullscreen,
    enterDVRMode,
    togglePiP,
    toggleMute,
    handleVolumeChange,
    applyAutoplayMute,
    toggleStats,
    resetChromeState
  };
}
