import { useCallback, useEffect, useRef } from 'react';
import type { Dispatch, MutableRefObject, RefObject, SetStateAction } from 'react';
import type { TFunction } from 'i18next';
import Hls from './lib/hlsRuntime';
import type { ErrorData, FragLoadedData, ManifestParsedData, LevelLoadedData } from 'hls.js';
import type { HlsInstanceRef, PlayerStats, PlayerStatus, V3SessionStatusResponse, VideoElementRef, PlayerAudioTrack } from '../../types/v3-player';
import type { AppError } from '../../types/errors';
import type { PlaybackEngineErrorContext } from '../../client-ts';
import { debugError, debugLog, debugWarn } from '../../utils/logging';
import type { PlaybackFailureReportOptions } from './semantics/playbackFailureSemantics';
import { classifyHlsFatalError, classifyMediaElementError } from './playbackErrorPresentation';
import {
  describeHlsRenderProbe,
  isBlackRenderSuspect,
  readPlaybackFrameCounters,
  type HlsRenderProbeSnapshot,
} from './playbackRenderProbe';
import { isInMemorySeekTarget } from './orchestrator/nativePlaybackHelpers';

type PlaybackEngineName = 'auto' | 'native' | 'hlsjs';
type ReportErrorFn = (
  event: 'error' | 'warning' | 'info',
  code: number,
  msg?: string,
  context?: PlaybackEngineErrorContext,
) => Promise<void>;
type PreferNativeFn = (videoEl?: VideoElementRef, hlsJsSupported?: boolean) => boolean;
type WaitForSessionReadyFn = (sessionId: string, maxAttempts?: number) => Promise<V3SessionStatusResponse>;
type PrimePlaybackAuthFn = (playbackUrl: string, source: string) => Promise<void>;

const NATIVE_STALL_RECOVERY_MS = 2500;
const HLS_STALL_RECOVERY_MS = 2200;
const PLAYBACK_WARNING_CODE_WAITING = 101;
const PLAYBACK_WARNING_CODE_STALLED = 102;
const PLAYBACK_WARNING_CODE_DECODER_RECOVERY = 103;
const PLAYBACK_WARNING_CODE_NETWORK_RETRY = 104;
const PLAYBACK_WARNING_CODE_HLS_NONFATAL = 105;
const PLAYBACK_WARNING_CODE_HLS_LEADGAP = 106;

// hls.js ErrorDetails (string-valued) for the non-fatal events that manifest a
// live stall / rough cold-start. hls.js self-recovers from these, so they were
// dropped silently - which left the backend blind to WHY playback froze. We now
// surface this stall class to server telemetry for diagnosis.
const NON_FATAL_STALL_DETAILS = new Set<string>([
  'bufferStalledError',
  'bufferSeekOverHole',
  'bufferNudgeOnStall',
  'bufferAppendError',
]);
const PLAYBACK_INFO_CODE_RECOVERED_BUFFERING = 211;
const PLAYBACK_INFO_CODE_RECOVERED_NETWORK = 212;
const PLAYBACK_INFO_CODE_RECOVERED_DECODER = 213;
const PLAYBACK_INFO_CODE_PROBE_WINDOW_STARTED = 220;
const PLAYBACK_INFO_CODE_PROBE_WINDOW_CONFIRMED = 221;
const PLAYBACK_INFO_CODE_HLSJS_RENDER_PLAYING = 240;
const PLAYBACK_INFO_CODE_HLSJS_RENDER_STABLE = 241;
const PLAYBACK_INFO_CODE_HLSJS_RENDER_BLACK = 242;
const PLAYBACK_INFO_CODE_HLSJS_RENDER_HEARTBEAT = 243;
const PROBE_CONFIRMATION_MS = 10_000;
const HLSJS_RENDER_PROBE_MS = 2_500;
const HLSJS_RENDER_HEARTBEAT_MS = 30_000;
// Stall-class warnings repeat throughout a live session (each stutter is one
// event); a strict once-per-session dedup hides all but the first, so allow
// re-reporting after a cooldown instead.
const PLAYBACK_WARNING_REPEAT_MS = 10_000;

interface UsePlaybackEngineProps {
  videoRef: RefObject<VideoElementRef>;
  hlsRef: MutableRefObject<HlsInstanceRef>;
  sessionIdRef: MutableRefObject<string | null>;
  isTeardownRef: MutableRefObject<boolean>;
  lastDecodedRef: MutableRefObject<number>;
  playbackEpochRef: MutableRefObject<number>;
  t: TFunction;
  reportError: ReportErrorFn;
  waitForSessionReady: WaitForSessionReadyFn;
  shouldPreferNativeHls: PreferNativeFn;
  primePlaybackAuth?: PrimePlaybackAuthFn;
  onPlaybackMilestone?: (milestone: 'manifest' | 'firstFrame') => void;
  runtimeProbeActive?: boolean;
  revealHoldMs?: number;
  setStats: Dispatch<SetStateAction<PlayerStats>>;
  setStatus: Dispatch<SetStateAction<PlayerStatus>>;
  clearPlaybackFailure: () => void;
  reportPlaybackFailure: (error: AppError, options?: PlaybackFailureReportOptions) => void;
  dispatchPlayerAction?: (action: any) => void;
  onAudioTracksUpdated?: (tracks: PlayerAudioTrack[]) => void;
  onAudioTrackSwitched?: (trackId: number) => void;
}

