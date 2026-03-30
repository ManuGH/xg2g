import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import { useTranslation } from 'react-i18next';
import Hls from '../lib/hlsRuntime';
import {
  postRecordingPlaybackInfo,
  type PlaybackCapabilities as PlaybackCapabilitiesContract,
  type PlaybackInfo,
  type PlaybackSourceProfile,
  type PlaybackTrace as PlaybackTraceContract,
  type PlaybackTraceFfmpegPlan,
  type PlaybackTraceOperator,
  type PlaybackTargetProfile,
} from '../../../client-ts';
import { getApiBaseUrl } from '../../../services/clientWrapper';
import { telemetry } from '../../../services/TelemetryService';
import type {
  V3PlayerProps,
  PlayerStatus,
  V3SessionResponse,
  V3SessionStatusResponse,
  HlsInstanceRef,
  VideoElementRef
} from '../../../types/v3-player';
import { useLiveSessionController } from '../useLiveSessionController';
import { usePlaybackEngine } from '../usePlaybackEngine';
import { usePlayerChrome } from '../usePlayerChrome';
import { resolveStartupOverlayLabel, resolveStartupOverlaySupport } from '../startupOverlayLabel';
import { useResume } from '../../resume/useResume';
import { ResumeState } from '../../resume/api';
import { Button, Card, StatusChip } from '../../../components/ui';
import { debugError, debugLog, debugWarn } from '../../../utils/logging';
import {
  PlayerError,
  readResponseBody,
  extractCapHashFromDecisionToken,
  hasTouchInput,
  canUseDesktopWebKitFullscreen,
  shouldForceNativeMobileHls,
  shouldPreferNativeWebKitHls
} from '../utils/playerHelpers';
import { detectPlaybackClientFamily } from '../utils/playbackClientFamily';
import { probeRuntimePlaybackCapabilities } from '../utils/playbackProbe';
import { normalizePlayerError } from '../../../lib/appErrors';
import { notifyAuthRequiredIfUnauthorizedResponse } from '../../../lib/httpProblem';
import { requestHostInputFocus, resolveHostEnvironment, setHostPlaybackActive } from '../../../lib/hostBridge';
import type { AppError } from '../../../types/errors';
import styles from './V3Player.module.css';

interface ApiErrorResponse {
  code?: string;
  message?: string;
  requestId?: string;
  details?: unknown;
}

type CapabilitySnapshot = Pick<
  PlaybackCapabilitiesContract,
  | 'capabilitiesVersion'
  | 'container'
  | 'videoCodecs'
  | 'audioCodecs'
  | 'deviceType'
  | 'supportsHls'
  | 'supportsRange'
  | 'allowTranscode'
  | 'runtimeProbeUsed'
  | 'runtimeProbeVersion'
  | 'clientFamilyFallback'
  | 'videoCodecSignals'
> & {
  hlsEngines?: string[];
  preferredHlsEngine?: string;
};

type PlaybackObservability = {
  clientPath: string | null;
  requestProfile: string | null;
  requestedIntent: string | null;
  resolvedIntent: string | null;
  qualityRung: string | null;
  audioQualityRung: string | null;
  videoQualityRung: string | null;
  degradedFrom: string | null;
  hostPressureBand: string | null;
  hostOverrideApplied: boolean;
  targetProfileHash: string | null;
  targetProfile: PlaybackTargetProfile | null;
  operator: PlaybackTraceOperator | null;
  selectedOutputKind: string | null;
};

function formatSourceProfileSummary(source: PlaybackSourceProfile | null | undefined): string {
  if (!source) return '-';

  const resolution = source.width && source.height ? `${source.width}x${source.height}` : null;
  const fps = source.fps ? `${source.fps}fps` : null;
  const video = [source.container || null, source.videoCodec || null, resolution, fps].filter(Boolean).join(' · ');
  const audio = [
    source.audioCodec || null,
    source.audioChannels ? `${source.audioChannels}ch` : null,
    source.audioBitrateKbps ? `@${source.audioBitrateKbps}k` : null,
  ].filter(Boolean).join('/');

  return [video || '-', audio ? `a:${audio}` : null].filter(Boolean).join(' · ');
}

function formatFfmpegPlanSummary(plan: PlaybackTraceFfmpegPlan | null | undefined): string {
  if (!plan) return '-';
  const video = [plan.videoMode || null, plan.videoCodec || null].filter(Boolean).join('/');
  const audio = [plan.audioMode || null, plan.audioCodec || null].filter(Boolean).join('/');
  const execution = plan.hwAccel || 'none';
  return [
    plan.inputKind || null,
    plan.packaging || plan.container || null,
    video ? `v:${video}` : null,
    audio ? `a:${audio}` : null,
    execution,
  ].filter(Boolean).join(' · ');
}

