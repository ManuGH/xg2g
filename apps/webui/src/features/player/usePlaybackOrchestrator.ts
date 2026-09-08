import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import type { RefObject } from 'react';
import { useTranslation } from 'react-i18next';
import Hls from './lib/hlsRuntime';
import {
  getSessionEvents,
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
  VideoElementRef,
  PlayerAudioTrack,
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
  clearNetworkStarvationHold,
  createAutomaticProfileMemory,
  gatherPlaybackClientContext,
  noteNetworkStarvation,
  normalizePlaybackProfileSelection,
  resolvePlaybackProfileForPreflight,
  resolvePlaybackRequestProfile,
  type PlaybackProfileSelection,
} from './utils/playbackRequestProfile';
import {
  applyPlaybackNetworkProbe,
  measurePlaybackNetwork,
} from './utils/playbackNetworkProbe';
import {
  resolvePlaybackLinkProfile,
  type PlaybackLinkProfile,
} from './utils/playbackLinkProfile';
import { normalizePlayerError } from '../../lib/appErrors';
import { notifyAuthRequiredIfUnauthorizedResponse } from '../../lib/httpProblem';
import { useTvInitialFocus } from '../../hooks/useTvInitialFocus';
import { translatePlaybackReason } from './utils/sessionReason';
import {
  createInitialPlaybackDomainState,
} from './orchestrator/playbackMachine';
import { usePlaybackController } from './orchestrator/usePlaybackController';
import { PlaybackHttpError, type PlaybackController } from './orchestrator/playbackController';
import { createDefaultLiveSessionTransport, type SessionReadyResult } from './orchestrator/liveSessionTransport';
import type { PlaybackCommand, PlaybackDomainState, PlaybackStopReason } from './orchestrator/playbackTypes';
import { sessionTimeline } from './orchestrator/sessionTimeline';
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
  resolvePlaybackObservability,
  type PlaybackObservability,
} from './orchestrator/observabilityFormatters';
import { buildContractState } from './orchestrator/contractErrors';
import {
  supportsManagedNativePlayback,
} from './orchestrator/nativePlaybackHelpers';
import { resolveSessionPhaseFromState } from './orchestrator/sessionPhase';
import { formatDvrPositionDisplay } from './orchestrator/dvrPositionDisplay';
import { usePlaybackStateSetters } from './orchestrator/usePlaybackStateSetters';
import { usePlaybackResourceCleanup } from './orchestrator/usePlaybackResourceCleanup';
import { useTelemetryEmitter } from './orchestrator/useTelemetryEmitter';
import { useDocumentVisibility } from './orchestrator/useDocumentVisibility';
import { useOnlineStatus } from './orchestrator/useOnlineStatus';
import { decideForegroundResume } from './orchestrator/foregroundResume';
import { decideOnlineRecovery } from './orchestrator/onlineRecovery';
import {
  shouldWatchForNetworkRecovery,
  useNetworkRecoveryWatchdog,
} from './orchestrator/useNetworkRecoveryWatchdog';
import { startResumePlaybackRecovery } from './orchestrator/resumePlaybackRecovery';
import { useBufferingOverlay } from './orchestrator/useBufferingOverlay';

