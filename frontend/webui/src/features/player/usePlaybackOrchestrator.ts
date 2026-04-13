import { useState, useEffect, useRef, useCallback, useMemo, useReducer } from 'react';
import type { Dispatch, RefObject, SetStateAction } from 'react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import Hls from './lib/hlsRuntime';
import {
  postRecordingPlaybackInfo,
  type IntentRequest,
  type PlaybackSourceProfile,
  type PlaybackTrace as PlaybackTraceContract,
  type PlaybackTraceFfmpegPlan,
  type PlaybackTraceOperator,
  type PlaybackTargetProfile,
} from '../../client-ts';
import { getApiBaseUrl } from '../../services/clientWrapper';
import { telemetry } from '../../services/TelemetryService';
import type {
  V3PlayerProps,
  PlayerStatus,
  V3SessionResponse,
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
  extractCapHashFromDecisionToken,
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
  buildPlaybackFailure,
  createInitialPlaybackDomainState,
  playbackMachine,
} from './orchestrator/playbackMachine';
import type {
  PlaybackContractState,
  SessionPhase,
  VodStreamMode,
} from './orchestrator/playbackTypes';
import { normalizePlaybackInfo } from './contracts/normalizePlaybackInfo';
import type {
  NormalizedBlockedPlaybackContract,
  NormalizedPlaybackDecisionObservability,
  NormalizedPlayablePlaybackContract,
} from './contracts/normalizedPlaybackTypes';
import {
  buildPlaybackAdvisorySignal,
  classifyNormalizedContractFailure,
  type PlaybackFailureReportOptions,
} from './semantics/playbackFailureSemantics';
import {
  mapPlaybackAdvisoryToTelemetryEvents,
  mapPlaybackFailureToTelemetryEvents,
} from './semantics/playbackTelemetryMapping';
import {
  getNativePlaybackState,
  onNativePlaybackState,
  requestHostInputFocus,
  resolveHostEnvironment,
  setHostPlaybackActive,
  startNativePlayback,
  stopNativePlayback,
} from '../../lib/hostBridge';
import type {
  HostEnvironment,
  NativePlaybackRequest,
  NativePlaybackState as HostNativePlaybackState,
} from '../../lib/hostBridge';
import type { AppError } from '../../types/errors';

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

function resolvePlaybackObservability(
  decision: NormalizedPlaybackDecisionObservability | null,
  clientPath: string | null
): PlaybackObservability | null {
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
    requestProfile: decision.requestProfile,
    requestedIntent: decision.requestedIntent,
    resolvedIntent: decision.resolvedIntent,
    qualityRung: decision.qualityRung,
    audioQualityRung: decision.audioQualityRung,
    videoQualityRung: decision.videoQualityRung,
    degradedFrom: decision.degradedFrom,
    hostPressureBand: decision.hostPressureBand,
    hostOverrideApplied: decision.hostOverrideApplied,
    targetProfileHash: decision.targetProfileHash,
    targetProfile: decision.targetProfile,
    operator: decision.operator,
    selectedOutputKind: decision.selectedOutputKind,
  };
}

function buildContractState(
  kind: PlaybackContractState['kind'],
  contract: NormalizedPlayablePlaybackContract,
  streamUrl: string | null,
): PlaybackContractState {
  return {
    kind,
    requestId: contract.observability.requestId,
    mode: contract.playback.mode,
    streamUrl,
    canSeek: contract.playback.seekable,
    live: contract.playback.live,
    autoplayAllowed: contract.playback.autoplayAllowed,
    sessionRequired: contract.session.required,
    sessionId: contract.session.sessionId,
    expiresAt: contract.session.expiresAt,
    decisionToken: contract.session.decisionToken,
    durationSeconds: contract.media.durationSeconds,
    startUnix: contract.media.startUnix,
    mimeType: contract.media.mimeType,
  };
}

function resolveContractFailureTitle(
  contract: NormalizedBlockedPlaybackContract,
  t: TFunction,
): string {
  switch (contract.failure.kind) {
    case 'auth':
      return t('player.authFailed');
    case 'session':
    case 'unavailable':
      return t('player.notAvailable');
    case 'unsupported':
      return contract.failure.code === 'playback_denied'
        ? t('player.playbackDenied')
        : t('player.serverError');
    case 'contract':
    default:
      return t('player.serverError');
  }
}

