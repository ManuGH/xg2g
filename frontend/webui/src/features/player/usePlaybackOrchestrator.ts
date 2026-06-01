import { useState, useEffect, useRef, useCallback, useMemo, useReducer } from 'react';
import type { RefObject } from 'react';
import { useTranslation } from 'react-i18next';
import Hls from './lib/hlsRuntime';
import {
  postRecordingPlaybackInfo,
  type PlaybackTrace as PlaybackTraceContract,
} from '../../client-ts';
import { getApiBaseUrl } from '../../services/clientWrapper';
import { telemetry } from '../../services/TelemetryService';
import type {
  V3PlayerProps,
  PlayerStatus,
  V3SessionSnapshot,
  HlsInstanceRef,
  VideoElementRef
} from '../../types/v3-player';
import { useLiveSessionController } from './useLiveSessionController';
import { usePlaybackEngine } from './usePlaybackEngine';
import { usePlayerChrome } from './usePlayerChrome';
import { resolveStartupOverlayLabel, resolveStartupOverlaySupport } from './startupOverlayLabel';
import { useResume } from '../resume/useResume';
import { ResumeState } from '../resume/api';
import { debugError, debugLog, debugWarn } from '../../utils/logging';
import {
  PlayerError,
  readResponseBody,
  hasTouchInput,
  canUseDesktopWebKitFullscreen,
  shouldForceNativeMobileHls,
  shouldPreferNativeWebKitHls
} from './utils/playerHelpers';
import { gatherPlaybackCapabilities, type CapabilitySnapshot } from './utils/playbackCapabilities';
import {
  buildPlaybackProfileHeaders,
  gatherPlaybackClientContext,
  resolvePlaybackRequestProfile,
} from './utils/playbackRequestProfile';
import { normalizePlayerError } from '../../lib/appErrors';
import { notifyAuthRequiredIfUnauthorizedResponse } from '../../lib/httpProblem';
import { useTvInitialFocus } from '../../hooks/useTvInitialFocus';
import {
  createInitialPlaybackDomainState,
  playbackMachine,
} from './orchestrator/playbackMachine';
import type { VodStreamMode } from './orchestrator/playbackTypes';
import { normalizePlaybackInfo } from './contracts/normalizePlaybackInfo';
import {
  requestHostInputFocus,
  resolveHostEnvironment,
  setHostPlaybackActive,
  stopNativePlayback,
} from '../../lib/hostBridge';
import type { AppError } from '../../types/errors';
import {
  formatSourceProfileSummary,
  formatFfmpegPlanSummary,
  formatFirstFrameLabel,
  formatFallbackSummary,
  formatStopSummary,
  formatHostPressureSummary,
  extractPlaybackTrace,
  formatClientPath,
  formatRequestProfileLabel,
  formatQualityRungLabel,
  formatBooleanLabel,
  formatTargetProfileSummary,
  formatExecutionLabel,
  resolvePlaybackObservability,
  type PlaybackObservability,
} from './orchestrator/observabilityFormatters';
import { buildContractState } from './orchestrator/contractErrors';
import {
  supportsManagedNativePlayback,
} from './orchestrator/nativePlaybackHelpers';
import { resolveSessionPhaseFromState } from './orchestrator/sessionPhase';
import { useEpochManager } from './orchestrator/useEpochManager';
import { usePlaybackStateSetters } from './orchestrator/usePlaybackStateSetters';
import { usePlaybackResourceCleanup } from './orchestrator/usePlaybackResourceCleanup';
import { useTelemetryEmitter } from './orchestrator/useTelemetryEmitter';
import { useDocumentVisibility } from './orchestrator/useDocumentVisibility';
import { decideForegroundResume } from './orchestrator/foregroundResume';
import { useBufferingOverlay } from './orchestrator/useBufferingOverlay';
import { useStartupElapsed } from './orchestrator/useStartupElapsed';
import { useNativeVideoReveal } from './orchestrator/useNativeVideoReveal';
import { useLiveNowPlaying } from './useLiveNowPlaying';
import { useNativePlaybackBridge } from './orchestrator/useNativePlaybackBridge';
import {
  buildAuthDeniedFailure,
  buildBlockedContractFailure,
  buildContractConsumedTelemetry,
  buildLeaseBusyFailure,
  buildLiveIntentBody,
  buildMissingDecisionTokenFailure,
  buildMissingOutputUrlFailure,
  buildRecordingGoneFailure,
  buildServiceRefRequiredFailure,
  buildUnsupportedLiveModeFailure,
  prepareForPlaybackAttempt,
  resolveLiveEngineFromMode,
  resolveResumeStateFromContract,
} from './orchestrator/startupHelpers';


export interface PlaybackOrchestratorRefs {
  containerRef: RefObject<HTMLDivElement | null>;
  videoRef: RefObject<VideoElementRef>;
  hlsRef: RefObject<HlsInstanceRef>;
  resumePrimaryActionRef: RefObject<HTMLButtonElement | null>;
}

export interface V3PlayerLabeledValue {
  label: string;
  value: string;
}

export interface V3PlayerViewState {
  channelName: string | null;
  programmeTitle: string | null;
  programmeDesc: string | null;
  useOverlayLayout: boolean;
  userIdle: boolean;
  showCloseButton: boolean;
  closeButtonLabel: string;
  showStatsOverlay: boolean;
  statsTitle: string;
  statusLabel: string;
  statusChipLabel: string;
  statusChipState: 'live' | 'error' | 'idle';
  statsRows: V3PlayerLabeledValue[];
  showNativeBufferingMask: boolean;
  hideVideoElement: boolean;
  showStartupBackdrop: boolean;
  showStartupOverlay: boolean;
  useNativeBufferingSafeOverlay: boolean;
  overlayStatusLabel: string;
  overlayStatusState: 'live' | 'idle';
  spinnerEyebrow: string;
  spinnerLabel: string;
  spinnerSupport: string;
  startupElapsedLabel: string;
  showOverlayStopAction: boolean;
  overlayStopLabel: string;
  videoClassName: string;
  autoPlay: boolean;
  error: AppError | null;
  showErrorDetails: boolean;
  errorRetryLabel: string;
  errorTelemetryRows: V3PlayerLabeledValue[];
  errorDetailToggleLabel: string | null;
  errorSessionLabel: string;
  showPlaybackChrome: boolean;
  showSeekControls: boolean;
  seekBack15mLabel: string;
  seekBack60sLabel: string;
  seekBack15sLabel: string;
  seekForward15sLabel: string;
  seekForward60sLabel: string;
  seekForward15mLabel: string;
  playPauseLabel: string;
  playPauseIcon: string;
  seekableStart: number;
  seekableEnd: number;
  startTimeDisplay: string;
  endTimeDisplay: string;
  windowDuration: number;
  relativePosition: number;
  isLiveMode: boolean;
  isAtLiveEdge: boolean;
  liveButtonLabel: string;
  showServiceInput: boolean;
  serviceRef: string;
  showManualStartButton: boolean;
  manualStartLabel: string;
  manualStartDisabled: boolean;
  showDvrModeButton: boolean;
  dvrModeLabel: string;
  showNativeFullscreenButton: boolean;
  nativeFullscreenTitle: string;
  nativeFullscreenLabel: string;
  showFullscreenButton: boolean;
  fullscreenLabel: string;
  fullscreenActive: boolean;
  showVolumeControls: boolean;
  audioToggleLabel: string;
  audioToggleIcon: string;
  audioToggleActive: boolean;
  canAdjustVolume: boolean;
  volume: number;
  deviceVolumeHint: string;
  showPipButton: boolean;
  pipTitle: string;
  pipLabel: string;
  pipActive: boolean;
  statsLabel: string;
  statsActive: boolean;
  showStopButton: boolean;
  stopLabel: string;
  showResumeOverlay: boolean;
  resumeTitle: string;
  resumePrompt: string;
  resumeActionLabel: string;
  startOverLabel: string;
  resumePositionSeconds: number | null;
  playback: {
    durationSeconds: number | null;
  };
}

export interface PlaybackOrchestratorActions {
  stopStream(skipClose?: boolean): Promise<void>;
  retry(): Promise<void>;
  seekBy(deltaSeconds: number): void;
  seekTo(positionSeconds: number): void;
  seekToLiveEdge(): void;
  togglePlayPause(): void;
  updateServiceRef(nextValue: string): void;
  submitServiceRef(nextValue?: string): void;
  startStream(refToUse?: string): void;
  enterDVRMode(): void;
  enterNativeFullscreen(): void;
  toggleFullscreen(): Promise<void>;
  toggleMute(): void;
  changeVolume(nextVolume: number): void;
  togglePiP(): Promise<void>;
  toggleStats(): void;
  toggleErrorDetails(): void;
  resumeFrom(positionSeconds: number): void;
  startOver(): void;
}

export interface UsePlaybackOrchestratorResult {
  viewState: V3PlayerViewState;
  actions: PlaybackOrchestratorActions;
}

