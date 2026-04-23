import { useCallback, useEffect, useRef } from 'react';
import type { Dispatch, MutableRefObject, RefObject, SetStateAction } from 'react';
import type { TFunction } from 'i18next';
import Hls from './lib/hlsRuntime';
import type { ErrorData, FragLoadedData, ManifestParsedData, LevelLoadedData } from 'hls.js';
import type { HlsInstanceRef, PlayerStats, PlayerStatus, V3SessionStatusResponse, VideoElementRef } from '../../types/v3-player';
import { debugError, debugLog, debugWarn } from '../../utils/logging';
import { classifyHlsFatalError, classifyMediaElementError } from './playbackErrorPresentation';
import {
  describeHlsRenderProbe,
  isBlackRenderSuspect,
  readPlaybackFrameCounters,
  type HlsRenderProbeSnapshot,
} from './playbackRenderProbe';

type PlaybackEngineName = 'auto' | 'native' | 'hlsjs';
type ReportErrorFn = (event: 'error' | 'warning' | 'info', code: number, msg?: string) => Promise<void>;
type PreferNativeFn = (videoEl?: VideoElementRef, hlsJsSupported?: boolean) => boolean;
type WaitForSessionReadyFn = (sessionId: string, maxAttempts?: number) => Promise<V3SessionStatusResponse>;
type PrimePlaybackAuthFn = (playbackUrl: string, source: string) => Promise<void>;

const NATIVE_STALL_RECOVERY_MS = 2500;
const HLS_STALL_RECOVERY_MS = 2200;
const PLAYBACK_WARNING_CODE_WAITING = 101;
const PLAYBACK_WARNING_CODE_STALLED = 102;
const PLAYBACK_WARNING_CODE_DECODER_RECOVERY = 103;
const PLAYBACK_WARNING_CODE_NETWORK_RETRY = 104;
const PLAYBACK_INFO_CODE_RECOVERED_BUFFERING = 211;
const PLAYBACK_INFO_CODE_RECOVERED_NETWORK = 212;
const PLAYBACK_INFO_CODE_RECOVERED_DECODER = 213;
const PLAYBACK_INFO_CODE_PROBE_WINDOW_STARTED = 220;
const PLAYBACK_INFO_CODE_PROBE_WINDOW_CONFIRMED = 221;
const PLAYBACK_INFO_CODE_HLSJS_RENDER_PLAYING = 240;
const PLAYBACK_INFO_CODE_HLSJS_RENDER_STABLE = 241;
const PLAYBACK_INFO_CODE_HLSJS_RENDER_BLACK = 242;
const PROBE_CONFIRMATION_MS = 10_000;
const HLSJS_RENDER_PROBE_MS = 2_500;

interface UsePlaybackEngineProps {
  videoRef: RefObject<VideoElementRef>;
  hlsRef: MutableRefObject<HlsInstanceRef>;
  sessionIdRef: MutableRefObject<string | null>;
  isTeardownRef: MutableRefObject<boolean>;
  lastDecodedRef: MutableRefObject<number>;
  t: TFunction;
  reportError: ReportErrorFn;
  waitForSessionReady: WaitForSessionReadyFn;
  shouldPreferNativeHls: PreferNativeFn;
  primePlaybackAuth?: PrimePlaybackAuthFn;
  runtimeProbeActive?: boolean;
  setStats: Dispatch<SetStateAction<PlayerStats>>;
  setStatus: Dispatch<SetStateAction<PlayerStatus>>;
  setError: Dispatch<SetStateAction<string | null>>;
  setErrorDetails: Dispatch<SetStateAction<string | null>>;
  setShowErrorDetails: Dispatch<SetStateAction<boolean>>;
  dispatchPlayerAction?: (action: any) => void;
}

interface PlaybackEngineController {
  resetPlaybackEngine: () => void;
  playHls: (url: string, engine?: PlaybackEngineName) => void;
  playDirectMp4: (url: string) => void;
  waitForDirectStream: (url: string) => Promise<void>;
}

function playbackRecoveryInfoForWarning(code: number): { code: number; message: string } | null {
  switch (code) {
    case PLAYBACK_WARNING_CODE_WAITING:
    case PLAYBACK_WARNING_CODE_STALLED:
      return { code: PLAYBACK_INFO_CODE_RECOVERED_BUFFERING, message: 'recovered_buffering' };
    case PLAYBACK_WARNING_CODE_NETWORK_RETRY:
      return { code: PLAYBACK_INFO_CODE_RECOVERED_NETWORK, message: 'recovered_network' };
    case PLAYBACK_WARNING_CODE_DECODER_RECOVERY:
      return { code: PLAYBACK_INFO_CODE_RECOVERED_DECODER, message: 'recovered_decoder' };
    default:
      return null;
  }
}