function buildBlockedContractError(
  contract: NormalizedBlockedPlaybackContract,
  t: TFunction,
): AppError {
  const title = resolveContractFailureTitle(contract, t);
  const detailParts = [
    contract.failure.message,
    contract.observability.backendReason,
    contract.observability.requestId ? `requestId=${contract.observability.requestId}` : null,
  ].filter(Boolean);

  return {
    title,
    detail: detailParts.length > 0 ? detailParts.join(' · ') : undefined,
    retryable: contract.failure.retryable,
    code: contract.failure.code,
    requestId: contract.observability.requestId ?? undefined,
  };
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
const NATIVE_PLAYER_STATE_IDLE = 1;
const NATIVE_PLAYER_STATE_BUFFERING = 2;
const NATIVE_PLAYER_STATE_READY = 3;
const NATIVE_PLAYER_STATE_ENDED = 4;

function supportsManagedNativePlayback(environment: HostEnvironment): boolean {
  return environment.supportsNativePlayback
    && (environment.platform === 'android' || environment.platform === 'android-tv');
}

function resolveNativePlaybackStatus(state: HostNativePlaybackState | null): PlayerStatus | null {
  if (!state?.activeRequest) {
    if (state?.lastError) {
      return 'error';
    }
    if (state?.playerState === NATIVE_PLAYER_STATE_ENDED) {
      return 'stopped';
    }
    return null;
  }

  if (state.lastError) {
    return 'error';
  }

  switch (state.playerState) {
    case NATIVE_PLAYER_STATE_BUFFERING:
      return state.session ? 'buffering' : 'starting';
    case NATIVE_PLAYER_STATE_READY:
      return state.playWhenReady ? 'playing' : 'paused';
    case NATIVE_PLAYER_STATE_ENDED:
      return 'stopped';
    case NATIVE_PLAYER_STATE_IDLE:
    default:
      return state.session ? 'buffering' : 'starting';
  }
}

function resolveSessionPhaseFromState(state: V3SessionSnapshot['state'] | undefined): SessionPhase | null {
  switch (state) {
    case 'STARTING':
    case 'IDLE':
    case 'PRIMING':
      return 'starting';
    case 'READY':
    case 'DRAINING':
      return 'ready';
    case 'STOPPING':
    case 'STOPPED':
    case 'CANCELLED':
      return 'stopped';
    case 'FAILED':
      return 'error';
    default:
      return null;
  }
}

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
  const playbackEpochRef = useRef(playbackState.epoch.playback);
  const sessionEpochRef = useRef(playbackState.epoch.session);
  const acceptedPlaybackEpochRef = useRef(playbackState.epoch.playback);
  const acceptedSessionEpochRef = useRef(playbackState.epoch.session);

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
  const [startupElapsedSeconds, setStartupElapsedSeconds] = useState(0);
  const [showBufferingOverlay, setShowBufferingOverlay] = useState(false);
  const [showNativeVideo, setShowNativeVideo] = useState(true);
  const [showNativeVideoVeil, setShowNativeVideoVeil] = useState(false);
  const [nativeVeilResumeArmed, setNativeVeilResumeArmed] = useState(false);
  const hostEnvironment = useMemo(() => resolveHostEnvironment(), []);
  const isNativePlaybackHost = supportsManagedNativePlayback(hostEnvironment);
  const [nativePlaybackState, setNativePlaybackState] = useState<HostNativePlaybackState | null>(null);
  const [nativeSessionId, setNativeSessionId] = useState<string | null>(null);

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
  const nativePlaybackWasActiveRef = useRef(false);
  const cleanupPlaybackResourcesRef = useRef<() => void>(() => {});
  const activeLiveSessionIdRef = useRef<string | null>(null);
  const lastFailureTelemetryKeyRef = useRef<string | null>(null);
  const lastAdvisoryTelemetryKeyRef = useRef<string | null>(null);

  const lastDecodedRef = useRef<number>(0);

  // Resume State
  const [resumeState, setResumeState] = useState<ResumeState | null>(null);
  const [showResumeOverlay, setShowResumeOverlay] = useState(false);
  const [isDocumentVisible, setIsDocumentVisible] = useState(
    () => typeof document === 'undefined' || document.visibilityState !== 'hidden'
  );

  useEffect(() => {
    playbackStateRef.current = playbackState;
    acceptedPlaybackEpochRef.current = playbackState.epoch.playback;
    acceptedSessionEpochRef.current = playbackState.epoch.session;
  }, [playbackState]);

  const allocatePlaybackEpoch = useCallback(() => {
    playbackEpochRef.current += 1;
    sessionEpochRef.current = 0;
    return playbackEpochRef.current;
  }, []);

  const beginPlaybackAttempt = useCallback((
    epoch: number,
    nextPlaybackMode: 'LIVE' | 'VOD' | 'UNKNOWN',
    nextStatus: PlayerStatus,
  ) => {
    acceptedPlaybackEpochRef.current = epoch;
    acceptedSessionEpochRef.current = 0;
    dispatchPlayback({
      type: 'normative.playback.attempt.started',
      epoch,
      playbackMode: nextPlaybackMode,
      status: nextStatus,
      requestedDuration,
    });
  }, [requestedDuration]);

  const markPlaybackStopped = useCallback((epoch: number) => {
    acceptedPlaybackEpochRef.current = epoch;
    acceptedSessionEpochRef.current = 0;
    dispatchPlayback({
      type: 'normative.playback.stopped',
      epoch,
    });
  }, []);

  const allocateSessionEpoch = useCallback((playbackEpoch: number) => {
    sessionEpochRef.current += 1;
    const sessionEpoch = sessionEpochRef.current;
    acceptedSessionEpochRef.current = sessionEpoch;
    dispatchPlayback({
      type: 'normative.session.attempt.started',
      playbackEpoch,
      sessionEpoch,
    });
    return sessionEpoch;
  }, []);

  const isStalePlaybackEpoch = useCallback((epoch: number) => (
    epoch !== playbackEpochRef.current
  ), []);

  const isStaleSessionEpoch = useCallback((playbackEpoch: number, sessionEpoch: number) => (
    playbackEpoch !== playbackEpochRef.current || sessionEpoch !== sessionEpochRef.current
  ), []);

  const setTraceId = useCallback<Dispatch<SetStateAction<string>>>((next) => {
    const currentTraceId = playbackStateRef.current.traceId;
    const resolvedTraceId = typeof next === 'function' ? next(currentTraceId) : next;
    dispatchPlayback({
      type: 'normative.playback.trace.updated',
      epoch: acceptedPlaybackEpochRef.current,
      traceId: resolvedTraceId,
    });
  }, []);

  const setStatus = useCallback<Dispatch<SetStateAction<PlayerStatus>>>((next) => {
    const currentStatus = playbackStateRef.current.status;
    const resolvedStatus = typeof next === 'function' ? next(currentStatus) : next;
    dispatchPlayback({
      type: 'normative.media.status.changed',
      epoch: acceptedPlaybackEpochRef.current,
      status: resolvedStatus,
    });
  }, []);

  const setPlaybackMode = useCallback<Dispatch<SetStateAction<'LIVE' | 'VOD' | 'UNKNOWN'>>>((next) => {
    const currentMode = playbackStateRef.current.playbackMode;
    const resolvedMode = typeof next === 'function' ? next(currentMode) : next;
    dispatchPlayback({
      type: 'normative.playback.mode.changed',
      epoch: acceptedPlaybackEpochRef.current,
      playbackMode: resolvedMode,
    });
  }, []);

  const setDurationSeconds = useCallback<Dispatch<SetStateAction<number | null>>>((next) => {
    const currentDurationSeconds = playbackStateRef.current.durationSeconds;
    const resolvedDurationSeconds = typeof next === 'function' ? next(currentDurationSeconds) : next;
    dispatchPlayback({
      type: 'normative.playback.duration.changed',
      epoch: acceptedPlaybackEpochRef.current,
      durationSeconds: resolvedDurationSeconds,
    });
  }, []);

  const setVodStreamMode = useCallback<Dispatch<SetStateAction<'direct_mp4' | 'native_hls' | 'hlsjs' | 'transcode' | null>>>((next) => {
    const currentMode = playbackStateRef.current.vodStreamMode;
    const resolvedMode = typeof next === 'function' ? next(currentMode) : next;
    dispatchPlayback({
      type: 'normative.playback.vod_mode.changed',
      epoch: acceptedPlaybackEpochRef.current,
      vodStreamMode: resolvedMode,
    });
  }, []);

  const setActiveHlsEngine = useCallback<Dispatch<SetStateAction<'native' | 'hlsjs' | null>>>((next) => {
    const currentEngine = playbackStateRef.current.activeHlsEngine;
    const resolvedEngine = typeof next === 'function' ? next(currentEngine) : next;
    dispatchPlayback({
      type: 'normative.media.engine.selected',
      epoch: acceptedPlaybackEpochRef.current,
      engine: resolvedEngine,
    });
  }, []);

  const setCanSeek = useCallback<Dispatch<SetStateAction<boolean>>>((next) => {
    const currentCanSeek = playbackStateRef.current.canSeek;
    const resolvedCanSeek = typeof next === 'function' ? next(currentCanSeek) : next;
    dispatchPlayback({
      type: 'normative.playback.seekability.changed',
      epoch: acceptedPlaybackEpochRef.current,
      canSeek: resolvedCanSeek,
    });
  }, []);

  const setStartUnix = useCallback<Dispatch<SetStateAction<number | null>>>((next) => {
    const currentStartUnix = playbackStateRef.current.startUnix;
    const resolvedStartUnix = typeof next === 'function' ? next(currentStartUnix) : next;
    dispatchPlayback({
      type: 'normative.playback.start_unix.changed',
      epoch: acceptedPlaybackEpochRef.current,
      startUnix: resolvedStartUnix,
    });
  }, []);

  const setPlayerError = useCallback((
    nextError: AppError | null,
    options: PlaybackFailureReportOptions & {
      messageKey?: string | null;
      playerStatus?: PlayerStatus;
    } = {},
  ) => {
    if (!nextError) {
      dispatchPlayback({ type: 'normative.playback.failure.cleared' });
      return;
    }
    dispatchPlayback({
      type: 'normative.playback.failure.raised',
      epoch: acceptedPlaybackEpochRef.current,
      status: options.playerStatus,
      failure: buildPlaybackFailure(nextError, options.source ?? 'orchestrator', {
        class: options.failureClass,
        code: options.code ?? nextError.code ?? undefined,
        message: nextError.title,
        terminal: options.terminal,
        retryable: options.retryable,
        recoverable: options.recoverable,
        userVisible: options.userVisible,
        policyImpact: options.policyImpact,
        messageKey: options.messageKey,
        telemetryContext: options.telemetryContext,
        telemetryReason: options.telemetryReason,
      }),
    });
  }, []);

  const reportPlaybackFailure = useCallback((
    nextError: AppError,
    options: PlaybackFailureReportOptions = {},
  ) => {
    setShowErrorDetails(false);
    setPlayerError(nextError, options);
  }, [setPlayerError]);

  const clearPlaybackFailure = useCallback(() => {
    dispatchPlayback({ type: 'normative.playback.failure.cleared' });
    setShowErrorDetails(false);
  }, []);

  const clearPlayerError = useCallback(() => {
    clearPlaybackFailure();
  }, [clearPlaybackFailure]);

  const recordContractAdvisories = useCallback((epoch: number, warnings: Parameters<typeof buildPlaybackAdvisorySignal>[0][]) => {
    warnings.forEach((warning) => {
      dispatchPlayback({
        type: 'advisory.signal.recorded',
        epoch,
        advisory: buildPlaybackAdvisorySignal(warning),
      });
    });
  }, []);

  useEffect(() => {
    if (!error?.detail) {
      setShowErrorDetails(false);
    }
  }, [error?.detail]);

  useEffect(() => {
    if (!failure) {
      lastFailureTelemetryKeyRef.current = null;
      return;
    }

    const telemetryKey = [
      playbackState.epoch.playback,
      failure.class,
      failure.code,
      failure.status ?? '-',
      failure.telemetryContext ?? '-',
      failure.appError?.requestId ?? '-',
    ].join(':');

    if (lastFailureTelemetryKeyRef.current === telemetryKey) {
      return;
    }

    lastFailureTelemetryKeyRef.current = telemetryKey;
    mapPlaybackFailureToTelemetryEvents(failure).forEach((event) => {
      telemetry.emit(event.type, event.payload);
    });
  }, [failure, playbackState.epoch.playback]);

  useEffect(() => {
    if (!lastAdvisory) {
      lastAdvisoryTelemetryKeyRef.current = null;
      return;
    }

    const telemetryKey = [
      playbackState.epoch.playback,
      lastAdvisory.code,
      lastAdvisory.source,
    ].join(':');

    if (lastAdvisoryTelemetryKeyRef.current === telemetryKey) {
      return;
    }

    lastAdvisoryTelemetryKeyRef.current = telemetryKey;
    mapPlaybackAdvisoryToTelemetryEvents(lastAdvisory).forEach((event) => {
      telemetry.emit(event.type, event.payload);
    });
  }, [lastAdvisory, playbackState.epoch.playback]);

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
  }, [mergeSessionPlaybackTrace, setTraceId]);

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
    playDirectMp4
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
    clearPlaybackFailure,
    reportPlaybackFailure
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
    nativePlaybackWasActiveRef.current = false;
    nativeVideoShownRef.current = false;
    nativeVideoHoldPositionRef.current = null;
    clearNativeVideoVeilTimers();
    setNativePlaybackState(null);
    setNativeSessionId(null);
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

  const beginNativePlayback = useCallback((request: NativePlaybackRequest): void => {
    const started = startNativePlayback(request);
    if (!started) {
      throw new Error('Native playback bridge unavailable');
    }

    nativePlaybackWasActiveRef.current = true;
    setNativePlaybackState({
      activeRequest: request,
      session: null,
      playerState: NATIVE_PLAYER_STATE_IDLE,
      playWhenReady: true,
      isInPip: false,
      lastError: null,
    });
    clearPlayerError();
    setActiveHlsEngine(null);
    if (request.kind === 'recording') {
      activeRecordingRef.current = request.recordingId;
      setActiveRecordingId(request.recordingId);
      setPlaybackMode('VOD');
    } else {
      activeRecordingRef.current = null;
      setActiveRecordingId(null);
      setPlaybackMode('LIVE');
    }
    setStatus('starting');
  }, [clearPlayerError, setPlaybackMode, setStatus]);

  const syncNativePlaybackState = useCallback((nextState: HostNativePlaybackState | null) => {
    if (!isNativePlaybackHost) {
      return;
    }

    setNativePlaybackState(nextState);

    const hadActiveNativePlayback = nativePlaybackWasActiveRef.current;
    const activeRequest = nextState?.activeRequest ?? null;
    const hasActiveNativePlayback = nextState != null && activeRequest != null;
    nativePlaybackWasActiveRef.current = hasActiveNativePlayback;

    if (!hasActiveNativePlayback) {
      if (hadActiveNativePlayback) {
        activeRecordingRef.current = null;
        setNativeSessionId(null);
        setActiveRecordingId(null);
        setActiveHlsEngine(null);
        setPlaybackMode('UNKNOWN');
        if (nextState?.lastError) {
          reportPlaybackFailure({
            title: nextState.lastError,
            retryable: true,
            code: 'NATIVE_HOST_ERROR',
          }, {
            source: 'native-host',
            failureClass: 'media',
            retryable: true,
            recoverable: true,
            terminal: false,
          });
          setStatus('error');
        } else {
          setStatus('stopped');
        }
      }
      return;
    }

    const resolvedState = nextState;
    const diagnostics = resolvedState.diagnostics ?? null;
    const nextNativeSessionId =
      resolvedState.session?.sessionId ??
      (typeof diagnostics?.trace?.sessionId === 'string' ? diagnostics.trace.sessionId : null) ??
      (resolvedState.session?.trace && typeof resolvedState.session.trace === 'object' && typeof resolvedState.session.trace.sessionId === 'string'
        ? resolvedState.session.trace.sessionId
        : null);
    setNativeSessionId(nextNativeSessionId);

    const nextTraceId = diagnostics?.requestId ?? resolvedState.session?.requestId ?? null;
    if (nextTraceId) {
      setTraceId(nextTraceId);
    }

    const nextProfileReason = resolvedState.session?.profileReason ?? diagnostics?.profileReason ?? null;
    setSessionProfileReason(nextProfileReason);

    if (diagnostics?.playbackInfo) {
      const diagnosticsContract = normalizePlaybackInfo(diagnostics.playbackInfo, {
        surface: nextNativeSessionId ? 'live' : 'recording',
        preferredHlsEngine: resolvePreferredHlsEngine(),
      });
      setPlaybackObservability(resolvePlaybackObservability(
        diagnosticsContract.observability.decision,
        typeof diagnostics.trace?.clientPath === 'string' ? diagnostics.trace.clientPath : 'android/native'
      ));
    }

    mergeSessionPlaybackTrace(extractPlaybackTrace(diagnostics?.trace));
    mergeSessionPlaybackTrace(extractPlaybackTrace(resolvedState.session?.trace));

    if (resolvedState.lastError) {
      reportPlaybackFailure({
        title: resolvedState.lastError,
        retryable: true,
        code: 'NATIVE_HOST_ERROR',
      }, {
        source: 'native-host',
        failureClass: 'media',
        retryable: true,
        recoverable: true,
        terminal: false,
      });
    } else {
      clearPlayerError();
    }

    if (activeRequest.kind === 'recording') {
      activeRecordingRef.current = activeRequest.recordingId;
      setActiveRecordingId(activeRequest.recordingId);
      setPlaybackMode('VOD');
    } else {
      activeRecordingRef.current = null;
      setActiveRecordingId(null);
      setPlaybackMode('LIVE');
    }
    setActiveHlsEngine(null);

    const mappedStatus = resolveNativePlaybackStatus(nextState);
    if (mappedStatus) {
      setStatus(mappedStatus);
    }
  }, [clearPlayerError, isNativePlaybackHost, resolvePreferredHlsEngine, setPlaybackMode, setPlayerError, setStatus]);

  const gatherPlaybackCapabilitiesForPlayer = useCallback(async (scope: 'live' | 'recording' = 'live'): Promise<CapabilitySnapshot> => {
    const video = videoRef.current as HTMLVideoElement | null;
    return gatherPlaybackCapabilities(scope, video);
  }, []);

  const startRecordingPlayback = useCallback(async (id: string): Promise<void> => {
    const playbackEpoch = allocatePlaybackEpoch();
    if (hasActivePlayback()) {
      await teardownActivePlayback();
    } else {
      clearPlaybackState();
    }
    beginPlaybackAttempt(playbackEpoch, 'VOD', 'building');
    activeRecordingRef.current = id;
    setActiveRecordingId(id);
    clearPlayerError();

    let abortController: AbortController | null = null;
    let requestCaps: CapabilitySnapshot | null = null;

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
            if (notifyAuthRequiredIfUnauthorizedResponse(response, 'V3Player.recordingPlaybackInfo')) {
              setStatus('error');
              reportPlaybackFailure({
                title: t('player.authFailed'),
                status: 401,
                retryable: false,
                code: 'AUTH_DENIED',
              }, {
                source: 'backend',
                failureClass: 'auth',
                retryable: false,
                recoverable: false,
                terminal: true,
              });
              return;
            }
            if (response.status === 403) {
              setStatus('error');
              reportPlaybackFailure({
                title: t('player.forbidden'),
                status: 403,
                retryable: false,
                code: 'AUTH_DENIED',
              }, {
                source: 'backend',
                failureClass: 'auth',
                retryable: false,
                recoverable: false,
                terminal: true,
              });
              return;
            }
            if (response.status === 410) {
              setStatus('error');
              reportPlaybackFailure({
                title: t('player.notAvailable'),
                status: 410,
                retryable: false,
                code: 'RECORDING_GONE',
              }, {
                source: 'backend',
                failureClass: 'contract',
                retryable: false,
                recoverable: false,
                terminal: true,
                telemetryContext: 'V3Player.recording.contract.blocked',
              });
              return;
            }
            if (response.status === 409) {
              const retryAfterHeader = response.headers.get('Retry-After');
              const retryAfter = retryAfterHeader ? parseInt(retryAfterHeader, 10) : 0;
              const retryHint = retryAfter > 0 ? ` ${t('player.retryAfter', { seconds: retryAfter })}` : '';
              setStatus('error');
              reportPlaybackFailure({
                title: `${t('player.leaseBusy')}${retryHint}`,
                status: 409,
                retryable: true,
                code: 'LEASE_BUSY',
              }, {
                source: 'backend',
                failureClass: 'contract',
                retryable: true,
                recoverable: false,
                terminal: false,
                telemetryContext: 'V3Player.recording.contract.blocked',
              });
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

        telemetry.emit('ui.contract.consumed', {
          mode: 'normalized',
          kind: normalizedContract.kind,
          fields: normalizedContract.kind === 'playable'
            ? ['kind', 'playback.mode', 'playback.outputUrl', 'playback.seekable']
            : ['kind', 'failure.kind', 'failure.code'],
        });

        if (normalizedContract.observability.requestId) {
          setTraceId(normalizedContract.observability.requestId);
        }
        setPlaybackObservability(resolvePlaybackObservability(
          normalizedContract.observability.decision,
          requestCaps.preferredHlsEngine ?? null
        ));

        if (normalizedContract.kind === 'blocked') {
          const blockedFailure = classifyNormalizedContractFailure(normalizedContract.failure);
          setStatus('error');
          reportPlaybackFailure(buildBlockedContractError(normalizedContract, t), {
            source: 'backend',
            failureClass: blockedFailure.class,
            code: blockedFailure.code,
            retryable: blockedFailure.retryable,
            recoverable: blockedFailure.recoverable,
            terminal: blockedFailure.terminal,
            policyImpact: blockedFailure.policyImpact,
            telemetryContext: 'V3Player.recording.contract.blocked',
            telemetryReason: normalizedContract.observability.backendReason ?? normalizedContract.failure.code,
          });
          return;
        }

        mode = normalizedContract.playback.mode;
        streamUrl = normalizedContract.playback.outputUrl ?? '';
        if (!streamUrl) {
          setStatus('error');
          reportPlaybackFailure({
            title: t('player.serverError'),
            detail: 'Normalized recording contract missing outputUrl',
            retryable: false,
            code: 'MISSING_OUTPUT_URL',
          }, {
            source: 'backend',
            failureClass: 'contract',
            retryable: false,
            recoverable: false,
            terminal: true,
            telemetryContext: 'V3Player.recording.output_url.missing',
          });
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

        const recordingIsSeekable = normalizedContract.playback.seekable;
        setCanSeek(recordingIsSeekable);
        if (normalizedContract.media.startUnix) setStartUnix(normalizedContract.media.startUnix);

        // Resume State
        if (
          recordingIsSeekable &&
          normalizedContract.resume &&
          normalizedContract.resume.posSeconds >= 15 &&
          !normalizedContract.resume.finished
        ) {
          const d = normalizedContract.resume.durationSeconds || (playbackDurationSeconds || 0);
          if (!d || normalizedContract.resume.posSeconds < d - 10) {
            setResumeState({
              posSeconds: normalizedContract.resume.posSeconds,
              durationSeconds: normalizedContract.resume.durationSeconds || undefined,
              finished: normalizedContract.resume.finished || undefined
            });
            setShowResumeOverlay(true);
          }
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
          if (hasActivePlayback() || nativePlaybackState?.activeRequest) {
            await teardownActivePlayback();
          } else {
            clearPlaybackState();
          }
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
        if (hasActivePlayback()) {
          await teardownActivePlayback();
        } else {
          clearPlaybackState();
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
        reportPlaybackFailure({
          title: t('player.serviceRefRequired'),
          retryable: false,
          code: 'SERVICE_REF_REQUIRED',
        }, {
          source: 'orchestrator',
          failureClass: 'contract',
          retryable: false,
          recoverable: false,
          terminal: true,
        });
        return;
      }
      const playbackEpoch = allocatePlaybackEpoch();
      if (hasActivePlayback()) {
        await teardownActivePlayback();
      } else {
        clearPlaybackState();
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

        telemetry.emit('ui.contract.consumed', {
          mode: 'normalized',
          kind: normalizedContract.kind,
          fields: normalizedContract.kind === 'playable'
            ? ['kind', 'playback.mode', 'session.decisionToken']
            : ['kind', 'failure.kind', 'failure.code'],
        });

        if (normalizedContract.observability.requestId) {
          setTraceId(normalizedContract.observability.requestId);
        }
        setPlaybackObservability(resolvePlaybackObservability(
          normalizedContract.observability.decision,
          requestCaps.preferredHlsEngine ?? null
        ));

        if (normalizedContract.kind === 'blocked') {
          const blockedFailure = classifyNormalizedContractFailure(normalizedContract.failure);
          setStatus('error');
          reportPlaybackFailure(buildBlockedContractError(normalizedContract, t), {
            source: 'backend',
            failureClass: blockedFailure.class,
            code: blockedFailure.code,
            retryable: blockedFailure.retryable,
            recoverable: blockedFailure.recoverable,
            terminal: blockedFailure.terminal,
            policyImpact: blockedFailure.policyImpact,
            telemetryContext: 'V3Player.live.contract.blocked',
            telemetryReason: normalizedContract.observability.backendReason ?? normalizedContract.failure.code,
          });
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
          reportPlaybackFailure({
            title: t('player.serverError'),
            detail: 'Backend live decision missing playbackDecisionToken',
            retryable: false,
            code: 'PLAYBACK_DECISION_TOKEN_MISSING',
          }, {
            source: 'backend',
            failureClass: 'contract',
            retryable: false,
            recoverable: false,
            terminal: true,
            telemetryContext: 'V3Player.live.playback_decision_token.missing',
          });
          return;
        }

        if (liveMode === 'native_hls') {
          liveEngine = 'native';
        } else if (liveMode === 'hlsjs') {
          liveEngine = 'hlsjs';
        } else if (liveMode === 'transcode') {
          liveEngine = resolvePreferredHlsEngineForCapabilities(requestCaps);
        } else {
          setStatus('error');
          reportPlaybackFailure({
            title: t('player.serverError'),
            detail: `Unsupported live playback mode: ${liveMode}`,
            retryable: false,
            code: 'UNSUPPORTED_LIVE_MODE',
          }, {
            source: 'backend',
            failureClass: 'contract',
            retryable: false,
            recoverable: false,
            terminal: true,
            telemetryContext: 'V3Player.live.mode.unsupported',
          });
          return;
        }

        const intentParams: Record<string, string> = {
          playback_mode: liveMode,
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

        const intentBody: IntentRequest = {
          type: 'stream.start',
          serviceRef: ref,
          playbackDecisionToken: liveDecisionToken,
          client: requestCaps,
          ...(Object.keys(intentParams).length > 0 ? { params: intentParams } : {})
        };
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

        const data: V3SessionResponse = await res.json();
        newSessionId = data.sessionId ?? null;
        if (!newSessionId) {
          throw new Error('Intent response missing sessionId');
        }
        if (data.requestId) setTraceId(data.requestId);
        setActiveSessionId(newSessionId);
        dispatchPlayback({
          type: 'normative.session.phase.changed',
          playbackEpoch,
          sessionEpoch,
          phase: 'starting',
          requestId: data.requestId ?? null,
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
          requestId: session.requestId ?? data.requestId ?? null,
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
    if (!isNativePlaybackHost) {
      setNativePlaybackState(null);
      nativePlaybackWasActiveRef.current = false;
      return;
    }

    syncNativePlaybackState(getNativePlaybackState());
    const unsubscribe = onNativePlaybackState((nextState) => {
      syncNativePlaybackState(nextState);
    });

    return () => {
      unsubscribe();
    };
  }, [isNativePlaybackHost, syncNativePlaybackState]);

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

  useEffect(() => {
    return () => {
      cleanupPlaybackResourcesRef.current();
    };
  }, []);

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