import { useStartupElapsed } from './orchestrator/useStartupElapsed';
import { useNativeVideoReveal } from './orchestrator/useNativeVideoReveal';
import { buildPlayerViewState, type V3PlayerLabeledValue, type V3PlayerViewState } from './components/playerViewStateModel';
import { useLiveNowPlaying } from './useLiveNowPlaying';
import { buildPlayerMediaSessionModel } from './components/playerMediaSessionModel';
import { getManagedMseAv1Support, formatManagedMseAv1 } from './utils/managedMseAv1';
import { useNativePlaybackBridge } from './orchestrator/useNativePlaybackBridge';
import {
  buildAuthDeniedFailure,
  buildBlockedContractFailure,
  buildContractConsumedTelemetry,
  buildLeaseBusyFailure,
  buildMissingOutputUrlFailure,
  buildRecordingGoneFailure,
  buildSessionExpiredFailure,
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

export type { V3PlayerLabeledValue, V3PlayerViewState };

export interface PlaybackOrchestratorActions {
  stopStream(skipClose?: boolean): Promise<void>;
  retry(): Promise<void>;
  seekBy(deltaSeconds: number): void;
  changeAudioTrack(trackId: number): void;
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
  changeProfile(profile: string): void;
}

export interface UsePlaybackOrchestratorResult {
  viewState: V3PlayerViewState;
  actions: PlaybackOrchestratorActions;
  controller: PlaybackController;
  playbackState: PlaybackDomainState;
}

function areAudioTrackListsEqual(current: PlayerAudioTrack[], next: PlayerAudioTrack[]): boolean {
  return current.length === next.length && current.every((track, index) => {
    const candidate = next[index];
    return candidate !== undefined
      && track.key === candidate.key
      && track.engineIndex === candidate.engineIndex
      && track.nativeId === candidate.nativeId
      && track.language === candidate.language
      && track.label === candidate.label
      && track.kind === candidate.kind
      && track.id === candidate.id
      && track.name === candidate.name;
  });
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
  const recordingTitle = 'recordingTitle' in props ? props.recordingTitle : undefined;
  const recordingDateLabel = 'recordingDateLabel' in props ? props.recordingDateLabel : undefined;
  const zapChannels = 'channels' in props ? props.channels : undefined;
  const onSwitchChannel = 'onSwitchChannel' in props ? props.onSwitchChannel : undefined;

  const [sRef, setSRef] = useState<string>(
    (channel?.serviceRef || channel?.id || '').trim()
  );
  const [explicitProfile, setExplicitProfile] = useState<PlaybackProfileSelection>(() => {
    try {
      return normalizePlaybackProfileSelection(localStorage.getItem('xg2g.player.explicitProfile'));
    } catch {
      return 'auto';
    }
  });
  const ttffStartT0Ref = useRef<number | null>(null);
  const ttffManifestT1Ref = useRef<number | null>(null);
  const [ttffMetrics, setTtffMetrics] = useState<{
    ttffMs: number;
    manifestMs: number;
    bufferMs: number;
  } | null>(null);

  const [audioTracks, setAudioTracks] = useState<PlayerAudioTrack[]>([]);
  const [activeAudioTrack, setActiveAudioTrack] = useState<number>(-1);
  const handleAudioTracksUpdated = useCallback((nextTracks: PlayerAudioTrack[]) => {
    setAudioTracks((currentTracks) => (
      areAudioTrackListsEqual(currentTracks, nextTracks) ? currentTracks : nextTracks
    ));
  }, []);

  const handleAttemptStarted = useCallback((epoch: number) => {
    sessionTimeline.beginAttempt(epoch);
    ttffStartT0Ref.current = performance.now();
    ttffManifestT1Ref.current = null;
    setTtffMetrics(null);
  }, []);

  const handlePlaybackMilestone = useCallback((milestone: 'manifest' | 'firstFrame') => {
    const startT0 = ttffStartT0Ref.current;
    if (startT0 === null) return;

    const now = performance.now();
    if (milestone === 'manifest') {
      if (ttffManifestT1Ref.current === null) {
        ttffManifestT1Ref.current = now;
        debugLog(`[V3Player] TTFF Milestone: Manifest loaded in ${Math.round(now - startT0)}ms`);
      }
    } else if (milestone === 'firstFrame') {
      const manifestT1 = ttffManifestT1Ref.current ?? now;
      const ttffMs = Math.round(now - startT0);
      const manifestMs = Math.round(manifestT1 - startT0);
      const bufferMs = Math.round(now - manifestT1);

      setTtffMetrics({ ttffMs, manifestMs, bufferMs });
      ttffStartT0Ref.current = null;

      telemetry.emit('ui.player.ttff', {
        ttffMs,
        manifestMs,
        bufferMs,
        playbackMode: playbackStateRef.current.playbackMode,
        serviceRef: sRef,
      });

      debugLog(`[V3Player] TTFF Complete: ${ttffMs}ms (Manifest: ${manifestMs}ms, Buffer: ${bufferMs}ms)`);
    }
  }, [sRef]);

  const executeCommandRef = useRef<((cmd: PlaybackCommand) => void | Promise<unknown>) | null>(null);
  const requestedDuration = useMemo(() => (duration && duration > 0 ? duration : null), [duration]);

  const apiBase = useMemo(() => {
    return getApiBaseUrl();
  }, []);

  const authHeaders = useCallback((hasBody: boolean = false): Record<string, string> => {
    const headers: Record<string, string> = {};
    if (hasBody) headers['Content-Type'] = 'application/json';
    if (token) headers.Authorization = `Bearer ${token}`;
    return headers;
  }, [token]);

  const recoverSessionCookieRef = useRef<(source: string) => Promise<boolean>>(async () => false);

  const transport = useMemo(
    () => createDefaultLiveSessionTransport({
      apiBase,
      authHeaders: (hasBody?: boolean) => authHeaders(Boolean(hasBody)),
      recoverSessionCookie: (source: string) => recoverSessionCookieRef.current(source),
    }),
    [apiBase, authHeaders],
  );

  const playbackEpochRef = useRef(1);

  const {
    controller,
    state: playbackState,
    dispatch: dispatchPlayback,
  } = usePlaybackController(
    transport,
    () => createInitialPlaybackDomainState(requestedDuration),
    useCallback((command) => {
      if (executeCommandRef.current) {
        return executeCommandRef.current(command);
      }
    }, []),
    useMemo(() => ({
      requestedDuration,
      onAttemptStarted: (epoch: number) => {
        playbackEpochRef.current = epoch;
        handleAttemptStarted(epoch);
      },
    }), [requestedDuration, handleAttemptStarted]),
  );

  const allocatePlaybackEpoch = useCallback(() => {
    const epoch = controller.allocatePlaybackEpoch();
    playbackEpochRef.current = epoch;
    return epoch;
  }, [controller]);
  const beginPlaybackAttempt = controller.beginPlaybackAttempt;
  const isStalePlaybackEpoch = controller.isStalePlaybackEpoch;

  const playbackStateRef = useRef(playbackState);
  playbackStateRef.current = playbackState;

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
  const recoveryStatusRef = useRef(status);
  recoveryStatusRef.current = status;
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
  const disposedRef = useRef(false);
  const lifecycleGenerationRef = useRef(0);
  const autoFallbackTimersRef = useRef<Set<number>>(new Set());
  const startIntentInFlight = useRef<boolean>(false);
  const pendingStartRef = useRef<{
    refToUse?: string;
    profileOverride?: string;
    lifecycleGeneration: number;
  } | null>(null);
  const startStreamRef = useRef<(refToUse?: string, profileOverride?: string) => Promise<void>>(async () => {});
  const retryInFlightRef = useRef(false);
  const stopCommandCompletionRef = useRef<Promise<void>>(Promise.resolve());
  const timelineReportCompletionRef = useRef<Promise<void>>(Promise.resolve());
  const isTeardownRef = useRef<boolean>(false);
  const userPauseIntentRef = useRef<boolean>(false);
  const nativeVideoTempMutedRef = useRef(false);
  const visibilityManagedPauseRef = useRef(false);
  const wasHiddenRef = useRef(false);
  const wasOfflineRef = useRef(false);
  const cleanupPlaybackResourcesRef = useRef<() => void>(() => {});
  const activeLiveSessionIdRef = useRef<string | null>(null);
  const automaticProfileMemoryRef = useRef(createAutomaticProfileMemory());
  const linkProfileRef = useRef<PlaybackLinkProfile>('stable');

  const isLifecycleActive = useCallback((generation: number): boolean => (
    !disposedRef.current && lifecycleGenerationRef.current === generation
  ), []);

  useEffect(() => {
    disposedRef.current = false;
    const autoFallbackTimers = autoFallbackTimersRef.current;

    return () => {
      disposedRef.current = true;
      lifecycleGenerationRef.current += 1;
      // React StrictMode replays effects as setup -> cleanup -> setup. Allow the
      // second setup to issue the real autostart while the old generation drains.
      mounted.current = false;
      pendingStartRef.current = null;
      autoFallbackTimers.forEach((timerId) => window.clearTimeout(timerId));
      autoFallbackTimers.clear();
    };
  }, []);

  const lastDecodedRef = useRef<number>(0);

  // Resume State
  const [resumeState, setResumeState] = useState<ResumeState | null>(null);
  const [showResumeOverlay, setShowResumeOverlay] = useState(false);
  const [anchorStartSec, setAnchorStartSec] = useState(0);
  const dismissedResumeRecordingIdRef = useRef<string | null>(null);
  const isDocumentVisible = useDocumentVisibility();
  const isOnline = useOnlineStatus();

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
    reportPlaybackFailure,
    clearPlaybackFailure,
    clearPlayerError,
    recordContractAdvisories,
  } = usePlaybackStateSetters({
    dispatchPlayback,
    playbackStateRef,
    acceptedPlaybackEpochRef: playbackEpochRef,
    setShowErrorDetails,
  });

  const onNativePlaybackConfirmed = useCallback(() => {
    setStatus((previous) => (
      previous === 'starting' ||
      previous === 'priming' ||
      previous === 'building' ||
      previous === 'buffering' ||
      previous === 'recovering'
        ? 'playing'
        : previous
    ));
  }, [setStatus]);

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
  }, [setTraceId]);

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
        playbackEpoch: playbackStateRef.current.epoch.playback,
        sessionEpoch: playbackStateRef.current.epoch.session,
        phase: sessionPhase,
        requestId: session.requestId ?? null,
      });
    }
    setSessionProfileReason(session.profileReason ?? null);
    mergeSessionPlaybackTrace(extractPlaybackTrace(session));
  }, [dispatchPlayback, mergeSessionPlaybackTrace, setTraceId]);

  const isCompactTouchLayout = useMemo(() => hasTouchInput(), []);

  const {
    sessionIdRef,
    connectionLost,
    reportError,
    reportSessionTimeline,
    ensureSessionCookie,
    activateLiveSession: activateLiveSessionBase,
    clearSessionLeaseState: clearSessionLeaseStateBase,
    waitForSessionReady,
    recoverSessionCookie,
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

  recoverSessionCookieRef.current = recoverSessionCookie;

  const activateLiveSession = useCallback((session: SessionReadyResult) => {
    activeLiveSessionIdRef.current = session.sessionId;
    activateLiveSessionBase(session);
  }, [activateLiveSessionBase]);

  const reportTimelineSnapshot = useCallback((reason: string, events: string[]): Promise<void> => {
    telemetry.emit('ui.player.timeline', { reason, events });
    const completion = reportSessionTimeline(reason, events);
    timelineReportCompletionRef.current = completion;
    return completion;
  }, [reportSessionTimeline]);

  const finalizeTimelineForReplacement = useCallback(async (): Promise<void> => {
    if (!sessionTimeline.hasActiveAttempt()) return;
    sessionTimeline.endAttempt('attempt_replaced');
    await reportTimelineSnapshot('attempt_replaced', sessionTimeline.describe());
  }, [reportTimelineSnapshot]);

  // Stabilize callback refs for the native trace poll so the effect never
  // re-creates its interval when authHeaders or mergeSessionPlaybackTrace are
  // recreated (e.g. when token changes).
  const authHeadersRef = useRef(authHeaders);
  const mergeSessionPlaybackTraceRef = useRef(mergeSessionPlaybackTrace);
  useEffect(() => { authHeadersRef.current = authHeaders; });
  useEffect(() => { mergeSessionPlaybackTraceRef.current = mergeSessionPlaybackTrace; });


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
    const abortController = new AbortController();

    const fetchInitialTrace = async () => {
      try {
        // raw-fetch-justified: initial native session trace lookup
        const res = await fetch(`${apiBase}/sessions/${nativeSessionId}`, { headers: authHeadersRef.current() });
        if (cancelled || !res.ok) {
          return;
        }
        const session = await res.json();
        if (cancelled) {
          return;
        }
        // extractPlaybackTrace descends into the response's `.trace` wrapper.
        mergeSessionPlaybackTraceRef.current(extractPlaybackTrace(session));
      } catch {
        // Best-effort telemetry; transient errors must never disturb playback.
      }
    };
    void fetchInitialTrace();

    void getSessionEvents({
      path: { sessionID: nativeSessionId },
      signal: abortController.signal,
      onSseEvent: (event) => {
        if (cancelled || abortController.signal.aborted) return;
        if (event.event === 'session.telemetry' && event.data && typeof event.data === 'object') {
          const telemetryData = event.data as { diagnostics?: Record<string, unknown> };
          if (telemetryData.diagnostics) {
            mergeSessionPlaybackTraceRef.current({ requestId: '', runtimeDiagnostics: telemetryData.diagnostics as any });
          }
        }
      },
      onSseError: () => {
        // Best-effort telemetry; transient errors must never disturb playback.
      }
    }).then(async ({ stream }) => {
      try {
        for await (const _ of stream) {
          if (cancelled || abortController.signal.aborted) break;
        }
      } catch {
        // Stream aborted or errored, ignore
      }
    }).catch(() => {
      // Ignore start errors
    });

    return () => {
      cancelled = true;
      abortController.abort();
    };
  }, [nativeSessionId, apiBase]);

  const clearSessionLeaseState = useCallback(() => {
    activeLiveSessionIdRef.current = null;
    clearSessionLeaseStateBase();
  }, [clearSessionLeaseStateBase]);

  // Live now-playing EPG (current programme title + synopsis, auto-refreshes
  // when the programme changes). Disabled for recordings (fixed title).
  const liveNowPlaying = useLiveNowPlaying(sRef, playbackMode === 'LIVE');

  // Lock-screen / control-center metadata + hardware-key channel zapping.
  const mediaSessionModel = useMemo(() => buildPlayerMediaSessionModel({
    t,
    playbackMode,
    liveProgramTitle: liveNowPlaying?.title,
    channelName: channel?.name,
    channelLogoUrl: channel?.logoUrl,
    normalizedRecordingTitle: recordingTitle ?? '',
    recordingDateLabel,
  }), [t, playbackMode, liveNowPlaying?.title, channel?.name, channel?.logoUrl, recordingTitle, recordingDateLabel]);

  const zapAdjacentChannel = useCallback((direction: 1 | -1) => {
    if (!zapChannels || zapChannels.length === 0 || !onSwitchChannel) return;
    const currentRef = sRef;
    const index = zapChannels.findIndex((c) => (c?.serviceRef || c?.id || '').trim() === currentRef);
    const nextIndex = index >= 0
      ? (index + direction + zapChannels.length) % zapChannels.length
      : 0;
    const target = zapChannels[nextIndex];
    if (target) onSwitchChannel(target);
  }, [zapChannels, onSwitchChannel, sRef]);

  const canZapChannels = playbackMode === 'LIVE' && !!onSwitchChannel && (zapChannels?.length ?? 0) > 1;
  const mediaSessionNextChannel = useMemo(
    () => (canZapChannels ? () => zapAdjacentChannel(1) : null),
    [canZapChannels, zapAdjacentChannel],
  );
  const mediaSessionPreviousChannel = useMemo(
    () => (canZapChannels ? () => zapAdjacentChannel(-1) : null),
    [canZapChannels, zapAdjacentChannel],
  );


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
    anchorStartSec,
    onSeekOffset: (targetSec: number) => {
      if (activeRecordingRef.current) {
        startRecordingPlayback(activeRecordingRef.current, undefined, Math.round(targetSec * 1000));
      }
    },
    setStatus,
    liveSeekWindow: null,
    allowNativeFullscreen: activeHlsEngine === 'native',
    shouldForceNativeMobileHls,
    canUseDesktopWebKitFullscreen,
    mediaTitle: mediaSessionModel.title,
    mediaSubtitle: mediaSessionModel.subtitle,
    mediaArtworkUrl: mediaSessionModel.artworkUrl,
    onNextChannel: mediaSessionNextChannel,
    onPreviousChannel: mediaSessionPreviousChannel
  });

  // Resume Hook
  useResume({
    recordingId: activeRecordingId || undefined,
    duration: durationSeconds,
    anchorStartSec,
    videoRef,
    isPlaying,
    isSeekable: canSeek,
    title: recordingTitle,
    channelName: channel?.name
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
    linkProfileRef,
    t,
    reportError,
    waitForSessionReady,
    shouldPreferNativeHls: shouldPreferNativeWebKitHls,
    revealHoldMs: props.revealHoldMs,
    setStats,
    setStatus,
    clearPlaybackFailure,
    reportPlaybackFailure,
    onPlaybackMilestone: handlePlaybackMilestone,
    onAudioTracksUpdated: handleAudioTracksUpdated,
    onAudioTrackSwitched: setActiveAudioTrack,
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
    onPlaybackConfirmed: onNativePlaybackConfirmed,
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
    setAudioTracks([]);
    setActiveAudioTrack(-1);
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
  }, [
    clearPlaybackSelection,
    clearVodFetch,
    clearVodRetry,
    hlsRef,
    isNativePlaybackHost,
    nativePlaybackState,
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
    clearSessionLeaseState();
    resetChromeState();
  }, [
    clearPlaybackSelection,
    clearSessionLeaseState,
    clearVodFetch,
    clearVodRetry,
    hasActivePlayback,
    isNativePlaybackHost,
    nativePlaybackState,
    resetChromeState,
    resetPlaybackEngine,
    sleep,
  ]);

  const prepareForNextPlaybackAttempt = useCallback(async (
    hasActiveNativeRequest: boolean = false,
  ): Promise<void> => {
    await finalizeTimelineForReplacement();
    const teardown = prepareForPlaybackAttempt({
      hasActivePlayback,
      teardownActivePlayback,
      clearPlaybackState,
      hasActiveNativeRequest,
    });
    if (teardown) await teardown;
  }, [clearPlaybackState, finalizeTimelineForReplacement, hasActivePlayback, teardownActivePlayback]);

  const gatherPlaybackCapabilitiesForPlayer = useCallback(async (scope: 'live' | 'recording' = 'live'): Promise<CapabilitySnapshot> => {
    const video = videoRef.current as HTMLVideoElement | null;
    return gatherPlaybackCapabilities(scope, video);
  }, [videoRef]);

  const startRecordingPlayback = useCallback(async (
    id: string,
    profileOverride?: string,
    startOffsetMs?: number,
    epochOverride?: number,
  ): Promise<void> => {
    const lifecycleGeneration = lifecycleGenerationRef.current;
    if (!isLifecycleActive(lifecycleGeneration)) return;
    const profileForAttempt = normalizePlaybackProfileSelection(profileOverride ?? explicitProfile);
    const playbackEpoch = typeof epochOverride === 'number' ? epochOverride : allocatePlaybackEpoch();
    await prepareForNextPlaybackAttempt();
    if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(playbackEpoch)) return;
    beginPlaybackAttempt(playbackEpoch, 'VOD', 'building', true, profileForAttempt !== 'auto');
    activeRecordingRef.current = id;
    setActiveRecordingId(id);
    clearPlayerError();

    let abortController: AbortController | null = null;
    let requestCaps: CapabilitySnapshot | null;

    try {
      await ensureSessionCookie();
      if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;

      let streamUrl = '';
      let mode: VodStreamMode = null;

      try {
        const maxMetaRetries = 20;
        const [capabilities, networkProbe] = await Promise.all([
          gatherPlaybackCapabilitiesForPlayer('recording'),
          measurePlaybackNetwork(apiBase),
        ]);
        requestCaps = capabilities;
        const requestContext = applyPlaybackNetworkProbe(
          requestCaps,
          gatherPlaybackClientContext(),
          networkProbe,

        );
        if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;
        const automaticRequestProfile = resolvePlaybackRequestProfile(
          requestContext,
          requestCaps,
          'recording',
          automaticProfileMemoryRef.current,
        );
        const requestProfile = resolvePlaybackProfileForPreflight(
          profileForAttempt,
          automaticRequestProfile,
        );
        linkProfileRef.current = resolvePlaybackLinkProfile({
          requestProfile,
          probeKind: networkProbe?.kind,
          network: requestContext.network,
        });
        setCapabilitySnapshot(requestCaps);
        let rawContract: unknown = null;

        for (let i = 0; i < maxMetaRetries; i++) {
          if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;

          const { data, error, response } = await postRecordingPlaybackInfo({
            path: { recordingId: id },
            query: startOffsetMs && startOffsetMs > 0 ? { start_ms: startOffsetMs } : undefined,
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
        if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;

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
        setAnchorStartSec(normalizedContract.media.anchorStartSec ?? (startOffsetMs ? startOffsetMs / 1000 : 0));

        const nextResume = resolveResumeStateFromContract(normalizedContract, playbackDurationSeconds);
        if (
          startOffsetMs === undefined &&
          nextResume &&
          dismissedResumeRecordingIdRef.current !== id
        ) {
          setResumeState(nextResume);
          setShowResumeOverlay(true);
          setStatus('idle');
          return;
        }
      } catch (e: unknown) {
        if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;
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
                if (isLifecycleActive(lifecycleGeneration) && activeRecordingRef.current === id) {
                  startRecordingPlayback(id, profileForAttempt, startOffsetMs);
                }
              }, delay);
              return;
            }
            throw new Error('503 Service Unavailable (No Retry-After)');
          }

          if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;
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
      if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(playbackEpoch) || activeRecordingRef.current !== id) return;
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
    apiBase,
    beginPlaybackAttempt,
    clearPlayerError,
    dispatchPlayback,
    ensureSessionCookie,
    explicitProfile,
    automaticProfileMemoryRef,
    linkProfileRef,
    gatherPlaybackCapabilitiesForPlayer,
    isLifecycleActive,
    isStalePlaybackEpoch,
    mergeSessionPlaybackTrace,
    normalizeRuntimePlaybackError,
    playDirectMp4,
    playHls,
    prepareForNextPlaybackAttempt,
    recordContractAdvisories,
    reportPlaybackFailure,
    resolvePreferredHlsEngineForCapabilities,
    setActiveHlsEngine,
    setCanSeek,
    setDurationSeconds,
    setStartUnix,
    setStatus,
    setTraceId,
    setVodStreamMode,
    sleep,
    t,
    vodFetchRef,
    vodRetryRef,
  ]);

  const startStream = useCallback(async (
    refToUse?: string,
    profileOverride?: string,
  ): Promise<void> => {
    const lifecycleGeneration = lifecycleGenerationRef.current;
    if (!isLifecycleActive(lifecycleGeneration)) return;

    const attemptEpoch = allocatePlaybackEpoch();
    playbackEpochRef.current = attemptEpoch;

    const profileForAttempt = normalizePlaybackProfileSelection(profileOverride ?? explicitProfile);
    startIntentInFlight.current = true;
    userPauseIntentRef.current = false;
    applyAutoplayMute();

    // Re-resolve at call time to avoid stale closure from useMemo/useCallback caching.
    const nativeHost = supportsManagedNativePlayback(resolveHostEnvironment());

    const initialMode: 'LIVE' | 'VOD' = recordingId || (src && requestedDuration) ? 'VOD' : 'LIVE';
    const initialStatus: PlayerStatus = src ? 'buffering' : 'starting';
    const hasSessionIntent = !nativeHost && Boolean(recordingId || (!src && (refToUse || sRef || '').trim()));
    beginPlaybackAttempt(attemptEpoch, initialMode, initialStatus, hasSessionIntent, profileForAttempt !== 'auto');

    try {
      if (recordingId) {
        debugLog('[V3Player] startStream: recordingId path', { recordingId, hasSrc: !!src });
        if (src) {
          debugWarn('[V3Player] Both recordingId and src provided; prioritizing recordingId (VOD path).');
        }
        if (nativeHost) {
          await prepareForNextPlaybackAttempt(Boolean(nativePlaybackState?.activeRequest));
          if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(attemptEpoch)) return;
          beginPlaybackAttempt(attemptEpoch, 'VOD', 'starting', false, profileForAttempt !== 'auto');
          beginNativePlayback({
            kind: 'recording',
            recordingId,
            profile: profileForAttempt === 'auto' ? undefined : profileForAttempt,
            authToken: token || undefined,
            startPositionMs: 0,
            title: channel?.name ?? recordingId,
          });
          return;
        }
        await startRecordingPlayback(recordingId, profileForAttempt, undefined, attemptEpoch);
        return;
      }

      if (src) {
        debugLog('[V3Player] startStream: src path', { hasSrc: true });
        await prepareForNextPlaybackAttempt();
        if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(attemptEpoch)) return;
        beginPlaybackAttempt(attemptEpoch, requestedDuration ? 'VOD' : 'LIVE', 'buffering', false, false);
        const srcEngine = resolvePreferredHlsEngine();
        playHls(src, srcEngine);
        setActiveHlsEngine(srcEngine);
        return;
      }

      const ref = (refToUse || sRef || '').trim();
      if (!ref) {
        beginPlaybackAttempt(attemptEpoch, 'LIVE', 'starting', false, false);
        setStatus('error');
        const failure = buildServiceRefRequiredFailure(t);
        reportPlaybackFailure(failure.appError, failure.options);
        return;
      }

      await prepareForNextPlaybackAttempt();
      if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(attemptEpoch)) return;

      beginPlaybackAttempt(attemptEpoch, 'LIVE', 'starting', !nativeHost, profileForAttempt !== 'auto');

      if (nativeHost) {
        beginNativePlayback({
          kind: 'live',
          serviceRef: ref,
          profile: profileForAttempt === 'auto' ? undefined : profileForAttempt,
          authToken: token || undefined,
          title: channel?.name ?? ref,
          logoUrl: channel?.logoUrl || undefined,
        });
        return;
      }

      clearPlayerError();

      try {
        await ensureSessionCookie();
        if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(attemptEpoch)) return;

        const [requestCaps, networkProbe] = await Promise.all([
          gatherPlaybackCapabilitiesForPlayer('live'),
          measurePlaybackNetwork(apiBase),
        ]);
        const requestContext = applyPlaybackNetworkProbe(
          requestCaps,
          gatherPlaybackClientContext(),
          networkProbe,
        );
        if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(attemptEpoch)) return;

        const automaticRequestProfile = resolvePlaybackRequestProfile(
          requestContext,
          requestCaps,
          'live',
          automaticProfileMemoryRef.current,
        );
        const requestProfile = resolvePlaybackProfileForPreflight(
          profileForAttempt,
          automaticRequestProfile,
        );
        linkProfileRef.current = resolvePlaybackLinkProfile({
          requestProfile,
          probeKind: networkProbe?.kind,
          network: requestContext.network,
        });

        setCapabilitySnapshot(requestCaps);

        const startLiveResult = await controller.startLive({
          serviceRef: ref,
          epoch: attemptEpoch,
          capabilities: requestCaps,
          profileHeaders: buildPlaybackProfileHeaders(requestProfile),
          requestedDuration,
        });

        if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(attemptEpoch)) {
          return;
        }

        if (startLiveResult.status === 'cancelled') {
          if (startLiveResult.reason === 'timeout') {
            setStatus('error');
            reportPlaybackFailure(
              normalizePlayerError(
                {
                  title: `${t('player.apiError')}: 408`,
                  status: 408,
                  detail: t('player.timeout') || 'Startup timed out',
                },
                {
                  fallbackTitle: t('player.streamFailed'),
                  status: 408,
                  retryable: true,
                },
              ),
              {
                source: 'backend',
                failureClass: 'session',
                code: 'TIMEOUT',
                retryable: true,
                recoverable: true,
                terminal: true,
              },
            );
          }
          return;
        }

        const readySession = startLiveResult.session;
        activateLiveSession(readySession);
        mergeSessionPlaybackTrace(extractPlaybackTrace(readySession));

        if (startLiveResult.contract) {
          setPlaybackObservability(
            resolvePlaybackObservability(
              startLiveResult.contract.observability.decision,
              requestCaps.preferredHlsEngine ?? null,
            ),
          );
          if (startLiveResult.contract.observability.requestId) {
            setTraceId(startLiveResult.contract.observability.requestId);
          }
        }

        if (readySession.trace) {
          mergeSessionPlaybackTrace(extractPlaybackTrace(readySession.trace));
        }

        if (readySession.requestId) {
          setTraceId(readySession.requestId);
        }

        const streamUrl = readySession.playbackUrl;
        if (!streamUrl) {
          throw new Error(t('player.streamUrlMissing'));
        }

        const normalizedMode = (readySession.mode as VodStreamMode) ?? 'LIVE';
        const engineDecision = resolveLiveEngineFromMode(
          normalizedMode,
          requestCaps,
          resolvePreferredHlsEngineForCapabilities,
        );
        if ('unsupported' in engineDecision) {
          setStatus('error');
          const failure = buildUnsupportedLiveModeFailure(normalizedMode as any, t);
          reportPlaybackFailure(failure.appError, failure.options);
          return;
        }
        const liveEngine = engineDecision.engine;

        setStatus('ready');
        playHls(streamUrl, liveEngine);
        setActiveHlsEngine(liveEngine);
      } catch (err) {
        if (!isLifecycleActive(lifecycleGeneration) || isStalePlaybackEpoch(attemptEpoch)) {
          return;
        }

        debugError(err);
        mergeSessionPlaybackTrace(extractPlaybackTrace(err));

        if (err instanceof PlaybackHttpError) {
          if (err.requestId) {
            setTraceId(err.requestId);
          }
          if (err.traceId) {
            mergeSessionPlaybackTrace({ requestId: err.traceId });
          }
        }

        if (
          err instanceof Error &&
          (err.message.includes('missing sessionId') ||
            err.message.includes('missing or invalid sessionId'))
        ) {
          const reqId = err instanceof PlaybackHttpError ? err.requestId : undefined;
          reportPlaybackFailure(
            normalizePlayerError(
              {
                title: t('player.sessionFailed'),
                detail: 'Intent response missing or invalid sessionId.',
                requestId: reqId,
              },
              {
                fallbackTitle: t('player.sessionFailed'),
              },
            ),
            {
              source: 'backend',
              failureClass: 'session',
              code: 'MALFORMED_INTENT',
              retryable: false,
              recoverable: false,
            },
          );
          setStatus('error');
          return;
        }

        if (err instanceof PlaybackHttpError) {

          if (err.status === 401 || err.status === 403) {
            if (err.status === 401) {
              notifyAuthRequiredIfUnauthorizedResponse(
                { status: 401, headers: err.headers } as Response,
                'V3Player.startIntent',
              );
            }
            const fallbackTitle = err.status === 401 ? t('player.authFailed') : t('player.forbidden');
            const problemTitle =
              err.data && typeof err.data === 'object' && typeof (err.data as any).title === 'string' && (err.data as any).title
                ? (err.data as any).title
                : fallbackTitle;
            const problemCode =
              err.data && typeof err.data === 'object' && typeof (err.data as any).code === 'string' && (err.data as any).code
                ? (err.data as any).code
                : 'AUTH_DENIED';
            setStatus('error');
            reportPlaybackFailure(
              normalizePlayerError(
                err.data ?? {
                  status: err.status,
                  title: problemTitle,
                  requestId: err.requestId,
                },
                {
                  fallbackTitle: problemTitle,
                  status: err.status,
                  retryable: false,
                },
              ),
              {
                source: 'backend',
                failureClass: 'auth',
                code: problemCode,
                retryable: false,
                recoverable: false,
                terminal: true,
              },
            );
            return;
          }

          if (err.status === 410) {
            const dataObj = (err.data && typeof err.data === 'object' ? err.data : null) as Record<string, unknown> | null;
            if (dataObj) {
              mergeSessionPlaybackTrace(extractPlaybackTrace(dataObj));
            }
            setStatus('error');
            const recoveredSessionAuth = Boolean(dataObj?.recoveredSessionAuth);
            const reason = (dataObj?.reason ?? dataObj?.code) as string | undefined;
            const reasonDetail = (dataObj?.reason_detail ?? dataObj?.reasonDetail ?? dataObj?.detail) as string | undefined;

            if (String(reason).includes('LEASE_BUSY') || String(reasonDetail).includes('LEASE_BUSY')) {
              reportPlaybackFailure(
                normalizePlayerError(
                  {
                    status: 410,
                    title: t('player.leaseBusy'),
                    requestId: err.requestId,
                    sessionId: err.sessionId,
                  },
                  {
                    fallbackTitle: t('player.leaseBusy'),
                    status: 410,
                    retryable: true,
                  },
                ),
                {
                  source: 'backend',
                  failureClass: 'session',
                  code: 'LEASE_BUSY',
                  retryable: true,
                  recoverable: false,
                },
              );
              return;
            }

            if (recoveredSessionAuth) {
              const failure = buildSessionExpiredFailure(t);
              reportPlaybackFailure(failure.appError, failure.options);
              return;
            }

            const translatedReason = translatePlaybackReason(reason, reasonDetail, t);
            const errorTitle = `${t('player.sessionFailed')}: ${translatedReason}`;
            reportPlaybackFailure(
              normalizePlayerError(
                {
                  ...(typeof err.data === 'object' && err.data !== null ? err.data : {}),
                  title: errorTitle,
                  status: 410,
                  requestId: err.requestId,
                  sessionId: err.sessionId,
                },
                {
                  fallbackTitle: errorTitle,
                  status: 410,
                  retryable: true,
                },
              ),
              {
                source: 'backend',
                failureClass: 'session',
                code: (typeof dataObj?.code === 'string' && dataObj.code) ? dataObj.code : (reason || 'SESSION_GONE'),
                retryable: true,
                recoverable: false,
              },
            );
            return;
          }

          const fallbackTitle = `${t('player.apiError')}: ${err.status}`;
          let errorTitle = fallbackTitle;
          let errorCode = `HTTP_${err.status}`;
          if (err.data && typeof err.data === 'object') {
            const dataObj = err.data as Record<string, unknown>;
            if (typeof dataObj.title === 'string' && dataObj.title) {
              errorTitle = dataObj.title;
            } else if (typeof dataObj.message === 'string' && dataObj.message) {
              errorTitle = dataObj.message;
            }
            if (typeof dataObj.code === 'string' && dataObj.code) {
              errorCode = dataObj.code;
            }
          }
          setStatus('error');
          reportPlaybackFailure(
            normalizePlayerError(
              err.data ?? {
                status: err.status,
                title: errorTitle,
                requestId: err.requestId,
              },
              {
                fallbackTitle: errorTitle,
                status: err.status,
                retryable: err.status === 409 || err.status >= 500,
              },
            ),
            {
              source: 'backend',
              failureClass: 'session',
              code: errorCode,
              retryable: err.status === 409 || err.status >= 500,
              recoverable: true,
              terminal: true,
            },
          );
          return;
        }

        if (
          err instanceof Error &&
          (err.message.includes('LEASE_BUSY') ||
            err.message.includes(t('player.leaseBusy')))
        ) {
          reportPlaybackFailure(
            normalizePlayerError(
              {
                status: 410,
                title: t('player.leaseBusy'),
              },
              {
                fallbackTitle: t('player.leaseBusy'),
                status: 410,
                retryable: true,
              },
            ),
            {
              source: 'backend',
              failureClass: 'session',
              code: 'LEASE_BUSY',
              retryable: true,
              recoverable: false,
            },
          );
          setStatus('error');
          return;
        }

        reportPlaybackFailure(
          normalizeRuntimePlaybackError(err, t('player.streamFailed')),
          {
            source: 'backend',
          },
        );
        setStatus('error');
      }
    } catch (err) {
      if (!isLifecycleActive(lifecycleGeneration)) return;
      debugError(err);
      reportPlaybackFailure(normalizeRuntimePlaybackError(err, t('player.streamFailed')), {
        source: 'orchestrator',
      });
      setStatus('error');
    } finally {
      startIntentInFlight.current = false;
    }
  }, [
    src,
    recordingId,
    sRef,
    explicitProfile,
    apiBase,
    clearPlayerError,
    ensureSessionCookie,
    mergeSessionPlaybackTrace,
    playHls,
    t,
    startRecordingPlayback,
    applyAutoplayMute,
    automaticProfileMemoryRef,
    linkProfileRef,
    gatherPlaybackCapabilitiesForPlayer,
    prepareForNextPlaybackAttempt,
    resolvePreferredHlsEngine,
    resolvePreferredHlsEngineForCapabilities,
    activateLiveSession,
    requestedDuration,
    beginNativePlayback,
    channel?.logoUrl,
    channel?.name,
    nativePlaybackState,
    allocatePlaybackEpoch,
    beginPlaybackAttempt,
    isLifecycleActive,
    isStalePlaybackEpoch,
    normalizeRuntimePlaybackError,
    reportPlaybackFailure,
    setActiveHlsEngine,
    setStatus,
    setTraceId,
    token,
    controller,
  ]);

  startStreamRef.current = startStream;

  const performLocalMediaTeardown = useCallback(async (
    _reason?: PlaybackStopReason,
  ): Promise<void> => {
    userPauseIntentRef.current = true;
    const timelineCompletion = timelineReportCompletionRef.current;
    await teardownActivePlayback();
    await timelineCompletion;
  }, [teardownActivePlayback]);

  const stopStream = useCallback(async (
    skipClose: boolean = false,
    reason: PlaybackStopReason = 'user_stop',
  ): Promise<void> => {
    await controller.stop(reason, !skipClose);
    if (onClose && !skipClose) onClose();
  }, [controller, onClose]);

  const handleRetry = useCallback(async () => {
    if (disposedRef.current) return;
    if (retryInFlightRef.current) {
      return;
    }
    retryInFlightRef.current = true;
    try {
      await stopStream(true, 'auto_recovery_restart');
      if (disposedRef.current) return;
      await startStream(sRef);
    } finally {
      retryInFlightRef.current = false;
    }
  }, [stopStream, startStream, sRef]);
  // --- Effects ---
  executeCommandRef.current = useCallback((command: PlaybackCommand) => {
    switch (command.type) {
      case 'command.timeline.record':
        sessionTimeline.record(command.kind as any, command.detail);
        break;
      case 'command.timeline.end_attempt':
        sessionTimeline.endAttempt(command.reason);
        break;
      case 'command.timeline.report':
        {
          const events = sessionTimeline.describe();
          return reportTimelineSnapshot(command.reason, events);
        }
      case 'command.playback.start':
        void startStream(command.serviceRef, command.explicitProfile);
        break;
      case 'command.playback.stop':
        {
          const stopPromise = performLocalMediaTeardown(command.reason);
          stopCommandCompletionRef.current = stopPromise;
          return stopPromise;
        }
      case 'command.telemetry.emit':
        telemetry.emit(command.eventName as any, command.payload);
        break;
      case 'command.playback.schedule_auto_fallback':
        {
          if (command.holdBandwidth) {
            noteNetworkStarvation(automaticProfileMemoryRef.current);
          }
          const lifecycleGeneration = lifecycleGenerationRef.current;
          const timerId = window.setTimeout(() => {
            autoFallbackTimersRef.current.delete(timerId);
            if (isLifecycleActive(lifecycleGeneration) && !isStalePlaybackEpoch(command.epoch)) {
              dispatchPlayback({
                type: 'intent.start.requested',
                epoch: command.epoch,
                kind: src ? 'src' : (playbackStateRef.current.playbackMode === 'VOD' ? 'vod' : 'live'),
                serviceRef: sRef,
                recordingId: recordingId || undefined,
                srcUrl: src || undefined,
                explicitProfile: command.profile ?? undefined,
              });
            }
          }, command.delayMs);
          autoFallbackTimersRef.current.add(timerId);
        }
        break;
    }
  }, [
    startStream,
    performLocalMediaTeardown,
    reportTimelineSnapshot,
    dispatchPlayback,
    isLifecycleActive,
    isStalePlaybackEpoch,
    sRef,
    recordingId,
    src,
  ]);

  // Update sRef on channel change
  useEffect(() => {
    if (channel) {
      const ref = (channel.serviceRef || channel.id || '').trim();
      if (ref) setSRef(ref);
    }
  }, [channel]);

  useEffect(() => {
    clearNetworkStarvationHold(automaticProfileMemoryRef.current);
  }, [sRef, recordingId]);

  useEffect(() => {
    if (!autoStart || mounted.current) return;
    // UI-INV-PLAYER-001: Autostart requires an explicit source.
    const normalizedRef = sRef.trim();
    const hasSource = !!(src || recordingId || normalizedRef);
    if (hasSource) {
      mounted.current = true;
      dispatchPlayback({
        type: 'intent.start.requested',
        epoch: allocatePlaybackEpoch(),
        kind: src ? 'src' : (recordingId ? 'vod' : 'live'),
        serviceRef: normalizedRef || undefined,
        recordingId: recordingId || undefined,
        srcUrl: src || undefined,
        explicitProfile: explicitProfile,
      });
    }
  }, [autoStart, src, recordingId, sRef, explicitProfile, allocatePlaybackEpoch, dispatchPlayback]);

  useEffect(() => {
    dispatchPlayback({
      type: 'system.requested_duration.synced',
      durationSeconds: requestedDuration,
    });
  }, [dispatchPlayback, requestedDuration]);

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
    isImmediateStartupStatus || status === 'buffering' || status === 'recovering' || shouldHoldNativeVideo;
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

    // hls.js + ManagedMediaSource hands the buffer back to the UA and the segment
    // loader is throttled/parked while backgrounded (MMS 'endstreaming'); on return
    // it can stay stalled, freezing buffering AND the DVR seekable window (so
    // scrubbing dies). Nudge hls.js to resume loading on the hidden->visible edge so
    // the buffer refills and the live/DVR playlist window refreshes. Gated on
    // hlsRef.current, so the native-HLS path (browser-owned buffer) is untouched.
    if (wasHidden && hlsRef.current) {
      try {
        hlsRef.current.startLoad();
      } catch (err) {
        debugWarn('[V3Player] hls resume startLoad failed', err);
      }
    }

    const action = decideForegroundResume({
      wasHidden,
      isPiP: document.pictureInPictureElement === video,
      status: recoveryStatusRef.current,
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

    // action === 'play'. A single play() right after a page-freeze often fizzles —
    // the element is still suspended and discards it, so the frame stays black until
    // the user mashes play a few times. Nudge play() until currentTime actually
    // advances (the only proof the decoder accepted the resume), bounded; the
    // returned cancel cleans it up if the page hides again mid-recovery.
    //
    // NOTE: the live status is read through recoveryStatusRef instead of making
    // it an effect dependency. The
    // `setStatus('paused'→'buffering')` call below would otherwise trigger a
    // re-render where React cleans up the current effect (calling the cancel
    // function) before the observation timer can fire, defeating the retry loop.
    // `hasTerminalStatus` (which tracks terminal states derived from `status`) is
    // already in deps and correctly re-runs the effect when the session is reaped.
    setStatus((current) => (current === 'paused' ? 'buffering' : current));
    return startResumePlaybackRecovery(video, {
      // Keep a user pause sacred even if it happens during the ~2s recovery window.
      shouldContinue: () => !userPauseIntentRef.current,
      onBlocked: (err: unknown) => {
        if ((err as { name?: string } | null)?.name === 'NotAllowedError') {
          // iOS blocked the programmatic resume; the play/pause control is the
          // user-gesture tap-to-resume.
          setStatus('paused');
        } else {
          debugWarn('[V3Player] Browser resume play blocked', err);
        }
      },
      onFailed: () => {
        debugWarn('[V3Player] Browser resume play failed to advance, retrying session');
        void handleRetry();
      },
    });
  }, [handleRetry, hasTerminalStatus, hlsRef, hostEnvironment.isTv, isDocumentVisible, isNativePlaybackHost, nativePlaybackState, setStatus, videoRef]);

  // Browser (non-TV) network-reconnect recovery. Flaky web — mobile data, wifi
  // handoffs, laptop sleep/wake — drops connectivity; on the offline->online
  // edge we re-establish a reaped session or nudge a still-alive stream back to
  // play, instead of leaving the user to hit Retry by hand. Mirrors the
  // foreground recovery above; TV keeps its own resume effect.
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

    if (!isOnline || connectionLost) {
      wasOfflineRef.current = true;
      return;
    }

    const wasOffline = wasOfflineRef.current;
    wasOfflineRef.current = false;

    const hasActiveSession = Boolean(
      sessionIdRef.current || nativePlaybackState?.session?.sessionId,
    );

    // hls.js parks its segment loader on a fatal network error during the
    // outage; nudge it to resume loading once connectivity is back (gated on
    // hlsRef so the native-HLS path, with its UA-owned buffer, is untouched).
    if (wasOffline && hasActiveSession && hlsRef.current) {
      try {
        hlsRef.current.startLoad();
      } catch (err) {
        debugWarn('[V3Player] hls reconnect startLoad failed', err);
      }
    }

    const action = decideOnlineRecovery({
      wasOffline,
      hasActiveSession,
      status: recoveryStatusRef.current,
      userPaused: userPauseIntentRef.current,
      hasTerminal: hasTerminalStatus,
    });

    if (action === 'none') {
      return;
    }

    if (action === 'retry') {
      // Session was reaped during the outage (heartbeat 410/404) — re-establish.
      void handleRetry();
      return;
    }

    // action === 'play'. As in foreground recovery, a single play() right after a
    // network stall often fizzles (the element discards it), so nudge play()
    // until currentTime advances, bounded; the returned cancel cleans it up if we
    // go offline again mid-recovery. The latest status is read through
    // recoveryStatusRef for the same reason documented on the foreground effect.
    setStatus((current) => (current === 'paused' ? 'buffering' : current));
    return startResumePlaybackRecovery(video, {
      shouldContinue: () => !userPauseIntentRef.current,
      onBlocked: (err: unknown) => {
        if ((err as { name?: string } | null)?.name === 'NotAllowedError') {
          setStatus('paused');
        } else {
          debugWarn('[V3Player] Reconnect resume play blocked', err);
        }
      },
      onFailed: () => {
        debugWarn('[V3Player] Reconnect resume play failed to advance, retrying session');
        void handleRetry();
      },
    });
  }, [connectionLost, handleRetry, hasTerminalStatus, hlsRef, hostEnvironment.isTv, isNativePlaybackHost, isOnline, nativePlaybackState, sessionIdRef, setStatus, videoRef]);

  useNetworkRecoveryWatchdog({
    apiBase,
    active: !hostEnvironment.isTv && status === 'error' && shouldWatchForNetworkRecovery(failure),
    intentKey: `${sRef}|${recordingId ?? ''}|${src ?? ''}`,
    healthy: status === 'playing',
    onReachable: handleRetry,
  });

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
  }, [containerRef, hostEnvironment.isTv, onClose]);
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
      ? resolveStartupOverlaySupport(sessionProfileReason, t, overlayStatus)
      : '';
  // Startup phase stepper: map the coarse player status onto the three
  // user-facing startup stages so the overlay can show WHERE the start
  // currently is instead of a generic indeterminate spinner.
  //   connect   -> intent/tuner handshake ('starting')
  //   transcode -> backend session spin-up ('priming' | 'building')
  //   buffer    -> first segments arriving ('buffering' | 'recovering')
  const startupPhaseIndex =
    overlayStatus === 'buffering' || overlayStatus === 'recovering'
      ? 2
      : overlayStatus === 'priming' || overlayStatus === 'building'
        ? 1
        : 0;
  const startupPhaseSteps = [
    { key: 'connect', label: t('player.startupPhases.connect', { defaultValue: 'Connect' }) },
    { key: 'transcode', label: t('player.startupPhases.transcode', { defaultValue: 'Transcode' }) },
    { key: 'buffer', label: t('player.startupPhases.buffer', { defaultValue: 'Buffer' }) },
  ].map((step, index) => ({
    ...step,
    state:
      index < startupPhaseIndex
        ? ('done' as const)
        : index === startupPhaseIndex
          ? ('active' as const)
          : ('pending' as const),
  }));
  const startupProgressPercent = [22, 58, 86][startupPhaseIndex] ?? 22;
  const isBufferingOverlayActive =
    (status === 'buffering' || status === 'recovering') && showBufferingOverlay;
  const showStartupOverlay =
    isImmediateStartupStatus ||
    isBufferingOverlayActive ||
    shouldHoldNativeVideo ||
    (isNativeEngine && showNativeVideoVeil);
  const useNativeBufferingSafeOverlay = shouldHoldNativeVideo;
  const showNativeBufferingMask = shouldHoldNativeVideo || showNativeVideoVeil;
  // Debounce the centered spinner card so a sub-500ms re-prepare (fast resume) does
  // not flash the "preparing" card. The black-frame covers (showNativeBufferingMask /
  // hideVideoElement) are derived separately and are NOT debounced, so delaying the
  // card can never expose a black frame. We only debounce immediate startup;
  // buffering/recovering overlays are already debounced by useBufferingOverlay.
  // Use the immediate flag directly so the waiting badge appears instantly
  // instead of flashing the player background on startup.
  const showSpinnerCard =
    isImmediateStartupStatus ||
    isBufferingOverlayActive ||
    shouldHoldNativeVideo ||
    (isNativeEngine && showNativeVideoVeil);
  // Minimal startup chrome (which hides the full controls, incl. the stop button)
  // tracks the DEBOUNCED card, not the raw overlay: during the pre-card debounce
  // window the full playback chrome — and therefore a reachable stop control — stays
  // mounted. Without this the stop affordance vanishes for 500ms on TV/overlay starts.
  const useMinimalStartupChrome = showSpinnerCard && (hostEnvironment.isTv || Boolean(onClose));
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
  const mseAv1Readout = useMemo(() => formatManagedMseAv1(getManagedMseAv1Support()), []);

  const currentPositionDisplay = formatDvrPositionDisplay(
    { isLiveMode, isAtLiveEdge, behindLiveSeconds, currentTimeDisplay },
    formatClock,
    (key, options) => t(key, options),
  );

  const liveDvrPreviewBaseUrl =
    isLiveMode && hasSeekWindow && effectiveSessionId
      ? `${apiBase}/sessions/${effectiveSessionId}/hls/preview.jpg`
      : null;
  const vodScrubPreviewBaseUrl =
    playbackMode === 'VOD' && activeRecordingId && canSeek
      ? `${apiBase}/recordings/${activeRecordingId}/scrub.jpg`
      : null;
  const dvrPreviewBaseUrl = liveDvrPreviewBaseUrl ?? vodScrubPreviewBaseUrl;
  const dvrPreviewSegmentSeconds = liveDvrPreviewBaseUrl ? 6 : 10;
  const dvrPreviewWindowStartUnix = startUnix && startUnix > 0 ? startUnix + seekableStart : null;

  const viewState = buildPlayerViewState({
    channel,
    playbackMode,
    liveNowPlaying,
    onClose,
    isIdle,
    status,
    showStats,
    showPlaybackChrome,
    isWebKitFullscreenActive,
    isFullscreen,
    prefersDesktopNativeFullscreen,
    supportsNativeFullscreen,
    mseAv1Readout,
    effectiveSessionId,
    sessionPlaybackTrace,
    traceId,
    effectiveClientPath,
    effectiveRequestProfile,
    effectiveRequestedIntent,
    effectiveResolvedIntent,
    effectiveQualityRung,
    effectiveAudioQualityRung,
    effectiveVideoQualityRung,
    effectiveDegradedFrom,
    effectiveHostPressureBand,
    effectiveHostOverrideApplied,
    effectiveForcedIntent,
    effectiveOperatorMaxQualityRung,
    effectiveOperatorRuleName,
    effectiveOperatorRuleScope,
    effectiveClientFallbackDisabled,
    effectiveOperatorOverrideApplied,
    sourceProfileSummary,
    effectiveTargetProfile,
    effectiveTargetProfileHash,
    ffmpegPlanSummary,
    firstFrameLabel,
    fallbackSummary,
    stopSummary,
    hostPressureSummary,
    playbackObservability,
    showNativeBufferingMask,
    stats,
    hlsRefCurrent: Boolean(hlsRef.current),
    seekableStart,
    seekableEnd,
    currentPlaybackTime,
    windowDuration,
    hasSeekWindow,
    isCompactTouchLayout,
    currentPositionDisplay,
    dvrPreviewBaseUrl,
    dvrPreviewSegmentSeconds,
    dvrPreviewWindowStartUnix,
    useMinimalStartupChrome,
    showStartupOverlay,
    showSpinnerCard,
    useNativeBufferingSafeOverlay,
    overlayStatus,
    spinnerLabel,
    spinnerSupport,
    startupPhaseSteps,
    startupProgressPercent,
    startupElapsedSeconds,
    autoStart,
    error,
    showErrorDetails,
    effectiveSessionLabel: `${t('common.session')}: ${effectiveSessionId || t('common.notAvailable')}`,
    isPlaying,
    ttffMetrics,
    startTimeDisplay,
    endTimeDisplay,
    relativePosition,
    isLiveMode,
    isAtLiveEdge,
    recordingId,
    src,
    sRef,
    startIntentInFlightRef: startIntentInFlight.current,
    showDvrModeButton,
    canToggleFullscreen,
    canEnterNativeFullscreen,
    canToggleMute,
    isMuted,
    canAdjustVolume,
    volume,
    canTogglePiP,
    isPip,
    showResumeOverlay,
    resumeState,
    explicitProfile,
    audioTracks,
    activeAudioTrack,
    durationSeconds,
    formatClock,
    t,
  });
  const actions: PlaybackOrchestratorActions = {
    stopStream,
    retry: handleRetry,
    seekBy,
    seekTo,
    changeAudioTrack(trackId: number) {
      if (hlsRef.current) {
        hlsRef.current.audioTrack = trackId;
      } else if (videoRef.current && 'audioTracks' in videoRef.current) {
        const tracks = (videoRef.current as any).audioTracks;
        if (tracks) {
          for (let i = 0; i < tracks.length; i++) {
            tracks[i].enabled = (i === trackId);
          }
        }
      }
    },
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
      dismissedResumeRecordingIdRef.current = activeRecordingRef.current;
      setShowResumeOverlay(false);
      if (activeRecordingRef.current && positionSeconds > 0) {
        startRecordingPlayback(activeRecordingRef.current, undefined, Math.round(positionSeconds * 1000));
      } else if (activeRecordingRef.current) {
        startRecordingPlayback(activeRecordingRef.current, undefined, 0);
      } else {
        seekWhenReady(positionSeconds);
      }
    },
    startOver() {
      dismissedResumeRecordingIdRef.current = activeRecordingRef.current;
      setShowResumeOverlay(false);
      if (activeRecordingRef.current) {
        startRecordingPlayback(activeRecordingRef.current, undefined, 0);
      } else {
        seekWhenReady(0);
      }
    },
    changeProfile(profile: string) {
      const normalizedProfile = normalizePlaybackProfileSelection(profile);
      setExplicitProfile(normalizedProfile);
      try {
        localStorage.setItem('xg2g.player.explicitProfile', normalizedProfile);
      } catch {
        // ignore
      }
      if (hasActivePlayback() || startIntentInFlight.current) {
        dispatchPlayback({
          type: 'intent.start.requested',
          epoch: allocatePlaybackEpoch(),
          kind: src ? 'src' : (recordingId ? 'vod' : 'live'),
          serviceRef: sRef || undefined,
          recordingId: recordingId || undefined,
          srcUrl: src || undefined,
          explicitProfile: normalizedProfile,
        });
      }
    },
  };

  return {
    viewState,
    actions,
    controller,
    playbackState,
  };
}
// cspell:ignore remux arrowleft arrowright enterpictureinpicture leavepictureinpicture kbps Remux