interface PlaybackEngineController {
  resetPlaybackEngine: () => void;
  playHls: (url: string, engine?: PlaybackEngineName) => void;
  playDirectMp4: (url: string) => void;
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
  playbackEpochRef,
  t,
  reportError,
  waitForSessionReady,
  shouldPreferNativeHls,
  primePlaybackAuth,
  onPlaybackMilestone,
  runtimeProbeActive = false,
  revealHoldMs,
  setStats,
  setStatus,
  clearPlaybackFailure,
  reportPlaybackFailure,
  onAudioTracksUpdated,
  onAudioTrackSwitched
}: UsePlaybackEngineProps): PlaybackEngineController {
  const lastHlsUrlRef = useRef<string | null>(null);
  const lastHlsEngineRef = useRef<PlaybackEngineName>('auto');
  const replayHlsRef = useRef<((url: string, engine?: PlaybackEngineName) => void) | null>(null);
  const decodeRecoveryInFlightRef = useRef(false);
  const decodeRecoveryAttemptsRef = useRef(0);
  const pendingNativeAutoplayRef = useRef<(() => void) | null>(null);
  const nativeStallRecoveryTimerRef = useRef<number | null>(null);
  const revealHoldRef = useRef(false);
  const revealTimerRef = useRef<number | null>(null);
  const hlsStallRecoveryTimerRef = useRef<number | null>(null);
  const hlsStallRecoveryAttemptsRef = useRef(0);
  const reportedPlayingSessionRef = useRef<string | null>(null);
  const reportedWarningKeysRef = useRef<Map<string, number>>(new Map());
  const pendingWarningRecoveryRef = useRef<{ code: number; message: string } | null>(null);
  const reportedProbeStartedSessionRef = useRef<string | null>(null);
  const reportedProbeConfirmedSessionRef = useRef<string | null>(null);
  const probeConfirmationTimerRef = useRef<number | null>(null);
  const activeHlsRenderProbeSessionRef = useRef<string | null>(null);
  const completedHlsRenderProbeSessionRef = useRef<string | null>(null);
  const hlsRenderProbeTimerRef = useRef<number | null>(null);
  const hlsRenderHeartbeatTimerRef = useRef<number | null>(null);
  const hlsRenderHeartbeatSessionRef = useRef<string | null>(null);
  const lastHlsRenderSnapshotRef = useRef<HlsRenderProbeSnapshot | null>(null);
  const networkRetryTimerRef = useRef<number | null>(null);

  const reportMediaFailure = useCallback((error: AppError, options: PlaybackFailureReportOptions = {}) => {
    reportPlaybackFailure(error, {
      source: 'media-element',
      ...options,
    });
  }, [reportPlaybackFailure]);

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
      onPlaybackMilestone?.('manifest');
      video.play().catch((err) => {
        debugWarn(label, err);
        // Autoplay was rejected (e.g. Safari/iOS gesture policy or Low-Power-Mode).
        // Mirror the hls.js path: clear the startup overlay and surface the play
        // control instead of leaving the status pinned on 'buffering' forever.
        setStatus((prev) => (prev === 'error' ? prev : 'ready'));
      });
    };

    pendingNativeAutoplayRef.current = onLoadedMetadata;
    video.addEventListener('loadedmetadata', onLoadedMetadata, { once: true });
  }, [clearPendingNativeAutoplay, setStatus]);

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
        reportPlaybackFailure({
          title: err instanceof Error && err.message ? err.message : t('player.authFailed'),
          detail: 'native_hls_auth_prime_failed',
          retryable: false,
          code: 'AUTH_PRIME_FAILED',
        }, {
          source: 'native-host',
          code: 'AUTH_PRIME_FAILED',
        });
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
  }, [isTeardownRef, primePlaybackAuth, reportPlaybackFailure, scheduleNativeAutoplay, sessionIdRef, setStatus, t, videoRef]);

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

  const clearNetworkRetry = useCallback(() => {
    if (networkRetryTimerRef.current !== null) {
      window.clearTimeout(networkRetryTimerRef.current);
      networkRetryTimerRef.current = null;
    }
  }, []);

  const clearHlsRenderProbe = useCallback((resetCompleted: boolean = false) => {
    if (hlsRenderProbeTimerRef.current !== null) {
      window.clearTimeout(hlsRenderProbeTimerRef.current);
      hlsRenderProbeTimerRef.current = null;
    }
    // The heartbeat must survive transient events (waiting/stalled/seeking
    // clear the probe with resetCompleted=false) — it is the instrument that
    // observes exactly those stalls. Only a real teardown/session switch
    // (resetCompleted=true) stops it.
    if (resetCompleted) {
      if (hlsRenderHeartbeatTimerRef.current !== null) {
        window.clearInterval(hlsRenderHeartbeatTimerRef.current);
        hlsRenderHeartbeatTimerRef.current = null;
      }
      hlsRenderHeartbeatSessionRef.current = null;
      lastHlsRenderSnapshotRef.current = null;
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
      playbackRate: videoEl.playbackRate,
      totalFrames: counters.totalFrames,
      droppedFrames: counters.droppedFrames,
    };
  }, [bufferedAheadSeconds]);

  const playbackEngineContext = useCallback((
    phase: NonNullable<PlaybackEngineErrorContext['phase']>,
    extras?: { engine?: PlaybackEngineErrorContext['engine']; recoveryAttempt?: number },
  ): PlaybackEngineErrorContext => {
    const liveEngine = lastHlsEngineRef.current;
    const engine: PlaybackEngineErrorContext['engine'] =
      extras?.engine ?? (liveEngine === 'auto' ? 'unknown' : liveEngine);
    const epoch = playbackEpochRef.current;
    const ctx: PlaybackEngineErrorContext = {
      engine,
      phase,
      playbackEpoch: epoch,
      attemptId: epoch,
    };
    if (extras?.recoveryAttempt !== undefined) {
      ctx.recoveryAttempt = extras.recoveryAttempt;
    }
    return ctx;
  }, [playbackEpochRef]);

  const scheduleHlsRenderProbe = useCallback((videoEl: NonNullable<VideoElementRef>, trackedSessionId: string) => {
    if (lastHlsEngineRef.current !== 'hlsjs') {
      clearHlsRenderProbe(false);
      return;
    }

    // Session-long heartbeat, started on the first 'playing' event and
    // independent of the one-shot startup probe below (an early seek/waiting
    // cancels that probe and 'playing' may never fire again, so the heartbeat
    // must not depend on the probe completing). dt/df against the previous
    // beat expose buffer drain, frame drops, and playhead stalls.
    if (hlsRenderHeartbeatSessionRef.current !== trackedSessionId) {
      if (hlsRenderHeartbeatTimerRef.current !== null) {
        window.clearInterval(hlsRenderHeartbeatTimerRef.current);
      }
      hlsRenderHeartbeatSessionRef.current = trackedSessionId;
      lastHlsRenderSnapshotRef.current = captureHlsRenderProbeSnapshot(videoEl);
      hlsRenderHeartbeatTimerRef.current = window.setInterval(() => {
        if (
          isTeardownRef.current ||
          sessionIdRef.current !== trackedSessionId ||
          lastHlsEngineRef.current !== 'hlsjs'
        ) {
          if (hlsRenderHeartbeatTimerRef.current !== null) {
            window.clearInterval(hlsRenderHeartbeatTimerRef.current);
            hlsRenderHeartbeatTimerRef.current = null;
          }
          hlsRenderHeartbeatSessionRef.current = null;
          lastHlsRenderSnapshotRef.current = null;
          return;
        }
        const beat = captureHlsRenderProbeSnapshot(videoEl);
        void reportError(
          'info',
          PLAYBACK_INFO_CODE_HLSJS_RENDER_HEARTBEAT,
          describeHlsRenderProbe('heartbeat', beat, lastHlsRenderSnapshotRef.current ?? undefined),
          playbackEngineContext('decode', { engine: 'hlsjs' }),
        );
        lastHlsRenderSnapshotRef.current = beat;
      }, HLSJS_RENDER_HEARTBEAT_MS);
    }

    if (
      completedHlsRenderProbeSessionRef.current === trackedSessionId ||
      activeHlsRenderProbeSessionRef.current === trackedSessionId
    ) {
      return;
    }

    const started = captureHlsRenderProbeSnapshot(videoEl);
    activeHlsRenderProbeSessionRef.current = trackedSessionId;
    void reportError(
      'info',
      PLAYBACK_INFO_CODE_HLSJS_RENDER_PLAYING,
      describeHlsRenderProbe('playing', started),
      playbackEngineContext('decode', { engine: 'hlsjs' }),
    );

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
        describeHlsRenderProbe(blackSuspect ? 'black_suspect' : 'stable', settled, started),
        playbackEngineContext('decode', { engine: 'hlsjs' }),
      );
    }, HLSJS_RENDER_PROBE_MS);
  }, [captureHlsRenderProbeSnapshot, clearHlsRenderProbe, isTeardownRef, playbackEngineContext, reportError, sessionIdRef]);

  const reportPlaybackWarning = useCallback((
    code: number,
    message: string,
    phase: NonNullable<PlaybackEngineErrorContext['phase']>,
    recoveryAttempt?: number,
    repeatable?: boolean,
  ) => {
    const trackedSessionId = sessionIdRef.current;
    if (!trackedSessionId) {
      return;
    }
    const key = `${trackedSessionId}:${code}`;
    const lastReportedAt = reportedWarningKeysRef.current.get(key);
    if (lastReportedAt !== undefined) {
      if (!repeatable || Date.now() - lastReportedAt < PLAYBACK_WARNING_REPEAT_MS) {
        return;
      }
    }
    reportedWarningKeysRef.current.set(key, Date.now());
    pendingWarningRecoveryRef.current = playbackRecoveryInfoForWarning(code);
    void reportError('warning', code, message, playbackEngineContext(
      phase,
      recoveryAttempt !== undefined ? { recoveryAttempt } : undefined,
    ));
  }, [playbackEngineContext, reportError, sessionIdRef]);

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
      clearNetworkRetry();
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
      onAudioTracksUpdated?.([]);
      onAudioTrackSwitched?.(-1);
    } finally {
      window.setTimeout(() => {
        isTeardownRef.current = false;
      }, 50);
    }
  }, [clearHlsRenderProbe, clearHlsStallRecovery, clearNativeStallRecovery, clearNetworkRetry, clearPendingNativeAutoplay, clearProbeConfirmation, hlsRef, isTeardownRef, onAudioTrackSwitched, onAudioTracksUpdated, videoRef]);

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
    clearPlaybackFailure();

    const trackedRecoveryAttempt = decodeRecoveryAttemptsRef.current;
    void (async () => {
      try {
        await reportError('error', code, message, playbackEngineContext('recovery', {
          engine: trackedEngine === 'auto' ? 'unknown' : trackedEngine,
          recoveryAttempt: trackedRecoveryAttempt,
        }));
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
        clearPlaybackFailure();
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
    playbackEngineContext,
    reportError,
    resetPlaybackEngine,
    sessionIdRef,
    clearPlaybackFailure,
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
        reportMediaFailure({
          title: t('player.networkError'),
          detail: `${trigger} (native stall recovery failed)`,
          retryable: true,
          code: 'NATIVE_STALL_RECOVERY_FAILED',
        }, {
          code: 'NATIVE_STALL_RECOVERY_FAILED',
          recoverable: true,
          terminal: false,
        });
      });

      if (!started) {
        setStatus('error');
        reportMediaFailure({
          title: t('player.networkError'),
          detail: `${trigger} (native stall recovery failed)`,
          retryable: true,
          code: 'NATIVE_STALL_RECOVERY_FAILED',
        }, {
          code: 'NATIVE_STALL_RECOVERY_FAILED',
          recoverable: true,
          terminal: false,
        });
      }
    }, NATIVE_STALL_RECOVERY_MS);
  }, [
    beginSessionDecodeRecovery,
    bufferedAheadSeconds,
    reportMediaFailure,
    hlsRef,
    isTeardownRef,
    sessionIdRef,
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
          reportMediaFailure({
            title: t('player.networkError'),
            detail: `${trigger} (hls.js stall recovery failed)`,
            retryable: true,
            code: 'HLSJS_STALL_RECOVERY_FAILED',
          }, {
            code: 'HLSJS_STALL_RECOVERY_FAILED',
            recoverable: true,
            terminal: false,
          });
        });
        if (!started) {
          setStatus('error');
          reportMediaFailure({
            title: t('player.networkError'),
            detail: `${trigger} (hls.js stall recovery failed)`,
            retryable: true,
            code: 'HLSJS_STALL_RECOVERY_FAILED',
          }, {
            code: 'HLSJS_STALL_RECOVERY_FAILED',
            recoverable: true,
            terminal: false,
          });
        }
      }
    }, HLS_STALL_RECOVERY_MS);
  }, [
    beginSessionDecodeRecovery,
    bufferedAheadSeconds,
    bufferedTailSeconds,
    hlsRef,
    isTeardownRef,
    reportMediaFailure,
    sessionIdRef,
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
    revealHoldRef.current = false;
    if (revealTimerRef.current !== null) {
      window.clearTimeout(revealTimerRef.current);
      revealTimerRef.current = null;
    }
    video.playbackRate = 1;
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
        // Own the buffer via ManagedMediaSource on Safari 17.1+ (the app-owned path
        // the migration relies on); pinned so a future hls.js default change can't
        // silently fall back to plain MSE (which breaks AirPlay and the MMS lifecycle).
        // ALWAYS prefer MMS. Safari requires MMS for AV1 decoding.
        // If MMS blocks sourceopen in background tabs, the backend session might rot,
        // but it will seamlessly recover when foregrounded instead of permanently failing
        // on unsupported plain MSE for AV1.
        preferManagedMediaSource: true,
        enableWorker: true,
        // Engages only when the playlist advertises EXT-X-PART (server flag
        // hls.lowLatency); on regular playlists this is a no-op, so the
        // stable non-LL path is unchanged.
        lowLatencyMode: true,
        backBufferLength: 300,
        maxBufferLength: 60,
        capLevelToPlayerSize: true,
        liveSyncDuration: 12,
        // Rate-based live catch-up is disabled: heartbeat telemetry showed the
        // latency controller periodically driving playbackRate to 1.05, which
        // Safari renders as visible judder (50fps video drops to ~29 eff. fps)
        // and time-compressed audio — the recurring "stutter + audio dropout".
        // With a multi-hour DVR window, slowly drifting behind the live edge
        // is harmless; 1 keeps hls.js from ever touching playbackRate.
        maxLiveSyncPlaybackRate: 1,
        // Broadcast copy/passthrough sources (DVB relay) deliver imperfect DTS,
        // so the muxed segments carry small timestamp gaps ("Invalid DTS …
        // replacing by guess"). hls.js's default maxBufferHole (0.1s) is too
        // tight to jump those, stranding live TV at the gap until the nudge
        // retries are exhausted ("hlsjs stall recovery failed"). Widen the
        // hole-skip and give the nudge a few more tries so playback rides over a
        // bad-DTS gap instead of stalling. No effect on clean streams: with no
        // buffer hole, none of these engage.
        maxBufferHole: 1.0,
        nudgeOffset: 0.2,
        nudgeMaxRetry: 6
      });
      hlsRef.current = hls;

      // Live startup gate: on a fresh live session the encoder edge is only
      // ~1 segment ahead, and starting playback the moment the first segment
      // is buffered leaves zero headroom — every PDT/encoder jitter then
      // surfaces as an immediate bufferStalledError (visible stall + recovery
      // jolt seconds after start). Live input is realtime-paced, so headroom
      // can only come from waiting: hold play() until a small buffer target
      // exists (or a cap elapses). VOD playlists open the gate immediately.
      const START_GATE_TARGET_SECONDS = 4.5;
      const START_GATE_TIMEOUT_MS = 6000;
      const SLOW_BUILD_RATE = 0.955;
      const SLOW_BUILD_TARGET_AHEAD_SECONDS = 8;
      const SLOW_BUILD_MAX_MS = 150000;
      let slowBuildActive = false;
      let slowBuildTimer: number | null = null;
      let startGateOpen = false;
      let startGateTimer: number | null = null;
      const bufferedAheadSeconds = (): number => {
        const gateVideo = videoRef.current;
        if (!gateVideo || gateVideo.buffered.length === 0) {
          return 0;
        }
        const from = Math.max(gateVideo.currentTime, gateVideo.buffered.start(0));
        return gateVideo.buffered.end(gateVideo.buffered.length - 1) - from;
      };
      const restorePlaybackRate = (reason: string) => {
        if (!slowBuildActive) {
          return;
        }
        slowBuildActive = false;
        if (slowBuildTimer !== null) {
          window.clearTimeout(slowBuildTimer);
          slowBuildTimer = null;
        }
        const rateVideo = videoRef.current;
        if (rateVideo && hlsRef.current === hls && rateVideo.playbackRate !== 1) {
          rateVideo.playbackRate = 1;
          debugLog('[V3Player] Slow-build complete', {
            reason,
            bufferedAhead: bufferedAheadSeconds().toFixed(2),
          });
        }
      };
      const openStartGate = (reason: string) => {
        if (startGateOpen) {
          return;
        }
        startGateOpen = true;
        if (startGateTimer !== null) {
          window.clearTimeout(startGateTimer);
          startGateTimer = null;
        }
        if (hlsRef.current !== hls) {
          return;
        }
        debugLog('[V3Player] Startup gate open', { reason, bufferedAhead: bufferedAheadSeconds().toFixed(2) });
        const gateVideo = videoRef.current;
        if (reason !== 'vod' && gateVideo && bufferedAheadSeconds() < SLOW_BUILD_TARGET_AHEAD_SECONDS) {
          slowBuildActive = true;
          gateVideo.playbackRate = SLOW_BUILD_RATE;
          slowBuildTimer = window.setTimeout(() => restorePlaybackRate('timeout'), SLOW_BUILD_MAX_MS);
        }
        videoRef.current?.play().catch((err) => {
          debugWarn('[V3Player] Autoplay failed', err);
          setStatus('ready');
        });
      };
      // The element-level autoplay attribute would bypass the gate (the
      // browser starts playback as soon as it deems readyState sufficient),
      // so playback ownership moves entirely to the gated play() call.
      video.autoplay = false;
      revealHoldRef.current = true;
      setStatus('buffering');

      hls.on(Hls.Events.LEVEL_SWITCHED, () => updateStats(hls));
      hls.on(Hls.Events.MANIFEST_PARSED, (_event, data: ManifestParsedData) => {
        onPlaybackMilestone?.('manifest');
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
        startGateTimer = window.setTimeout(() => openStartGate('timeout'), START_GATE_TIMEOUT_MS);
      });

      hls.on(Hls.Events.BUFFER_APPENDED, () => {
        if (!startGateOpen && bufferedAheadSeconds() >= START_GATE_TARGET_SECONDS) {
          openStartGate('buffer_target');
        }
        if (slowBuildActive && bufferedAheadSeconds() >= SLOW_BUILD_TARGET_AHEAD_SECONDS) {
          restorePlaybackRate('target_reached');
        }
      });

      hls.on(Hls.Events.LEVEL_LOADED, (_event, data: LevelLoadedData) => {
        if (data.details.live === false) {
          revealHoldRef.current = false;
          if (!startGateOpen) {
            openStartGate('vod');
          }
        }
        const hasContent = data.details.totalduration > 0 || (data.details.fragments && data.details.fragments.length > 0);
        setStatus((prev) => {
          if (revealHoldRef.current) {
            return prev;
          }
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

      hls.on(Hls.Events.AUDIO_TRACKS_UPDATED, (_event, data) => {
        if (data.audioTracks && onAudioTracksUpdated) {
          onAudioTracksUpdated(data.audioTracks.map(t => ({ id: t.id, name: t.name, language: t.lang, key: 'hls-' + t.id, engineIndex: t.id })));
        }
      });
      hls.on(Hls.Events.AUDIO_TRACK_SWITCHED, (_event, data) => {
        if (onAudioTrackSwitched) {
          onAudioTrackSwitched(data.id);
        }
      });

      let mediaRecoveryAttempted = false;
      let networkRetryCount = 0;
      const maxNetworkRetries = 6;
      const networkBackoffCapMs = 30_000;

      hls.on(Hls.Events.ERROR, (_event, data: ErrorData) => {
        if (!data.fatal) {
          // Non-fatal hls.js events are how a live stall / rough cold-start
          // manifests, but were dropped silently here - leaving the backend
          // blind to WHY playback froze. Surface the stall class to server
          // telemetry (deduped per session by reportPlaybackWarning) so the
          // reason is diagnosable from logs. Recovery behaviour is unchanged:
          // hls.js still self-recovers; this only adds observability.
          if (NON_FATAL_STALL_DETAILS.has(data.details as string)) {
            const sv = videoRef.current;
            const sct = sv ? sv.currentTime : -1;
            const srs = sv ? sv.readyState : -1;
            let sranges = '';
            if (sv) {
              try {
                for (let i = 0; i < sv.buffered.length; i++) {
                  sranges += sv.buffered.start(i).toFixed(2) + '-' + sv.buffered.end(i).toFixed(2) + ';';
                }
              } catch {
                sranges = 'unknown';
              }
            }
            reportPlaybackWarning(
              PLAYBACK_WARNING_CODE_HLS_NONFATAL,
              `hls_nonfatal:${data.details} ct=${sct.toFixed(2)} rs=${srs} buffered=[${sranges}]`,
              'decode',
              undefined,
              true,
            );
            // Cold-start leading-gap unstick: when the first playable (audio+video)
            // data begins a beat after video (cold tune - the audio transcode starts
            // ~1-2s in), the playhead sits at 0 BEFORE the buffer with a gap wider than
            // maxBufferHole, so the element stalls at 0 and hls.js will not auto-jump a
            // gap that large. Seek to the start of real data so playback begins at once.
            // Scoped to the startup leading gap (playhead before all buffer, near 0) so
            // it never touches DVR seeks, which always land inside the buffered range.
            try {
              if (sv && sv.buffered.length > 0) {
                const leadStart = sv.buffered.start(0);
                if (sv.currentTime < leadStart - 0.1 && sv.currentTime < 5) {
                  reportPlaybackWarning(
                    PLAYBACK_WARNING_CODE_HLS_LEADGAP,
                    `hls_leadgap_seek ${sv.currentTime.toFixed(2)}->${leadStart.toFixed(2)}`,
                    'decode',
                  );
                  sv.currentTime = leadStart + 0.05;
                }
              }
            } catch {
              // Ignore DOMExceptions when accessing buffered ranges during error states
            }
          }
          return;
        }

        const presentation = classifyHlsFatalError(data, t, lastHlsUrlRef.current);
        const httpStatus = extractHlsHttpStatus(data);
        switch (data.type) {
          case Hls.ErrorTypes.NETWORK_ERROR:
            if (httpStatus === 401 && sessionIdRef.current) {
              void reportError('error', 0, `${data.type}: ${data.details}`, playbackEngineContext('network', { engine: 'hlsjs' }));
              debugWarn('[V3Player] NETWORK_ERROR 401: attempting session recovery before failing');
              const started = beginSessionDecodeRecovery(0, `${data.type}: ${data.details}`, (recoveryErr) => {
                debugError('[V3Player] NETWORK_ERROR 401: session recovery failed', recoveryErr);
                hlsRef.current?.destroy();
                hlsRef.current = null; // null the ref so pending retry/stall timers (guarded by hlsRef.current !== hls / !hls) bail instead of calling startLoad() on a destroyed instance
                setStatus('error');
                reportPlaybackFailure({
                  title: recoveryErr instanceof Error && recoveryErr.message
                    ? recoveryErr.message
                    : presentation.title,
                  detail: presentation.details,
                  status: recoveryErr && typeof recoveryErr === 'object' && 'status' in recoveryErr
                    ? (recoveryErr as { status?: number }).status
                    : httpStatus,
                  retryable: false,
                  code: recoveryErr && typeof recoveryErr === 'object' && 'code' in recoveryErr
                    ? ((recoveryErr as { code?: string }).code ?? undefined)
                    : 'AUTH_RECOVERY_FAILED',
                }, {
                  source: 'native-host',
                  code: recoveryErr && typeof recoveryErr === 'object' && 'code' in recoveryErr
                    ? ((recoveryErr as { code?: string }).code ?? undefined)
                    : 'AUTH_RECOVERY_FAILED',
                });
              });
              if (!started) {
                hlsRef.current?.destroy();
                hlsRef.current = null; // null the ref so pending retry/stall timers (guarded by hlsRef.current !== hls / !hls) bail instead of calling startLoad() on a destroyed instance
                setStatus('error');
                reportMediaFailure({
                  title: presentation.title,
                  detail: presentation.details,
                  status: httpStatus,
                  retryable: true,
                  code: 'HLS_NETWORK_AUTH_BLOCKED',
                }, {
                  code: 'HLS_NETWORK_AUTH_BLOCKED',
                  recoverable: true,
                  terminal: false,
                });
              }
              return;
            }
            if (networkRetryCount < maxNetworkRetries) {
              const backoffMs = Math.min(1000 * Math.pow(2, networkRetryCount), networkBackoffCapMs);
              networkRetryCount++;
              reportPlaybackWarning(PLAYBACK_WARNING_CODE_NETWORK_RETRY, 'hlsjs_network_retry', 'network', networkRetryCount);
              debugWarn(`[V3Player] NETWORK_ERROR recovery attempt ${networkRetryCount}/${maxNetworkRetries}, backoff ${backoffMs}ms`);
              setStatus('recovering');
              networkRetryTimerRef.current = window.setTimeout(() => {
                networkRetryTimerRef.current = null;
                // The engine may have been torn down or replaced during the
                // backoff window; calling startLoad() on a destroyed instance
                // throws and pins it in memory.
                if (isTeardownRef.current || hlsRef.current !== hls) {
                  return;
                }
                hls.startLoad();
              }, backoffMs);
            } else {
              if (sessionIdRef.current) {
                void reportError('error', 0, `${data.type}: ${data.details}`, playbackEngineContext('network', {
                  engine: 'hlsjs',
                  recoveryAttempt: networkRetryCount,
                }));
              }
              debugError(`[V3Player] NETWORK_ERROR: max retries (${maxNetworkRetries}) exhausted`);
              hlsRef.current?.destroy();
              hlsRef.current = null; // null the ref so pending retry/stall timers bail instead of calling startLoad() on a destroyed instance
              setStatus('error');
              reportMediaFailure({
                title: presentation.title,
                detail: `${presentation.details} • ${maxNetworkRetries} retries exhausted`,
                status: httpStatus,
                retryable: true,
                code: 'HLS_NETWORK_RETRIES_EXHAUSTED',
              }, {
                code: 'HLS_NETWORK_RETRIES_EXHAUSTED',
                recoverable: true,
                terminal: false,
              });
            }
            break;
          case Hls.ErrorTypes.MEDIA_ERROR:
            if (!mediaRecoveryAttempted) {
              mediaRecoveryAttempted = true;
              reportPlaybackWarning(PLAYBACK_WARNING_CODE_DECODER_RECOVERY, 'hlsjs_media_recovery', 'recovery', 1);
              debugWarn('[V3Player] MEDIA_ERROR: attempting single recovery');
              setStatus('recovering');
              hls.recoverMediaError();
            } else {
              debugWarn('[V3Player] MEDIA_ERROR: local recovery exhausted, attempting session reattach');
              const started = beginSessionDecodeRecovery(3, `${data.type}: ${data.details}`, () => {
                debugError('[V3Player] MEDIA_ERROR: session reattach failed, failing terminally');
                hlsRef.current?.destroy();
                hlsRef.current = null; // null the ref so pending retry/stall timers (guarded by hlsRef.current !== hls / !hls) bail instead of calling startLoad() on a destroyed instance
                setStatus('error');
                reportMediaFailure({
                  title: presentation.title,
                  detail: `${presentation.details} • media recovery failed`,
                  retryable: true,
                  code: 'HLS_MEDIA_RECOVERY_FAILED',
                }, {
                  code: 'HLS_MEDIA_RECOVERY_FAILED',
                  recoverable: true,
                  terminal: false,
                });
              });
              if (!started) {
                debugError('[V3Player] MEDIA_ERROR: recovery already attempted, failing terminally');
                hlsRef.current?.destroy();
                hlsRef.current = null; // null the ref so pending retry/stall timers (guarded by hlsRef.current !== hls / !hls) bail instead of calling startLoad() on a destroyed instance
                setStatus('error');
                reportMediaFailure({
                  title: presentation.title,
                  detail: `${presentation.details} • media recovery failed`,
                  retryable: true,
                  code: 'HLS_MEDIA_RECOVERY_FAILED',
                }, {
                  code: 'HLS_MEDIA_RECOVERY_FAILED',
                  recoverable: true,
                  terminal: false,
                });
              }
            }
            break;
          default:
            if (sessionIdRef.current) {
              void reportError('error', 0, `${data.type}: ${data.details}`, playbackEngineContext('decode', { engine: 'hlsjs' }));
            }
            hlsRef.current?.destroy();
            hlsRef.current = null; // null the ref so pending retry/stall timers bail instead of calling startLoad() on a destroyed instance
            setStatus('error');
            reportMediaFailure({
              title: presentation.title,
              detail: presentation.details,
              status: httpStatus,
              retryable: true,
              code: 'HLS_FATAL_ERROR',
            }, {
              code: 'HLS_FATAL_ERROR',
            });
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
  }, [beginSessionDecodeRecovery, clearHlsRenderProbe, clearHlsStallRecovery, clearNativeStallRecovery, clearPendingNativeAutoplay, hlsRef, isTeardownRef, lastDecodedRef, playbackEngineContext, reportError, reportPlaybackWarning, sessionIdRef, setStats, setStatus, shouldPreferNativeHls, startNativeHlsPlayback, t, updateStats, videoRef]);

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
    revealHoldRef.current = false;
    if (revealTimerRef.current !== null) {
      window.clearTimeout(revealTimerRef.current);
      revealTimerRef.current = null;
    }
    video.playbackRate = 1;

    lastHlsUrlRef.current = null;
    lastHlsEngineRef.current = 'auto';
    lastDecodedRef.current = 0;
    setStats((prev) => ({ ...prev, bandwidth: 0, resolution: 'Original (Direct)', fps: 0, levelIndex: -1 }));
    debugLog('[V3Player] Switching to Direct MP4 Mode:', url);
    video.src = url;
    video.load();
    video.play().catch((err) => {
      debugWarn('Autoplay failed', err);
      // Autoplay rejected: clear the startup overlay and show the play control rather
      // than staying stuck on 'buffering' (mirrors the hls.js and native-HLS paths).
      setStatus((prev) => (prev === 'error' ? prev : 'ready'));
    });
  }, [clearHlsRenderProbe, clearHlsStallRecovery, clearNativeStallRecovery, clearPendingNativeAutoplay, hlsRef, lastDecodedRef, setStats, setStatus, videoRef]);

  useEffect(() => {
    const videoEl = videoRef.current;
    if (!videoEl) return;

    const cancelPendingReveal = () => {
      if (revealTimerRef.current !== null) {
        window.clearTimeout(revealTimerRef.current);
        revealTimerRef.current = null;
      }
    };

    const onWaiting = () => {
      if (decodeRecoveryInFlightRef.current) {
        debugLog('[V3Player] Event: waiting ignored during decode recovery');
        return;
      }
      if (revealHoldRef.current) {
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
      cancelPendingReveal();
      clearProbeConfirmation();
      clearHlsRenderProbe(false);
      setStatus('buffering');
      reportPlaybackWarning(PLAYBACK_WARNING_CODE_WAITING, 'waiting', 'decode');
      scheduleNativeStallRecovery(videoEl, 'waiting');
      scheduleHlsStallRecovery(videoEl, 'waiting');
    };

    const onStalled = () => {
      if (decodeRecoveryInFlightRef.current) {
        debugLog('[V3Player] Event: stalled ignored during decode recovery');
        return;
      }
      if (revealHoldRef.current) {
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

      if (bufferHealth > 0.5 || (!videoEl.paused && videoEl.readyState >= 3)) {
        debugLog(`[V3Player] Event: stalled (ignored, buffer=${bufferHealth.toFixed(1)}s, playing=${!videoEl.paused})`);
        clearNativeStallRecovery();
        clearHlsStallRecovery();
        return;
      }

      debugLog('[V3Player] Event: stalled -> buffering');
      cancelPendingReveal();
      clearProbeConfirmation();
      clearHlsRenderProbe(false);
      setStatus('buffering');
      reportPlaybackWarning(PLAYBACK_WARNING_CODE_STALLED, 'stalled', 'decode');
      scheduleNativeStallRecovery(videoEl, 'stalled');
      scheduleHlsStallRecovery(videoEl, 'stalled');
    };

    const onSeeking = () => {
      if (decodeRecoveryInFlightRef.current) {
        debugLog('[V3Player] Event: seeking ignored during decode recovery');
        return;
      }
      if (revealHoldRef.current) {
        return;
      }

      clearNativeStallRecovery();
      clearHlsStallRecovery();
      clearProbeConfirmation();
      clearHlsRenderProbe(false);

      // In-buffer seek: when the seek target is already buffered and decodable
      // (currentTime == target the moment 'seeking' fires), playback resumes
      // instantly, so do NOT flash the buffering veil — Plex-like in-memory
      // scrubbing. onWaiting/onStalled remain the safety net if this prediction
      // is wrong; worst case is the old behavior, never a stuck black frame.
      const headroom = bufferedAheadSeconds(videoEl);
      if (isInMemorySeekTarget({ paused: videoEl.paused, readyState: videoEl.readyState, bufferedAheadSeconds: headroom })) {
        debugLog('[V3Player] Event: seeking (in-buffer, no veil)', {
          headroom: headroom.toFixed(1),
          readyState: videoEl.readyState,
        });
        return;
      }

      debugLog('[V3Player] Event: seeking -> buffering');
      setStatus('buffering');
    };

    const onPlaying = () => {
      onPlaybackMilestone?.('firstFrame');
      debugLog('[V3Player] Event: playing');
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
          void reportError('info', PLAYBACK_INFO_CODE_PROBE_WINDOW_STARTED, 'probe_window_started', playbackEngineContext('decode'));
        } else {
          void reportError('info', 200, 'playing', playbackEngineContext('decode'));
        }
      } else if (trackedSessionId && pendingWarningRecoveryRef.current) {
        void reportError(
          'info',
          pendingWarningRecoveryRef.current.code,
          pendingWarningRecoveryRef.current.message,
          playbackEngineContext('recovery'),
        );
        pendingWarningRecoveryRef.current = null;
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
          void reportError('info', PLAYBACK_INFO_CODE_PROBE_WINDOW_CONFIRMED, 'probe_window_confirmed', playbackEngineContext('decode'));
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
      if (revealHoldRef.current) {
        if (revealTimerRef.current === null) {
          const holdMs = revealHoldMs ?? 1800;
          if (holdMs <= 0) {
            revealHoldRef.current = false;
            setStatus('playing');
          } else {
            revealTimerRef.current = window.setTimeout(() => {
              revealTimerRef.current = null;
              revealHoldRef.current = false;
              setStatus('playing');
            }, holdMs);
          }
        }
      } else {
        setStatus('playing');
      }
      clearPlaybackFailure();
    };

    const onPause = () => {
      if (isTeardownRef.current) {
        return;
      }
      cancelPendingReveal();
      clearNativeStallRecovery();
      clearHlsStallRecovery();
      clearProbeConfirmation();
      clearHlsRenderProbe(false);
      setStatus((prev) => (prev === 'error' ? prev : (revealHoldRef.current ? 'buffering' : 'paused')));
    };

    const onSeeked = () => {
      clearNativeStallRecovery();
      clearHlsStallRecovery();
      if (revealHoldRef.current) {
        return;
      }
      setStatus((prev) => (prev === 'error' ? prev : (videoEl.paused ? 'paused' : 'playing')));
    };

    const onError = () => {
      if (isTeardownRef.current) return;
      if (!videoEl.currentSrc || videoEl.currentSrc === 'about:blank') return;

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
      cancelPendingReveal();
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
          reportMediaFailure({
            title: presentation.title,
            detail: `${presentation.details} • native recovery failed`,
            retryable: true,
            code: 'NATIVE_MEDIA_RECOVERY_FAILED',
          }, {
            code: 'NATIVE_MEDIA_RECOVERY_FAILED',
            recoverable: true,
            terminal: false,
          });
        })) {
          return;
        }

        void reportError('error', safeCode, message, playbackEngineContext('decode'));
      }

      setStatus('error');
      reportMediaFailure({
        title: presentation.title,
        detail: presentation.details,
        retryable: true,
        code: 'MEDIA_ELEMENT_ERROR',
      }, {
        code: 'MEDIA_ELEMENT_ERROR',
      });
    };

    const onTimeUpdate = () => {
      // Ground-truth buffering recovery. `timeupdate` only fires while
      // currentTime is genuinely advancing, so if the FSM is still pinned at
      // 'buffering' while the element is decoding (a transient waiting/seeking
      // after pause->resume that never got a follow-up 'playing' event), the
      // resume already succeeded. Unstick it to 'playing' so the reveal path
      // shows the picture instead of holding the veil indefinitely.
      // Device-confirmed 2026-06-01: audio plays, currentTime advances,
      // readyState 4, but the element stayed veiled because status stuck at
      // 'buffering'.
      if (isTeardownRef.current || videoEl.paused) {
        return;
      }
      if (revealHoldRef.current) {
        // The video is demonstrably advancing, break the hold immediately
        if (revealTimerRef.current !== null) {
          window.clearTimeout(revealTimerRef.current);
          revealTimerRef.current = null;
        }
        revealHoldRef.current = false;
        setStatus('playing');
        return;
      }
      clearNativeStallRecovery();
      clearHlsStallRecovery();
      setStatus((prev) => {
        if (prev === 'starting' || prev === 'priming' || prev === 'building' || prev === 'buffering') return 'playing';
        // Also un-stick the in-place recoveries (hls.js recoverMediaError /
        // startLoad) which can leave status pinned at 'recovering' while the
        // element decodes again. Do NOT touch the async session-reattach path
        // (decodeRecoveryInFlightRef true): it still holds the stale source for
        // ~850ms before teardown+replay, so flipping to 'playing' there would
        // un-veil right before the source is swapped.
        if (prev === 'recovering' && !decodeRecoveryInFlightRef.current) return 'playing';
        return prev;
      });
    };

    videoEl.addEventListener('waiting', onWaiting);
    videoEl.addEventListener('stalled', onStalled);
    videoEl.addEventListener('seeking', onSeeking);
    videoEl.addEventListener('seeked', onSeeked);
    videoEl.addEventListener('playing', onPlaying);
    videoEl.addEventListener('pause', onPause);
    videoEl.addEventListener('timeupdate', onTimeUpdate);
    const onLoadedMetadataGeneral = () => {
      onPlaybackMilestone?.('manifest');
    };
    videoEl.addEventListener('loadedmetadata', onLoadedMetadataGeneral);
    videoEl.addEventListener('error', onError);

    const mapNativeAudioTracks = () => {
      if (isTeardownRef.current || (!videoEl.currentSrc && !videoEl.getAttribute('src'))) return;
      if (!('audioTracks' in videoEl)) return;
      const tracks = (videoEl as any).audioTracks;
      if (!tracks || tracks.length === 0) return;

      const mappedTracks: PlayerAudioTrack[] = [];
      let activeId = -1;

      for (let i = 0; i < tracks.length; i++) {
        const track = tracks[i];
        mappedTracks.push({
          id: i,
          name: track.label || track.language || `Track ${i + 1}`,
          language: track.language,
          key: 'native-' + i,
          engineIndex: i,
        });
        if (track.enabled) {
          activeId = i;
        }
      }

      if (onAudioTracksUpdated) {
        onAudioTracksUpdated(mappedTracks);
      }
      if (activeId !== -1 && onAudioTrackSwitched) {
        onAudioTrackSwitched(activeId);
      }
    };

    if ('audioTracks' in videoEl) {
      const tracks = (videoEl as any).audioTracks;
      if (tracks && tracks.addEventListener) {
        tracks.addEventListener('addtrack', mapNativeAudioTracks);
        tracks.addEventListener('removetrack', mapNativeAudioTracks);
        tracks.addEventListener('change', mapNativeAudioTracks);
        // Fire initially in case tracks already exist
        mapNativeAudioTracks();
      }
    }

    return () => {
      cancelPendingReveal();
      clearProbeConfirmation();
      clearHlsRenderProbe(false);
      videoEl.removeEventListener('waiting', onWaiting);
      videoEl.removeEventListener('stalled', onStalled);
      videoEl.removeEventListener('seeking', onSeeking);
      videoEl.removeEventListener('seeked', onSeeked);
      videoEl.removeEventListener('playing', onPlaying);
      videoEl.removeEventListener('pause', onPause);
      videoEl.removeEventListener('timeupdate', onTimeUpdate);
      videoEl.removeEventListener('loadedmetadata', onLoadedMetadataGeneral);
      videoEl.removeEventListener('error', onError);

      if ('audioTracks' in videoEl) {
        const tracks = (videoEl as any).audioTracks;
        if (tracks && tracks.removeEventListener) {
          tracks.removeEventListener('addtrack', mapNativeAudioTracks);
          tracks.removeEventListener('removetrack', mapNativeAudioTracks);
          tracks.removeEventListener('change', mapNativeAudioTracks);
        }
      }
    };
  }, [beginSessionDecodeRecovery, bufferedAheadSeconds, clearHlsRenderProbe, clearHlsStallRecovery, clearNativeStallRecovery, clearProbeConfirmation, hlsRef, isTeardownRef, onAudioTrackSwitched, onAudioTracksUpdated, playbackEngineContext, reportError, reportPlaybackWarning, runtimeProbeActive, scheduleHlsRenderProbe, scheduleHlsStallRecovery, scheduleNativeStallRecovery, sessionIdRef, setStatus, t, videoRef]);

  // Unmount-only cleanup: clear all recovery/retry timers so stale callbacks
  // can't fire after the component unmounts. Do NOT put these in the main
  // useEffect cleanup above — that effect re-runs when deps change and would
  // clear timers mid-recovery, breaking the hls.js network retry test.
  useEffect(() => {
    return () => {
      clearNetworkRetry();
      clearNativeStallRecovery();
      clearHlsStallRecovery();
      clearProbeConfirmation();
      clearHlsRenderProbe(true);
    };
  }, [clearHlsRenderProbe, clearHlsStallRecovery, clearNativeStallRecovery, clearNetworkRetry, clearProbeConfirmation]);

  return {
    resetPlaybackEngine,
    playHls,
    playDirectMp4,
  };
}