function formatFirstFrameLabel(firstFrameAtMs: number | null | undefined): string {
  if (!firstFrameAtMs || firstFrameAtMs <= 0) return '-';
  return new Date(firstFrameAtMs).toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

function formatFallbackSummary(trace: PlaybackTraceContract | null | undefined): string {
  if (!trace) return '-';
  const count = typeof trace.fallbackCount === 'number' ? trace.fallbackCount : 0;
  const lastReason = trace.lastFallbackReason || null;
  if (count <= 0 && !lastReason) return '-';
  return [count > 0 ? String(count) : null, lastReason].filter(Boolean).join(' · ');
}

function formatStopSummary(trace: PlaybackTraceContract | null | undefined): string {
  if (!trace) return '-';
  return [trace.stopClass || null, trace.stopReason || null].filter(Boolean).join(' · ') || '-';
}

function formatHostPressureSummary(hostPressureBand: string | null, hostOverrideApplied: boolean): string {
  if (!hostPressureBand) return '-';
  return hostOverrideApplied ? `${hostPressureBand} · applied` : hostPressureBand;
}

function extractPlaybackTrace(value: unknown): PlaybackTraceContract | null {
  if (!value || typeof value !== 'object') {
    return null;
  }

  const record = value as Record<string, unknown>;
  if (typeof record.requestId === 'string' && (
    'sessionId' in record ||
    'source' in record ||
    'targetProfileHash' in record ||
    'targetProfile' in record ||
    'ffmpegPlan' in record ||
    'stopReason' in record ||
    'stopClass' in record
  )) {
    return record as unknown as PlaybackTraceContract;
  }

  if ('trace' in record) {
    return extractPlaybackTrace(record.trace);
  }
  if ('body' in record) {
    return extractPlaybackTrace(record.body);
  }
  if ('details' in record) {
    return extractPlaybackTrace(record.details);
  }

  return null;
}

function readLegacyTargetProfileHash(decision: PlaybackInfo['decision']): string | null {
  if (!decision || typeof decision !== 'object') {
    return null;
  }
  const value = (decision as Record<string, unknown>).targetProfileHash;
  return typeof value === 'string' ? value : null;
}

function readLegacyTargetProfile(decision: PlaybackInfo['decision']): PlaybackTargetProfile | null {
  if (!decision || typeof decision !== 'object') {
    return null;
  }
  const value = (decision as Record<string, unknown>).targetProfile;
  return value && typeof value === 'object' ? value as PlaybackTargetProfile : null;
}

function readLegacyDurationMs(playbackInfo: PlaybackInfo): number | null {
  const value = (playbackInfo as Record<string, unknown>).durationMs;
  return typeof value === 'number' && value > 0 ? value : null;
}

function formatClientPath(snapshot: CapabilitySnapshot | null): string {
  if (!snapshot) return '-';
  const preferred = snapshot.preferredHlsEngine ?? '-';
  const engines = snapshot.hlsEngines?.length ? snapshot.hlsEngines.join('/') : null;
  return engines ? `${preferred} (${engines})` : preferred;
}

function formatRequestProfileLabel(profile: string | null): string {
  switch (profile) {
    case 'generic':
    case 'high':
      return 'compatible';
    case 'low':
      return 'bandwidth';
    case 'copy':
      return 'direct';
    default:
      return profile || '-';
  }
}

function resolveAutoTranscodeCodecs(snapshot: CapabilitySnapshot | null): string[] {
  if (!snapshot) return [];

  const out: string[] = [];
  const signals = Array.isArray(snapshot.videoCodecSignals) ? snapshot.videoCodecSignals : [];
  const signalFor = (codec: string) => signals.find((signal) => signal.codec === codec);

  const av1 = signalFor('av1');
  if (av1?.supported && av1.powerEfficient) {
    out.push('av1');
  }

  const hevc = signalFor('hevc');
  if (hevc?.supported && (hevc.powerEfficient || hevc.smooth)) {
    out.push('hevc');
  }

  if (snapshot.videoCodecs.includes('h264') || out.length === 0) {
    out.push('h264');
  }

  return Array.from(new Set(out));
}

function formatQualityRungLabel(rung: string | null): string {
  if (!rung) return '-';
  return rung.split('_').join(' ');
}

function formatBooleanLabel(value: boolean): string {
  return value ? 'yes' : 'no';
}

function formatTargetProfileSummary(target: PlaybackTargetProfile | null): string {
  if (!target) return '-';

  const videoMode = target.video?.mode || '-';
  const videoCodec = target.video?.codec ? `/${target.video.codec}` : '';
  const videoCRF = target.video?.crf ? `/crf${target.video.crf}` : '';
  const videoPreset = target.video?.preset ? `/${target.video.preset}` : '';
  const audioMode = target.audio?.mode || '-';
  const audioCodec = target.audio?.codec ? `/${target.audio.codec}` : '';
  const audioChannels = target.audio?.channels ? `/${target.audio.channels}ch` : '';
  const audioBitrate = target.audio?.bitrateKbps ? `@${target.audio.bitrateKbps}k` : '';
  const packaging = target.packaging || target.container || '-';

  return [
    packaging,
    `v:${videoMode}${videoCodec}${videoCRF}${videoPreset}`,
    `a:${audioMode}${audioCodec}${audioChannels}${audioBitrate}`
  ].join(' · ');
}

function formatExecutionLabel(target: PlaybackTargetProfile | null): string {
  if (!target?.hwAccel || target.hwAccel === 'none') {
    return 'CPU';
  }
  return target.hwAccel.toUpperCase();
}

function extractPlaybackObservability(
  info: PlaybackInfo,
  clientPath: string | null
): PlaybackObservability | null {
  const decision = info.decision;
  if (!decision) {
    if (!clientPath) return null;
    return {
      clientPath,
      requestProfile: null,
      requestedIntent: null,
      resolvedIntent: null,
      qualityRung: null,
      audioQualityRung: null,
      videoQualityRung: null,
      degradedFrom: null,
      hostPressureBand: null,
      hostOverrideApplied: false,
      targetProfileHash: null,
      targetProfile: null,
      operator: null,
      selectedOutputKind: null,
    };
  }

  return {
    clientPath,
    requestProfile: decision.trace?.requestProfile ?? null,
    requestedIntent: decision.trace?.requestedIntent ?? null,
    resolvedIntent: decision.trace?.resolvedIntent ?? null,
    qualityRung: decision.trace?.qualityRung ?? null,
    audioQualityRung: decision.trace?.audioQualityRung ?? null,
    videoQualityRung: decision.trace?.videoQualityRung ?? null,
    degradedFrom: decision.trace?.degradedFrom ?? null,
    hostPressureBand: decision.trace?.hostPressureBand ?? null,
    hostOverrideApplied: decision.trace?.hostOverrideApplied ?? false,
    targetProfileHash: readLegacyTargetProfileHash(decision) ?? decision.trace?.targetProfileHash ?? null,
    targetProfile: readLegacyTargetProfile(decision) ?? decision.trace?.targetProfile ?? null,
    operator: decision.trace?.operator ?? null,
    selectedOutputKind: decision.selectedOutputKind ?? null,
  };
}

function resolvePlaybackDurationSeconds(playbackInfo: PlaybackInfo): number | null {
  const legacyDurationMs = readLegacyDurationMs(playbackInfo);
  if (legacyDurationMs) {
    return legacyDurationMs / 1000;
  }
  if (typeof playbackInfo.durationSeconds === 'number' && playbackInfo.durationSeconds > 0) {
    return playbackInfo.durationSeconds;
  }
  return null;
}

type NativeVideoRevealThresholds = {
  stableMs: number;
  retryMs: number;
  minBufferSeconds: number;
  minAdvanceSeconds: number;
  requirePlaybackResume: boolean;
};

const NATIVE_VIDEO_REVEAL_STARTUP: NativeVideoRevealThresholds = {
  stableMs: 650,
  retryMs: 250,
  minBufferSeconds: 0.75,
  minAdvanceSeconds: 0.12,
  requirePlaybackResume: false,
};

const NATIVE_VIDEO_REVEAL_REBUFFER: NativeVideoRevealThresholds = {
  stableMs: 420,
  retryMs: 160,
  minBufferSeconds: 0.5,
  minAdvanceSeconds: 0.22,
  requirePlaybackResume: true,
};

const NATIVE_VIDEO_REBUFFER_VEIL_MS = 2300;
const NATIVE_VIDEO_UNVEIL_AFTER_PLAYING_MS = 140;

function V3Player(props: V3PlayerProps) {
  const { t } = useTranslation();
  const { token, autoStart, onClose, duration } = props;
  const channel = 'channel' in props ? props.channel : undefined;
  const src = 'src' in props ? props.src : undefined;
  const recordingId = 'recordingId' in props ? props.recordingId : undefined;

  const [sRef, setSRef] = useState<string>(
    (channel?.serviceRef || channel?.id || '').trim()
  );

  // Traceability State
  const [traceId, setTraceId] = useState<string>('-');

  const [status, setStatus] = useState<PlayerStatus>('idle');
  const [error, setError] = useState<AppError | null>(null);
  const [showErrorDetails, setShowErrorDetails] = useState(false);
  const [capabilitySnapshot, setCapabilitySnapshot] = useState<CapabilitySnapshot | null>(null);
  const [playbackObservability, setPlaybackObservability] = useState<PlaybackObservability | null>(null);
  const [sessionPlaybackTrace, setSessionPlaybackTrace] = useState<PlaybackTraceContract | null>(null);
  const [sessionProfileReason, setSessionProfileReason] = useState<string | null>(null);
  const [startupElapsedSeconds, setStartupElapsedSeconds] = useState(0);
  const [showBufferingOverlay, setShowBufferingOverlay] = useState(false);
  const [showNativeVideo, setShowNativeVideo] = useState(true);
  const [showNativeVideoVeil, setShowNativeVideoVeil] = useState(false);
  const [nativeVeilResumeArmed, setNativeVeilResumeArmed] = useState(false);
  const hostEnvironment = useMemo(() => resolveHostEnvironment(), []);

  const containerRef = useRef<HTMLDivElement>(null);
  const videoRef = useRef<VideoElementRef>(null);
  const hlsRef = useRef<HlsInstanceRef>(null);
  const mounted = useRef<boolean>(false);
  const vodRetryRef = useRef<number | null>(null);
  const recordingTimeoutRef = useRef<number | null>(null);
  const vodFetchRef = useRef<AbortController | null>(null);
  const activeRecordingRef = useRef<string | null>(null);
  const [activeRecordingId, setActiveRecordingId] = useState<string | null>(null);
  const startIntentInFlight = useRef<boolean>(false);
  // ADR-00X: Profile-related refs removed (universal policy only)
  const isTeardownRef = useRef<boolean>(false);
  const userPauseIntentRef = useRef<boolean>(false);
  const startupStartedAtRef = useRef<number | null>(null);
  const bufferingOverlayTimerRef = useRef<number | null>(null);
  const nativeVideoRevealTimerRef = useRef<number | null>(null);
  const nativeVideoVeilRevealTimerRef = useRef<number | null>(null);
  const nativeVideoVeilClearTimerRef = useRef<number | null>(null);
  const nativeVideoShownRef = useRef(false);
  const nativeVideoHoldPositionRef = useRef<number | null>(null);
  const nativeVideoTempMutedRef = useRef(false);
  const nativeManagedPauseRef = useRef(false);
  const visibilityManagedPauseRef = useRef(false);

  const [durationSeconds, setDurationSeconds] = useState<number | null>(
    duration && duration > 0 ? duration : null
  );
  const [playbackMode, setPlaybackMode] = useState<'LIVE' | 'VOD' | 'UNKNOWN'>('UNKNOWN');
  const [vodStreamMode, setVodStreamMode] = useState<'direct_mp4' | 'native_hls' | 'hlsjs' | 'transcode' | null>(null);
  const [activeHlsEngine, setActiveHlsEngine] = useState<'native' | 'hlsjs' | null>(null);

  // P3-4: Truth State
  const [canSeek, setCanSeek] = useState(true);
  const [startUnix, setStartUnix] = useState<number | null>(null);

  const lastDecodedRef = useRef<number>(0);

  // Resume State
  const [resumeState, setResumeState] = useState<ResumeState | null>(null);
  const [showResumeOverlay, setShowResumeOverlay] = useState(false);
  const [isDocumentVisible, setIsDocumentVisible] = useState(
    () => typeof document === 'undefined' || document.visibilityState !== 'hidden'
  );

  const setPlayerError = useCallback((nextError: AppError | null) => {
    setError(nextError);
  }, []);

  const clearPlayerError = useCallback(() => {
    setError(null);
    setShowErrorDetails(false);
  }, []);

  const setLegacyError = useCallback<Dispatch<SetStateAction<string | null>>>((next) => {
    setError((current) => {
      const currentTitle = current?.title ?? null;
      const resolvedTitle = typeof next === 'function' ? next(currentTitle) : next;
      if (!resolvedTitle) {
        return null;
      }
      return {
        title: resolvedTitle,
        detail: current?.detail,
        status: current?.status,
        retryable: current?.retryable ?? true,
      };
    });
  }, []);

  const setLegacyErrorDetails = useCallback<Dispatch<SetStateAction<string | null>>>((next) => {
    setError((current) => {
      if (!current) {
        return current;
      }
      const currentDetail = current.detail ?? null;
      const resolvedDetail = typeof next === 'function' ? next(currentDetail) : next;
      return {
        ...current,
        detail: resolvedDetail ?? undefined,
      };
    });
  }, []);

  useEffect(() => {
    if (!error?.detail) {
      setShowErrorDetails(false);
    }
  }, [error?.detail]);

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

  const handleSessionSnapshot = useCallback((session: V3SessionStatusResponse) => {
    if (session.requestId) {
      setTraceId(session.requestId);
    }
    setSessionProfileReason(session.profileReason ?? null);
    mergeSessionPlaybackTrace(extractPlaybackTrace(session));
  }, [mergeSessionPlaybackTrace]);

  // Explicitly static/memoized apiBase
  const apiBase = useMemo(() => {
    return getApiBaseUrl();
  }, []);
  const requestedDuration = useMemo(() => (duration && duration > 0 ? duration : null), [duration]);
  const isCompactTouchLayout = useMemo(() => hasTouchInput(), []);

  const {
    sessionIdRef,
    authHeaders,
    reportError,
    ensureSessionCookie,
    setActiveSessionId,
    clearSessionLeaseState,
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
    setError: setLegacyError,
    readResponseBody,
    createPlayerError: (message, details) => new PlayerError(message, details),
    onSessionSnapshot: handleSessionSnapshot,
  });

  const {
    showStats,
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
    allowNativeFullscreen: activeHlsEngine === 'native',
    shouldForceNativeMobileHls,
    canUseDesktopWebKitFullscreen
  });

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
    playDirectMp4,
    waitForDirectStream
  } = usePlaybackEngine({
    videoRef,
    hlsRef,
    sessionIdRef,
    isTeardownRef,
    lastDecodedRef,
    t,
    reportError,
    waitForSessionReady,
    shouldPreferNativeHls: shouldPreferNativeWebKitHls,
    setStats,
    setStatus,
    setError: setLegacyError,
    setErrorDetails: setLegacyErrorDetails,
    setShowErrorDetails
  });

  // --- Core Helpers & Wrappers (Memoized) ---

  const clearRecordingTimeout = useCallback(() => {
    if (recordingTimeoutRef.current !== null) {
      window.clearTimeout(recordingTimeoutRef.current);
      recordingTimeoutRef.current = null;
    }
  }, []);

  const clearVodRetry = useCallback(() => {
    if (vodRetryRef.current !== null) {
      window.clearTimeout(vodRetryRef.current);
      vodRetryRef.current = null;
    }
    clearRecordingTimeout();
  }, [clearRecordingTimeout]);

  const clearVodFetch = useCallback(() => {
    if (vodFetchRef.current) {
      vodFetchRef.current.abort();
      vodFetchRef.current = null;
    }
  }, []);

  const clearNativeVideoVeilTimers = useCallback(() => {
    if (nativeVideoVeilRevealTimerRef.current !== null) {
      window.clearTimeout(nativeVideoVeilRevealTimerRef.current);
      nativeVideoVeilRevealTimerRef.current = null;
    }
    if (nativeVideoVeilClearTimerRef.current !== null) {
      window.clearTimeout(nativeVideoVeilClearTimerRef.current);
      nativeVideoVeilClearTimerRef.current = null;
    }
  }, []);

  const clearPlaybackSelection = useCallback(() => {
    activeRecordingRef.current = null;
    nativeVideoShownRef.current = false;
    nativeVideoHoldPositionRef.current = null;
    clearNativeVideoVeilTimers();
    setActiveRecordingId(null);
    setVodStreamMode(null);
    setActiveHlsEngine(null);
    setShowNativeVideo(true);
    setShowNativeVideoVeil(false);
    setNativeVeilResumeArmed(false);
    setCapabilitySnapshot(null);
    setPlaybackObservability(null);
    setSessionPlaybackTrace(null);
    setSessionProfileReason(null);
  }, [clearNativeVideoVeilTimers]);

  const clearPlaybackState = useCallback(() => {
    clearPlaybackSelection();
    clearVodRetry();
    clearVodFetch();
    clearSessionLeaseState();
    resetChromeState();
  }, [clearPlaybackSelection, clearSessionLeaseState, clearVodFetch, clearVodRetry, resetChromeState]);

  const clearNativeVideoRevealTimer = useCallback(() => {
    if (nativeVideoRevealTimerRef.current !== null) {
      window.clearTimeout(nativeVideoRevealTimerRef.current);
      nativeVideoRevealTimerRef.current = null;
    }
  }, []);

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

  const cleanupPlaybackResources = useCallback(() => {
    const activeHls = hlsRef.current;
    const activeSessionId = sessionIdRef.current;
    const activeVideo = videoRef.current;

    if (activeHls) activeHls.destroy();
    if (activeVideo) {
      activeVideo.pause();
      activeVideo.src = '';
    }

    clearVodRetry();
    clearVodFetch();
    clearPlaybackSelection();
    void sendStopIntent(activeSessionId, true);
  }, [clearPlaybackSelection, clearVodFetch, clearVodRetry, hlsRef, sendStopIntent, sessionIdRef, videoRef]);

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
    const hadActivePlayback = hasActivePlayback();

    clearPlaybackSelection();
    clearVodRetry();
    clearVodFetch();
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
  ]);

  const prepareFreshPlayback = useCallback((mode: 'LIVE' | 'VOD') => {
    setDurationSeconds(requestedDuration);
    setPlaybackMode(mode);
  }, [requestedDuration]);

  const gatherPlaybackCapabilities = useCallback(async (scope: 'live' | 'recording' = 'live'): Promise<CapabilitySnapshot> => {
    const video = videoRef.current as HTMLVideoElement | null;
    const probe = await probeRuntimePlaybackCapabilities(video, scope);
    const clientFamilyFallback = detectPlaybackClientFamily(video);

    const capabilities: CapabilitySnapshot = {
      capabilitiesVersion: 3,
      container: probe.containers,
      videoCodecs: probe.videoCodecs,
      videoCodecSignals: probe.videoCodecSignals,
      audioCodecs: probe.audioCodecs,
      hlsEngines: probe.hlsEngines.length > 0 ? probe.hlsEngines : undefined,
      preferredHlsEngine: probe.preferredHlsEngine ?? undefined,
      supportsHls: probe.hlsEngines.length > 0,
      supportsRange: probe.supportsRange,
      allowTranscode: true,
      deviceType: 'web',
      runtimeProbeUsed: probe.usedRuntimeProbe,
      runtimeProbeVersion: probe.version,
      clientFamilyFallback,
    };

    return capabilities;
  }, []);

  const startRecordingPlayback = useCallback(async (id: string): Promise<void> => {
    if (hasActivePlayback()) {
      await teardownActivePlayback();
    } else {
      clearPlaybackState();
    }
    activeRecordingRef.current = id;
    setActiveRecordingId(id);
    setStatus('building');
    clearPlayerError();
    setTraceId('-');
    setPlaybackMode('VOD');

    let abortController: AbortController | null = null;
    let requestCaps: CapabilitySnapshot | null = null;

    try {
      await ensureSessionCookie();

      // Determine Playback Mode from backend PlaybackInfo (single source of truth).
      let streamUrl = '';
      let mode: 'native_hls' | 'hlsjs' | 'direct_mp4' | 'transcode' | 'deny' = 'deny';

      try {
        const maxMetaRetries = 20;
        requestCaps = await gatherPlaybackCapabilities('recording');
        setCapabilitySnapshot(requestCaps);
        let pInfo: PlaybackInfo | undefined;

        for (let i = 0; i < maxMetaRetries; i++) {
          if (activeRecordingRef.current !== id) return;

          const { data, error, response } = await postRecordingPlaybackInfo({
            path: { recordingId: id },
            body: requestCaps
          });

          if (error) {
            if (notifyAuthRequiredIfUnauthorizedResponse(response, 'V3Player.recordingPlaybackInfo')) {
              telemetry.emit('ui.error', { status: 401, code: 'AUTH_DENIED' });
              setStatus('error');
              setPlayerError({
                title: t('player.authFailed'),
                status: 401,
                retryable: false,
              });
              return;
            }
            if (response.status === 403) {
              telemetry.emit('ui.error', { status: response.status, code: 'AUTH_DENIED' });
              setStatus('error');
              setPlayerError({
                title: t('player.forbidden'),
                status: 403,
                retryable: false,
              });
              return;
            }
            if (response.status === 410) {
              telemetry.emit('ui.error', { status: 410, code: 'GONE' });
              throw new Error(t('player.notAvailable'));
            }
            if (response.status === 409) {
              const retryAfterHeader = response.headers.get('Retry-After');
              const retryAfter = retryAfterHeader ? parseInt(retryAfterHeader, 10) : 0;
              const retryHint = retryAfter > 0 ? ` ${t('player.retryAfter', { seconds: retryAfter })}` : '';
              telemetry.emit('ui.error', { status: 409, code: 'LEASE_BUSY', retry_after: retryAfter });
              setStatus('error');
              setPlayerError({
                title: `${t('player.leaseBusy')}${retryHint}`,
                status: 409,
                retryable: true,
              });
              return;
            }
            if (response.status === 503) {
              const retryAfter = response.headers.get('Retry-After');
              if (retryAfter) {
                const seconds = parseInt(retryAfter, 10);
                telemetry.emit('ui.error', { status: 503, code: 'UNAVAILABLE', retry_after: seconds });
                setStatus('building');
                setLegacyErrorDetails(`${t('player.preparing')} (${seconds}s)`);
                await sleep(seconds * 1000);
                continue;
              } else {
                throw new Error('503 Service Unavailable (No Retry-After)');
              }
            }
            throw new Error(JSON.stringify(error));
          }

          if (data) {
            pInfo = data;
            break;
          }
        }

        if (!pInfo) {
          throw new Error("PlaybackInfo timeout");
        }

        debugLog('[V3Player] Playback Info:', pInfo);

        if (!pInfo.mode) {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.mode.missing',
            reason: 'MODE_MISSING'
          });
          throw new Error('Backend decision missing mode');
        }
        // Map backend 'hls' to local preferred HLS engine if needed
        const rawMode = pInfo.mode;
        if (rawMode === 'hls') {
          mode = resolvePreferredHlsEngineForCapabilities(requestCaps) === 'native' ? 'native_hls' : 'hlsjs';
        } else {
          mode = rawMode as any; // fall back to other modes or deny
        }

        if (!['native_hls', 'hlsjs', 'direct_mp4', 'transcode', 'deny'].includes(mode)) {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.mode.invalid',
            reason: String(mode)
          });
          throw new Error(`Unsupported backend playback mode: ${mode}`);
        }
        if (mode === 'deny') {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.mode.deny',
            reason: pInfo.decisionReason || pInfo.decision?.reasons?.[0] || 'unknown'
          });
          throw new Error(t('player.playbackDenied'));
        }
        if (!pInfo.decision?.selectedOutputUrl) {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.decision.selectionMissing',
            reason: 'DECISION_SELECTION_MISSING'
          });
          throw new Error('Backend decision missing selectedOutputUrl');
        }

        streamUrl = pInfo.decision.selectedOutputUrl;

        telemetry.emit('ui.contract.consumed', {
          mode: 'backend',
          fields: ['mode', 'decision.selectedOutputUrl']
        });

        if (streamUrl.startsWith('/')) {
          streamUrl = `${window.location.origin}${streamUrl}`;
        }

        // Add Cache Busting to prevent sticky 503s
        streamUrl += (streamUrl.includes('?') ? '&' : '?') + `cb=${Date.now()}`;

        setVodStreamMode(mode as any);

        // Truth Consumption
        const playbackDurationSeconds = resolvePlaybackDurationSeconds(pInfo);
        if (playbackDurationSeconds && playbackDurationSeconds > 0) {
          setDurationSeconds(playbackDurationSeconds);
        }

        if (pInfo.requestId) setTraceId(pInfo.requestId);
        setPlaybackObservability(extractPlaybackObservability(
          pInfo,
          requestCaps.preferredHlsEngine ?? null
        ));
        const recordingIsSeekable = pInfo.isSeekable !== undefined ? Boolean(pInfo.isSeekable) : true;
        setCanSeek(recordingIsSeekable);
        if (pInfo.startUnix) setStartUnix(pInfo.startUnix);

        // Resume State
        if (recordingIsSeekable && pInfo.resume && pInfo.resume.posSeconds >= 15 && (!pInfo.resume.finished)) {
          const d = pInfo.resume.durationSeconds || (playbackDurationSeconds || 0);
          if (!d || pInfo.resume.posSeconds < d - 10) {
            setResumeState({
              posSeconds: pInfo.resume.posSeconds,
              durationSeconds: pInfo.resume.durationSeconds || undefined,
              finished: pInfo.resume.finished || undefined
            });
            setShowResumeOverlay(true);
          }
        }
      } catch (e: unknown) {
        if (activeRecordingRef.current !== id) return;
        setStatus('error');
        mergeSessionPlaybackTrace(extractPlaybackTrace(e));
        setPlayerError(normalizePlayerError(e, {
          fallbackTitle: t('player.serverError'),
        }));
        return;
      }

      // --- EXECUTION PATHS ---
      if (mode === 'direct_mp4') {
        try {
          isTeardownRef.current = false;
          await waitForDirectStream(streamUrl);
          if (activeRecordingRef.current !== id) return;
          setStatus('buffering');
          setActiveHlsEngine(null);
          playDirectMp4(streamUrl);
          return;
        } catch (_error) {
          if (activeRecordingRef.current !== id) return;
          setStatus('error');
          setPlayerError({
            title: t('player.timeout'),
            retryable: true,
          });
          return;
        }
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

          if (activeRecordingRef.current !== id) return;
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
      if (activeRecordingRef.current !== id) return;
      debugError(err);
      mergeSessionPlaybackTrace(extractPlaybackTrace(err));
      setPlayerError(normalizePlayerError(err, {
        fallbackTitle: t('player.serverError'),
      }));
      setStatus('error');
    } finally {
      if (vodFetchRef.current === abortController) vodFetchRef.current = null;
    }
  }, [clearPlaybackState, clearPlayerError, ensureSessionCookie, gatherPlaybackCapabilities, hasActivePlayback, mergeSessionPlaybackTrace, playDirectMp4, playHls, resolvePreferredHlsEngineForCapabilities, setLegacyErrorDetails, setPlayerError, sleep, t, teardownActivePlayback, waitForDirectStream]);

  const startStream = useCallback(async (refToUse?: string): Promise<void> => {
    if (startIntentInFlight.current) return;
    startIntentInFlight.current = true;
    userPauseIntentRef.current = false;
    applyAutoplayMute();

    try {
      if (recordingId) {
        debugLog('[V3Player] startStream: recordingId path', { recordingId, hasSrc: !!src });
        if (src) {
          debugWarn('[V3Player] Both recordingId and src provided; prioritizing recordingId (VOD path).');
        }
        await startRecordingPlayback(recordingId);
        return;
      }

      if (src) {
        debugLog('[V3Player] startStream: src path', { hasSrc: true });
        if (hasActivePlayback()) {
          await teardownActivePlayback();
        } else {
          clearPlaybackState();
        }
        prepareFreshPlayback(requestedDuration ? 'VOD' : 'LIVE');
        setStatus('buffering');
        setTraceId('-');
        const srcEngine = resolvePreferredHlsEngine();
        playHls(src, srcEngine);
        setActiveHlsEngine(srcEngine);
        return;
      }

      const ref = (refToUse || sRef || '').trim();
      if (!ref) {
        setStatus('error');
        setPlayerError({
          title: t('player.serviceRefRequired'),
          retryable: false,
        });
        return;
      }
      if (hasActivePlayback()) {
        await teardownActivePlayback();
      } else {
        clearPlaybackState();
      }
      prepareFreshPlayback('LIVE');
      let newSessionId: string | null = null;
      setStatus('starting');
      clearPlayerError();
      setTraceId('-');

      try {
        await ensureSessionCookie();

        // SSOT: live playback mode is decided by backend from measured capabilities.
        let liveMode: 'native_hls' | 'hlsjs' | 'direct_mp4' | 'transcode' | 'deny' = 'deny';
        let liveEngine: 'native' | 'hlsjs' = 'hlsjs';

        const requestCaps = await gatherPlaybackCapabilities('live');
        const preferredHlsEngine = resolvePreferredHlsEngineForCapabilities(requestCaps);
        setCapabilitySnapshot(requestCaps);
        // raw-fetch-justified: live decision request posts dynamic capability payload not covered by generated wrapper flow.
        const liveResponse = await fetch(`${apiBase}/live/stream-info`, {
          method: 'POST',
          headers: authHeaders(true),
          body: JSON.stringify({
            serviceRef: ref,
            capabilities: requestCaps
          })
        });
        const { json: liveInfoJson } = await readResponseBody(liveResponse);
        const liveInfo = liveInfoJson as PlaybackInfo;
        const liveError = (!liveResponse.ok) ? liveInfoJson as any : null;
        const liveRequestId =
          (typeof liveInfo?.requestId === 'string' ? liveInfo.requestId : undefined) ||
          liveResponse.headers.get('X-Request-ID') ||
          undefined;
        if (liveRequestId) {
          setTraceId(liveRequestId);
        }

        if (!liveResponse.ok) {
          if (notifyAuthRequiredIfUnauthorizedResponse(liveResponse, 'V3Player.liveStreamInfo')) {
            setStatus('error');
            setPlayerError(normalizePlayerError(liveError ?? {
              status: 401,
              title: t('player.authFailed'),
              requestId: liveRequestId,
            }, {
              fallbackTitle: t('player.authFailed'),
              status: 401,
              retryable: false,
            }));
            return;
          }
          if (liveResponse.status === 403) {
            setStatus('error');
            setPlayerError(normalizePlayerError(liveError ?? {
              status: 403,
              title: t('player.forbidden'),
              requestId: liveRequestId,
            }, {
              fallbackTitle: t('player.forbidden'),
              status: 403,
              retryable: false,
            }));
            return;
          }
          throw normalizePlayerError(liveError ?? {
            status: liveResponse.status,
            title: `${t('player.apiError')}: ${liveResponse.status}`,
            requestId: liveRequestId,
          }, {
            fallbackTitle: `${t('player.apiError')}: ${liveResponse.status}`,
            status: liveResponse.status,
          });
        }

        if (!liveInfo?.mode) {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.live.mode.missing',
            reason: 'MODE_MISSING'
          });
          throw new Error('Backend live decision missing mode');
        }

        const beMode = String(liveInfo.mode);
        switch (beMode) {
          case 'native_hls':
            liveMode = 'native_hls';
            liveEngine = 'native';
            break;
          case 'hlsjs':
          case 'hls':
          case 'direct_stream':
            liveEngine = preferredHlsEngine;
            liveMode = liveEngine === 'native' ? 'native_hls' : 'hlsjs';
            break;
          case 'transcode':
            liveMode = 'transcode';
            liveEngine = preferredHlsEngine;
            break;
          case 'direct_mp4':
            liveMode = 'direct_mp4';
            break;
          case 'deny':
            liveMode = 'deny';
            break;
          default:
            telemetry.emit('ui.failclosed', {
              context: 'V3Player.live.mode.invalid',
              reason: beMode || String(liveMode)
            });
            throw new Error(`Unsupported backend live playback mode: ${beMode}`);
        }

        if (!liveInfo.requestId) {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.live.request_id.missing',
            reason: 'REQUEST_ID_MISSING'
          });
          throw new Error('Backend live decision missing requestId');
        }
        setTraceId(liveInfo.requestId);
        setPlaybackObservability(extractPlaybackObservability(
          liveInfo,
          requestCaps.preferredHlsEngine ?? null
        ));

        if (liveMode === 'deny') {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.live.mode.deny',
            reason: liveInfo.reason || liveInfo.decision?.reasons?.[0] || 'unknown'
          });
          setStatus('error');
          setPlayerError({
            title: t('player.playbackDenied'),
            retryable: false,
          });
          return;
        }

        const liveDecisionToken = liveInfo.playbackDecisionToken;
        if (!liveDecisionToken) {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.live.playback_decision_token.missing',
            reason: 'PLAYBACK_DECISION_TOKEN_MISSING'
          });
          throw new Error('Backend live decision missing playbackDecisionToken');
        }

        if (liveMode === 'native_hls') {
          liveEngine = 'native';
        } else if (liveMode === 'hlsjs') {
          liveEngine = 'hlsjs';
        } else if (liveMode === 'transcode') {
          liveEngine = resolvePreferredHlsEngineForCapabilities(requestCaps);
        } else {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.live.mode.unsupported',
            reason: liveMode
          });
          throw new Error(`Unsupported live playback mode: ${liveMode}`);
        }

        telemetry.emit('ui.contract.consumed', {
          mode: 'backend',
          fields: ['mode']
        });

        const intentParams: Record<string, string> = {
          playback_mode: liveMode,
          // Keep canonical contract key for downstream compatibility checks.
          playback_decision_token: liveDecisionToken
        };
        if (requestCaps.clientFamilyFallback) {
          intentParams.client_family = requestCaps.clientFamilyFallback;
        }
        if (requestCaps.preferredHlsEngine) {
          intentParams.preferred_hls_engine = requestCaps.preferredHlsEngine;
        }
        if (requestCaps.deviceType) {
          intentParams.device_type = requestCaps.deviceType;
        }
        const autoCodecs = resolveAutoTranscodeCodecs(requestCaps);
        if (autoCodecs.length > 0) {
          intentParams.codecs = autoCodecs.join(',');
        }
        const capHash = extractCapHashFromDecisionToken(liveDecisionToken);
        if (capHash) {
          intentParams.capHash = capHash;
        }

        // raw-fetch-justified: stream.start intent needs explicit payload shaping and immediate RFC7807 handling.
        const res = await fetch(`${apiBase}/intents`, {
          method: 'POST',
          headers: authHeaders(true),
          body: JSON.stringify({
            type: 'stream.start',
            serviceRef: ref,
            playbackDecisionToken: liveDecisionToken,
            ...(Object.keys(intentParams).length > 0 ? { params: intentParams } : {})
          })
        });

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

              telemetry.emit('ui.auth_error', {
                status: res.status,
                code: problem.code || null,
                title: problem.title || null,
                detail: problem.detail || null
              });
            }
          } catch {
            // Body parse failed – fall through with generic message
          }
          setStatus('error');
          setPlayerError(normalizePlayerError(problemBody ?? {
            status: res.status,
            title: errorTitle,
          }, {
            fallbackTitle: errorTitle,
            status: res.status,
            retryable: false,
          }));
          return;
        }

        if (res.status === 409) {
          const retryAfterHeader = res.headers.get('Retry-After');
          const retryAfter = retryAfterHeader ? parseInt(retryAfterHeader, 10) : 0;
          const retryHint = retryAfter > 0 ? ` ${t('player.retryAfter', { seconds: retryAfter })}` : '';
          let apiErr: ApiErrorResponse | null = null;
          try {
            apiErr = await res.json();
          } catch {
            apiErr = null;
          }
          setStatus('error');
          setPlayerError(normalizePlayerError(apiErr ?? {
            status: 409,
            title: `${t('player.leaseBusy')}${retryHint}`,
          }, {
            fallbackTitle: `${t('player.leaseBusy')}${retryHint}`,
            status: 409,
            retryable: true,
          }));
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

        const data: V3SessionResponse = await res.json();
        newSessionId = data.sessionId;
        if (data.requestId) setTraceId(data.requestId);
        setActiveSessionId(newSessionId);
        const session = await waitForSessionReady(newSessionId);

        setStatus('ready');
        const streamUrl = session.playbackUrl;
        if (!streamUrl) {
          throw new Error(t('player.streamUrlMissing'));
        }
        playHls(streamUrl, liveEngine);
        setActiveHlsEngine(liveEngine);

      } catch (err) {
        if (newSessionId) {
          await sendStopIntent(newSessionId);
        }
        clearSessionLeaseState();
        debugError(err);
        mergeSessionPlaybackTrace(extractPlaybackTrace(err));
        setPlayerError(normalizePlayerError(err, {
          fallbackTitle: t('player.serverError'),
        }));
        setStatus('error');
      }
    } finally {
      startIntentInFlight.current = false;
    }
  }, [src, recordingId, sRef, apiBase, authHeaders, clearPlaybackState, clearPlayerError, ensureSessionCookie, waitForSessionReady, hasActivePlayback, mergeSessionPlaybackTrace, playHls, sendStopIntent, clearSessionLeaseState, t, startRecordingPlayback, applyAutoplayMute, gatherPlaybackCapabilities, resolvePreferredHlsEngine, resolvePreferredHlsEngineForCapabilities, setActiveSessionId, setPlayerError, prepareFreshPlayback, requestedDuration, teardownActivePlayback]);

  const stopStream = useCallback(async (skipClose: boolean = false): Promise<void> => {
    userPauseIntentRef.current = true;
    await teardownActivePlayback();
    setPlaybackMode('UNKNOWN');
    setStatus('stopped');
    setTraceId('-');
    if (onClose && !skipClose) onClose();
  }, [onClose, setPlaybackMode, teardownActivePlayback]);

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
    if (requestedDuration) {
      setDurationSeconds(requestedDuration);
    }
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
  }, [hasTerminalStatus, hostEnvironment.isTv, isDocumentVisible, setStatus, status, videoRef]);

  useEffect(() => {
    if (bufferingOverlayTimerRef.current !== null) {
      window.clearTimeout(bufferingOverlayTimerRef.current);
      bufferingOverlayTimerRef.current = null;
    }

    if (status !== 'buffering') {
      setShowBufferingOverlay(false);
      return;
    }

    bufferingOverlayTimerRef.current = window.setTimeout(() => {
      bufferingOverlayTimerRef.current = null;
      setShowBufferingOverlay(true);
    }, 325);

    return () => {
      if (bufferingOverlayTimerRef.current !== null) {
        window.clearTimeout(bufferingOverlayTimerRef.current);
        bufferingOverlayTimerRef.current = null;
      }
    };
  }, [status]);

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
    showNativeVideo,
    status,
    videoRef,
  ]);

  useEffect(() => {
    if (!isOverlayStartupStatus) {
      startupStartedAtRef.current = null;
      setStartupElapsedSeconds(0);
      return;
    }

    if (startupStartedAtRef.current === null) {
      startupStartedAtRef.current = Date.now();
    }

    const updateElapsed = () => {
      const startedAt = startupStartedAtRef.current;
      if (startedAt === null) {
        setStartupElapsedSeconds(0);
        return;
      }
      setStartupElapsedSeconds(Math.max(0, Math.floor((Date.now() - startedAt) / 1000)));
    };

    updateElapsed();
    const timer = window.setInterval(updateElapsed, 1000);
    return () => window.clearInterval(timer);
  }, [isOverlayStartupStatus]);

  // Cleanup effect
  useEffect(() => cleanupPlaybackResources, [cleanupPlaybackResources]);

  useEffect(() => {
    const handleVisibilityChange = () => {
      setIsDocumentVisible(document.visibilityState !== 'hidden');
    };

    document.addEventListener('visibilitychange', handleVisibilityChange);
    window.addEventListener('pageshow', handleVisibilityChange);
    window.addEventListener('pagehide', handleVisibilityChange);

    return () => {
      document.removeEventListener('visibilitychange', handleVisibilityChange);
      window.removeEventListener('pageshow', handleVisibilityChange);
      window.removeEventListener('pagehide', handleVisibilityChange);
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

  useEffect(() => {
    const video = videoRef.current;
    if (!video) {
      return;
    }

    if (isNativeEngine && showNativeVideoVeil && showNativeVideo) {
      if (nativeVeilResumeArmed) {
        return;
      }
      if (!nativeManagedPauseRef.current && !video.paused) {
        video.dataset.xg2gManagedPause = '1';
        nativeManagedPauseRef.current = true;
        video.pause();
      }
      return;
    }

    if (nativeManagedPauseRef.current) {
      delete video.dataset.xg2gManagedPause;
      nativeManagedPauseRef.current = false;
      if (isNativeEngine && !hasTerminalStatus && status !== 'paused') {
        void video.play().catch((err) => debugWarn('[V3Player] Native veil resume play blocked', err));
      }
    }
  }, [hasTerminalStatus, isNativeEngine, nativeVeilResumeArmed, showNativeVideo, showNativeVideoVeil, status, videoRef]);

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

    if (!video.paused && video.readyState >= 3) {
      releaseVeil();
      return;
    }

    video.addEventListener('playing', handlePlaying, { once: true });

    delete video.dataset.xg2gManagedPause;
    nativeManagedPauseRef.current = false;
    void video.play().catch((err) => {
      debugWarn('[V3Player] Native veil resume play blocked', err);
      setNativeVeilResumeArmed(false);
    });

    return () => {
      video.removeEventListener('playing', handlePlaying);
    };
  }, [clearNativeVideoVeilTimers, isNativeEngine, nativeVeilResumeArmed, showNativeVideo, showNativeVideoVeil, videoRef]);

  const effectiveClientPath =
    sessionPlaybackTrace?.clientPath ||
    playbackObservability?.clientPath ||
    formatClientPath(capabilitySnapshot);
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
    playbackObservability?.qualityRung ??
    null;
  const effectiveAudioQualityRung =
    sessionPlaybackTrace?.audioQualityRung ??
    playbackObservability?.audioQualityRung ??
    null;
  const effectiveVideoQualityRung =
    sessionPlaybackTrace?.videoQualityRung ??
    playbackObservability?.videoQualityRung ??
    null;
  const effectiveDegradedFrom =
    sessionPlaybackTrace?.degradedFrom ??
    playbackObservability?.degradedFrom ??
    null;
  const effectiveTargetProfile =
    sessionPlaybackTrace?.targetProfile ??
    playbackObservability?.targetProfile ??
    null;
  const effectiveTargetProfileHash =
    sessionPlaybackTrace?.targetProfileHash ??
    playbackObservability?.targetProfileHash ??
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

  return (
    <div
      ref={containerRef}
      className={[
        styles.container,
        'animate-enter',
        onClose ? styles.overlay : null,
        isIdle ? styles.userIdle : null,
      ].filter(Boolean).join(' ')}
    >
      {onClose && (
        <button
          onClick={() => void stopStream()}
          className={styles.closeButton}
          aria-label={t('player.closePlayer')}
        >
          ✕
        </button>
      )}

      {/* Stats Overlay */}
      {showStats && (
        <div className={styles.statsOverlay}>
          <Card variant="standard">
            <Card.Header>
              <Card.Title>{t('player.statsTitle', { defaultValue: 'Technical Stats' })}</Card.Title>
            </Card.Header>
            <Card.Content className={styles.statsGrid}>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.status')}</span>
                <StatusChip
                  state={status === 'ready' ? 'live' : status === 'error' ? 'error' : 'idle'}
                  label={t(`player.statusStates.${status}`, { defaultValue: status })}
                />
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('common.session', { defaultValue: 'Session' })}</span>
                <span className={styles.statsValue}>{sessionIdRef.current || '-'}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('common.requestId', { defaultValue: 'Request ID' })}</span>
                <span className={styles.statsValue}>{sessionPlaybackTrace?.requestId || traceId}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.clientPath', { defaultValue: 'Client Path' })}</span>
                <span className={styles.statsValue}>{effectiveClientPath || '-'}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.requestProfile', { defaultValue: 'Request Profile' })}</span>
                <span className={styles.statsValue}>{formatRequestProfileLabel(effectiveRequestProfile)}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.requestedIntent', { defaultValue: 'Requested Intent' })}</span>
                <span className={styles.statsValue}>{formatRequestProfileLabel(effectiveRequestedIntent)}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.resolvedIntent', { defaultValue: 'Resolved Intent' })}</span>
                <span className={styles.statsValue}>{formatRequestProfileLabel(effectiveResolvedIntent)}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.qualityRung', { defaultValue: 'Quality Rung' })}</span>
                <span className={styles.statsValue}>{formatQualityRungLabel(effectiveQualityRung)}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.audioQualityRung', { defaultValue: 'Audio Quality Rung' })}</span>
                <span className={styles.statsValue}>{formatQualityRungLabel(effectiveAudioQualityRung)}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.videoQualityRung', { defaultValue: 'Video Quality Rung' })}</span>
                <span className={styles.statsValue}>{formatQualityRungLabel(effectiveVideoQualityRung)}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.degradedFrom', { defaultValue: 'Degraded From' })}</span>
                <span className={styles.statsValue}>{formatRequestProfileLabel(effectiveDegradedFrom)}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.hostPressure', { defaultValue: 'Host Pressure' })}</span>
                <span className={styles.statsValue}>{effectiveHostPressureBand || '-'}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.hostOverrideApplied', { defaultValue: 'Host Override Applied' })}</span>
                <span className={styles.statsValue}>{formatBooleanLabel(effectiveHostOverrideApplied)}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.forcedIntent', { defaultValue: 'Forced Intent' })}</span>
                <span className={styles.statsValue}>{formatRequestProfileLabel(effectiveForcedIntent)}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.operatorMaxQualityRung', { defaultValue: 'Operator Max Quality' })}</span>
                <span className={styles.statsValue}>{formatQualityRungLabel(effectiveOperatorMaxQualityRung)}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.operatorRuleName', { defaultValue: 'Operator Rule' })}</span>
                <span className={styles.statsValue}>{effectiveOperatorRuleName || '-'}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.operatorRuleScope', { defaultValue: 'Operator Rule Scope' })}</span>
                <span className={styles.statsValue}>{effectiveOperatorRuleScope || '-'}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.clientFallbackDisabled', { defaultValue: 'Client Fallback Disabled' })}</span>
                <span className={styles.statsValue}>{formatBooleanLabel(effectiveClientFallbackDisabled)}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.operatorOverrideApplied', { defaultValue: 'Operator Override Applied' })}</span>
                <span className={styles.statsValue}>{formatBooleanLabel(effectiveOperatorOverrideApplied)}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.sourceProfile', { defaultValue: 'Source Profile' })}</span>
                <span className={styles.statsValue}>{sourceProfileSummary}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.outputProfile', { defaultValue: 'Output Profile' })}</span>
                <span className={styles.statsValue}>{formatTargetProfileSummary(effectiveTargetProfile)}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.profileHash', { defaultValue: 'Profile Hash' })}</span>
                <span className={styles.statsValue}>{effectiveTargetProfileHash || '-'}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.execution', { defaultValue: 'Execution' })}</span>
                <span className={styles.statsValue}>{formatExecutionLabel(effectiveTargetProfile)}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.ffmpegPlan', { defaultValue: 'FFmpeg Plan' })}</span>
                <span className={styles.statsValue}>{ffmpegPlanSummary}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.firstFrame', { defaultValue: 'First Frame' })}</span>
                <span className={styles.statsValue}>{firstFrameLabel}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.fallbacks', { defaultValue: 'Fallbacks' })}</span>
                <span className={styles.statsValue}>{fallbackSummary}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.stopReason', { defaultValue: 'Stop' })}</span>
                <span className={styles.statsValue}>{stopSummary}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.outputKind', { defaultValue: 'Output Kind' })}</span>
                <span className={styles.statsValue}>{playbackObservability?.selectedOutputKind || '-'}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.resolution')}</span>
                <span className={styles.statsValue}>{stats.resolution}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.bandwidth')}</span>
                <span className={styles.statsValue}>{stats.bandwidth > 0 ? `${stats.bandwidth} kbps` : '-'}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.bufferHealth')}</span>
                <span className={styles.statsValue}>{stats.bufferHealth}s</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.latency')}</span>
                <span className={styles.statsValue}>{stats.latency !== null ? stats.latency + 's' : '-'}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.fps')}</span>
                <span className={styles.statsValue}>{stats.fps}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.dropped')}</span>
                <span className={styles.statsValue}>{stats.droppedFrames}</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.hlsLevel')}</span>
                <span className={styles.statsValue}>{
                  hlsRef.current ? (stats.levelIndex === -1 ? 'Auto' : stats.levelIndex) : 'Native / Direct'
                }</span>
              </div>
              <div className={styles.statsRow}>
                <span className={styles.statsLabel}>{t('player.segDuration')}</span>
                <span className={styles.statsValue}>{stats.buffer > 0 ? `${stats.buffer}s` : '-'}</span>
              </div>
            </Card.Content>
          </Card>
        </div>
      )}

      <div
        className={[
          styles.videoWrapper,
          showNativeBufferingMask ? styles.videoWrapperMasked : null,
        ].filter(Boolean).join(' ')}
      >
        {channel && <h3 className={styles.overlayTitle}>{channel.name}</h3>}
        {showNativeBufferingMask && (
          <div
            className={styles.nativeBufferingMask}
            aria-hidden="true"
          ></div>
        )}

        {/* PREPARING Overlay (VOD Remux) */}
        {showStartupOverlay && (
          <div
            className={[
              styles.spinnerOverlay,
              useNativeBufferingSafeOverlay ? styles.spinnerOverlaySafe : null,
            ].filter(Boolean).join(' ')}
            ref={() => debugLog('[V3Player] Spinner Rendered', { status, fullscreen: isFullscreen })}
          >
            <div className={styles.spinnerBadge}>
              <div className={`${styles.spinner} spinner-base`}></div>
            </div>
            <div className={styles.spinnerContent}>
              <div className={styles.spinnerEyebrow}>
                {t('player.startupSurfaceEyebrow', { defaultValue: 'Live startup' })}
              </div>
              {channel && <h2 className={styles.spinnerTitle}>{channel.name}</h2>}
              <div className={styles.spinnerStatusRow}>
                <StatusChip
                  state={overlayStatus === 'buffering' ? 'live' : 'idle'}
                  label={t(`player.statusStates.${overlayStatus}`, { defaultValue: overlayStatus })}
                />
              </div>
              <div className={styles.spinnerLabel}>{spinnerLabel}</div>
              <div className={styles.spinnerSupport}>{spinnerSupport}</div>
                <div className={styles.spinnerMeta}>
                  <div className={styles.spinnerProgressTrack} aria-hidden="true">
                    <div className={`${styles.spinnerProgressFill} animate-startup-progress`}></div>
                  </div>
                  <div className={styles.spinnerElapsed}>
                    {t('player.startupElapsed', {
                    defaultValue: 'Wait {{seconds}}s',
                    seconds: startupElapsedSeconds,
                  })}
                </div>
              </div>
            </div>
          </div>
        )}

        <video
          ref={videoRef}
          controls={false}
          playsInline
          webkit-playsinline=""
          preload="metadata"
          autoPlay={!!autoStart}
          className={[
            styles.videoElement,
            showNativeBufferingMask ? styles.videoElementHidden : null,
          ].filter(Boolean).join(' ')}
        />
      </div>

      {/* Error Toast */}
      {error && (
        <div className={styles.errorToast} aria-live="polite" role="alert">
          <div className={styles.errorMain}>
            <span className={styles.errorText}>⚠ {error.title}</span>
            {error.retryable ? (
              <Button variant="secondary" size="sm" onClick={handleRetry}>{t('common.retry')}</Button>
            ) : null}
          </div>
          {showVerboseErrorTelemetry && (stopSummary !== '-' || fallbackSummary !== '-' || ffmpegPlanSummary !== '-' || hostPressureSummary !== '-') && (
            <div className={styles.errorTelemetry}>
              {stopSummary !== '-' && (
                <div className={styles.errorTelemetryRow}>
                  <span className={styles.errorTelemetryLabel}>{t('player.stopReason', { defaultValue: 'Stop' })}</span>
                  <span className={styles.errorTelemetryValue}>{stopSummary}</span>
                </div>
              )}
              {hostPressureSummary !== '-' && (
                <div className={styles.errorTelemetryRow}>
                  <span className={styles.errorTelemetryLabel}>{t('player.hostPressure', { defaultValue: 'Host Pressure' })}</span>
                  <span className={styles.errorTelemetryValue}>{hostPressureSummary}</span>
                </div>
              )}
              {fallbackSummary !== '-' && (
                <div className={styles.errorTelemetryRow}>
                  <span className={styles.errorTelemetryLabel}>{t('player.fallbacks', { defaultValue: 'Fallbacks' })}</span>
                  <span className={styles.errorTelemetryValue}>{fallbackSummary}</span>
                </div>
              )}
              {ffmpegPlanSummary !== '-' && (
                <div className={styles.errorTelemetryRow}>
                  <span className={styles.errorTelemetryLabel}>{t('player.ffmpegPlan', { defaultValue: 'FFmpeg Plan' })}</span>
                  <span className={styles.errorTelemetryValue}>{ffmpegPlanSummary}</span>
                </div>
              )}
            </div>
          )}
          {error.detail && (
            <button
              onClick={() => setShowErrorDetails(!showErrorDetails)}
              className={styles.errorDetailsButton}
            >
              {showErrorDetails ? t('common.hideDetails') : t('common.showDetails')}
            </button>
          )}
          {showErrorDetails && error.detail && (
            <div className={styles.errorDetailsContent}>
              <pre className={styles.errorDetailsPre}>{error.detail}</pre>
              <br />
              {t('common.session')}: {sessionIdRef.current || t('common.notAvailable')}
            </div>
          )}
        </div>
      )}

      {/* Controls & Status Bar */}
      <div className={styles.controlsHeader}>
        {hasSeekWindow ? (
          <div className={[styles.vodControls, styles.seekControls].join(' ')}>
            <div className={styles.seekButtons}>
              <Button variant="ghost" size="sm" onClick={() => seekBy(-900)} title={t('player.seekBack15m')} aria-label={t('player.seekBack15m')}>
                ↺ 15m
              </Button>
              <Button variant="ghost" size="sm" onClick={() => seekBy(-60)} title={t('player.seekBack60s')} aria-label={t('player.seekBack60s')}>
                ↺ 60s
              </Button>
              <Button variant="ghost" size="sm" onClick={() => seekBy(-15)} title={t('player.seekBack15s')} aria-label={t('player.seekBack15s')}>
                ↺ 15s
              </Button>
            </div>

            <Button
              variant="primary"
              size="icon"
              className={styles.playPauseButton}
              onClick={togglePlayPause}
              title={isPlaying ? t('player.pause') : t('player.play')}
              aria-label={isPlaying ? t('player.pause') : t('player.play')}
            >
              {isPlaying ? '⏸' : '▶'}
            </Button>

            <div className={styles.seekSliderGroup}>
              <span className={styles.vodTime}>{startTimeDisplay}</span>
              <input
                type="range"
                min="0"
                max={windowDuration}
                step="0.1"
                className={styles.vodSlider}
                value={relativePosition}
                onChange={(e) => {
                  const newVal = parseFloat(e.target.value);
                  seekTo(seekableStart + newVal);
                }}
              />
              <span className={styles.vodTimeTotal}>{endTimeDisplay}</span>
            </div>

            <div className={styles.seekButtons}>
              <Button variant="ghost" size="sm" onClick={() => seekBy(15)} title={t('player.seekForward15s')} aria-label={t('player.seekForward15s')}>
                +15s
              </Button>
              <Button variant="ghost" size="sm" onClick={() => seekBy(60)} title={t('player.seekForward60s')} aria-label={t('player.seekForward60s')}>
                +60s
              </Button>
              <Button variant="ghost" size="sm" onClick={() => seekBy(900)} title={t('player.seekForward15m')} aria-label={t('player.seekForward15m')}>
                +15m
              </Button>
            </div>

            {isLiveMode && (
              <button
                className={[styles.liveButton, isAtLiveEdge ? styles.liveButtonActive : null].filter(Boolean).join(' ')}
                onClick={() => seekTo(seekableEnd)}
                title={t('player.goLive')}
              >
                LIVE
              </button>
            )}
          </div>
        ) : (
          !channel && !recordingId && !src && (
            <input
              type="text"
              className={styles.serviceInput}
              value={sRef}
              onChange={(e) => setSRef(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault();
                  const nextRef = e.currentTarget.value;
                  void startStream(nextRef);
                }
              }}
            />
          )
        )}

        {/* ADR-00X: Profile dropdown removed (universal policy only) */}

        {!autoStart && !src && !recordingId && (
          <Button
            onClick={() => startStream()}
            disabled={status === 'starting' || status === 'priming'}
          >
            ▶ {t('common.startStream')}
          </Button>
        )}

        {/* DVR Mode Button (Safari Only / Fallback) */}
        {showDvrModeButton && !canToggleFullscreen && (
          <Button onClick={enterDVRMode} title={t('player.dvrMode')}>
            📺 DVR
          </Button>
        )}

        <div className={styles.utilityControls}>
          {canToggleFullscreen && (
            <Button
              variant="ghost"
              size="sm"
              active={isFullscreen}
              onClick={() => void toggleFullscreen()}
              title={isFullscreen
                ? t('player.exitFullscreenLabel', { defaultValue: 'Exit fullscreen' })
                : t('player.fullscreenLabel', { defaultValue: 'Fullscreen' })}
            >
              ⛶ {isFullscreen
                ? t('player.exitFullscreenLabel', { defaultValue: 'Exit fullscreen' })
                : t('player.fullscreenLabel', { defaultValue: 'Fullscreen' })}
            </Button>
          )}

          {canToggleMute && (
            <div className={styles.volumeControl}>
              <Button
                variant={isMuted ? 'primary' : 'ghost'}
                size="sm"
                className={styles.audioToggleButton}
                onClick={toggleMute}
                title={audioToggleLabel}
                aria-label={audioToggleLabel}
                aria-pressed={!isMuted}
              >
                <span className={styles.audioToggleIcon} aria-hidden="true">{audioToggleIcon}</span>
                <span>{audioToggleLabel}</span>
              </Button>
              {canAdjustVolume ? (
                <input
                  type="range"
                  min="0"
                  max="1"
                  step="0.05"
                  className={styles.volumeSlider}
                  value={isMuted ? 0 : volume}
                  onChange={(e) => handleVolumeChange(parseFloat(e.target.value))}
                />
              ) : (
                <span className={styles.deviceVolumeHint}>
                  {t('player.deviceVolumeHint', { defaultValue: 'Use device buttons' })}
                </span>
              )}
            </div>
          )}

          {canTogglePiP && (
            <Button
              variant="ghost"
              size="sm"
              active={isPip}
              onClick={() => void togglePiP()}
              title={t('player.pipTitle')}
            >
              📺 {t('player.pipLabel')}
            </Button>
          )}

          <Button
            variant="ghost"
            size="sm"
            active={showStats}
            onClick={toggleStats}
            title={t('player.statsTitle')}
          >
            📊 {t('player.statsLabel')}
          </Button>

          {!onClose && (
            <Button variant="danger" onClick={() => void stopStream()}>
              ⏹ {t('common.stop')}
            </Button>
          )}
        </div>
      </div>
      {/* Resume Overlay */}
      {showResumeOverlay && resumeState && (
        <div className={styles.resumeOverlay}>
          <div className={styles.resumeContent}>
            <h3>{t('player.resumeTitle')}</h3>
            <p>{t('player.resumePrompt', { time: formatClock(resumeState.posSeconds) })}</p>
            <div className={styles.resumeActions}>
              <Button
                onClick={() => {
                  seekWhenReady(resumeState.posSeconds);
                  setShowResumeOverlay(false);
                }}
              >
                {t('player.resumeAction')}
              </Button>
              <Button
                variant="secondary"
                onClick={() => {
                  seekWhenReady(0);
                  setShowResumeOverlay(false);
                }}
              >
                {t('player.startOver')}
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export default V3Player;
// cspell:ignore remux arrowleft arrowright enterpictureinpicture leavepictureinpicture kbps Remux
