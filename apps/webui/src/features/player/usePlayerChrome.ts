import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { Dispatch, MutableRefObject, RefObject, SetStateAction } from 'react';
import type { HlsInstanceRef, PlayerStats, SafariVideoElement, VideoElementRef } from '../../types/v3-player';
import { debugLog, debugWarn } from '../../utils/logging';
import { onHostMediaKey } from '../../lib/hostBridge';
import { hasTouchInput } from './utils/playerHelpers';

import { useDvrTimelineController, readActualSeekableBounds } from './useDvrTimelineController';
import type { LiveSeekWindowHint } from './useDvrTimelineController';

type PlaybackMode = 'LIVE' | 'VOD' | 'UNKNOWN';
type ForceNativeFn = (videoEl?: VideoElementRef) => boolean;
type DesktopFullscreenFn = (videoEl?: VideoElementRef) => boolean;

function canUseWebKitPresentationModePiP(video: unknown): boolean {
  if (!video || typeof video !== 'object') return false;
  const webkitVideo = video as {
    webkitSupportsPresentationMode?: (mode: string) => boolean;
    webkitSetPresentationMode?: (mode: string) => void;
  };
  return (
    typeof webkitVideo.webkitSupportsPresentationMode === 'function' &&
    webkitVideo.webkitSupportsPresentationMode('picture-in-picture') === true &&
    typeof webkitVideo.webkitSetPresentationMode === 'function'
  );
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
  anchorStartSec?: number;
  onSeekOffset?: (targetSeconds: number) => void;
  liveSeekWindow?: LiveSeekWindowHint | null;
  onUserPlayIntent?: () => void;
  onUserPauseIntent?: () => void;
  onEngineObservation?: (observation: 'canplay' | 'playing_confirmed' | 'stalled_confirmed') => void;
  allowNativeFullscreen: boolean;
  shouldForceNativeMobileHls: ForceNativeFn;
  canUseDesktopWebKitFullscreen: DesktopFullscreenFn;
  onNativeFullscreenExit?: (details: { currentTime: number | null; wasPaused: boolean }) => void;
  mediaTitle?: string | null;
  mediaSubtitle?: string | null;
  mediaArtworkUrl?: string | null;
  /** Live channel zapping via media-session nexttrack/previoustrack (lock screen, headset). */
  onNextChannel?: (() => void) | null;
  onPreviousChannel?: (() => void) | null;
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
  anchorStartSec = 0,
  onSeekOffset,
  liveSeekWindow,
  onUserPlayIntent,
  onUserPauseIntent,
  onEngineObservation,
  allowNativeFullscreen,
  shouldForceNativeMobileHls,
  canUseDesktopWebKitFullscreen,
  onNativeFullscreenExit,
  mediaTitle,
  mediaSubtitle,
  mediaArtworkUrl,
  onNextChannel,
  onPreviousChannel,
}: UsePlayerChromeProps): PlayerChromeController {
  const dvr = useDvrTimelineController({
    videoRef,
    playbackMode,
    canSeek,
    durationSeconds,
    startUnix,
    anchorStartSec,
    onSeekOffset,
    liveSeekWindow,
    userPauseIntentRef,
    liveEdgeSeekSafetyGapSeconds,
  });

  const [showStats, setShowStats] = useState(false);
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
  const [nativeFullscreenPending, setNativeFullscreenPending] = useState(false);
  const lastNonZeroVolumeRef = useRef<number>(1);
  const userExplicitlyMutedRef = useRef(false);
  const programmaticVolumeChangeRef = useRef(false);
  const idleTimerRef = useRef<number | null>(null);
  const pendingNativeFullscreenRef = useRef(false);
  const appliedTouchDvrDefaultRef = useRef(false);
  const isTouchDevice = useMemo(() => hasTouchInput(), []);
  const idleDelayMs = isTouchDevice ? 2400 : 3000;

  const shouldUseTouchWebKitFullscreen = useCallback((videoEl?: VideoElementRef) => {
    if (!videoEl?.webkitEnterFullscreen) return false;
    return shouldForceNativeMobileHls(videoEl);
  }, [shouldForceNativeMobileHls]);

  const {
    canRunSeekCommand,
    seekTo,
    seekBy,
    refreshSeekableState,
  } = dvr;

  const logNativeFullscreenProbe = useCallback((reason: string, video: SafariVideoElement) => {
    const { start, end } = dvr.readSeekableBounds(video);
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
  }, [allowNativeFullscreen, canSeek, canUseDesktopWebKitFullscreen, dvr, playbackMode]);

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
  }, [canEnterNativeFullscreenNow, playbackMode]);

  const requiresVerifiedDesktopLiveWindow = useCallback(() => (
    playbackMode === 'LIVE' &&
    !!dvr.normalizedLiveSeekWindow &&
    dvr.normalizedLiveSeekWindow.end - dvr.normalizedLiveSeekWindow.start >= 8
  ), [dvr.normalizedLiveSeekWindow, playbackMode]);

  const canEnterDesktopNativeFullscreenNow = useCallback((video: SafariVideoElement) => {
    if (!canEnterNativeFullscreenNow(video)) {
      return false;
    }
    if (!requiresVerifiedDesktopLiveWindow()) {
      return true;
    }

    const actualWindow = readActualSeekableBounds(video);
    return !!actualWindow && actualWindow.end - actualWindow.start >= 8;
  }, [canEnterNativeFullscreenNow, requiresVerifiedDesktopLiveWindow]);

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
    onUserPlayIntent?.();
    video.play().catch((err) => debugWarn('Play failed', err));
  }, [clearAutoplayMuteIfNeeded, onUserPlayIntent, userPauseIntentRef, videoRef]);

  const pause = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;
    userPauseIntentRef.current = true;
    video.pause();
    onUserPauseIntent?.();
  }, [onUserPauseIntent, userPauseIntentRef, videoRef]);

  const stop = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;

    userPauseIntentRef.current = true;
    video.pause();
    onUserPauseIntent?.();
  }, [onUserPauseIntent, userPauseIntentRef, videoRef]);

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

    // Intentional Architecture Decision: Touch WebKit Fullscreen Priority (iOS / iPadOS)
    // On Touch WebKit devices (iPhone and iPadOS), we deliberately route fullscreen
    // to the native AVPlayer (video.webkitEnterFullscreen()) BEFORE attempting W3C
    // container.requestFullscreen().
    //
    // Rationale:
    // 1. Rock-solid system integration: Native AVPlayer guarantees smooth PiP transitions,
    //    hardware media control synchronization, AirPlay 2 routing, and battery-efficient decoding.
    // 2. Avoid MSE/buffer handoff instability: Touch WebKit native HLS playback maintains continuous
    //    session state without the risk of buffer stalling during container fullscreen transitions.
    // 3. Trade-off: On iPadOS, this replaces xg2g's custom HTML overlay chrome with Apple's native
    //    system playback UI in fullscreen mode. This is an explicit, intentional trade-off in favor
    //    of playback stability and system feature parity.
    //
    // Note: Engine and fullscreen routing throughout the player rely strictly on feature detection
    // (canPlayType + webkitEnterFullscreen / webkitSupportsPresentationMode + maxTouchPoints),
    // never on UA sniffing, osName, or surface layout flags.
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
    const video = videoRef.current as any;
    if (!video) return;

    // 1. Standard W3C API
    if (document.pictureInPictureEnabled && typeof video.requestPictureInPicture === 'function') {
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
      return;
    }

    // 2. Apple iPadOS / WebKit native presentation mode fallback
    if (canUseWebKitPresentationModePiP(video)) {
      try {
        const currentMode = video.webkitPresentationMode;
        video.webkitSetPresentationMode(currentMode === 'picture-in-picture' ? 'inline' : 'picture-in-picture');
        setIsPip(video.webkitPresentationMode === 'picture-in-picture');
      } catch (err) {
        debugWarn('WebKit PiP failed', err);
      }
    }
  }, [videoRef]);

  const toggleMute = useCallback(() => {
    const video = videoRef.current;
    if (!video) return;

    programmaticVolumeChangeRef.current = true;
    if (!video.muted) {
      if (video.volume > 0) {
        lastNonZeroVolumeRef.current = video.volume;
      }
      userExplicitlyMutedRef.current = true;
      video.muted = true;
      setIsMuted(true);
      return;
    }

    userExplicitlyMutedRef.current = false;
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
    programmaticVolumeChangeRef.current = true;
    video.volume = newVolume;
    setVolume(newVolume);
    if (newVolume > 0) {
      lastNonZeroVolumeRef.current = newVolume;
    }
    const shouldMute = newVolume === 0;
    userExplicitlyMutedRef.current = shouldMute;
    video.muted = shouldMute;
    setIsMuted(shouldMute);
  }, [videoRef]);

  const applyAutoplayMute = useCallback(() => {
    if (!autoStart) return;
    const video = videoRef.current;
    if (!video) return;
    programmaticVolumeChangeRef.current = true;
    userExplicitlyMutedRef.current = false;
    video.muted = true;
    setIsMuted(true);
  }, [autoStart, videoRef]);

  const toggleStats = useCallback(() => {
    setShowStats((prev) => !prev);
  }, []);

  const resetChromeState = useCallback(() => {
    dvr.resetDvrTimeline();
    setNativeFullscreenPending(false);
    pendingNativeFullscreenRef.current = false;
    appliedTouchDvrDefaultRef.current = false;
  }, [dvr]);

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
    // Absolute seeks from the lock-screen/OS scrubber (iOS 15+, macOS).
    setHandler('seekto', canMediaSeek ? (details) => {
      if (typeof details?.seekTime === 'number' && Number.isFinite(details.seekTime)) {
        seekTo(details.seekTime);
      }
    } : null);
    // Live channel zapping from lock screen / headset buttons.
    setHandler('nexttrack', onNextChannel ? () => onNextChannel() : null);
    setHandler('previoustrack', onPreviousChannel ? () => onPreviousChannel() : null);

    return () => {
      setHandler('play', null);
      setHandler('pause', null);
      setHandler('stop', null);
      setHandler('seekbackward', null);
      setHandler('seekforward', null);
      setHandler('seekto', null);
      setHandler('nexttrack', null);
      setHandler('previoustrack', null);
    };
  }, [canRunSeekCommand, onNextChannel, onPreviousChannel, pause, play, seekBy, seekTo, stop]);

  // Auto-PiP (Chromium 120+): the browser fires this media-session action on
  // its own when the user switches tabs/apps while media plays, letting the
  // video continue in picture-in-picture without a user gesture. Browsers
  // without the action reject setActionHandler, which setHandler-style
  // try/catch below turns into a silent no-op.
  useEffect(() => {
    if (typeof navigator === 'undefined' || !('mediaSession' in navigator)) {
      return;
    }
    const mediaSession = navigator.mediaSession;
    const setAutoPipHandler = (handler: MediaSessionActionHandler | null) => {
      try {
        mediaSession.setActionHandler('enterpictureinpicture' as MediaSessionAction, handler);
      } catch {
        // Action not supported by this browser; auto-PiP simply stays off.
      }
    };

    setAutoPipHandler(() => {
      const video = videoRef.current;
      if (!video || document.pictureInPictureElement === video) {
        return;
      }
      if (typeof video.requestPictureInPicture === 'function') {
        video.requestPictureInPicture().catch((err: unknown) => {
          debugWarn('Auto picture-in-picture request rejected', err);
        });
      }
    });

    return () => setAutoPipHandler(null);
  }, [videoRef]);

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
  }, [dvr.normalizedLiveSeekWindow, flushPendingNativeFullscreen, playbackMode]);

  useEffect(() => {
    const video = videoRef.current;
    if (
      !video ||
      appliedTouchDvrDefaultRef.current ||
      !allowNativeFullscreen ||
      !dvr.normalizedLiveSeekWindow ||
      !shouldForceNativeMobileHls(video)
    ) {
      return;
    }

    const liveEdge = dvr.normalizedLiveSeekWindow?.liveEdge ?? dvr.normalizedLiveSeekWindow?.end;
    const windowStart = dvr.normalizedLiveSeekWindow?.start ?? 0;
    if (!liveEdge) return;
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
    dvr.setCurrentPlaybackTime(target);
    appliedTouchDvrDefaultRef.current = true;
  }, [
    allowNativeFullscreen,
    dvr,
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

      if (video.readyState >= 3 && !video.paused) {
        onEngineObservation?.('playing_confirmed');
      } else if (video.readyState >= 3 && video.paused) {
        onEngineObservation?.('canplay');
      }
    }, 1000);

    return () => window.clearInterval(interval);
  }, [hlsRef, lastDecodedRef, onEngineObservation, playbackMode, showStats, videoRef]);

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

    const onVolumeChange = () => {
      if (programmaticVolumeChangeRef.current) {
        programmaticVolumeChangeRef.current = false;
        return;
      }

      // On mobile WebKit / touch devices, when hardware volume buttons are pressed,
      // WebKit fires 'volumechange'. If the video was playing muted due to autoplay
      // policy, automatically un-mute so the user gets audio immediately.
      if (video.muted && shouldForceNativeMobileHls(video) && !userExplicitlyMutedRef.current) {
        video.muted = false;
      }
      setVolume(video.volume);
      setIsMuted(video.muted);
      if (video.volume > 0) {
        lastNonZeroVolumeRef.current = video.volume;
      }
    };

    setVolume(video.volume);
    setIsMuted(video.muted);
    if (video.volume > 0) {
      lastNonZeroVolumeRef.current = video.volume;
    }

    video.addEventListener('volumechange', onVolumeChange);
    return () => {
      video.removeEventListener('volumechange', onVolumeChange);
    };
  }, [shouldForceNativeMobileHls, videoRef]);

  useEffect(() => {
    const video = videoRef.current;
    const container = containerRef.current;
    const nativeMobileHls = allowNativeFullscreen && shouldForceNativeMobileHls(video);
    const pipAvailable =
      typeof document !== 'undefined' &&
      !!video &&
      ((!!document.pictureInPictureEnabled && typeof video.requestPictureInPicture === 'function') ||
        canUseWebKitPresentationModePiP(video));
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
    const onPipChange = () => {
      const videoEl = videoRef.current as any;
      setIsPip(
        !!document.pictureInPictureElement ||
          (videoEl && videoEl.webkitPresentationMode === 'picture-in-picture')
      );
    };

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
      video.addEventListener('webkitpresentationmodechanged', onPipChange);

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
        video.removeEventListener('webkitpresentationmodechanged', onPipChange);

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

  const showDvrModeButton = dvr.hasLiveDvrWindow && allowNativeFullscreen && shouldForceNativeMobileHls(videoRef.current);
  const supportsNativeFullscreen = allowNativeFullscreen && typeof videoRef.current?.webkitEnterFullscreen === 'function';
  const canEnterNativeFullscreen = supportsNativeFullscreen && !isTouchDevice;
  const prefersDesktopNativeFullscreen = !!videoRef.current && allowNativeFullscreen && canUseDesktopWebKitFullscreen(videoRef.current);

  return {
    showStats,
    currentPlaybackTime: dvr.currentPlaybackTime,
    seekableStart: dvr.seekableStart,
    seekableEnd: dvr.seekableEnd,
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
    windowDuration: dvr.windowDuration,
    relativePosition: dvr.relativePosition,
    hasSeekWindow: dvr.hasSeekWindow,
    hasLiveDvrWindow: dvr.hasLiveDvrWindow,
    isLiveMode: dvr.isLiveMode,
    isAtLiveEdge: dvr.isAtLiveEdge,
    showDvrModeButton,
    startTimeDisplay: dvr.startTimeDisplay,
    endTimeDisplay: dvr.endTimeDisplay,
    currentTimeDisplay: dvr.currentTimeDisplay,
    behindLiveSeconds: dvr.behindLiveSeconds,
    formatClock: dvr.formatClock,
    seekTo: dvr.seekTo,
    seekToLiveEdge: dvr.seekToLiveEdge,
    seekBy: dvr.seekBy,
    seekWhenReady: dvr.seekWhenReady,
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