function extractHlsHttpStatus(data: ErrorData): number | undefined {
  const candidates = [
    (data as { response?: { code?: number; status?: number } }).response?.code,
    (data as { response?: { code?: number; status?: number } }).response?.status,
    (data as { networkDetails?: { status?: number } }).networkDetails?.status,
  ];

  return candidates.find((value): value is number => typeof value === 'number');
}
export function usePlaybackEngine({
  videoRef,
  hlsRef,
  sessionIdRef,
  isTeardownRef,
  lastDecodedRef,
  t,
  reportError,
  waitForSessionReady,
  shouldPreferNativeHls,
  primePlaybackAuth,
  runtimeProbeActive = false,
  setStats,
  setStatus,
  setError,
  setErrorDetails,
  setShowErrorDetails
}: UsePlaybackEngineProps): PlaybackEngineController {
  const lastHlsUrlRef = useRef<string | null>(null);
  const lastHlsEngineRef = useRef<PlaybackEngineName>('auto');
  const replayHlsRef = useRef<((url: string, engine?: PlaybackEngineName) => void) | null>(null);
  const decodeRecoveryInFlightRef = useRef(false);
  const decodeRecoveryAttemptsRef = useRef(0);
  const pendingNativeAutoplayRef = useRef<(() => void) | null>(null);
  const nativeStallRecoveryTimerRef = useRef<number | null>(null);
  const hlsStallRecoveryTimerRef = useRef<number | null>(null);
  const hlsStallRecoveryAttemptsRef = useRef(0);
  const reportedPlayingSessionRef = useRef<string | null>(null);
  const reportedWarningKeysRef = useRef<Set<string>>(new Set());
  const pendingWarningRecoveryRef = useRef<{ code: number; message: string } | null>(null);
  const reportedProbeStartedSessionRef = useRef<string | null>(null);
  const reportedProbeConfirmedSessionRef = useRef<string | null>(null);
  const probeConfirmationTimerRef = useRef<number | null>(null);
  const activeHlsRenderProbeSessionRef = useRef<string | null>(null);
  const completedHlsRenderProbeSessionRef = useRef<string | null>(null);
  const hlsRenderProbeTimerRef = useRef<number | null>(null);

  const clearPendingNativeAutoplay = useCallback(() => {
    const video = videoRef.current;
    const pendingHandler = pendingNativeAutoplayRef.current;
    if (video && pendingHandler) {
      video.removeEventListener('loadedmetadata', pendingHandler);
    }
    pendingNativeAutoplayRef.current = null;
  }, [videoRef]);

  const scheduleNativeAutoplay = useCallback((video: NonNullable<VideoElementRef>, label: string) => {
    clearPendingNativeAutoplay();

    const onLoadedMetadata = () => {
      pendingNativeAutoplayRef.current = null;
      video.play().catch((err) => debugWarn(label, err));
    };

    pendingNativeAutoplayRef.current = onLoadedMetadata;
    video.addEventListener('loadedmetadata', onLoadedMetadata, { once: true });
  }, [clearPendingNativeAutoplay]);

  const startNativeHlsPlayback = useCallback((url: string, autoplayLabel: string) => {
    const video = videoRef.current;
    if (!video) {
      return;
    }

    const trackedSessionId = sessionIdRef.current;
    lastHlsUrlRef.current = url;
    lastHlsEngineRef.current = 'native';

    void (async () => {
      try {
        await primePlaybackAuth?.(url, 'usePlaybackEngine.playHls.native');
      } catch (err) {
        debugWarn('[V3Player] Native HLS auth priming failed', err);
        if (
          isTeardownRef.current ||
          (trackedSessionId !== null && sessionIdRef.current !== trackedSessionId) ||
          lastHlsUrlRef.current !== url ||
          lastHlsEngineRef.current !== 'native'
        ) {
          return;
        }

        setStatus('error');
        setError(err instanceof Error && err.message ? err.message : t('player.authFailed'));
        setErrorDetails('native_hls_auth_prime_failed');
        return;
      }

      if (
        isTeardownRef.current ||
        (trackedSessionId !== null && sessionIdRef.current !== trackedSessionId) ||
        lastHlsUrlRef.current !== url ||
        lastHlsEngineRef.current !== 'native'
      ) {
        return;
      }

      video.src = url;
      scheduleNativeAutoplay(video, autoplayLabel);
    })();
  }, [isTeardownRef, primePlaybackAuth, scheduleNativeAutoplay, sessionIdRef, setError, setErrorDetails, setStatus, t, videoRef]);

  const clearNativeStallRecovery = useCallback(() => {
    if (nativeStallRecoveryTimerRef.current !== null) {
      window.clearTimeout(nativeStallRecoveryTimerRef.current);
      nativeStallRecoveryTimerRef.current = null;
    }
  }, []);

  const clearHlsStallRecovery = useCallback(() => {
    if (hlsStallRecoveryTimerRef.current !== null) {
      window.clearTimeout(hlsStallRecoveryTimerRef.current);
      hlsStallRecoveryTimerRef.current = null;
    }
  }, []);

  const clearProbeConfirmation = useCallback(() => {
    if (probeConfirmationTimerRef.current !== null) {
      window.clearTimeout(probeConfirmationTimerRef.current);
      probeConfirmationTimerRef.current = null;
    }
  }, []);

  const clearHlsRenderProbe = useCallback((resetCompleted: boolean = false) => {
    if (hlsRenderProbeTimerRef.current !== null) {
      window.clearTimeout(hlsRenderProbeTimerRef.current);
      hlsRenderProbeTimerRef.current = null;
    }
    activeHlsRenderProbeSessionRef.current = null;
    if (resetCompleted) {
      completedHlsRenderProbeSessionRef.current = null;
    }
  }, []);

  const bufferedAheadSeconds = useCallback((videoEl: NonNullable<VideoElementRef>): number => {
    if (!videoEl.buffered.length) {
      return 0;
    }

    for (let i = 0; i < videoEl.buffered.length; i++) {
      if (videoEl.currentTime >= videoEl.buffered.start(i) && videoEl.currentTime <= videoEl.buffered.end(i)) {
        return videoEl.buffered.end(i) - videoEl.currentTime;
      }
    }

    return 0;
  }, []);

  const bufferedTailSeconds = useCallback((videoEl: NonNullable<VideoElementRef>): number => {
    if (!videoEl.buffered.length) {
      return 0;
    }
    try {
      const tail = videoEl.buffered.end(videoEl.buffered.length - 1);
      return Math.max(0, tail - videoEl.currentTime);
    } catch {
      return 0;
    }
  }, []);

  const captureHlsRenderProbeSnapshot = useCallback((videoEl: NonNullable<VideoElementRef>): HlsRenderProbeSnapshot => {
    const counters = readPlaybackFrameCounters(videoEl);
    return {
      currentTime: videoEl.currentTime,
      readyState: videoEl.readyState,
      networkState: videoEl.networkState,
      videoWidth: videoEl.videoWidth,
      videoHeight: videoEl.videoHeight,
      paused: videoEl.paused,
      bufferedAhead: bufferedAheadSeconds(videoEl),
      totalFrames: counters.totalFrames,
      droppedFrames: counters.droppedFrames,
    };
  }, [bufferedAheadSeconds]);

  const scheduleHlsRenderProbe = useCallback((videoEl: NonNullable<VideoElementRef>, trackedSessionId: string) => {
    if (lastHlsEngineRef.current !== 'hlsjs') {
      clearHlsRenderProbe(false);
      return;
    }
    if (
      completedHlsRenderProbeSessionRef.current === trackedSessionId ||
      activeHlsRenderProbeSessionRef.current === trackedSessionId
    ) {
      return;
    }

    const started = captureHlsRenderProbeSnapshot(videoEl);
    activeHlsRenderProbeSessionRef.current = trackedSessionId;
    void reportError('info', PLAYBACK_INFO_CODE_HLSJS_RENDER_PLAYING, describeHlsRenderProbe('playing', started));

    hlsRenderProbeTimerRef.current = window.setTimeout(() => {
      hlsRenderProbeTimerRef.current = null;
      if (
        isTeardownRef.current ||
        sessionIdRef.current !== trackedSessionId ||
        lastHlsEngineRef.current !== 'hlsjs'
      ) {
        activeHlsRenderProbeSessionRef.current = null;
        return;
      }

      const settled = captureHlsRenderProbeSnapshot(videoEl);
      const blackSuspect = isBlackRenderSuspect(started, settled);
      activeHlsRenderProbeSessionRef.current = null;
      completedHlsRenderProbeSessionRef.current = trackedSessionId;
      void reportError(
        'info',
        blackSuspect ? PLAYBACK_INFO_CODE_HLSJS_RENDER_BLACK : PLAYBACK_INFO_CODE_HLSJS_RENDER_STABLE,
        describeHlsRenderProbe(blackSuspect ? 'black_suspect' : 'stable', settled, started)
      );
    }, HLSJS_RENDER_PROBE_MS);
  }, [captureHlsRenderProbeSnapshot, clearHlsRenderProbe, isTeardownRef, reportError, sessionIdRef]);

  const reportPlaybackWarning = useCallback((code: number, message: string) => {
    const trackedSessionId = sessionIdRef.current;
    if (!trackedSessionId) {
      return;
    }
    const key = `${trackedSessionId}:${code}`;
    if (reportedWarningKeysRef.current.has(key)) {
      return;
    }
    reportedWarningKeysRef.current.add(key);
    pendingWarningRecoveryRef.current = playbackRecoveryInfoForWarning(code);
    void reportError('warning', code, message);
  }, [reportError, sessionIdRef]);

  const updateStats = useCallback((hls: Hls) => {
    if (!hls) return;
    const idx = hls.currentLevel === -1 ? 0 : hls.currentLevel;
    const level = hls.levels ? hls.levels[idx] : undefined;

    if (level) {
      setStats((prev) => {
        let bandwidth = prev.bandwidth;
        let resolution = prev.resolution;

        if (level.bitrate) bandwidth = Math.round(level.bitrate / 1024);
        if (level.width && level.height) {
          resolution = `${level.width}x${level.height}`;
        }

        return {
          ...prev,
          bandwidth,
          resolution,
          levelIndex: hls.currentLevel
        };
      });
      return;
    }

    setStats((prev) => ({
      ...prev,
      levelIndex: hls.currentLevel
    }));
  }, [setStats]);

  const resetPlaybackEngine = useCallback(() => {
    isTeardownRef.current = true;
    try {
      clearPendingNativeAutoplay();
      clearNativeStallRecovery();
      clearHlsStallRecovery();
      hlsStallRecoveryAttemptsRef.current = 0;
      lastHlsUrlRef.current = null;
      lastHlsEngineRef.current = 'auto';
      reportedPlayingSessionRef.current = null;
      reportedProbeStartedSessionRef.current = null;
      reportedProbeConfirmedSessionRef.current = null;
      reportedWarningKeysRef.current.clear();
      pendingWarningRecoveryRef.current = null;
      clearProbeConfirmation();
      clearHlsRenderProbe(true);
      if (hlsRef.current) {
        hlsRef.current.destroy();
        hlsRef.current = null;
      }
      if (videoRef.current) {
        videoRef.current.pause();
        videoRef.current.removeAttribute('src');
        videoRef.current.load();
      }
    } finally {
      window.setTimeout(() => {
        isTeardownRef.current = false;
      }, 50);
    }
  }, [clearHlsRenderProbe, clearHlsStallRecovery, clearNativeStallRecovery, clearPendingNativeAutoplay, clearProbeConfirmation, hlsRef, isTeardownRef, videoRef]);

  const beginSessionDecodeRecovery = useCallback((
    code: number,
    message: string,
    onFailure: (err: unknown) => void
  ): boolean => {
    if (
      !sessionIdRef.current ||
      !lastHlsUrlRef.current ||
      decodeRecoveryInFlightRef.current ||
      decodeRecoveryAttemptsRef.current >= 2
    ) {
      return false;
    }

    const trackedSessionId = sessionIdRef.current;
    const trackedUrl = lastHlsUrlRef.current;
    const trackedEngine = lastHlsEngineRef.current;

    decodeRecoveryInFlightRef.current = true;
    decodeRecoveryAttemptsRef.current += 1;
    setStatus('recovering');
    setError(null);
    setErrorDetails(null);
    setShowErrorDetails(false);

    void (async () => {
      try {
        await reportError('error', code, message);
        await new Promise((resolve) => window.setTimeout(resolve, 750));

        if (sessionIdRef.current !== trackedSessionId) {
          return;
        }

        const session = await waitForSessionReady(trackedSessionId, 80);

        if (sessionIdRef.current !== trackedSessionId) {
          return;
        }

        resetPlaybackEngine();
        await new Promise((resolve) => window.setTimeout(resolve, 100));

        if (sessionIdRef.current !== trackedSessionId) {
          return;
        }

        const nextUrl = session.playbackUrl || trackedUrl;
        if (!nextUrl) {
          throw new Error('Recovered session missing playbackUrl');
        }

        setStatus('buffering');
        setError(null);
        setErrorDetails(null);
        setShowErrorDetails(false);
        replayHlsRef.current?.(nextUrl, trackedEngine);
      } catch (recoveryErr) {
        debugWarn('[V3Player] Decode recovery failed', recoveryErr);
        onFailure(recoveryErr);
      } finally {
        decodeRecoveryInFlightRef.current = false;
      }
    })();

    return true;
  }, [
    reportError,
    resetPlaybackEngine,
    sessionIdRef,
    setError,
    setErrorDetails,
    setShowErrorDetails,
    setStatus,
    waitForSessionReady
  ]);

  const scheduleNativeStallRecovery = useCallback((
    videoEl: NonNullable<VideoElementRef>,
    trigger: 'waiting' | 'stalled'
  ) => {
    if (
      decodeRecoveryInFlightRef.current ||
      nativeStallRecoveryTimerRef.current !== null ||
      hlsRef.current ||
      !sessionIdRef.current ||
      !lastHlsUrlRef.current ||
      lastHlsEngineRef.current !== 'native' ||
      videoEl.paused
    ) {
      return;
    }

    const startingTime = videoEl.currentTime;
    const startingSrc = videoEl.currentSrc;

    nativeStallRecoveryTimerRef.current = window.setTimeout(() => {
      nativeStallRecoveryTimerRef.current = null;

      if (
        isTeardownRef.current ||
        decodeRecoveryInFlightRef.current ||
        hlsRef.current ||
        !sessionIdRef.current ||
        !lastHlsUrlRef.current ||
        lastHlsEngineRef.current !== 'native' ||
        videoEl.paused
      ) {
        return;
      }

      const progressed = Math.abs(videoEl.currentTime - startingTime) > 0.25;
      const bufferHealth = bufferedAheadSeconds(videoEl);
      const readyForPlayback = videoEl.readyState >= 3;

      if (videoEl.currentSrc !== startingSrc || progressed || readyForPlayback || bufferHealth > 0.5) {
        return;
      }

      const started = beginSessionDecodeRecovery(4, `native_hls_${trigger}`, () => {
        setStatus('error');
        setError(t('player.networkError'));
        setErrorDetails(`${trigger} (native stall recovery failed)`);
      });

      if (!started) {
        setStatus('error');
        setError(t('player.networkError'));
        setErrorDetails(`${trigger} (native stall recovery failed)`);
      }
    }, NATIVE_STALL_RECOVERY_MS);
  }, [
    beginSessionDecodeRecovery,
    bufferedAheadSeconds,
    hlsRef,
    isTeardownRef,
    sessionIdRef,
    setError,
    setErrorDetails,
    setStatus,
    t
  ]);

  const scheduleHlsStallRecovery = useCallback((
    videoEl: NonNullable<VideoElementRef>,
    trigger: 'waiting' | 'stalled'
  ) => {
    if (
      decodeRecoveryInFlightRef.current ||
      hlsStallRecoveryTimerRef.current !== null ||
      !hlsRef.current ||
      !sessionIdRef.current ||
      !lastHlsUrlRef.current ||
      lastHlsEngineRef.current !== 'hlsjs' ||
      videoEl.paused
    ) {
      return;
    }

    const startingTime = videoEl.currentTime;

    hlsStallRecoveryTimerRef.current = window.setTimeout(() => {
      hlsStallRecoveryTimerRef.current = null;

      const hls = hlsRef.current;
      if (
        isTeardownRef.current ||
        decodeRecoveryInFlightRef.current ||
        !hls ||
        !sessionIdRef.current ||
        !lastHlsUrlRef.current ||
        lastHlsEngineRef.current !== 'hlsjs' ||
        videoEl.paused
      ) {
        return;
      }

      const progressed = Math.abs(videoEl.currentTime - startingTime) > 0.25;
      const bufferHealth = bufferedAheadSeconds(videoEl);
      const tailHealth = bufferedTailSeconds(videoEl);
      const readyForPlayback = videoEl.readyState >= 3;

      if (progressed || (readyForPlayback && bufferHealth > 0.5)) {
        return;
      }

      hlsStallRecoveryAttemptsRef.current += 1;
      const attempts = hlsStallRecoveryAttemptsRef.current;
      debugWarn('[V3Player] hls.js stall recovery', {
        trigger,
        attempts,
        bufferHealth,
        tailHealth,
        readyState: videoEl.readyState,
      });

      if (tailHealth > 0.4) {
        try {
          const tailPosition = videoEl.buffered.end(videoEl.buffered.length - 1);
          const nextTime = Math.max(videoEl.currentTime, tailPosition - 0.15);
          if (Number.isFinite(nextTime) && nextTime > videoEl.currentTime + 0.05) {
            videoEl.currentTime = nextTime;
          }
        } catch {
          // Best-effort live-edge nudge only.
        }
      }

      hls.startLoad();
      void videoEl.play().catch((err) => debugWarn('[V3Player] hls.js stall autoplay retry failed', err));

      if (attempts >= 2) {
        const started = beginSessionDecodeRecovery(4, `hlsjs_${trigger}`, () => {
          setStatus('error');
          setError(t('player.networkError'));
          setErrorDetails(`${trigger} (hls.js stall recovery failed)`);
        });
        if (!started) {
          setStatus('error');
          setError(t('player.networkError'));
          setErrorDetails(`${trigger} (hls.js stall recovery failed)`);
        }
      }
    }, HLS_STALL_RECOVERY_MS);
  }, [
    beginSessionDecodeRecovery,
    bufferedAheadSeconds,
    bufferedTailSeconds,
    hlsRef,
    isTeardownRef,
    sessionIdRef,
    setError,
    setErrorDetails,
    setStatus,
    t
  ]);

  const playHls = useCallback((url: string, engine: PlaybackEngineName = 'auto') => {
    const video = videoRef.current;
    if (!video) return;

    clearPendingNativeAutoplay();
    clearNativeStallRecovery();
    clearHlsStallRecovery();
    clearHlsRenderProbe(true);
    hlsStallRecoveryAttemptsRef.current = 0;
    lastDecodedRef.current = 0;

    const hlsJsSupported = Hls.isSupported();
    const preferNativeHls = shouldPreferNativeHls(video, hlsJsSupported);
    const canPlayNative = !!video.canPlayType('application/vnd.apple.mpegurl');
    const preferNative =
      preferNativeHls ||
      engine === 'native' ||
      (engine === 'auto' && canPlayNative && !hlsJsSupported);
    const canUseHlsJs = (engine === 'hlsjs' || engine === 'auto') && !preferNativeHls;
    const usingHlsJs = !preferNative && canUseHlsJs && hlsJsSupported;

    try {
      if (engine === 'hlsjs' || usingHlsJs) {
        (video as HTMLVideoElement & { disableRemotePlayback?: boolean }).disableRemotePlayback = true;
        video.setAttribute('disableremoteplayback', '');
      } else {
        (video as HTMLVideoElement & { disableRemotePlayback?: boolean }).disableRemotePlayback = false;
        video.removeAttribute('disableremoteplayback');
      }
    } catch {
      // Best-effort: iOS MMS can require remote playback to be disabled.
    }

    if (preferNativeHls && engine === 'hlsjs') {
      debugLog('[V3Player] Overriding hls.js engine to native HLS on WebKit');
    }

    if (usingHlsJs) {
      lastHlsUrlRef.current = url;
      lastHlsEngineRef.current = 'hlsjs';
      if (hlsRef.current) {
        hlsRef.current.destroy();
      }
      const hls = new Hls({
        debug: false,
        enableWorker: true,
        lowLatencyMode: false,
        backBufferLength: 300,
        maxBufferLength: 60,
        capLevelToPlayerSize: true
      });
      hlsRef.current = hls;

      hls.on(Hls.Events.LEVEL_SWITCHED, () => updateStats(hls));
      hls.on(Hls.Events.MANIFEST_PARSED, (_event, data: ManifestParsedData) => {
        debugLog('[V3Player] HLS Manifest Parsed', { levels: data.levels.length });

        if (hls.currentLevel === -1 && data.levels.length > 0) {
          hls.startLevel = -1;
        }

        updateStats(hls);
        if (data.levels && data.levels.length > 0) {
          const first = data.levels[0];
          if (first) {
            setStats((prev) => ({ ...prev, fps: first.frameRate || 0 }));
          }
        }
        videoRef.current?.play().catch((err) => {
          debugWarn('[V3Player] Autoplay failed', err);
          setStatus('ready');
        });
      });

      hls.on(Hls.Events.LEVEL_LOADED, (_event, data: LevelLoadedData) => {
        const hasContent = data.details.totalduration > 0 || (data.details.fragments && data.details.fragments.length > 0);
        setStatus((prev) => {
          if (hasContent && prev === 'buffering') {
            debugLog('[V3Player] Level Loaded with content, forcing READY state');
            return 'ready';
          }
          return prev;
        });
      });

      hls.on(Hls.Events.FRAG_LOADED, (_event, data: FragLoadedData) => {
        debugLog('[V3Player] HLS Frag Loaded', { sn: data.frag.sn });
        if (hls.currentLevel >= 0) {
          updateStats(hls);
        }
        setStats((prev) => ({
          ...prev,
          levelIndex: hls.currentLevel
        }));
      });

      hls.loadSource(url);
      hls.attachMedia(video);

      let mediaRecoveryAttempted = false;
      let networkRetryCount = 0;
      const maxNetworkRetries = 6;
      const networkBackoffCapMs = 30_000;

      hls.on(Hls.Events.ERROR, (_event, data: ErrorData) => {
        if (!data.fatal) {
          return;
        }

        const presentation = classifyHlsFatalError(data, t, lastHlsUrlRef.current);
        const httpStatus = extractHlsHttpStatus(data);
        switch (data.type) {
          case Hls.ErrorTypes.NETWORK_ERROR:
            if (httpStatus === 401 && sessionIdRef.current) {
              void reportError('error', 0, `${data.type}: ${data.details}`);
              debugWarn('[V3Player] NETWORK_ERROR 401: attempting session recovery before failing');
              const started = beginSessionDecodeRecovery(0, `${data.type}: ${data.details}`, (recoveryErr) => {
                debugError('[V3Player] NETWORK_ERROR 401: session recovery failed', recoveryErr);
                hlsRef.current?.destroy();
                setStatus('error');
                setError(
                  recoveryErr instanceof Error && recoveryErr.message
                    ? recoveryErr.message
                    : presentation.title
                );
                setErrorDetails(presentation.details);
              });
              if (!started) {
                hlsRef.current?.destroy();
                setStatus('error');
                setError(presentation.title);
                setErrorDetails(presentation.details);
              }
              return;
            }
            if (networkRetryCount < maxNetworkRetries) {
              const backoffMs = Math.min(1000 * Math.pow(2, networkRetryCount), networkBackoffCapMs);
              networkRetryCount++;
              reportPlaybackWarning(PLAYBACK_WARNING_CODE_NETWORK_RETRY, 'hlsjs_network_retry');
              debugWarn(`[V3Player] NETWORK_ERROR recovery attempt ${networkRetryCount}/${maxNetworkRetries}, backoff ${backoffMs}ms`);
              setStatus('recovering');
              window.setTimeout(() => hls.startLoad(), backoffMs);
            } else {
              if (sessionIdRef.current) {
                void reportError('error', 0, `${data.type}: ${data.details}`);
              }
              debugError(`[V3Player] NETWORK_ERROR: max retries (${maxNetworkRetries}) exhausted`);
              hlsRef.current?.destroy();
              setStatus('error');
              setError(presentation.title);
              setErrorDetails(`${presentation.details} • ${maxNetworkRetries} retries exhausted`);
            }
            break;
          case Hls.ErrorTypes.MEDIA_ERROR:
            if (!mediaRecoveryAttempted) {
              mediaRecoveryAttempted = true;
              reportPlaybackWarning(PLAYBACK_WARNING_CODE_DECODER_RECOVERY, 'hlsjs_media_recovery');
              debugWarn('[V3Player] MEDIA_ERROR: attempting single recovery');
              setStatus('recovering');
              hls.recoverMediaError();
            } else {
              debugWarn('[V3Player] MEDIA_ERROR: local recovery exhausted, attempting session reattach');
              const started = beginSessionDecodeRecovery(3, `${data.type}: ${data.details}`, () => {
                debugError('[V3Player] MEDIA_ERROR: session reattach failed, failing terminally');
                hlsRef.current?.destroy();
                setStatus('error');
                setError(presentation.title);
                setErrorDetails(`${presentation.details} • media recovery failed`);
              });
              if (!started) {
                debugError('[V3Player] MEDIA_ERROR: recovery already attempted, failing terminally');
                hlsRef.current?.destroy();
                setStatus('error');
                setError(presentation.title);
                setErrorDetails(`${presentation.details} • media recovery failed`);
              }
            }
            break;
          default:
            if (sessionIdRef.current) {
              void reportError('error', 0, `${data.type}: ${data.details}`);
            }
            hlsRef.current?.destroy();
            setStatus('error');
            setError(presentation.title);
            setErrorDetails(presentation.details);
            break;
        }
      });

      hls.on(Hls.Events.FRAG_LOADED, () => {
        networkRetryCount = 0;
      });

      return;
    }

    if (canPlayNative && (engine === 'native' || engine === 'auto')) {
      startNativeHlsPlayback(url, '[V3Player] Native play blocked');
      return;
    }

    if (engine === 'auto') {
      startNativeHlsPlayback(url, '[V3Player] Auto fallback play blocked');
      return;
    }

    throw new Error('HLS playback engine not available');
  }, [beginSessionDecodeRecovery, clearHlsRenderProbe, clearHlsStallRecovery, clearNativeStallRecovery, clearPendingNativeAutoplay, hlsRef, lastDecodedRef, reportError, reportPlaybackWarning, sessionIdRef, setError, setErrorDetails, setStats, setStatus, shouldPreferNativeHls, startNativeHlsPlayback, t, updateStats, videoRef]);

  replayHlsRef.current = playHls;

  const playDirectMp4 = useCallback((url: string) => {
    clearPendingNativeAutoplay();
    clearNativeStallRecovery();
    clearHlsStallRecovery();
    clearHlsRenderProbe(true);
    hlsStallRecoveryAttemptsRef.current = 0;
    if (hlsRef.current) {
      hlsRef.current.destroy();
      hlsRef.current = null;
    }
    const video = videoRef.current;
    if (!video) return;

    lastHlsUrlRef.current = null;
    lastHlsEngineRef.current = 'auto';
    lastDecodedRef.current = 0;
    setStats((prev) => ({ ...prev, bandwidth: 0, resolution: 'Original (Direct)', fps: 0, levelIndex: -1 }));
    debugLog('[V3Player] Switching to Direct MP4 Mode:', url);
    video.src = url;
    video.load();
    video.play().catch((err) => debugWarn('Autoplay failed', err));
  }, [clearHlsRenderProbe, clearHlsStallRecovery, clearNativeStallRecovery, clearPendingNativeAutoplay, hlsRef, lastDecodedRef, setStats, videoRef]);

  const waitForDirectStream = useCallback(async (url: string): Promise<void> => {
    const maxRetries = 100;
    let retries = 0;

    while (retries < maxRetries) {
      if (isTeardownRef.current) throw new Error('Playback cancelled');

      try {
        const res = await fetch(url, { method: 'HEAD', cache: 'no-store' });

        if (res.ok || res.status === 206) {
          return;
        }

        if (res.status === 503) {
          await new Promise((resolve) => window.setTimeout(resolve, 1000));
        } else {
          throw new Error(`Unexpected status: ${res.status}`);
        }
      } catch (err) {
        debugWarn('[V3Player] Probe failed', err);
        throw new Error(t('player.networkError'));
      }

      retries++;
    }

    debugWarn('[V3Player] Direct Stream Timeout after', maxRetries, 'attempts');
    throw new Error(t('player.timeout'));
  }, [isTeardownRef, t]);

  useEffect(() => {
    const videoEl = videoRef.current;
    if (!videoEl) return;

    const onWaiting = () => {
      if (decodeRecoveryInFlightRef.current) {
        debugLog('[V3Player] Event: waiting ignored during decode recovery');
        return;
      }

      let bufferHealth = 0;
      if (videoEl.buffered.length > 0) {
        for (let i = 0; i < videoEl.buffered.length; i++) {
          if (videoEl.currentTime >= videoEl.buffered.start(i) && videoEl.currentTime <= videoEl.buffered.end(i)) {
            bufferHealth = videoEl.buffered.end(i) - videoEl.currentTime;
            break;
          }
        }
      }

      if (videoEl.readyState >= 3 && bufferHealth > 0.5) {
        debugLog(`[V3Player] Event: waiting (ignored, buffer=${bufferHealth.toFixed(1)}s)`);
        clearNativeStallRecovery();
        clearHlsStallRecovery();
        return;
      }

      debugLog('[V3Player] Event: waiting -> buffering', { readyState: videoEl.readyState, buff: bufferHealth.toFixed(1) });
      clearProbeConfirmation();
      clearHlsRenderProbe(false);
      setStatus('buffering');
      reportPlaybackWarning(PLAYBACK_WARNING_CODE_WAITING, 'waiting');
      scheduleNativeStallRecovery(videoEl, 'waiting');
      scheduleHlsStallRecovery(videoEl, 'waiting');
    };

    const onStalled = () => {
      if (decodeRecoveryInFlightRef.current) {
        debugLog('[V3Player] Event: stalled ignored during decode recovery');
        return;
      }

      let bufferHealth = 0;
      if (videoEl.buffered.length > 0) {
        for (let i = 0; i < videoEl.buffered.length; i++) {
          if (videoEl.currentTime >= videoEl.buffered.start(i) && videoEl.currentTime <= videoEl.buffered.end(i)) {
            bufferHealth = videoEl.buffered.end(i) - videoEl.currentTime;
            break;
          }
        }
      }

      if (bufferHealth > 1.0) {
        debugLog(`[V3Player] Event: stalled (ignored, buffer=${bufferHealth.toFixed(1)}s)`);
        clearNativeStallRecovery();
        clearHlsStallRecovery();
        return;
      }

      debugLog('[V3Player] Event: stalled -> buffering');
      clearProbeConfirmation();
      clearHlsRenderProbe(false);
      setStatus('buffering');
      reportPlaybackWarning(PLAYBACK_WARNING_CODE_STALLED, 'stalled');
      scheduleNativeStallRecovery(videoEl, 'stalled');
      scheduleHlsStallRecovery(videoEl, 'stalled');
    };

    const onSeeking = () => {
      if (decodeRecoveryInFlightRef.current) {
        debugLog('[V3Player] Event: seeking ignored during decode recovery');
        return;
      }

      clearNativeStallRecovery();
      clearHlsStallRecovery();
      clearProbeConfirmation();
      clearHlsRenderProbe(false);
      debugLog('[V3Player] Event: seeking -> buffering');
      setStatus('buffering');
    };

    const onPlaying = () => {
      debugLog('[V3Player] Event: playing -> playing');
      clearNativeStallRecovery();
      clearHlsStallRecovery();
      decodeRecoveryInFlightRef.current = false;
      decodeRecoveryAttemptsRef.current = 0;
      hlsStallRecoveryAttemptsRef.current = 0;
      const trackedSessionId = sessionIdRef.current;
      if (trackedSessionId && reportedPlayingSessionRef.current !== trackedSessionId) {
        reportedPlayingSessionRef.current = trackedSessionId;
        if (runtimeProbeActive) {
          reportedProbeStartedSessionRef.current = trackedSessionId;
          void reportError('info', PLAYBACK_INFO_CODE_PROBE_WINDOW_STARTED, 'probe_window_started');
        } else {
          void reportError('info', 200, 'playing');
        }
      } else if (trackedSessionId && pendingWarningRecoveryRef.current) {
        void reportError('info', pendingWarningRecoveryRef.current.code, pendingWarningRecoveryRef.current.message);
      }
      if (
        runtimeProbeActive &&
        trackedSessionId &&
        reportedProbeStartedSessionRef.current === trackedSessionId &&
        reportedProbeConfirmedSessionRef.current !== trackedSessionId
      ) {
        clearProbeConfirmation();
        probeConfirmationTimerRef.current = window.setTimeout(() => {
          if (sessionIdRef.current !== trackedSessionId) {
            return;
          }
          reportedProbeConfirmedSessionRef.current = trackedSessionId;
          void reportError('info', PLAYBACK_INFO_CODE_PROBE_WINDOW_CONFIRMED, 'probe_window_confirmed');
        }, PROBE_CONFIRMATION_MS);
      } else if (!runtimeProbeActive) {
        clearProbeConfirmation();
      }
      if (trackedSessionId && lastHlsEngineRef.current === 'hlsjs') {
        scheduleHlsRenderProbe(videoEl, trackedSessionId);
      } else {
        clearHlsRenderProbe(false);
      }
      pendingWarningRecoveryRef.current = null;
      reportedWarningKeysRef.current.clear();
      setStatus('playing');
      setError(null);
      setErrorDetails(null);
      setShowErrorDetails(false);
    };

    const onPause = () => {
      if (isTeardownRef.current) {
        return;
      }
      if (videoEl.dataset.xg2gManagedPause === '1') {
        debugLog('[V3Player] Event: pause (managed native veil hold ignored)');
        return;
      }
      clearNativeStallRecovery();
      clearHlsStallRecovery();
      clearProbeConfirmation();
      clearHlsRenderProbe(false);
      setStatus((prev) => (prev === 'error' ? prev : 'paused'));
    };

    const onSeeked = () => {
      clearNativeStallRecovery();
      clearHlsStallRecovery();
      setStatus((prev) => (prev === 'error' ? prev : (videoEl.paused ? 'paused' : 'playing')));
    };

    const onError = () => {
      if (isTeardownRef.current) return;
      if (!videoEl.currentSrc || videoEl.currentSrc === 'about:blank' || !videoEl.getAttribute('src')) return;

      const err = videoEl.error;
      const diagnostics = {
        code: err?.code,
        message: err?.message,
        currentSrc: videoEl.currentSrc,
        readyState: videoEl.readyState,
        networkState: videoEl.networkState,
        buffered: Array.from({ length: videoEl.buffered.length }, (_, i) => ({
          start: videoEl.buffered.start(i),
          end: videoEl.buffered.end(i)
        })),
        videoWidth: videoEl.videoWidth,
        videoHeight: videoEl.videoHeight,
        paused: videoEl.paused,
        hlsJsActive: !!hlsRef.current
      };

      debugError('[V3Player] Video Element Error:', diagnostics);
      clearNativeStallRecovery();
      clearHlsStallRecovery();
      clearProbeConfirmation();
      clearHlsRenderProbe(false);
      const presentation = classifyMediaElementError({
        code: err?.code,
        message: err?.message,
        currentSrc: videoEl.currentSrc,
        readyState: videoEl.readyState,
        networkState: videoEl.networkState,
        hlsJsActive: !!hlsRef.current,
      }, t);

      if (err && sessionIdRef.current) {
        const safeCode = typeof err.code === 'number' ? err.code : 0;
        const message = err.message || JSON.stringify(diagnostics);
        const shouldAttemptNativeRecovery =
          !hlsRef.current &&
          lastHlsEngineRef.current === 'native' &&
          (safeCode === 3 || safeCode === 4);

        if (shouldAttemptNativeRecovery && beginSessionDecodeRecovery(safeCode, message, () => {
          setStatus('error');
          setError(presentation.title);
          setErrorDetails(`${presentation.details} • native recovery failed`);
        })) {
          return;
        }

        void reportError('error', safeCode, message);
      }

      setStatus('error');
      setError(presentation.title);
      setErrorDetails(presentation.details);
    };

    videoEl.addEventListener('waiting', onWaiting);
    videoEl.addEventListener('stalled', onStalled);
    videoEl.addEventListener('seeking', onSeeking);
    videoEl.addEventListener('seeked', onSeeked);
    videoEl.addEventListener('playing', onPlaying);
    videoEl.addEventListener('pause', onPause);
    videoEl.addEventListener('error', onError);

    return () => {
      clearProbeConfirmation();
      clearHlsRenderProbe(false);
      videoEl.removeEventListener('waiting', onWaiting);
      videoEl.removeEventListener('stalled', onStalled);
      videoEl.removeEventListener('seeking', onSeeking);
      videoEl.removeEventListener('seeked', onSeeked);
      videoEl.removeEventListener('playing', onPlaying);
      videoEl.removeEventListener('pause', onPause);
      videoEl.removeEventListener('error', onError);
    };
  }, [beginSessionDecodeRecovery, clearHlsRenderProbe, clearHlsStallRecovery, clearNativeStallRecovery, clearProbeConfirmation, hlsRef, isTeardownRef, reportError, reportPlaybackWarning, runtimeProbeActive, scheduleHlsRenderProbe, scheduleHlsStallRecovery, scheduleNativeStallRecovery, sessionIdRef, setError, setErrorDetails, setShowErrorDetails, setStatus, videoRef]);

  return {
    resetPlaybackEngine,
    playHls,
    playDirectMp4,
    waitForDirectStream
  };
}