export function usePlaybackOrchestrator(
  props: V3PlayerProps,
  {
    containerRef,
    videoRef,
    hlsRef,
    resumePrimaryActionRef,
  }: PlaybackOrchestratorRefs,
): UsePlaybackOrchestratorResult {
  const { t } = useTranslation();
  const { token, autoStart, onClose, duration } = props;
  const channel = 'channel' in props ? props.channel : undefined;
  const src = 'src' in props ? props.src : undefined;
  const recordingId = 'recordingId' in props ? props.recordingId : undefined;

  const [sRef, setSRef] = useState<string>(
    (channel?.serviceRef || channel?.id || '').trim()
  );
  const requestedDuration = useMemo(() => (duration && duration > 0 ? duration : null), [duration]);
  const [playbackState, dispatchPlayback] = useReducer(
    playbackMachine,
    requestedDuration,
    createInitialPlaybackDomainState,
  );
  const playbackStateRef = useRef(playbackState);
  const {
    playbackEpochRef,
    acceptedPlaybackEpochRef,
    acceptedSessionEpochRef,
    allocatePlaybackEpoch,
    beginPlaybackAttempt,
    markPlaybackStopped,
    allocateSessionEpoch,
    isStalePlaybackEpoch,
    isStaleSessionEpoch,
  } = useEpochManager({
    initialEpoch: playbackState.epoch,
    trackedEpoch: playbackState.epoch,
    dispatchPlayback,
    requestedDuration,
  });

  const {
    traceId,
    status,
    playbackMode,
    vodStreamMode,
    activeHlsEngine,
    durationSeconds,
    canSeek,
    startUnix,
    failure,
    lastAdvisory,
  } = playbackState;
  const error = failure?.appError ?? null;
  const [showErrorDetails, setShowErrorDetails] = useState(false);
  const [capabilitySnapshot, setCapabilitySnapshot] = useState<CapabilitySnapshot | null>(null);
  const [playbackObservability, setPlaybackObservability] = useState<PlaybackObservability | null>(null);
  const [sessionPlaybackTrace, setSessionPlaybackTrace] = useState<PlaybackTraceContract | null>(null);
  const [sessionProfileReason, setSessionProfileReason] = useState<string | null>(null);
  const hostEnvironment = useMemo(() => resolveHostEnvironment(), []);
  const isNativePlaybackHost = supportsManagedNativePlayback(hostEnvironment);

  const mounted = useRef<boolean>(false);
  const {
    vodRetryRef,
    vodFetchRef,
    nativeVideoRevealTimerRef,
    nativeVideoVeilRevealTimerRef,
    nativeVideoVeilClearTimerRef,
    clearVodRetry,
    clearVodFetch,
    clearNativeVideoVeilTimers,
    clearNativeVideoRevealTimer,
  } = usePlaybackResourceCleanup();
  const activeRecordingRef = useRef<string | null>(null);
  const [activeRecordingId, setActiveRecordingId] = useState<string | null>(null);
  const startIntentInFlight = useRef<boolean>(false);
  // ADR-00X: Profile-related refs removed (universal policy only)
  const isTeardownRef = useRef<boolean>(false);
  const userPauseIntentRef = useRef<boolean>(false);
  const nativeVideoTempMutedRef = useRef(false);
  const visibilityManagedPauseRef = useRef(false);
  const wasHiddenRef = useRef(false);
  const cleanupPlaybackResourcesRef = useRef<() => void>(() => {});
  const activeLiveSessionIdRef = useRef<string | null>(null);

  const lastDecodedRef = useRef<number>(0);

  // Resume State
  const [resumeState, setResumeState] = useState<ResumeState | null>(null);
  const [showResumeOverlay, setShowResumeOverlay] = useState(false);
  const isDocumentVisible = useDocumentVisibility();

  useEffect(() => {
    playbackStateRef.current = playbackState;
  }, [playbackState]);

  const {
    setTraceId,
    setStatus,
    setPlaybackMode,
    setDurationSeconds,
    setVodStreamMode,
    setActiveHlsEngine,
    setCanSeek,
    setStartUnix,
    setPlayerError,
    reportPlaybackFailure,
    clearPlaybackFailure,
    clearPlayerError,
    recordContractAdvisories,
  } = usePlaybackStateSetters({
    dispatchPlayback,
    playbackStateRef,
    acceptedPlaybackEpochRef,
    setShowErrorDetails,
  });

  useEffect(() => {
    if (!error?.detail) {
      setShowErrorDetails(false);
    }
  }, [error?.detail]);

  useTelemetryEmitter({
    failure,
    lastAdvisory,
    playbackEpoch: playbackState.epoch.playback,
  });

  const normalizeRuntimePlaybackError = useCallback((value: unknown, fallbackTitle: string): AppError => {
    const status =
      value && typeof value === 'object' && 'status' in value && typeof (value as { status?: unknown }).status === 'number'
        ? (value as { status: number }).status
        : undefined;

    return normalizePlayerError(value, {
      fallbackTitle,
      status,
    });
  }, []);

  const sleep = useCallback((ms: number): Promise<void> => (
    new Promise(resolve => setTimeout(resolve, ms))
  ), []);

  const resolvePreferredHlsEngine = useCallback((): 'native' | 'hlsjs' => {
    const hlsJsSupported = Hls.isSupported();
    if (shouldPreferNativeWebKitHls(videoRef.current, hlsJsSupported)) {
      return 'native';
    }
    return hlsJsSupported ? 'hlsjs' : 'native';
  }, [videoRef]);

  const resolvePreferredHlsEngineForCapabilities = useCallback((
    capabilities?: Pick<CapabilitySnapshot, 'preferredHlsEngine'> | null
  ): 'native' | 'hlsjs' => {
    if (capabilities?.preferredHlsEngine === 'native' || capabilities?.preferredHlsEngine === 'hlsjs') {
      return capabilities.preferredHlsEngine;
    }
    return resolvePreferredHlsEngine();
  }, [resolvePreferredHlsEngine]);

  const mergeSessionPlaybackTrace = useCallback((nextTrace: PlaybackTraceContract | null) => {
    if (!nextTrace) {
      return;
    }
    setSessionPlaybackTrace((current) => ({
      ...(current ?? {}),
      ...nextTrace,
      source: nextTrace.source ?? current?.source,
      targetProfile: nextTrace.targetProfile ?? current?.targetProfile,
      ffmpegPlan: nextTrace.ffmpegPlan ?? current?.ffmpegPlan,
      fallbackCount: nextTrace.fallbackCount ?? current?.fallbackCount,
      lastFallbackReason: nextTrace.lastFallbackReason ?? current?.lastFallbackReason,
      stopReason: nextTrace.stopReason ?? current?.stopReason,
      stopClass: nextTrace.stopClass ?? current?.stopClass,
      firstFrameAtMs: nextTrace.firstFrameAtMs ?? current?.firstFrameAtMs,
    }));
    if (nextTrace.requestId) {
      setTraceId(nextTrace.requestId);
    }
  }, []);

  const {
    nativePlaybackState,
    nativeSessionId,
    beginNativePlayback,
    resetBridgeState,
  } = useNativePlaybackBridge({
    isNativePlaybackHost,
    resolvePreferredHlsEngine,
    pipeline: {
      setActiveHlsEngine,
      setActiveRecordingId,
      setPlaybackMode,
      setStatus,
      setTraceId,
      setSessionProfileReason,
      setPlaybackObservability,
      mergeSessionPlaybackTrace,
      clearPlayerError,
      reportPlaybackFailure,
    },
    activeRecordingRef,
  });

  const handleSessionSnapshot = useCallback((session: V3SessionSnapshot) => {
    const activeLiveSessionId = activeLiveSessionIdRef.current;
    if (session.sessionId) {
      if (!activeLiveSessionId || session.sessionId !== activeLiveSessionId) {
        return;
      }
    }
    if (session.requestId) {
      setTraceId(session.requestId);
    }
    const sessionPhase = resolveSessionPhaseFromState(session.state);
    if (sessionPhase && activeLiveSessionId) {
      dispatchPlayback({
        type: 'normative.session.phase.changed',
        playbackEpoch: acceptedPlaybackEpochRef.current,
        sessionEpoch: acceptedSessionEpochRef.current,
        phase: sessionPhase,
        requestId: session.requestId ?? null,
      });
    }
    setSessionProfileReason(session.profileReason ?? null);
    mergeSessionPlaybackTrace(extractPlaybackTrace(session));
  }, [acceptedPlaybackEpochRef, acceptedSessionEpochRef, mergeSessionPlaybackTrace, setTraceId]);

  // Explicitly static/memoized apiBase
  const apiBase = useMemo(() => {
    return getApiBaseUrl();
  }, []);
  const isCompactTouchLayout = useMemo(() => hasTouchInput(), []);

  const {
    sessionIdRef,
    authHeaders,
    reportError,
    ensureSessionCookie,
    setActiveSessionId: setActiveSessionIdBase,
    clearSessionLeaseState: clearSessionLeaseStateBase,
    sendStopIntent,
    waitForSessionReady
  } = useLiveSessionController({
    token,
    apiBase,
    t,
    videoRef,
    setPlaybackMode,
    setDurationSeconds,
    setStatus,
    clearPlaybackFailure,
    reportPlaybackFailure,
    readResponseBody,
    createPlayerError: (message, details) => new PlayerError(message, details),
    onSessionSnapshot: handleSessionSnapshot,
  });

  const setActiveSessionId = useCallback((nextSessionId: string | null) => {
    activeLiveSessionIdRef.current = nextSessionId;
    setActiveSessionIdBase(nextSessionId);
  }, [setActiveSessionIdBase]);

  // Native playback (managed Safari/iOS native HLS) runs through the native
  // bridge and never drives the MSE controller's snapshot loop, so the executed
  // session trace (GET /sessions) previously never reached the stats panel —
  // it fell back to the pre-roll prediction and could mislabel container/codec.
  // Poll the native session's trace read-only and merge it. This is telemetry
  // ONLY: it never sends heartbeats or stop intents, so the bridge's session
  // lifecycle is untouched and playback cannot be affected.
  useEffect(() => {
    // Tie the poll's lifetime to nativeSessionId only. The bridge nulls it on
    // stop, so we never keep polling a cleanly-ended session (the lingering-poll
    // edge); the broader nativePlaybackState.session.sessionId can outlive an
    // ended session until full teardown.
    if (!nativeSessionId) {
      return;
    }
    let cancelled = false;
    const pollNativeTrace = async () => {
      try {
        const res = await fetch(`${apiBase}/sessions/${nativeSessionId}`, { headers: authHeaders() });
        if (cancelled || !res.ok) {
          return;
        }
        const session = await res.json();
        if (cancelled) {
          return;
        }
        // extractPlaybackTrace descends into the response's `.trace` wrapper.
        mergeSessionPlaybackTrace(extractPlaybackTrace(session));
      } catch {
        // Best-effort telemetry; transient errors must never disturb playback.
      }
    };
    void pollNativeTrace();
    const intervalId = window.setInterval(pollNativeTrace, 5000);
    return () => {
      cancelled = true;
      window.clearInterval(intervalId);
    };
  }, [nativeSessionId, apiBase, authHeaders, mergeSessionPlaybackTrace]);

  const clearSessionLeaseState = useCallback(() => {
    activeLiveSessionIdRef.current = null;
    clearSessionLeaseStateBase();
  }, [clearSessionLeaseStateBase]);

  const {
    showStats,
    currentPlaybackTime,
    seekableStart,
    seekableEnd,
    supportsNativeFullscreen,
    canEnterNativeFullscreen,
    prefersDesktopNativeFullscreen,
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
    isLiveMode,
    isAtLiveEdge,
    showDvrModeButton,
    startTimeDisplay,
    endTimeDisplay,
    formatClock,
    seekTo,
    seekToLiveEdge,
    seekBy,
    seekWhenReady,
    togglePlayPause,
    toggleFullscreen,
    enterNativeFullscreen,
    enterDVRMode,
    togglePiP,
    toggleMute,
    handleVolumeChange,
    applyAutoplayMute,
    toggleStats,
    resetChromeState
  } = usePlayerChrome({
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
    liveSeekWindow: null,
    allowNativeFullscreen: activeHlsEngine === 'native',
    shouldForceNativeMobileHls,
    canUseDesktopWebKitFullscreen
  });

  // Live now-playing EPG (current programme title + synopsis, auto-refreshes
  // when the programme changes). Disabled for recordings (fixed title).
  const liveNowPlaying = useLiveNowPlaying(sRef, playbackMode === 'LIVE');

  // Resume Hook
  useResume({
    recordingId: activeRecordingId || undefined,
    duration: durationSeconds,
    videoRef,
    isPlaying,
    isSeekable: canSeek
  });

  const {
    resetPlaybackEngine,
    playHls,
    playDirectMp4
  } = usePlaybackEngine({
    videoRef,
    hlsRef,
    sessionIdRef,
    isTeardownRef,
    lastDecodedRef,
    playbackEpochRef,
    t,
    reportError,
    waitForSessionReady,
    shouldPreferNativeHls: shouldPreferNativeWebKitHls,
    setStats,
    setStatus,
    clearPlaybackFailure,
    reportPlaybackFailure
  });

  // --- Core Helpers & Wrappers (Memoized) ---

  const getBufferedAheadSeconds = useCallback((): number => {
    const video = videoRef.current;
    if (!video || !video.buffered.length) {
      return 0;
    }

    for (let i = 0; i < video.buffered.length; i++) {
      const start = video.buffered.start(i);
      const end = video.buffered.end(i);
      if (video.currentTime >= start && video.currentTime <= end) {
        return Math.max(0, end - video.currentTime);
      }
    }

    const finalEnd = video.buffered.end(video.buffered.length - 1);
    return finalEnd > video.currentTime ? finalEnd - video.currentTime : 0;
  }, [videoRef]);

  const {
    showNativeVideo,
    showNativeVideoVeil,
    resetNativeVideoState,
  } = useNativeVideoReveal({
    isNativeEngine: activeHlsEngine === 'native',
    status,
    videoRef,
    getBufferedAheadSeconds,
    nativeVideoRevealTimerRef,
    nativeVideoVeilRevealTimerRef,
    nativeVideoVeilClearTimerRef,
    clearNativeVideoRevealTimer,
    clearNativeVideoVeilTimers,
  });

  const clearPlaybackSelection = useCallback(() => {
    activeRecordingRef.current = null;
    resetNativeVideoState();
    resetBridgeState();
    setActiveRecordingId(null);
    setVodStreamMode(null);
    setActiveHlsEngine(null);
    setCapabilitySnapshot(null);
    setPlaybackObservability(null);
    setSessionPlaybackTrace(null);
    setSessionProfileReason(null);
  }, [resetBridgeState, resetNativeVideoState, setActiveHlsEngine, setVodStreamMode]);

  const clearPlaybackState = useCallback(() => {
    clearPlaybackSelection();
    clearVodRetry();
    clearVodFetch();
    clearSessionLeaseState();
    resetChromeState();
  }, [clearPlaybackSelection, clearSessionLeaseState, clearVodFetch, clearVodRetry, resetChromeState]);

  const cleanupPlaybackResources = useCallback(() => {
    const activeHls = hlsRef.current;
    const activeSessionId = sessionIdRef.current;
    const activeVideo = videoRef.current;
    const hasNativePlayback = isNativePlaybackHost && nativePlaybackState?.activeRequest;

    if (activeHls) activeHls.destroy();
    if (activeVideo) {
      activeVideo.pause();
      activeVideo.src = '';
    }

    clearVodRetry();
    clearVodFetch();
    clearPlaybackSelection();
    if (hasNativePlayback) {
      stopNativePlayback();
    }
    void sendStopIntent(activeSessionId, true);
  }, [
    clearPlaybackSelection,
    clearVodFetch,
    clearVodRetry,
    hlsRef,
    isNativePlaybackHost,
    nativePlaybackState,
    sendStopIntent,
    sessionIdRef,
    videoRef,
  ]);

  useEffect(() => {
    cleanupPlaybackResourcesRef.current = cleanupPlaybackResources;
  }, [cleanupPlaybackResources]);

  const hasActivePlayback = useCallback((): boolean => {
    const videoEl = videoRef.current;
    return Boolean(
      sessionIdRef.current ||
      activeRecordingRef.current ||
      hlsRef.current ||
      videoEl?.currentSrc ||
      videoEl?.getAttribute('src')
    );
  }, [hlsRef, sessionIdRef, videoRef]);

  const teardownActivePlayback = useCallback(async (): Promise<void> => {
    const activeSessionId = sessionIdRef.current;
    const hadNativePlayback = isNativePlaybackHost && Boolean(nativePlaybackState?.activeRequest);
    const hadActivePlayback = hasActivePlayback();

    clearPlaybackSelection();
    clearVodRetry();
    clearVodFetch();
    if (hadNativePlayback) {
      stopNativePlayback();
    }
    if (hadActivePlayback) {
      resetPlaybackEngine();
      await sleep(75);
    }
    if (activeSessionId) {
      await sendStopIntent(activeSessionId);
    }
    clearSessionLeaseState();
    resetChromeState();
  }, [
    clearPlaybackSelection,
    clearSessionLeaseState,
    clearVodFetch,
    clearVodRetry,
    hasActivePlayback,
    resetChromeState,
    resetPlaybackEngine,
    sendStopIntent,
    sessionIdRef,
    sleep,
    isNativePlaybackHost,
    nativePlaybackState,
  ]);

  const gatherPlaybackCapabilitiesForPlayer = useCallback(async (scope: 'live' | 'recording' = 'live'): Promise<CapabilitySnapshot> => {
    const video = videoRef.current as HTMLVideoElement | null;
    return gatherPlaybackCapabilities(scope, video);
  }, []);

  const startRecordingPlayback = useCallback(async (id: string): Promise<void> => {
    const playbackEpoch = allocatePlaybackEpoch();
    {
      const teardown = prepareForPlaybackAttempt({ hasActivePlayback, teardownActivePlayback, clearPlaybackState });
      if (teardown) await teardown;
    }
    beginPlaybackAttempt(playbackEpoch, 'VOD', 'building');
    activeRecordingRef.current = id;
    setActiveRecordingId(id);
    clearPlayerError();

    let abortController: AbortController | null = null;
    let requestCaps: CapabilitySnapshot | null;

    try {
      await ensureSessionCookie();
      if (isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;

      let streamUrl = '';
      let mode: VodStreamMode = null;

      try {
        const maxMetaRetries = 20;
        requestCaps = await gatherPlaybackCapabilitiesForPlayer('recording');
        const requestProfile = resolvePlaybackRequestProfile(
          gatherPlaybackClientContext(),
          requestCaps,
          'recording'
        );
        setCapabilitySnapshot(requestCaps);
        let rawContract: unknown = null;

        for (let i = 0; i < maxMetaRetries; i++) {
          if (isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;

          const { data, error, response } = await postRecordingPlaybackInfo({
            path: { recordingId: id },
            body: requestCaps,
            headers: buildPlaybackProfileHeaders(requestProfile),
          });

          if (error) {
            if (!response) {
              throw new Error(JSON.stringify(error));
            }
            if (notifyAuthRequiredIfUnauthorizedResponse(response, 'V3Player.recordingPlaybackInfo')) {
              setStatus('error');
              const failure = buildAuthDeniedFailure(t, 401);
              reportPlaybackFailure(failure.appError, failure.options);
              return;
            }
            if (response.status === 403) {
              setStatus('error');
              const failure = buildAuthDeniedFailure(t, 403);
              reportPlaybackFailure(failure.appError, failure.options);
              return;
            }
            if (response.status === 410) {
              setStatus('error');
              const failure = buildRecordingGoneFailure(t);
              reportPlaybackFailure(failure.appError, failure.options);
              return;
            }
            if (response.status === 409) {
              const retryAfterHeader = response.headers.get('Retry-After');
              const retryAfter = retryAfterHeader ? parseInt(retryAfterHeader, 10) : 0;
              setStatus('error');
              const failure = buildLeaseBusyFailure(retryAfter, t);
              reportPlaybackFailure(failure.appError, failure.options);
              return;
            }
            if (response.status === 503) {
              const retryAfter = response.headers.get('Retry-After');
              if (retryAfter) {
                const seconds = parseInt(retryAfter, 10);
                setStatus('building');
                recordContractAdvisories(playbackEpoch, [{
                  code: 'recording_retry_after',
                  message: `${t('player.preparing')} (${seconds}s)`,
                  source: 'backend',
                }]);
                await sleep(seconds * 1000);
                continue;
              } else {
                throw new Error('503 Service Unavailable (No Retry-After)');
              }
            }
            throw new Error(JSON.stringify(error));
          }

          if (data) {
            rawContract = data;
            break;
          }
        }

        if (!rawContract) {
          throw new Error("PlaybackInfo timeout");
        }
        if (isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;

        const preferredHlsEngine = resolvePreferredHlsEngineForCapabilities(requestCaps);
        const normalizedContract = normalizePlaybackInfo(rawContract, {
          surface: 'recording',
          preferredHlsEngine,
        });

        debugLog('[V3Player] Normalized recording contract:', normalizedContract);
        recordContractAdvisories(playbackEpoch, normalizedContract.advisory.warnings);

        telemetry.emit('ui.contract.consumed', buildContractConsumedTelemetry(normalizedContract, 'recording'));

        if (normalizedContract.observability.requestId) {
          setTraceId(normalizedContract.observability.requestId);
        }
        setPlaybackObservability(resolvePlaybackObservability(
          normalizedContract.observability.decision,
          requestCaps.preferredHlsEngine ?? null
        ));

        if (normalizedContract.kind === 'blocked') {
          setStatus('error');
          const failure = buildBlockedContractFailure(normalizedContract, 'recording', t);
          reportPlaybackFailure(failure.appError, failure.options);
          return;
        }

        mode = normalizedContract.playback.mode;
        streamUrl = normalizedContract.playback.outputUrl ?? '';
        if (!streamUrl) {
          setStatus('error');
          const failure = buildMissingOutputUrlFailure(t);
          reportPlaybackFailure(failure.appError, failure.options);
          return;
        }

        dispatchPlayback({
          type: 'normative.playback.contract.resolved',
          epoch: playbackEpoch,
          contract: buildContractState('recording', normalizedContract, streamUrl),
        });

        if (streamUrl.startsWith('/')) {
          streamUrl = `${window.location.origin}${streamUrl}`;
        }

        // Add Cache Busting to prevent sticky 503s
        streamUrl += (streamUrl.includes('?') ? '&' : '?') + `cb=${Date.now()}`;

        setVodStreamMode(mode);

        const playbackDurationSeconds = normalizedContract.media.durationSeconds;
        if (playbackDurationSeconds && playbackDurationSeconds > 0) {
          setDurationSeconds(playbackDurationSeconds);
        }

        setCanSeek(normalizedContract.playback.seekable);
        if (normalizedContract.media.startUnix) setStartUnix(normalizedContract.media.startUnix);

        const nextResume = resolveResumeStateFromContract(normalizedContract, playbackDurationSeconds);
        if (nextResume) {
          setResumeState(nextResume);
          setShowResumeOverlay(true);
        }
      } catch (e: unknown) {
        if (isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;
        setStatus('error');
        mergeSessionPlaybackTrace(extractPlaybackTrace(e));
        reportPlaybackFailure(normalizeRuntimePlaybackError(e, t('player.serverError')), {
          source: 'backend',
        });
        return;
      }

      // --- EXECUTION PATHS ---
      if (mode === 'direct_mp4') {
        // Direct MP4 start stays thin-client: the media element is the source of
        // truth for playability, so we do not gate startup on browser-side probes.
        isTeardownRef.current = false;
        if (isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;
        setStatus('buffering');
        setActiveHlsEngine(null);
        playDirectMp4(streamUrl);
        return;
      }

      if (mode === 'native_hls' || mode === 'hlsjs' || mode === 'transcode') {
        const controller = new AbortController();
        abortController = controller;
        vodFetchRef.current = controller;
        try {
          const res = await fetch(streamUrl, {
            method: 'HEAD',
            signal: controller.signal
          });

          if (res.status === 404) {
            throw new Error(t('player.recordingNotFound'));
          }

          if (res.status === 503) {
            const retryAfter = res.headers.get('Retry-After');
            if (retryAfter) {
              const delay = parseInt(retryAfter, 10) * 1000;
              setStatus('building');
              vodRetryRef.current = window.setTimeout(() => {
                if (activeRecordingRef.current === id) startRecordingPlayback(id);
              }, delay);
              return;
            }
            throw new Error('503 Service Unavailable (No Retry-After)');
          }

          if (isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;
          setStatus('buffering');
          const engine: 'native' | 'hlsjs' = mode === 'native_hls'
            ? 'native'
            : resolvePreferredHlsEngineForCapabilities(requestCaps);
          playHls(streamUrl, engine);
          setActiveHlsEngine(engine);
        } finally {
          if (vodFetchRef.current === controller) vodFetchRef.current = null;
        }
      }
    } catch (err: unknown) {
      if (isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;
      debugError(err);
      mergeSessionPlaybackTrace(extractPlaybackTrace(err));
      reportPlaybackFailure(normalizeRuntimePlaybackError(err, t('player.serverError')), {
        source: 'backend',
      });
      setStatus('error');
    } finally {
      if (vodFetchRef.current === abortController) vodFetchRef.current = null;
    }
  }, [
    allocatePlaybackEpoch,
    beginPlaybackAttempt,
    clearPlaybackState,
    clearPlayerError,
    ensureSessionCookie,
    gatherPlaybackCapabilitiesForPlayer,
    hasActivePlayback,
    isStalePlaybackEpoch,
    mergeSessionPlaybackTrace,
    playDirectMp4,
    playHls,
    reportPlaybackFailure,
    resolvePreferredHlsEngineForCapabilities,
    sleep,
    t,
    teardownActivePlayback,
    vodFetchRef,
    vodRetryRef,
  ]);

  const startStream = useCallback(async (refToUse?: string): Promise<void> => {
    if (startIntentInFlight.current) return;
    startIntentInFlight.current = true;
    userPauseIntentRef.current = false;
    applyAutoplayMute();

    // Re-resolve at call time to avoid stale closure from useMemo/useCallback caching.
    const nativeHost = supportsManagedNativePlayback(resolveHostEnvironment());

    try {
      if (recordingId) {
        debugLog('[V3Player] startStream: recordingId path', { recordingId, hasSrc: !!src });
        if (src) {
          debugWarn('[V3Player] Both recordingId and src provided; prioritizing recordingId (VOD path).');
        }
        if (nativeHost) {
          const playbackEpoch = allocatePlaybackEpoch();
          const teardown = prepareForPlaybackAttempt({
            hasActivePlayback,
            teardownActivePlayback,
            clearPlaybackState,
            hasActiveNativeRequest: Boolean(nativePlaybackState?.activeRequest),
          });
          if (teardown) await teardown;
          beginPlaybackAttempt(playbackEpoch, 'VOD', 'starting');
          beginNativePlayback({
            kind: 'recording',
            recordingId,
            authToken: token || undefined,
            startPositionMs: 0,
            title: channel?.name ?? recordingId,
          });
          return;
        }
        await startRecordingPlayback(recordingId);
        return;
      }

      if (src) {
        debugLog('[V3Player] startStream: src path', { hasSrc: true });
        const playbackEpoch = allocatePlaybackEpoch();
        {
      const teardown = prepareForPlaybackAttempt({ hasActivePlayback, teardownActivePlayback, clearPlaybackState });
      if (teardown) await teardown;
    }
        beginPlaybackAttempt(playbackEpoch, requestedDuration ? 'VOD' : 'LIVE', 'buffering');
        const srcEngine = resolvePreferredHlsEngine();
        playHls(src, srcEngine);
        setActiveHlsEngine(srcEngine);
        return;
      }

      const ref = (refToUse || sRef || '').trim();
      if (!ref) {
        setStatus('error');
        const failure = buildServiceRefRequiredFailure(t);
        reportPlaybackFailure(failure.appError, failure.options);
        return;
      }
      const playbackEpoch = allocatePlaybackEpoch();
      {
      const teardown = prepareForPlaybackAttempt({ hasActivePlayback, teardownActivePlayback, clearPlaybackState });
      if (teardown) await teardown;
    }
      beginPlaybackAttempt(playbackEpoch, 'LIVE', 'starting');
      let newSessionId: string | null = null;
      let sessionEpoch = 0;
      clearPlayerError();

      if (nativeHost) {
        beginNativePlayback({
          kind: 'live',
          serviceRef: ref,
          authToken: token || undefined,
          title: channel?.name ?? ref,
          logoUrl: channel?.logoUrl || undefined,
        });
        return;
      }

      try {
        await ensureSessionCookie();
        if (isStalePlaybackEpoch(playbackEpoch)) return;

        let liveMode: VodStreamMode = null;
        let liveEngine: 'native' | 'hlsjs' = 'hlsjs';

        const requestCaps = await gatherPlaybackCapabilitiesForPlayer('live');
        const requestProfile = resolvePlaybackRequestProfile(
          gatherPlaybackClientContext(),
          requestCaps,
          'live'
        );
        const preferredHlsEngine = resolvePreferredHlsEngineForCapabilities(requestCaps);
        setCapabilitySnapshot(requestCaps);
        // raw-fetch-justified: live decision request posts dynamic capability payload not covered by generated wrapper flow.
        const liveResponse = await fetch(`${apiBase}/live/stream-info`, {
          method: 'POST',
          headers: {
            ...(authHeaders(true) as Record<string, string>),
            ...buildPlaybackProfileHeaders(requestProfile),
          },
          body: JSON.stringify({
            serviceRef: ref,
            capabilities: requestCaps
          })
        });
        const { json: liveInfoJson } = await readResponseBody(liveResponse);
        const liveError = (!liveResponse.ok) ? liveInfoJson as any : null;
        const liveRequestId =
          (typeof liveInfoJson === 'object' && liveInfoJson !== null && typeof (liveInfoJson as { requestId?: unknown }).requestId === 'string'
            ? (liveInfoJson as { requestId: string }).requestId
            : undefined) ||
          liveResponse.headers.get('X-Request-ID') ||
          undefined;
        if (isStalePlaybackEpoch(playbackEpoch)) return;
        if (liveRequestId) {
          setTraceId(liveRequestId);
        }

        if (!liveResponse.ok) {
          const retryAfterHeader = liveResponse.headers.get('Retry-After');
          const retryAfterSeconds = retryAfterHeader ? parseInt(retryAfterHeader, 10) : undefined;
          if (notifyAuthRequiredIfUnauthorizedResponse(liveResponse, 'V3Player.liveStreamInfo')) {
            setStatus('error');
            reportPlaybackFailure(normalizePlayerError(liveError ?? {
              status: 401,
              title: t('player.authFailed'),
              requestId: liveRequestId,
            }, {
              fallbackTitle: t('player.authFailed'),
              status: 401,
              retryable: false,
            }), {
              source: 'backend',
              failureClass: 'auth',
              code: 'AUTH_DENIED',
              retryable: false,
              recoverable: false,
              terminal: true,
            });
            return;
          }
          if (liveResponse.status === 403) {
            setStatus('error');
            reportPlaybackFailure(normalizePlayerError(liveError ?? {
              status: 403,
              title: t('player.forbidden'),
              requestId: liveRequestId,
            }, {
              fallbackTitle: t('player.forbidden'),
              status: 403,
              retryable: false,
            }), {
              source: 'backend',
              failureClass: 'auth',
              code: 'AUTH_DENIED',
              retryable: false,
              recoverable: false,
              terminal: true,
            });
            return;
          }
          throw normalizePlayerError(liveError ?? {
            status: liveResponse.status,
            title: `${t('player.apiError')}: ${liveResponse.status}`,
            requestId: liveRequestId,
            retryAfterSeconds,
          }, {
            fallbackTitle: `${t('player.apiError')}: ${liveResponse.status}`,
            status: liveResponse.status,
          });
        }

        const normalizedContract = normalizePlaybackInfo(liveInfoJson, {
          surface: 'live',
          preferredHlsEngine,
        });

        debugLog('[V3Player] Normalized live contract:', normalizedContract);
        recordContractAdvisories(playbackEpoch, normalizedContract.advisory.warnings);

        telemetry.emit('ui.contract.consumed', buildContractConsumedTelemetry(normalizedContract, 'live'));

        if (normalizedContract.observability.requestId) {
          setTraceId(normalizedContract.observability.requestId);
        }
        setPlaybackObservability(resolvePlaybackObservability(
          normalizedContract.observability.decision,
          requestCaps.preferredHlsEngine ?? null
        ));

        if (normalizedContract.kind === 'blocked') {
          setStatus('error');
          const failure = buildBlockedContractFailure(normalizedContract, 'live', t);
          reportPlaybackFailure(failure.appError, failure.options);
          return;
        }

        liveMode = normalizedContract.playback.mode;
        dispatchPlayback({
          type: 'normative.playback.contract.resolved',
          epoch: playbackEpoch,
          contract: buildContractState('live', normalizedContract, normalizedContract.playback.outputUrl),
        });

        const liveDecisionToken = normalizedContract.session.decisionToken;
        if (!liveDecisionToken) {
          setStatus('error');
          const failure = buildMissingDecisionTokenFailure(t);
          reportPlaybackFailure(failure.appError, failure.options);
          return;
        }

        const engineDecision = resolveLiveEngineFromMode(liveMode, requestCaps, resolvePreferredHlsEngineForCapabilities);
        if ('unsupported' in engineDecision) {
          setStatus('error');
          const failure = buildUnsupportedLiveModeFailure(liveMode, t);
          reportPlaybackFailure(failure.appError, failure.options);
          return;
        }
        liveEngine = engineDecision.engine;

        const intentBody = buildLiveIntentBody(ref, liveDecisionToken, requestCaps, liveMode);
        sessionEpoch = allocateSessionEpoch(playbackEpoch);

        // raw-fetch-justified: stream.start intent needs explicit payload shaping and immediate RFC7807 handling.
        const res = await fetch(`${apiBase}/intents`, {
          method: 'POST',
          headers: authHeaders(true),
          body: JSON.stringify(intentBody)
        });
        if (isStaleSessionEpoch(playbackEpoch, sessionEpoch)) return;

        if (res.status === 401 || res.status === 403) {
          const isUnauthorized = notifyAuthRequiredIfUnauthorizedResponse(res, 'V3Player.startIntent');
          let errorTitle = isUnauthorized ? t('player.authFailed') : t('player.forbidden');
          let problemBody: unknown = null;
          try {
            const ct = res.headers.get('content-type') || '';
            if (ct.includes('application/problem+json') || ct.includes('application/json')) {
              const problem = await res.json();
              if (problem.title) errorTitle = problem.title;
              problemBody = problem;
            }
          } catch {
            // Body parse failed – fall through with generic message
          }
          setStatus('error');
          reportPlaybackFailure(normalizePlayerError(problemBody ?? {
            status: res.status,
            title: errorTitle,
          }, {
            fallbackTitle: errorTitle,
            status: res.status,
            retryable: false,
          }), {
            source: 'backend',
            failureClass: 'auth',
            code:
              problemBody && typeof problemBody === 'object' && 'code' in problemBody
                ? ((problemBody as { code?: string }).code ?? 'AUTH_DENIED')
                : 'AUTH_DENIED',
            retryable: false,
            recoverable: false,
            terminal: true,
          });
          return;
        }

        if (!res.ok) {
          let errorMsg = `${t('player.apiError')}: ${res.status}`;
          let errorPayload: unknown = null;
          let errorDetails: string | null = null;
          try {
            const { json, text } = await readResponseBody(res);
            const responseRequestId =
              (json && typeof json === 'object' ? (json.requestId as string | undefined) : undefined) ||
              res.headers.get('X-Request-ID') ||
              undefined;

            if (json && typeof json === 'object') {
              const title = typeof json.title === 'string' && json.title ? json.title : null;
              const message = typeof json.message === 'string' && json.message ? json.message : null;
              if (title) {
                errorMsg = title;
              } else if (message) {
                errorMsg = message;
              }

              const detailParts: string[] = [];
              if (typeof json.code === 'string' && json.code) detailParts.push(`code=${json.code}`);
              if (typeof json.detail === 'string' && json.detail) detailParts.push(json.detail);
              if (json.details) {
                detailParts.push(typeof json.details === 'string' ? json.details : JSON.stringify(json.details));
              }
              if (responseRequestId) detailParts.push(`requestId=${responseRequestId}`);
              if (detailParts.length > 0) {
                errorDetails = detailParts.join(' · ');
              }
              errorPayload = {
                ...json,
                status: res.status,
                requestId: responseRequestId,
              };
            } else if (text) {
              errorDetails = text;
            }
          } catch (e) {
            debugWarn("Failed to parse error response", e);
          }
          throw normalizePlayerError(errorPayload ?? {
            status: res.status,
            title: errorMsg,
            details: errorDetails,
          }, {
            fallbackTitle: errorMsg,
            fallbackDetail: errorDetails ?? undefined,
            status: res.status,
          });
        }

        // raw-fetch-justified bypasses the generated client, so the
        // IntentAcceptedResponse arrives as unvalidated JSON. Guard the one
        // load-bearing field explicitly: sessionId must be a non-empty string
        // (a non-string truthy value would otherwise slip through `?? null` and
        // poison the session poll). On violation, surface a typed contract error
        // carrying the requestId — like the other failures in this flow, not a
        // bare Error.
        const intentJson: unknown = await res.json();
        const intentRecord = intentJson && typeof intentJson === 'object' ? (intentJson as Record<string, unknown>) : null;
        const intentRequestId =
          (typeof intentRecord?.requestId === 'string' ? intentRecord.requestId : undefined) ??
          (res.headers?.get ? res.headers.get('X-Request-ID') : undefined) ??
          undefined;
        newSessionId = typeof intentRecord?.sessionId === 'string' ? intentRecord.sessionId.trim() || null : null;
        if (!newSessionId) {
          throw normalizePlayerError(
            { title: t('player.sessionFailed'), detail: 'Intent response missing or invalid sessionId.', requestId: intentRequestId },
            { fallbackTitle: t('player.sessionFailed') },
          );
        }
        if (intentRequestId) setTraceId(intentRequestId);
        setActiveSessionId(newSessionId);
        dispatchPlayback({
          type: 'normative.session.phase.changed',
          playbackEpoch,
          sessionEpoch,
          phase: 'starting',
          requestId: intentRequestId ?? null,
        });
        const session = await waitForSessionReady(newSessionId);
        if (isStaleSessionEpoch(playbackEpoch, sessionEpoch)) {
          await sendStopIntent(newSessionId);
          return;
        }

        dispatchPlayback({
          type: 'normative.session.phase.changed',
          playbackEpoch,
          sessionEpoch,
          phase: 'ready',
          requestId: session.requestId ?? intentRequestId ?? null,
        });
        setStatus('ready');
        const streamUrl = session.playbackUrl;
        if (!streamUrl) {
          throw new Error(t('player.streamUrlMissing'));
        }
        playHls(streamUrl, liveEngine);
        setActiveHlsEngine(liveEngine);

      } catch (err) {
        const stalePlayback = isStalePlaybackEpoch(playbackEpoch);
        const staleSession = sessionEpoch > 0 && isStaleSessionEpoch(playbackEpoch, sessionEpoch);
        if (stalePlayback || staleSession) {
          if (newSessionId) {
            await sendStopIntent(newSessionId);
          }
          return;
        }
        if (newSessionId) {
          await sendStopIntent(newSessionId);
        }
        if (!newSessionId || sessionIdRef.current === newSessionId) {
          clearSessionLeaseState();
        }
        debugError(err);
        mergeSessionPlaybackTrace(extractPlaybackTrace(err));
        reportPlaybackFailure(normalizeRuntimePlaybackError(err, t('player.serverError')), {
          source: newSessionId ? 'native-host' : 'backend',
        });
        setStatus('error');
      }
    } finally {
      startIntentInFlight.current = false;
    }
  }, [src, recordingId, sRef, apiBase, authHeaders, clearPlaybackState, clearPlayerError, ensureSessionCookie, waitForSessionReady, hasActivePlayback, mergeSessionPlaybackTrace, playHls, sendStopIntent, clearSessionLeaseState, t, startRecordingPlayback, applyAutoplayMute, gatherPlaybackCapabilitiesForPlayer, resolvePreferredHlsEngine, resolvePreferredHlsEngineForCapabilities, setActiveSessionId, setPlayerError, requestedDuration, teardownActivePlayback, beginNativePlayback, channel?.name, nativePlaybackState, allocatePlaybackEpoch, beginPlaybackAttempt, isStalePlaybackEpoch, allocateSessionEpoch, isStaleSessionEpoch, sessionIdRef]);

  const stopStream = useCallback(async (skipClose: boolean = false): Promise<void> => {
    userPauseIntentRef.current = true;
    const stopEpoch = allocatePlaybackEpoch();
    await teardownActivePlayback();
    markPlaybackStopped(stopEpoch);
    if (onClose && !skipClose) onClose();
  }, [allocatePlaybackEpoch, markPlaybackStopped, onClose, teardownActivePlayback]);

  const handleRetry = useCallback(async () => {
    try {
      await stopStream(true);
    } finally {
      startIntentInFlight.current = false;
      void startStream();
    }
  }, [stopStream, startStream]);
  // --- Effects ---
  // Update sRef on channel change
  useEffect(() => {
    if (channel) {
      const ref = (channel.serviceRef || channel.id || '').trim();
      if (ref) setSRef(ref);
    }
  }, [channel]);

  useEffect(() => {
    if (!autoStart || mounted.current) return;
    // UI-INV-PLAYER-001: Autostart requires an explicit source.
    const normalizedRef = sRef.trim();
    const hasSource = !!(src || recordingId || normalizedRef);
    if (hasSource) {
      mounted.current = true;
      startStream(normalizedRef || undefined);
    }
  }, [autoStart, src, recordingId, sRef, startStream]);

  useEffect(() => {
    dispatchPlayback({
      type: 'system.requested_duration.synced',
      durationSeconds: requestedDuration,
    });
  }, [requestedDuration]);

  const isImmediateStartupStatus =
    status === 'starting' || status === 'priming' || status === 'building';
  const isNativeEngine = activeHlsEngine === 'native';
  const hasTerminalStatus = status === 'idle' || status === 'error' || status === 'stopped';
  const shouldKeepHostAwake =
    hostEnvironment.supportsKeepScreenAwake &&
    isDocumentVisible &&
    !hasTerminalStatus &&
    status !== 'paused';
  const shouldHoldNativeVideo =
    isNativeEngine && !showNativeVideo && !hasTerminalStatus;
  const isOverlayStartupStatus =
    isImmediateStartupStatus || status === 'buffering' || shouldHoldNativeVideo;
  const overlayStatus: PlayerStatus = shouldHoldNativeVideo ? 'buffering' : status;

  useEffect(() => {
    if (!hostEnvironment.supportsKeepScreenAwake) {
      return;
    }

    setHostPlaybackActive(shouldKeepHostAwake);
    return () => setHostPlaybackActive(false);
  }, [hostEnvironment.supportsKeepScreenAwake, shouldKeepHostAwake]);

  useEffect(() => {
    if (isNativePlaybackHost && nativePlaybackState?.activeRequest) {
      return;
    }

    if (!hostEnvironment.isTv) {
      return;
    }

    const video = videoRef.current;
    if (!video) {
      return;
    }

    const inPictureInPicture = document.pictureInPictureElement === video;
    if (!isDocumentVisible && !inPictureInPicture) {
      if (!video.paused && !userPauseIntentRef.current && !hasTerminalStatus) {
        visibilityManagedPauseRef.current = true;
        video.pause();
        setStatus('paused');
      }
      return;
    }

    if (!visibilityManagedPauseRef.current) {
      return;
    }

    visibilityManagedPauseRef.current = false;
    if (userPauseIntentRef.current || hasTerminalStatus) {
      return;
    }

    setStatus((current) => (current === 'paused' ? 'buffering' : current));
    void video.play().catch((err) => {
      debugWarn('[V3Player] Host resume play blocked', err);
    });
  }, [hasTerminalStatus, hostEnvironment.isTv, isDocumentVisible, isNativePlaybackHost, nativePlaybackState, setStatus, status, videoRef]);

  // Browser (non-TV) foreground recovery. iOS Safari and desktop browsers
  // suspend the decoder while backgrounded and do not auto-resume inline
  // <video> on return — the frame stays black/frozen. Repair only on the
  // hidden->visible edge; deliberately NO pause-on-hide (that would break
  // desktop tab-switches). TV keeps its own effect above, untouched.
  useEffect(() => {
    if (hostEnvironment.isTv) {
      return;
    }
    if (isNativePlaybackHost && nativePlaybackState?.activeRequest) {
      return;
    }

    const video = videoRef.current;
    if (!video) {
      return;
    }

    if (!isDocumentVisible) {
      wasHiddenRef.current = true;
      return;
    }

    const wasHidden = wasHiddenRef.current;
    wasHiddenRef.current = false;

    const action = decideForegroundResume({
      wasHidden,
      isPiP: document.pictureInPictureElement === video,
      status,
      userPaused: userPauseIntentRef.current,
      hasTerminal: hasTerminalStatus,
    });

    if (action === 'none') {
      return;
    }

    if (action === 'retry') {
      // Reaped session (heartbeat 410/404 during background) — re-establish.
      void handleRetry();
      return;
    }

    // action === 'play'
    setStatus((current) => (current === 'paused' ? 'buffering' : current));
    void video.play().catch((err: unknown) => {
      if ((err as { name?: string } | null)?.name === 'NotAllowedError') {
        // iOS blocked the programmatic resume; the existing play/pause control
        // is the user-gesture tap-to-resume.
        setStatus('paused');
      } else {
        debugWarn('[V3Player] Browser resume play blocked', err);
      }
    });
  }, [handleRetry, hasTerminalStatus, hostEnvironment.isTv, isDocumentVisible, isNativePlaybackHost, nativePlaybackState, setStatus, status, videoRef]);

  const showBufferingOverlay = useBufferingOverlay(status);

  const startupElapsedSeconds = useStartupElapsed(isOverlayStartupStatus);

  useEffect(() => {
    return () => {
      cleanupPlaybackResourcesRef.current();
    };
  }, []);

  useEffect(() => {
    if (!hostEnvironment.isTv) {
      return;
    }

    const frame = window.requestAnimationFrame(() => {
      requestHostInputFocus();

      const activeElement = document.activeElement as HTMLElement | null;
      if (activeElement && activeElement !== document.body && activeElement !== document.documentElement) {
        return;
      }

      const nextFocusTarget = containerRef.current?.querySelector<HTMLElement>(
        'button:not([disabled]), a[href], input:not([disabled]), [tabindex]:not([tabindex="-1"])',
      );
      nextFocusTarget?.focus();
    });

    return () => window.cancelAnimationFrame(frame);
  }, [hostEnvironment.isTv, onClose]);
  useTvInitialFocus({
    enabled: hostEnvironment.isTv && showResumeOverlay,
    targetRef: resumePrimaryActionRef,
  });

  // Overlay styles
  // ADR-00X: Overlay styles are controlled via styles.overlay in V3Player.module.css
  // Static layout styles are in V3Player.module.css (scoped)

  const spinnerLabel =
    isOverlayStartupStatus
      ? (overlayStatus === 'buffering' && playbackMode === 'VOD' && activeRecordingRef.current && vodStreamMode === 'direct_mp4')
        ? t('player.preparingDirectPlay') // Show explicit preparing for VOD buffering
        : resolveStartupOverlayLabel(
          overlayStatus,
          `${t(`player.statusStates.${overlayStatus}`, { defaultValue: overlayStatus })}…`,
          sessionProfileReason,
          t,
        )
      : '';
  const spinnerSupport =
    isOverlayStartupStatus
      ? resolveStartupOverlaySupport(sessionProfileReason, t)
      : '';
  const showStartupOverlay =
    isImmediateStartupStatus ||
    (status === 'buffering' && showBufferingOverlay) ||
    shouldHoldNativeVideo;
  const useNativeBufferingSafeOverlay = shouldHoldNativeVideo;
  const showNativeBufferingMask = shouldHoldNativeVideo || showNativeVideoVeil;
  const useMinimalStartupChrome = showStartupOverlay && (hostEnvironment.isTv || Boolean(onClose));
  const showPlaybackChrome = !useMinimalStartupChrome;

  useEffect(() => {
    const video = videoRef.current;
    if (!video) {
      return;
    }

    if (isNativeEngine && showNativeBufferingMask) {
      if (!video.muted) {
        video.muted = true;
        nativeVideoTempMutedRef.current = true;
      }
      return;
    }

    if (nativeVideoTempMutedRef.current) {
      video.muted = false;
      nativeVideoTempMutedRef.current = false;
    }
  }, [isNativeEngine, showNativeBufferingMask, videoRef]);

  // Statistics never lie: once a session exists (or is starting), the EXECUTION-
  // OUTPUT fields must reflect that session's executed trace, never the pre-roll
  // prediction (playbackObservability). The trace lands via the native poll / MSE
  // snapshot loop; until then show nothing rather than a guess. Request-context
  // fields (client path, profile, intents, host) keep the preview — they describe
  // the request, not the output, so the preview is faithful during startup.
  const hasActiveOrStartingSession =
    Boolean(sessionIdRef.current || nativeSessionId || nativePlaybackState?.session?.sessionId) ||
    sessionPlaybackTrace !== null;
  const effectiveClientPath =
    sessionPlaybackTrace?.clientPath ||
    playbackObservability?.clientPath ||
    formatClientPath(capabilitySnapshot);
  const effectiveSessionId =
    sessionIdRef.current ||
    nativeSessionId ||
    nativePlaybackState?.session?.sessionId ||
    sessionPlaybackTrace?.sessionId ||
    null;
  const effectiveRequestProfile =
    sessionPlaybackTrace?.requestProfile ??
    playbackObservability?.requestProfile ??
    null;
  const effectiveRequestedIntent =
    sessionPlaybackTrace?.requestedIntent ??
    playbackObservability?.requestedIntent ??
    effectiveRequestProfile;
  const effectiveResolvedIntent =
    sessionPlaybackTrace?.resolvedIntent ??
    playbackObservability?.resolvedIntent ??
    null;
  const effectiveQualityRung =
    sessionPlaybackTrace?.qualityRung ??
    (!hasActiveOrStartingSession ? playbackObservability?.qualityRung : null) ??
    null;
  const effectiveAudioQualityRung =
    sessionPlaybackTrace?.audioQualityRung ??
    (!hasActiveOrStartingSession ? playbackObservability?.audioQualityRung : null) ??
    null;
  const effectiveVideoQualityRung =
    sessionPlaybackTrace?.videoQualityRung ??
    (!hasActiveOrStartingSession ? playbackObservability?.videoQualityRung : null) ??
    null;
  const effectiveDegradedFrom =
    sessionPlaybackTrace?.degradedFrom ??
    (!hasActiveOrStartingSession ? playbackObservability?.degradedFrom : null) ??
    null;
  const effectiveTargetProfile =
    sessionPlaybackTrace?.targetProfile ??
    (!hasActiveOrStartingSession ? playbackObservability?.targetProfile : null) ??
    null;
  const effectiveTargetProfileHash =
    sessionPlaybackTrace?.targetProfileHash ??
    (!hasActiveOrStartingSession ? playbackObservability?.targetProfileHash : null) ??
    null;
  const effectiveOperator =
    sessionPlaybackTrace?.operator ??
    playbackObservability?.operator ??
    null;
  const effectiveHostPressureBand =
    sessionPlaybackTrace?.hostPressureBand ??
    playbackObservability?.hostPressureBand ??
    null;
  const effectiveHostOverrideApplied =
    sessionPlaybackTrace?.hostOverrideApplied ??
    playbackObservability?.hostOverrideApplied ??
    false;
  const effectiveForcedIntent = effectiveOperator?.forcedIntent ?? null;
  const effectiveOperatorMaxQualityRung = effectiveOperator?.maxQualityRung ?? null;
  const effectiveOperatorRuleName = effectiveOperator?.ruleName ?? null;
  const effectiveOperatorRuleScope = effectiveOperator?.ruleScope ?? null;
  const effectiveClientFallbackDisabled = effectiveOperator?.clientFallbackDisabled ?? false;
  const effectiveOperatorOverrideApplied = effectiveOperator?.overrideApplied ?? false;
  const sourceProfileSummary = formatSourceProfileSummary(sessionPlaybackTrace?.source);
  const ffmpegPlanSummary = formatFfmpegPlanSummary(sessionPlaybackTrace?.ffmpegPlan);
  const firstFrameLabel = formatFirstFrameLabel(sessionPlaybackTrace?.firstFrameAtMs);
  const fallbackSummary = formatFallbackSummary(sessionPlaybackTrace);
  const stopSummary = formatStopSummary(sessionPlaybackTrace);
  const hostPressureSummary = formatHostPressureSummary(effectiveHostPressureBand, effectiveHostOverrideApplied);
  const showVerboseErrorTelemetry = !isCompactTouchLayout;
  const audioToggleLabel = isMuted ? t('player.unmute') : t('player.mute');
  const audioToggleIcon = isMuted ? '🔊' : '🔇';
  const statsTitle = t('player.statsTitle', { defaultValue: 'Technical Stats' });
  const hlsLevelValue = hlsRef.current ? (stats.levelIndex === -1 ? 'Auto' : String(stats.levelIndex)) : 'Native / Direct';
  const fullscreenPathValue = isWebKitFullscreenActive
    ? 'native-webkit'
    : isFullscreen
      ? 'container'
      : prefersDesktopNativeFullscreen
        ? 'desktop-webkit-ready'
        : supportsNativeFullscreen
          ? 'webkit-available'
          : 'web-only';
  const statsRows: V3PlayerLabeledValue[] = [
    { label: t('common.session', { defaultValue: 'Session' }), value: effectiveSessionId || '-' },
    { label: t('common.requestId', { defaultValue: 'Request ID' }), value: sessionPlaybackTrace?.requestId || traceId },
    { label: t('player.clientPath', { defaultValue: 'Client Path' }), value: effectiveClientPath || '-' },
    { label: t('player.requestProfile', { defaultValue: 'Request Profile' }), value: formatRequestProfileLabel(effectiveRequestProfile) },
    { label: t('player.requestedIntent', { defaultValue: 'Requested Intent' }), value: formatRequestProfileLabel(effectiveRequestedIntent) },
    { label: t('player.resolvedIntent', { defaultValue: 'Resolved Intent' }), value: formatRequestProfileLabel(effectiveResolvedIntent) },
    { label: t('player.qualityRung', { defaultValue: 'Quality Rung' }), value: formatQualityRungLabel(effectiveQualityRung) },
    { label: t('player.audioQualityRung', { defaultValue: 'Audio Quality Rung' }), value: formatQualityRungLabel(effectiveAudioQualityRung) },
    { label: t('player.videoQualityRung', { defaultValue: 'Video Quality Rung' }), value: formatQualityRungLabel(effectiveVideoQualityRung) },
    { label: t('player.degradedFrom', { defaultValue: 'Degraded From' }), value: formatRequestProfileLabel(effectiveDegradedFrom) },
    { label: t('player.hostPressure', { defaultValue: 'Host Pressure' }), value: effectiveHostPressureBand || '-' },
    { label: t('player.hostOverrideApplied', { defaultValue: 'Host Override Applied' }), value: formatBooleanLabel(effectiveHostOverrideApplied) },
    { label: t('player.forcedIntent', { defaultValue: 'Forced Intent' }), value: formatRequestProfileLabel(effectiveForcedIntent) },
    { label: t('player.operatorMaxQualityRung', { defaultValue: 'Operator Max Quality' }), value: formatQualityRungLabel(effectiveOperatorMaxQualityRung) },
    { label: t('player.operatorRuleName', { defaultValue: 'Operator Rule' }), value: effectiveOperatorRuleName || '-' },
    { label: t('player.operatorRuleScope', { defaultValue: 'Operator Rule Scope' }), value: effectiveOperatorRuleScope || '-' },
    { label: t('player.clientFallbackDisabled', { defaultValue: 'Client Fallback Disabled' }), value: formatBooleanLabel(effectiveClientFallbackDisabled) },
    { label: t('player.operatorOverrideApplied', { defaultValue: 'Operator Override Applied' }), value: formatBooleanLabel(effectiveOperatorOverrideApplied) },
    { label: t('player.sourceProfile', { defaultValue: 'Source Profile' }), value: sourceProfileSummary },
    { label: t('player.outputProfile', { defaultValue: 'Output Profile' }), value: formatTargetProfileSummary(effectiveTargetProfile) },
    { label: t('player.profileHash', { defaultValue: 'Profile Hash' }), value: effectiveTargetProfileHash || '-' },
    { label: t('player.execution', { defaultValue: 'Execution' }), value: formatExecutionLabel(effectiveTargetProfile) },
    { label: t('player.ffmpegPlan', { defaultValue: 'FFmpeg Plan' }), value: ffmpegPlanSummary },
    { label: t('player.firstFrame', { defaultValue: 'First Frame' }), value: firstFrameLabel },
    { label: t('player.fallbacks', { defaultValue: 'Fallbacks' }), value: fallbackSummary },
    { label: t('player.stopReason', { defaultValue: 'Stop' }), value: stopSummary },
    { label: t('player.outputKind', { defaultValue: 'Output Kind' }), value: playbackObservability?.selectedOutputKind || '-' },
    { label: t('player.resolution'), value: stats.resolution },
    { label: t('player.bandwidth'), value: stats.bandwidth > 0 ? `${stats.bandwidth} kbps` : '-' },
    { label: t('player.bufferHealth'), value: `${stats.bufferHealth}s` },
    { label: t('player.latency'), value: stats.latency !== null ? `${stats.latency}s` : '-' },
    { label: t('player.fps'), value: String(stats.fps) },
    { label: t('player.dropped'), value: String(stats.droppedFrames) },
    { label: t('player.hlsLevel'), value: hlsLevelValue },
    { label: t('player.segDuration'), value: stats.buffer > 0 ? `${stats.buffer}s` : '-' },
    { label: t('player.seekableRange', { defaultValue: 'Seekable' }), value: `${formatClock(seekableStart)} -> ${formatClock(seekableEnd)}` },
    { label: t('player.playhead', { defaultValue: 'Playhead' }), value: formatClock(currentPlaybackTime) },
    { label: t('player.seekWindow', { defaultValue: 'Seek Window' }), value: hasSeekWindow ? formatClock(windowDuration) : '-' },
    { label: t('player.fullscreenPath', { defaultValue: 'Fullscreen Path' }), value: fullscreenPathValue },
  ];
  const errorTelemetryRows: V3PlayerLabeledValue[] = showVerboseErrorTelemetry
    ? [
      stopSummary !== '-' ? { label: t('player.stopReason', { defaultValue: 'Stop' }), value: stopSummary } : null,
      hostPressureSummary !== '-' ? { label: t('player.hostPressure', { defaultValue: 'Host Pressure' }), value: hostPressureSummary } : null,
      fallbackSummary !== '-' ? { label: t('player.fallbacks', { defaultValue: 'Fallbacks' }), value: fallbackSummary } : null,
      ffmpegPlanSummary !== '-' ? { label: t('player.ffmpegPlan', { defaultValue: 'FFmpeg Plan' }), value: ffmpegPlanSummary } : null,
    ].filter((row): row is V3PlayerLabeledValue => row !== null)
    : [];
  const viewState: V3PlayerViewState = {
    channelName: channel?.name ?? null,
    programmeTitle: playbackMode === 'LIVE' ? (liveNowPlaying.title ?? channel?.name ?? null) : (channel?.name ?? null),
    programmeDesc: playbackMode === 'LIVE' ? liveNowPlaying.desc : null,
    useOverlayLayout: Boolean(onClose),
    userIdle: isIdle,
    showCloseButton: Boolean(onClose),
    closeButtonLabel: t('player.closePlayer'),
    showStatsOverlay: showStats && showPlaybackChrome,
    statsTitle,
    statusLabel: t('player.status'),
    statusChipLabel: t(`player.statusStates.${status}`, { defaultValue: status }),
    statusChipState: status === 'ready' ? 'live' : status === 'error' ? 'error' : 'idle',
    statsRows,
    showNativeBufferingMask,
    hideVideoElement: showNativeBufferingMask,
    showStartupBackdrop: useMinimalStartupChrome,
    showStartupOverlay,
    useNativeBufferingSafeOverlay,
    overlayStatusLabel: t(`player.statusStates.${overlayStatus}`, { defaultValue: overlayStatus }),
    overlayStatusState: overlayStatus === 'buffering' ? 'live' : 'idle',
    spinnerEyebrow: t('player.startupSurfaceEyebrow', { defaultValue: 'Live startup' }),
    spinnerLabel,
    spinnerSupport,
    startupElapsedLabel: t('player.startupElapsed', {
      defaultValue: 'Wait {{seconds}}s',
      seconds: startupElapsedSeconds,
    }),
    showOverlayStopAction: useMinimalStartupChrome && !onClose,
    overlayStopLabel: t('common.stop'),
    videoClassName: '',
    autoPlay: Boolean(autoStart),
    error,
    showErrorDetails,
    errorRetryLabel: t('common.retry'),
    errorTelemetryRows,
    errorDetailToggleLabel: error?.detail ? (showErrorDetails ? t('common.hideDetails') : t('common.showDetails')) : null,
    errorSessionLabel: `${t('common.session')}: ${effectiveSessionId || t('common.notAvailable')}`,
    showPlaybackChrome,
    showSeekControls: hasSeekWindow,
    seekBack15mLabel: t('player.seekBack15m'),
    seekBack60sLabel: t('player.seekBack60s'),
    seekBack15sLabel: t('player.seekBack15s'),
    seekForward15sLabel: t('player.seekForward15s'),
    seekForward60sLabel: t('player.seekForward60s'),
    seekForward15mLabel: t('player.seekForward15m'),
    playPauseLabel: isPlaying ? t('player.pause') : t('player.play'),
    playPauseIcon: isPlaying ? '⏸' : '▶',
    seekableStart,
    seekableEnd,
    startTimeDisplay,
    endTimeDisplay,
    windowDuration,
    relativePosition,
    isLiveMode,
    isAtLiveEdge,
    liveButtonLabel: t('player.goLive'),
    showServiceInput: !hasSeekWindow && !channel && !recordingId && !src,
    serviceRef: sRef,
    showManualStartButton: !autoStart && !src && !recordingId,
    manualStartLabel: t('common.startStream'),
    manualStartDisabled: startIntentInFlight.current,
    showDvrModeButton: showDvrModeButton && !canToggleFullscreen,
    dvrModeLabel: t('player.dvrMode'),
    showNativeFullscreenButton: prefersDesktopNativeFullscreen && canEnterNativeFullscreen && !isFullscreen,
    nativeFullscreenTitle: t('player.nativeFullscreenTitle', { defaultValue: 'Open Apple player' }),
    nativeFullscreenLabel: t('player.nativeFullscreenLabel', { defaultValue: 'Native' }),
    showFullscreenButton: canToggleFullscreen,
    fullscreenLabel: isFullscreen
      ? t('player.exitFullscreenLabel', { defaultValue: 'Exit fullscreen' })
      : t('player.fullscreenLabel', { defaultValue: 'Fullscreen' }),
    fullscreenActive: isFullscreen,
    showVolumeControls: canToggleMute,
    audioToggleLabel,
    audioToggleIcon,
    audioToggleActive: !isMuted,
    canAdjustVolume,
    volume: isMuted ? 0 : volume,
    deviceVolumeHint: t('player.deviceVolumeHint', { defaultValue: 'Use device buttons' }),
    showPipButton: canTogglePiP,
    pipTitle: t('player.pipTitle'),
    pipLabel: t('player.pipLabel'),
    pipActive: isPip,
    statsLabel: t('player.statsLabel'),
    statsActive: showStats,
    showStopButton: !onClose,
    stopLabel: t('common.stop'),
    showResumeOverlay: showResumeOverlay && Boolean(resumeState),
    resumeTitle: t('player.resumeTitle'),
    resumePrompt: resumeState
      ? t('player.resumePrompt', { time: formatClock(resumeState.posSeconds) })
      : '',
    resumeActionLabel: t('player.resumeAction'),
    startOverLabel: t('player.startOver'),
    resumePositionSeconds: resumeState?.posSeconds ?? null,
    playback: {
      durationSeconds,
    },
  };
  const actions: PlaybackOrchestratorActions = {
    stopStream,
    retry: handleRetry,
    seekBy,
    seekTo,
    seekToLiveEdge,
    togglePlayPause,
    updateServiceRef: setSRef,
    submitServiceRef(nextValue) {
      void startStream(nextValue);
    },
    startStream(refToUse) {
      void startStream(refToUse);
    },
    enterDVRMode,
    enterNativeFullscreen,
    toggleFullscreen,
    toggleMute,
    changeVolume: handleVolumeChange,
    togglePiP,
    toggleStats,
    toggleErrorDetails() {
      setShowErrorDetails((current) => !current);
    },
    resumeFrom(positionSeconds) {
      seekWhenReady(positionSeconds);
      setShowResumeOverlay(false);
    },
    startOver() {
      seekWhenReady(0);
      setShowResumeOverlay(false);
    },
  };

  return {
    viewState,
    actions,
  };
}
// cspell:ignore remux arrowleft arrowright enterpictureinpicture leavepictureinpicture kbps Remux
