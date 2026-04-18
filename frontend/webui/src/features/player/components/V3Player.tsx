import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import type { Dispatch, KeyboardEvent, PointerEvent, SetStateAction } from 'react';
import { useTranslation } from 'react-i18next';
import Hls from '../lib/hlsRuntime';
import {
  postRecordingPlaybackInfo,
  type IntentRequest,
  type PlaybackInfo,
  type PlaybackSourceProfile,
  type PlaybackTrace as PlaybackTraceContract,
  type PlaybackTraceFfmpegPlan,
  type PlaybackTraceOperator,
  type PlaybackTraceRuntimeReplay,
  type PlaybackTraceRuntimeTick,
  type PlaybackTargetProfile,
} from '../../../client-ts';
import { getApiBaseUrl } from '../../../services/clientWrapper';
import { telemetry } from '../../../services/TelemetryService';
import type {
  V3PlayerProps,
  PlayerStatus,
  V3SessionResponse,
  V3SessionSnapshot,
  HlsInstanceRef,
  VideoElementRef
} from '../../../types/v3-player';
import { useLiveSessionController } from '../useLiveSessionController';
import { usePlaybackEngine } from '../usePlaybackEngine';
import { usePlayerChrome } from '../usePlayerChrome';
import {
  resolveRuntimePolicyErrorSupport,
  resolveRuntimePolicyStartupSupport,
  resolveStartupOverlayLabel,
  resolveStartupOverlaySupport,
} from '../startupOverlayLabel';
import { PlayerErrorSurface } from './PlayerErrorSurface';
import { PlayerRuntimeMeta, PlayerRuntimeMetaPanel } from './PlayerRuntimeMeta';
import { PlayerRuntimeReplayExport } from './PlayerRuntimeReplayExport';
import { PlayerStartupSurface } from './PlayerStartupSurface';
import { useResume } from '../../resume/useResume';
import { ResumeState } from '../../resume/api';
import { Button } from '../../../components/ui';
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
import { gatherPlaybackCapabilities, type CapabilitySnapshot } from '../utils/playbackCapabilities';
import {
  buildPlaybackProfileHeaders,
  gatherPlaybackClientContext,
  resolvePlaybackRequestProfile,
} from '../utils/playbackRequestProfile';
import { normalizePlayerError } from '../../../lib/appErrors';
import { notifyAuthRequiredIfUnauthorizedResponse } from '../../../lib/httpProblem';
import { useTvInitialFocus } from '../../../hooks/useTvInitialFocus';
import {
  getNativePlaybackState,
  onNativePlaybackState,
  requestHostInputFocus,
  resolveHostEnvironment,
  setHostPlaybackActive,
  startNativePlayback,
  stopNativePlayback,
} from '../../../lib/hostBridge';
import type {
  HostEnvironment,
  NativePlaybackRequest,
  NativePlaybackState as HostNativePlaybackState,
} from '../../../lib/hostBridge';
import type { AppError } from '../../../types/errors';
import styles from './V3Player.module.css';

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

type PlaybackWindowKind = 'live' | 'live-dvr' | 'vod' | 'unknown';

function PlayGlyph() {
  return (
    <svg className={[styles.controlIcon, styles.playPauseIcon].join(' ')} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="M8 6.5v11l9-5.5-9-5.5Z" fill="currentColor" />
    </svg>
  );
}

function PauseGlyph() {
  return (
    <svg className={[styles.controlIcon, styles.playPauseIcon].join(' ')} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="M7 6h3.5v12H7V6Zm6.5 0H17v12h-3.5V6Z" fill="currentColor" />
    </svg>
  );
}

function VolumeGlyph({ muted }: { muted: boolean }) {
  return muted ? (
    <svg className={styles.controlIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="M10 8.25 6.9 11H4v2h2.9L10 15.75V8.25Z" fill="currentColor" />
      <path d="m14.25 9.25 5.5 5.5M19.75 9.25l-5.5 5.5" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  ) : (
    <svg className={styles.controlIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="M10 8.25 6.9 11H4v2h2.9L10 15.75V8.25Z" fill="currentColor" />
      <path d="M14.5 9.3a4.4 4.4 0 0 1 0 5.4M17.2 7a7.6 7.6 0 0 1 0 10" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  );
}

function FullscreenGlyph() {
  return (
    <svg className={styles.controlIcon} viewBox="0 0 24 24" aria-hidden="true" focusable="false">
      <path d="M8 4H4v4M16 4h4v4M8 20H4v-4M20 20h-4v-4" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

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

function resolvePlaybackWindowKind(
  playbackMode: 'LIVE' | 'VOD' | 'UNKNOWN',
  hasLiveDvrWindow: boolean
): PlaybackWindowKind {
  if (playbackMode === 'LIVE') {
    return hasLiveDvrWindow ? 'live-dvr' : 'live';
  }
  if (playbackMode === 'VOD') {
    return 'vod';
  }
  return 'unknown';
}

function normalizePlaybackWindowKind(value: string | null | undefined): PlaybackWindowKind {
  switch (value) {
    case 'live':
    case 'live-dvr':
    case 'vod':
      return value;
    default:
      return 'unknown';
  }
}

function mergePlaybackTraceOperator(
  primary: PlaybackTraceOperator | null | undefined,
  fallback: PlaybackTraceOperator | null | undefined
): PlaybackTraceOperator | null {
  if (!primary && !fallback) {
    return null;
  }

  return {
    forcedIntent: primary?.forcedIntent ?? fallback?.forcedIntent ?? null,
    maxQualityRung: primary?.maxQualityRung ?? fallback?.maxQualityRung ?? null,
    runtimePolicyAction: primary?.runtimePolicyAction ?? fallback?.runtimePolicyAction ?? null,
    runtimePolicyPhase: primary?.runtimePolicyPhase ?? fallback?.runtimePolicyPhase ?? null,
    runtimePolicyConstraints: primary?.runtimePolicyConstraints ?? fallback?.runtimePolicyConstraints ?? null,
    runtimePolicyReplay: primary?.runtimePolicyReplay ?? fallback?.runtimePolicyReplay ?? null,
    runtimePolicyReasons: primary?.runtimePolicyReasons ?? fallback?.runtimePolicyReasons ?? null,
    runtimePolicyTimeline: primary?.runtimePolicyTimeline ?? fallback?.runtimePolicyTimeline ?? null,
    runtimeProbeCandidate: primary?.runtimeProbeCandidate ?? fallback?.runtimeProbeCandidate ?? null,
    runtimeProbeFailureStreak: primary?.runtimeProbeFailureStreak ?? fallback?.runtimeProbeFailureStreak ?? null,
    runtimeProbeSuccessStreak: primary?.runtimeProbeSuccessStreak ?? fallback?.runtimeProbeSuccessStreak ?? null,
    ruleName: primary?.ruleName ?? fallback?.ruleName ?? null,
    ruleScope: primary?.ruleScope ?? fallback?.ruleScope ?? null,
    clientFallbackDisabled: primary?.clientFallbackDisabled ?? fallback?.clientFallbackDisabled,
    overrideApplied: primary?.overrideApplied ?? fallback?.overrideApplied,
  };
}

function formatRuntimePolicyPhaseLabel(value: string | null | undefined): string {
  switch (value) {
    case 'probing':
      return 'Probing';
    case 'cooldown':
      return 'Cooldown';
    case 'probe_regressed':
      return 'Probe regressed';
    case 'recovering':
      return 'Recovering';
    case 'degraded':
      return 'Degraded';
    case 'stable':
      return 'Stable';
    default:
      return '-';
  }
}

function resolveRuntimePolicyPhaseState(value: string | null | undefined) {
  switch (value) {
    case 'probing':
      return 'pending' as const;
    case 'cooldown':
    case 'recovering':
      return 'warning' as const;
    case 'probe_regressed':
    case 'degraded':
      return 'error' as const;
    case 'stable':
      return 'success' as const;
    default:
      return 'idle' as const;
  }
}

function formatRuntimeTimelineTime(value: string | null | undefined): string {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '-';
  }
  return date.toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

function humanizeRuntimeToken(value: string | null | undefined): string {
  if (!value) return '-';
  return value.replace(/_/g, ' ');
}

function formatRuntimeTimelineEntry(entry: PlaybackTraceRuntimeTick): string {
  const parts = [
    formatRuntimeTimelineTime(entry.tickAt),
    humanizeRuntimeToken(entry.confidenceState),
    humanizeRuntimeToken(entry.policyAction),
    entry.plannedTransition ? `plan:${humanizeRuntimeToken(entry.plannedTransition)}` : null,
    entry.executedTransition ? `run:${humanizeRuntimeToken(entry.executedTransition)}` : null,
    entry.activeStep ? `step:${humanizeRuntimeToken(entry.activeStep)}` : null,
    entry.probeState ? `probe:${humanizeRuntimeToken(entry.probeState)}` : null,
    entry.blockers?.length ? `block:${entry.blockers.join('/')}` : null,
  ].filter(Boolean);
  return parts.join(' · ');
}

function resolveRuntimePolicyMetaHint(
  phase: string | null | undefined,
  probeCandidate: string | null | undefined,
  maxQualityRung: string | null | undefined
): string | null {
  switch (phase) {
    case 'probing':
      return probeCandidate ?? null;
    case 'cooldown':
      return maxQualityRung ?? null;
    default:
      return null;
  }
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
    targetProfileHash: decision.trace?.targetProfileHash ?? null,
    targetProfile: decision.trace?.targetProfile ?? null,
    operator: decision.trace?.operator ?? null,
    selectedOutputKind: decision.selectedOutputKind ?? null,
  };
}

function resolvePlaybackDurationSeconds(playbackInfo: PlaybackInfo): number | null {
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

function V3Player(props: V3PlayerProps) {
  const { t } = useTranslation();
  const { token, autoStart, onClose, duration, startPositionSeconds, suppressResumePrompt } = props;
  const channel = 'channel' in props ? props.channel : undefined;
  const src = 'src' in props ? props.src : undefined;
  const recordingId = 'recordingId' in props ? props.recordingId : undefined;
  const recordingTitle = 'recordingTitle' in props ? props.recordingTitle : undefined;
  const recordingDescription = 'recordingDescription' in props ? props.recordingDescription : undefined;
  const recordingDateLabel = 'recordingDateLabel' in props ? props.recordingDateLabel : undefined;
  const recordingLengthLabel = 'recordingLengthLabel' in props ? props.recordingLengthLabel : undefined;
  const recordingLayoutMode = 'layoutMode' in props ? props.layoutMode : undefined;
  const normalizedRecordingTitle = typeof recordingTitle === 'string' ? recordingTitle.trim() : '';
  const isRecordingPageLayout = Boolean(recordingId && recordingLayoutMode === 'page');
  const useOverlayShell = Boolean(onClose && !isRecordingPageLayout);

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
  const isNativePlaybackHost = supportsManagedNativePlayback(hostEnvironment);
  const [nativePlaybackState, setNativePlaybackState] = useState<HostNativePlaybackState | null>(null);
  const [nativeSessionId, setNativeSessionId] = useState<string | null>(null);

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
  const nativePlaybackWasActiveRef = useRef(false);
  const cleanupPlaybackResourcesRef = useRef<() => void>(() => {});

  const [durationSeconds, setDurationSeconds] = useState<number | null>(
    duration && duration > 0 ? duration : null
  );
  const [playbackMode, setPlaybackMode] = useState<'LIVE' | 'VOD' | 'UNKNOWN'>('UNKNOWN');
  const [sessionWindowKind, setSessionWindowKind] = useState<PlaybackWindowKind>('unknown');
  const [vodStreamMode, setVodStreamMode] = useState<'direct_mp4' | 'native_hls' | 'hlsjs' | 'transcode' | null>(null);
  const [activeHlsEngine, setActiveHlsEngine] = useState<'native' | 'hlsjs' | null>(null);
  const [liveSeekWindow, setLiveSeekWindow] = useState<{ start: number; end: number; liveEdge: number | null } | null>(null);

  // P3-4: Truth State
  const [canSeek, setCanSeek] = useState(true);
  const [startUnix, setStartUnix] = useState<number | null>(null);

  const lastDecodedRef = useRef<number>(0);

  // Resume State
  const [resumeState, setResumeState] = useState<ResumeState | null>(null);
  const [showResumeOverlay, setShowResumeOverlay] = useState(false);
  const resumePrimaryActionRef = useRef<HTMLButtonElement>(null);
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

  const handleSessionSnapshot = useCallback((session: V3SessionSnapshot) => {
    if (session.requestId) {
      setTraceId(session.requestId);
    }
    setSessionProfileReason(session.profileReason ?? null);
    mergeSessionPlaybackTrace(extractPlaybackTrace(session));
    const snapshotWindowKind = normalizePlaybackWindowKind(session.windowKind);
    if (session.mode === 'LIVE') {
      setSessionWindowKind(snapshotWindowKind);
      const seekableStart = typeof session.seekableStartSeconds === 'number' ? session.seekableStartSeconds : null;
      const seekableEnd = typeof session.seekableEndSeconds === 'number' ? session.seekableEndSeconds : null;
      const liveEdge = typeof session.liveEdgeSeconds === 'number' ? session.liveEdgeSeconds : null;
      if (seekableStart !== null && seekableEnd !== null && seekableEnd > seekableStart) {
        setLiveSeekWindow({
          start: Math.max(0, seekableStart),
          end: Math.max(seekableStart, seekableEnd),
          liveEdge: liveEdge !== null ? Math.max(seekableEnd, liveEdge) : seekableEnd,
        });
      } else if (typeof session.durationSeconds === 'number' && session.durationSeconds > 0) {
        const derivedEnd = liveEdge ?? seekableEnd ?? session.durationSeconds;
        const derivedStart = Math.max(0, derivedEnd - session.durationSeconds);
        setLiveSeekWindow({
          start: derivedStart,
          end: Math.max(derivedStart, derivedEnd),
          liveEdge: liveEdge ?? derivedEnd,
        });
      } else {
        setLiveSeekWindow(null);
      }
    } else if (session.mode === 'RECORDING') {
      setSessionWindowKind(snapshotWindowKind !== 'unknown' ? snapshotWindowKind : 'vod');
      setLiveSeekWindow(null);
    } else {
      setSessionWindowKind(snapshotWindowKind);
    }
  }, [mergeSessionPlaybackTrace]);

  // Explicitly static/memoized apiBase
  const apiBase = useMemo(() => {
    return getApiBaseUrl();
  }, []);
  const requestedDuration = useMemo(() => (duration && duration > 0 ? duration : null), [duration]);
  const requestedStartPositionSeconds = useMemo(
    () => (startPositionSeconds && Number.isFinite(startPositionSeconds) && startPositionSeconds > 0
      ? startPositionSeconds
      : 0),
    [startPositionSeconds]
  );
  const isCompactTouchLayout = useMemo(() => hasTouchInput(), []);
  const mergedPlaybackOperator = useMemo(
    () => mergePlaybackTraceOperator(sessionPlaybackTrace?.operator, playbackObservability?.operator),
    [playbackObservability?.operator, sessionPlaybackTrace?.operator]
  );
  const isRuntimeProbeActive = mergedPlaybackOperator?.runtimePolicyAction === 'probe_up';

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
    hasLiveDvrWindow,
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
    liveSeekWindow,
    setStatus,
    allowNativeFullscreen: activeHlsEngine === 'native' || vodStreamMode === 'direct_mp4',
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
    runtimeProbeActive: isRuntimeProbeActive,
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
    nativePlaybackWasActiveRef.current = false;
    nativeVideoShownRef.current = false;
    nativeVideoHoldPositionRef.current = null;
    clearNativeVideoVeilTimers();
    setNativePlaybackState(null);
    setNativeSessionId(null);
    setActiveRecordingId(null);
    setVodStreamMode(null);
    setActiveHlsEngine(null);
    setSessionWindowKind('unknown');
    setLiveSeekWindow(null);
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

  const prepareFreshPlayback = useCallback((mode: 'LIVE' | 'VOD') => {
    setDurationSeconds(requestedDuration);
    setPlaybackMode(mode);
  }, [requestedDuration]);

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
          setPlayerError({
            title: nextState.lastError,
            retryable: true,
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
      setPlaybackObservability(extractPlaybackObservability(
        diagnostics.playbackInfo as PlaybackInfo,
        typeof diagnostics.trace?.clientPath === 'string' ? diagnostics.trace.clientPath : 'android/native'
      ));
    }

    mergeSessionPlaybackTrace(extractPlaybackTrace(diagnostics?.trace));
    mergeSessionPlaybackTrace(extractPlaybackTrace(resolvedState.session?.trace));

    if (resolvedState.lastError) {
      setPlayerError({
        title: resolvedState.lastError,
        retryable: true,
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
  }, [clearPlayerError, isNativePlaybackHost, setPlaybackMode, setPlayerError, setStatus]);

  const gatherPlaybackCapabilitiesForPlayer = useCallback(async (scope: 'live' | 'recording' = 'live'): Promise<CapabilitySnapshot> => {
    const video = videoRef.current as HTMLVideoElement | null;
    return gatherPlaybackCapabilities(scope, video);
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
    setResumeState(null);
    setShowResumeOverlay(false);
    setTraceId('-');
    setPlaybackMode('VOD');

    let abortController: AbortController | null = null;
    let requestCaps: CapabilitySnapshot | null = null;
    let recordingIsSeekable = false;

    try {
      await ensureSessionCookie();

      // Determine Playback Mode from backend PlaybackInfo (single source of truth).
      let streamUrl = '';
      let mode: 'native_hls' | 'hlsjs' | 'direct_mp4' | 'transcode' | 'deny' = 'deny';

      try {
        const maxMetaRetries = 20;
        requestCaps = await gatherPlaybackCapabilitiesForPlayer('recording');
        const requestProfile = resolvePlaybackRequestProfile(
          gatherPlaybackClientContext(),
          requestCaps,
          'recording'
        );
        setCapabilitySnapshot(requestCaps);
        let pInfo: PlaybackInfo | undefined;

        for (let i = 0; i < maxMetaRetries; i++) {
          if (activeRecordingRef.current !== id) return;

          const { data, error, response } = await postRecordingPlaybackInfo({
            path: { recordingId: id },
            body: requestCaps,
            headers: buildPlaybackProfileHeaders(requestProfile),
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
        recordingIsSeekable = typeof pInfo.isSeekable === 'boolean' ? pInfo.isSeekable : false;
        if (typeof pInfo.isSeekable !== 'boolean') {
          telemetry.emit('ui.failclosed', {
            context: 'V3Player.isSeekable.missing',
            reason: 'IS_SEEKABLE_MISSING'
          });
        }
        setCanSeek(recordingIsSeekable);
        if (pInfo.startUnix) setStartUnix(pInfo.startUnix);

        // Resume State
        if (!suppressResumePrompt && recordingIsSeekable && pInfo.resume && pInfo.resume.posSeconds >= 15 && (!pInfo.resume.finished)) {
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
          if (recordingIsSeekable && requestedStartPositionSeconds > 0) {
            seekWhenReady(requestedStartPositionSeconds);
          }
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
          if (recordingIsSeekable && requestedStartPositionSeconds > 0) {
            seekWhenReady(requestedStartPositionSeconds);
          }
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
  }, [clearPlaybackState, clearPlayerError, ensureSessionCookie, gatherPlaybackCapabilitiesForPlayer, hasActivePlayback, mergeSessionPlaybackTrace, playDirectMp4, playHls, requestedStartPositionSeconds, resolvePreferredHlsEngineForCapabilities, seekWhenReady, setLegacyErrorDetails, setPlayerError, sleep, suppressResumePrompt, t, teardownActivePlayback, waitForDirectStream]);

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
          if (hasActivePlayback() || nativePlaybackState?.activeRequest) {
            await teardownActivePlayback();
          } else {
            clearPlaybackState();
          }
          prepareFreshPlayback('VOD');
          beginNativePlayback({
            kind: 'recording',
            recordingId,
            authToken: token || undefined,
            startPositionMs: Math.round(requestedStartPositionSeconds * 1000),
            title: normalizedRecordingTitle || channel?.name || recordingId,
          });
          return;
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

        // SSOT: live playback mode is decided by backend from measured capabilities.
        let liveMode: 'native_hls' | 'hlsjs' | 'direct_mp4' | 'transcode' | 'deny' = 'deny';
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
          const retryAfterHeader = liveResponse.headers.get('Retry-After');
          const retryAfterSeconds = retryAfterHeader ? parseInt(retryAfterHeader, 10) : undefined;
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
            retryAfterSeconds,
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

        // raw-fetch-justified: stream.start intent needs explicit payload shaping and immediate RFC7807 handling.
        const res = await fetch(`${apiBase}/intents`, {
          method: 'POST',
          headers: authHeaders(true),
          body: JSON.stringify(intentBody)
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
  }, [src, recordingId, sRef, apiBase, authHeaders, clearPlaybackState, clearPlayerError, ensureSessionCookie, waitForSessionReady, hasActivePlayback, mergeSessionPlaybackTrace, playHls, sendStopIntent, clearSessionLeaseState, t, startRecordingPlayback, applyAutoplayMute, gatherPlaybackCapabilitiesForPlayer, resolvePreferredHlsEngine, resolvePreferredHlsEngineForCapabilities, setActiveSessionId, setPlayerError, prepareFreshPlayback, requestedDuration, requestedStartPositionSeconds, teardownActivePlayback, beginNativePlayback, channel?.name, channel?.logoUrl, nativePlaybackState, normalizedRecordingTitle, token]);

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

  const effectiveOperator = mergedPlaybackOperator;
  const effectiveOperatorMaxQualityRung = effectiveOperator?.maxQualityRung ?? null;
  const effectiveRuntimePolicyPhase = effectiveOperator?.runtimePolicyPhase ?? null;
  const effectiveRuntimeProbeCandidate = effectiveOperator?.runtimeProbeCandidate ?? null;
  const runtimePolicyMetaHint = resolveRuntimePolicyMetaHint(
    effectiveRuntimePolicyPhase,
    effectiveRuntimeProbeCandidate,
    effectiveOperatorMaxQualityRung,
  );
  const runtimePolicyCopyHint = runtimePolicyMetaHint
    ? formatQualityRungLabel(runtimePolicyMetaHint)
    : null;
  const runtimePolicyStartupSupport = resolveRuntimePolicyStartupSupport(
    effectiveRuntimePolicyPhase,
    runtimePolicyCopyHint,
    t,
  );
  const runtimePolicyErrorSupport = resolveRuntimePolicyErrorSupport(
    effectiveRuntimePolicyPhase,
    runtimePolicyCopyHint,
    t,
  );
  const isRecordingStartupSurface = Boolean(recordingId || activeRecordingId || activeRecordingRef.current);
  const startupTitle = channel?.name || normalizedRecordingTitle || (isRecordingStartupSurface
    ? t('player.recordingFallbackTitle', { defaultValue: 'Recording' })
    : '');
  const spinnerLabel =
    isOverlayStartupStatus
      ? isRecordingStartupSurface
        ? (overlayStatus === 'buffering' && playbackMode === 'VOD' && vodStreamMode === 'direct_mp4')
          ? t('player.preparingDirectPlay') // Show explicit preparing for VOD buffering
          : t('player.preparingRecordingPlayback', { defaultValue: 'Opening recording…' })
        : resolveStartupOverlayLabel(
            overlayStatus,
            `${t(`player.statusStates.${overlayStatus}`, { defaultValue: overlayStatus })}…`,
            sessionProfileReason,
            t,
          )
      : '';
  const spinnerSupport =
    isOverlayStartupStatus
      ? isRecordingStartupSurface
        ? t('player.recordingStartupSupport', { defaultValue: 'Preparing the source. Playback will start shortly.' })
        : runtimePolicyStartupSupport || resolveStartupOverlaySupport(sessionProfileReason, t)
      : '';
  const startupStatusLabel = isRecordingStartupSurface
    ? t('player.recordingStartupStatus', { defaultValue: 'Opening' })
    : t(`player.statusStates.${overlayStatus}`, { defaultValue: overlayStatus });
  const showStartupOverlay =
    isImmediateStartupStatus ||
    (status === 'buffering' && showBufferingOverlay) ||
    shouldHoldNativeVideo;
  const useNativeBufferingSafeOverlay = shouldHoldNativeVideo;
  const showNativeBufferingMask = shouldHoldNativeVideo || showNativeVideoVeil;
  const useMinimalStartupChrome = showStartupOverlay && (hostEnvironment.isTv || useOverlayShell || isRecordingPageLayout);
  const showPlaybackChrome = !useMinimalStartupChrome;
  const showRecordingWatchLayout = Boolean(recordingId && !isFullscreen && (useOverlayShell || isRecordingPageLayout));
  const recordingWatchTitle = startupTitle || t('player.recordingFallbackTitle', { defaultValue: 'Recording' });

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
      // Keep the veil visual-only. Programmatic pause/resume here can strand
      // native playback behind a manual play gesture after a brief rebuffer.
      if (nativeManagedPauseRef.current) {
        delete video.dataset.xg2gManagedPause;
        nativeManagedPauseRef.current = false;
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
  const effectiveHostPressureBand =
    sessionPlaybackTrace?.hostPressureBand ??
    playbackObservability?.hostPressureBand ??
    null;
  const effectiveHostOverrideApplied =
    sessionPlaybackTrace?.hostOverrideApplied ??
    playbackObservability?.hostOverrideApplied ??
    false;
  const effectiveForcedIntent = effectiveOperator?.forcedIntent ?? null;
  const effectiveOperatorRuleName = effectiveOperator?.ruleName ?? null;
  const effectiveOperatorRuleScope = effectiveOperator?.ruleScope ?? null;
  const effectiveRuntimePolicyAction = effectiveOperator?.runtimePolicyAction ?? null;
  const effectiveRuntimePolicyConstraints = effectiveOperator?.runtimePolicyConstraints ?? null;
  const effectiveRuntimePolicyReplay: PlaybackTraceRuntimeReplay | null = effectiveOperator?.runtimePolicyReplay ?? null;
  const effectiveRuntimePolicyReasons = effectiveOperator?.runtimePolicyReasons ?? null;
  const effectiveRuntimePolicyTimeline = effectiveOperator?.runtimePolicyTimeline ?? null;
  const effectiveRuntimeProbeFailureStreak = effectiveOperator?.runtimeProbeFailureStreak ?? null;
  const effectiveRuntimeProbeSuccessStreak = effectiveOperator?.runtimeProbeSuccessStreak ?? null;
  const effectiveClientFallbackDisabled = effectiveOperator?.clientFallbackDisabled ?? false;
  const effectiveOperatorOverrideApplied = effectiveOperator?.overrideApplied ?? false;
  const runtimePolicyReasonsSummary =
    effectiveRuntimePolicyReasons?.filter(Boolean).join(', ') || '-';
  const runtimePolicyConstraintsSummary =
    effectiveRuntimePolicyConstraints?.filter(Boolean).join(', ') || '-';
  const runtimeProbeTrustSummary =
    effectiveRuntimeProbeSuccessStreak != null || effectiveRuntimeProbeFailureStreak != null
      ? `success ${effectiveRuntimeProbeSuccessStreak ?? 0} / fail ${effectiveRuntimeProbeFailureStreak ?? 0}`
      : '-';
  const runtimePolicyTimelineEntries =
    effectiveRuntimePolicyTimeline?.slice(-6).reverse() ?? [];
  const runtimePolicyTimelineSummaryEntries = runtimePolicyTimelineEntries.map((entry) => ({
    key: `${entry.tickAt}-${entry.policyAction ?? 'hold'}-${entry.plannedTransition ?? 'noop'}`,
    value: formatRuntimeTimelineEntry(entry),
  }));
  const showRuntimePolicyMeta = Boolean(
    effectiveRuntimePolicyPhase &&
      effectiveRuntimePolicyPhase !== 'stable'
  );
  const runtimePolicyPhaseLabel = t(`player.runtimePolicyPhases.${effectiveRuntimePolicyPhase ?? 'unknown'}`, {
    defaultValue: formatRuntimePolicyPhaseLabel(effectiveRuntimePolicyPhase),
  });
  const runtimePolicyPhaseState = resolveRuntimePolicyPhaseState(effectiveRuntimePolicyPhase);
  const sourceProfileSummary = formatSourceProfileSummary(sessionPlaybackTrace?.source);
  const ffmpegPlanSummary = formatFfmpegPlanSummary(sessionPlaybackTrace?.ffmpegPlan);
  const firstFrameLabel = formatFirstFrameLabel(sessionPlaybackTrace?.firstFrameAtMs);
  const fallbackSummary = formatFallbackSummary(sessionPlaybackTrace);
  const stopSummary = formatStopSummary(sessionPlaybackTrace);
  const hostPressureSummary = formatHostPressureSummary(effectiveHostPressureBand, effectiveHostOverrideApplied);
  const showVerboseErrorTelemetry = !isCompactTouchLayout;
  const runtimePolicyMetaHintLabel = runtimePolicyMetaHint
    ? formatQualityRungLabel(runtimePolicyMetaHint)
    : null;
  const audioToggleLabel = isMuted ? t('player.unmute') : t('player.mute');
  const useTheaterControlsLayout = Boolean(isRecordingPageLayout && !isFullscreen && hasSeekWindow);
  const inferredPlaybackWindowKind = resolvePlaybackWindowKind(playbackMode, hasLiveDvrWindow);
  const playbackWindowKind = sessionWindowKind !== 'unknown' ? sessionWindowKind : inferredPlaybackWindowKind;
  const liveWindowEdge = liveSeekWindow?.liveEdge ?? seekableEnd;
  const liveWindowLagSeconds = Math.max(0, Math.round(liveWindowEdge - currentPlaybackTime));
  const hasLiveWindowPlayhead = hasLiveDvrWindow && currentPlaybackTime >= Math.max(0, seekableStart - 1);
  const liveWindowStateLabel = !hasLiveDvrWindow
    ? '-'
    : !hasLiveWindowPlayhead
      ? t('player.liveWindowReady', { defaultValue: 'Window ready' })
      : isAtLiveEdge
        ? t('player.liveWindowAtEdge', { defaultValue: 'At live edge' })
        : t('player.liveWindowBehindEdge', {
            defaultValue: '{{seconds}}s behind live',
            seconds: liveWindowLagSeconds,
          });
  const runtimeStatsRows = useMemo(() => ([
    {
      key: 'status',
      label: t('player.status'),
      value: (
        <span role="status">
          {t(`player.statusStates.${status}`, { defaultValue: status })}
        </span>
      ),
    },
    { key: 'session', label: t('common.session', { defaultValue: 'Session' }), value: effectiveSessionId || '-' },
    { key: 'request-id', label: t('common.requestId', { defaultValue: 'Request ID' }), value: sessionPlaybackTrace?.requestId || traceId },
    { key: 'client-path', label: t('player.clientPath', { defaultValue: 'Client Path' }), value: effectiveClientPath || '-' },
    { key: 'request-profile', label: t('player.requestProfile', { defaultValue: 'Request Profile' }), value: formatRequestProfileLabel(effectiveRequestProfile) },
    { key: 'requested-intent', label: t('player.requestedIntent', { defaultValue: 'Requested Intent' }), value: formatRequestProfileLabel(effectiveRequestedIntent) },
    { key: 'resolved-intent', label: t('player.resolvedIntent', { defaultValue: 'Resolved Intent' }), value: formatRequestProfileLabel(effectiveResolvedIntent) },
    { key: 'quality-rung', label: t('player.qualityRung', { defaultValue: 'Quality Rung' }), value: formatQualityRungLabel(effectiveQualityRung) },
    { key: 'audio-quality-rung', label: t('player.audioQualityRung', { defaultValue: 'Audio Quality Rung' }), value: formatQualityRungLabel(effectiveAudioQualityRung) },
    { key: 'video-quality-rung', label: t('player.videoQualityRung', { defaultValue: 'Video Quality Rung' }), value: formatQualityRungLabel(effectiveVideoQualityRung) },
    { key: 'degraded-from', label: t('player.degradedFrom', { defaultValue: 'Degraded From' }), value: formatRequestProfileLabel(effectiveDegradedFrom) },
    { key: 'host-pressure-band', label: t('player.hostPressure', { defaultValue: 'Host Pressure' }), value: effectiveHostPressureBand || '-' },
    { key: 'host-override', label: t('player.hostOverrideApplied', { defaultValue: 'Host Override Applied' }), value: formatBooleanLabel(effectiveHostOverrideApplied) },
    { key: 'forced-intent', label: t('player.forcedIntent', { defaultValue: 'Forced Intent' }), value: formatRequestProfileLabel(effectiveForcedIntent) },
    { key: 'operator-max-quality', label: t('player.operatorMaxQualityRung', { defaultValue: 'Operator Max Quality' }), value: formatQualityRungLabel(effectiveOperatorMaxQualityRung) },
    { key: 'runtime-policy-action', label: t('player.runtimePolicyAction', { defaultValue: 'Runtime Policy Action' }), value: effectiveRuntimePolicyAction || '-' },
    {
      key: 'runtime-policy-phase',
      label: t('player.runtimePolicyPhase', { defaultValue: 'Runtime Policy Phase' }),
      value: runtimePolicyPhaseLabel,
    },
    { key: 'runtime-probe-target', label: t('player.runtimeProbeCandidate', { defaultValue: 'Runtime Probe Target' }), value: formatQualityRungLabel(effectiveRuntimeProbeCandidate) },
    { key: 'runtime-policy-reasons', label: t('player.runtimePolicyReasons', { defaultValue: 'Runtime Policy Reasons' }), value: runtimePolicyReasonsSummary },
    { key: 'runtime-policy-constraints', label: t('player.runtimePolicyConstraints', { defaultValue: 'Runtime Policy Constraints' }), value: runtimePolicyConstraintsSummary },
    { key: 'runtime-probe-trust', label: t('player.runtimeProbeTrust', { defaultValue: 'Runtime Probe Trust' }), value: runtimeProbeTrustSummary },
    {
      key: 'runtime-policy-timeline',
      label: t('player.runtimePolicyTimeline', { defaultValue: 'Runtime Timeline' }),
      value: runtimePolicyTimelineSummaryEntries.length > 0 ? (
        <div className={styles.runtimeTimelineList}>
          {runtimePolicyTimelineSummaryEntries.map((entry) => (
            <span key={entry.key}>{entry.value}</span>
          ))}
        </div>
      ) : '-',
    },
    { key: 'operator-rule-name', label: t('player.operatorRuleName', { defaultValue: 'Operator Rule' }), value: effectiveOperatorRuleName || '-' },
    { key: 'operator-rule-scope', label: t('player.operatorRuleScope', { defaultValue: 'Operator Rule Scope' }), value: effectiveOperatorRuleScope || '-' },
    { key: 'client-fallback-disabled', label: t('player.clientFallbackDisabled', { defaultValue: 'Client Fallback Disabled' }), value: formatBooleanLabel(effectiveClientFallbackDisabled) },
    { key: 'operator-override-applied', label: t('player.operatorOverrideApplied', { defaultValue: 'Operator Override Applied' }), value: formatBooleanLabel(effectiveOperatorOverrideApplied) },
    { key: 'source-profile', label: t('player.sourceProfile', { defaultValue: 'Source Profile' }), value: sourceProfileSummary },
    { key: 'output-profile', label: t('player.outputProfile', { defaultValue: 'Output Profile' }), value: formatTargetProfileSummary(effectiveTargetProfile) },
    { key: 'profile-hash', label: t('player.profileHash', { defaultValue: 'Profile Hash' }), value: effectiveTargetProfileHash || '-' },
    { key: 'execution', label: t('player.execution', { defaultValue: 'Execution' }), value: formatExecutionLabel(effectiveTargetProfile) },
    { key: 'ffmpeg-plan', label: t('player.ffmpegPlan', { defaultValue: 'FFmpeg Plan' }), value: ffmpegPlanSummary },
    { key: 'first-frame', label: t('player.firstFrame', { defaultValue: 'First Frame' }), value: firstFrameLabel },
    { key: 'fallbacks', label: t('player.fallbacks', { defaultValue: 'Fallbacks' }), value: fallbackSummary },
    { key: 'stop-reason', label: t('player.stopReason', { defaultValue: 'Stop' }), value: stopSummary },
    { key: 'output-kind', label: t('player.outputKind', { defaultValue: 'Output Kind' }), value: playbackObservability?.selectedOutputKind || '-' },
    {
      key: 'playback-window',
      label: t('player.playbackWindow', { defaultValue: 'Playback Window' }),
      value: t(`player.playbackWindowKinds.${playbackWindowKind}`, { defaultValue: playbackWindowKind }),
    },
    { key: 'resolution', label: t('player.resolution'), value: stats.resolution },
    { key: 'bandwidth', label: t('player.bandwidth'), value: stats.bandwidth > 0 ? `${stats.bandwidth} kbps` : '-' },
    { key: 'buffer-health', label: t('player.bufferHealth'), value: `${stats.bufferHealth}s` },
    { key: 'latency', label: t('player.latency'), value: stats.latency !== null ? `${stats.latency}s` : '-' },
    { key: 'fps', label: t('player.fps'), value: stats.fps },
    { key: 'dropped', label: t('player.dropped'), value: stats.droppedFrames },
    {
      key: 'hls-level',
      label: t('player.hlsLevel'),
      value: hlsRef.current ? (stats.levelIndex === -1 ? 'Auto' : stats.levelIndex) : 'Native / Direct',
    },
    { key: 'segment-duration', label: t('player.segDuration'), value: stats.buffer > 0 ? `${stats.buffer}s` : '-' },
    {
      key: 'seekable-range',
      label: t('player.seekableRange', { defaultValue: 'Seekable' }),
      value: `${formatClock(seekableStart)} -> ${formatClock(seekableEnd)}`,
    },
    { key: 'playhead', label: t('player.playhead', { defaultValue: 'Playhead' }), value: formatClock(currentPlaybackTime) },
    { key: 'seek-window', label: t('player.seekWindow', { defaultValue: 'Seek Window' }), value: hasSeekWindow ? formatClock(windowDuration) : '-' },
    ...(hasLiveDvrWindow ? [{
      key: 'live-window-state',
      label: t('player.liveWindowState', { defaultValue: 'Live Window' }),
      value: liveWindowStateLabel,
    }] : []),
    {
      key: 'fullscreen-path',
      label: t('player.fullscreenPath', { defaultValue: 'Fullscreen Path' }),
      value: isWebKitFullscreenActive
        ? 'native-webkit'
        : isFullscreen
          ? 'container'
          : prefersDesktopNativeFullscreen
            ? 'desktop-webkit-ready'
            : supportsNativeFullscreen
              ? 'webkit-available'
              : 'web-only',
    },
  ]), [
    currentPlaybackTime,
    effectiveAudioQualityRung,
    effectiveClientFallbackDisabled,
    effectiveClientPath,
    effectiveDegradedFrom,
    effectiveForcedIntent,
    effectiveHostOverrideApplied,
    effectiveHostPressureBand,
    effectiveOperatorMaxQualityRung,
    effectiveOperatorOverrideApplied,
    effectiveOperatorRuleName,
    effectiveOperatorRuleScope,
    effectiveQualityRung,
    effectiveRequestProfile,
    effectiveRequestedIntent,
    effectiveResolvedIntent,
    effectiveRuntimePolicyAction,
    effectiveRuntimeProbeCandidate,
    effectiveSessionId,
    effectiveTargetProfile,
    effectiveTargetProfileHash,
    effectiveVideoQualityRung,
    fallbackSummary,
    ffmpegPlanSummary,
    firstFrameLabel,
    hasLiveDvrWindow,
    hasSeekWindow,
    isFullscreen,
    isWebKitFullscreenActive,
    liveWindowStateLabel,
    playbackObservability?.selectedOutputKind,
    playbackWindowKind,
    prefersDesktopNativeFullscreen,
    runtimePolicyConstraintsSummary,
    runtimePolicyPhaseLabel,
    runtimePolicyPhaseState,
    runtimePolicyReasonsSummary,
    runtimePolicyTimelineSummaryEntries,
    runtimeProbeTrustSummary,
    seekableEnd,
    seekableStart,
    sessionPlaybackTrace?.requestId,
    sourceProfileSummary,
    stats.bandwidth,
    stats.buffer,
    stats.bufferHealth,
    stats.droppedFrames,
    stats.fps,
    stats.latency,
    stats.levelIndex,
    stats.resolution,
    status,
    stopSummary,
    supportsNativeFullscreen,
    t,
    traceId,
    windowDuration,
  ]);
  const seekProgressPercent = windowDuration > 0
    ? `${Math.min(100, Math.max(0, (relativePosition / windowDuration) * 100))}%`
    : '0%';
  const seekFromPointer = useCallback((clientX: number, track: HTMLDivElement) => {
    if (windowDuration <= 0) {
      return;
    }

    const rect = track.getBoundingClientRect();
    if (rect.width <= 0) {
      return;
    }

    const ratio = Math.min(1, Math.max(0, (clientX - rect.left) / rect.width));
    seekTo(seekableStart + ratio * windowDuration);
  }, [seekTo, seekableStart, windowDuration]);
  const handleVodScrubPointerDown = useCallback((event: PointerEvent<HTMLDivElement>) => {
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    seekFromPointer(event.clientX, event.currentTarget);
  }, [seekFromPointer]);
  const handleVodScrubPointerMove = useCallback((event: PointerEvent<HTMLDivElement>) => {
    if ((event.buttons & 1) !== 1) {
      return;
    }
    seekFromPointer(event.clientX, event.currentTarget);
  }, [seekFromPointer]);
  const handleVodScrubKeyDown = useCallback((event: KeyboardEvent<HTMLDivElement>) => {
    if (windowDuration <= 0) {
      return;
    }

    switch (event.key) {
      case 'ArrowLeft':
        event.preventDefault();
        seekBy(-15);
        break;
      case 'ArrowRight':
        event.preventDefault();
        seekBy(15);
        break;
      case 'Home':
        event.preventDefault();
        seekTo(seekableStart);
        break;
      case 'End':
        event.preventDefault();
        seekTo(seekableEnd);
        break;
      default:
        break;
    }
  }, [seekBy, seekTo, seekableEnd, seekableStart, windowDuration]);

  return (
    <div
      ref={containerRef}
      data-xg2g-player-root="true"
      data-playback-window={playbackWindowKind}
      className={[
        styles.container,
        'animate-enter',
        useOverlayShell ? styles.overlay : null,
        isRecordingPageLayout ? styles.recordingPage : null,
        isFullscreen ? styles.fullscreenActive : null,
        isIdle ? styles.userIdle : null,
      ].filter(Boolean).join(' ')}
    >
      <div className={showRecordingWatchLayout ? styles.watchSurface : undefined}>
        <div
          className={[
            styles.playerFrame,
            useOverlayShell ? styles.playerFrameOverlay : null,
            isRecordingPageLayout ? styles.playerFramePage : null,
            showRecordingWatchLayout ? styles.playerFrameWatchSurface : null,
            isFullscreen ? styles.playerFrameFullscreen : null,
          ].filter(Boolean).join(' ')}
        >
          {useOverlayShell && (
            <button
              onClick={() => void stopStream()}
              className={styles.closeButton}
              aria-label={t('player.closePlayer')}
            >
              ✕
            </button>
          )}

      <PlayerRuntimeMetaPanel
        show={showStats && showPlaybackChrome}
        title={t('player.statsTitle', { defaultValue: 'Technical Stats' })}
        actions={<PlayerRuntimeReplayExport replay={effectiveRuntimePolicyReplay} />}
        rows={runtimeStatsRows}
      />

      <div
        className={[
          styles.videoWrapper,
          showNativeBufferingMask ? styles.videoWrapperMasked : null,
        ].filter(Boolean).join(' ')}
      >
        {channel && showPlaybackChrome && <h3 className={styles.overlayTitle}>{channel.name}</h3>}
        {showNativeBufferingMask && (
          <div
            className={styles.nativeBufferingMask}
            aria-hidden="true"
          ></div>
        )}
        {useMinimalStartupChrome && (
          <div className={styles.startupBackdrop} aria-hidden="true"></div>
        )}

        {/* PREPARING Overlay (VOD Remux) */}
        <PlayerStartupSurface
          show={showStartupOverlay}
          isRecordingStartupSurface={isRecordingStartupSurface}
          useNativeBufferingSafeOverlay={useNativeBufferingSafeOverlay}
          startupTitle={startupTitle}
          startupEyebrow={t('player.startupSurfaceEyebrow', { defaultValue: 'Live startup' })}
          startupStatusState={overlayStatus === 'buffering' ? 'live' : 'idle'}
          startupStatusLabel={startupStatusLabel}
          spinnerLabel={spinnerLabel}
          spinnerSupport={spinnerSupport}
          startupElapsedLabel={t('player.startupElapsed', {
            defaultValue: 'Wait {{seconds}}s',
            seconds: startupElapsedSeconds,
          })}
          showRuntimePolicyMeta={showRuntimePolicyMeta}
          runtimePolicyPhase={effectiveRuntimePolicyPhase}
          runtimePolicyPhaseState={runtimePolicyPhaseState}
          runtimePolicyPhaseLabel={runtimePolicyPhaseLabel}
          runtimePolicyMetaHint={runtimePolicyMetaHintLabel}
          useMinimalStartupChrome={useMinimalStartupChrome}
          showStopAction={!onClose}
          stopLabel={t('common.stop')}
          onStop={() => void stopStream()}
        />

        <video
          ref={videoRef}
          controls={false}
          playsInline
          webkit-playsinline=""
          x-webkit-airplay="allow"
          preload="metadata"
          autoPlay={!!autoStart}
          className={[
            styles.videoElement,
            showNativeBufferingMask ? styles.videoElementHidden : null,
          ].filter(Boolean).join(' ')}
        />
      </div>

      {/* Error Toast */}
      <PlayerErrorSurface
        error={error}
        onRetry={handleRetry}
        showRuntimePolicyMeta={showRuntimePolicyMeta}
        runtimePolicyPhase={effectiveRuntimePolicyPhase}
        runtimePolicyPhaseState={runtimePolicyPhaseState}
        runtimePolicyPhaseLabel={runtimePolicyPhaseLabel}
        runtimePolicyMetaHint={runtimePolicyMetaHintLabel}
        runtimePolicyErrorSupport={runtimePolicyErrorSupport}
        showVerboseErrorTelemetry={showVerboseErrorTelemetry}
        stopSummary={stopSummary}
        hostPressureSummary={hostPressureSummary}
        fallbackSummary={fallbackSummary}
        ffmpegPlanSummary={ffmpegPlanSummary}
        stopLabel={t('player.stopReason', { defaultValue: 'Stop' })}
        hostPressureLabel={t('player.hostPressure', { defaultValue: 'Host Pressure' })}
        fallbackLabel={t('player.fallbacks', { defaultValue: 'Fallbacks' })}
        ffmpegPlanLabel={t('player.ffmpegPlan', { defaultValue: 'FFmpeg Plan' })}
        retryLabel={t('common.retry')}
        showErrorDetails={showErrorDetails}
        onToggleDetails={() => setShowErrorDetails(!showErrorDetails)}
        hideDetailsLabel={t('common.hideDetails')}
        showDetailsLabel={t('common.showDetails')}
        sessionLabel={t('common.session')}
        sessionValue={effectiveSessionId || t('common.notAvailable')}
      />

      {/* Controls & Status Bar */}
      {showPlaybackChrome && (
        <div
          className={[
            styles.controlsHeader,
            useTheaterControlsLayout ? styles.controlsHeaderTheater : null,
          ].filter(Boolean).join(' ')}
        >
          {hasSeekWindow ? (
            <div
              className={[
                styles.vodControls,
                styles.seekControls,
                useTheaterControlsLayout ? styles.vodControlsTheater : null,
              ].filter(Boolean).join(' ')}
            >
              <div className={styles.vodScrubArea}>
                <div className={styles.vodScrubTimes}>
                  <span>{startTimeDisplay}</span>
                  <span>{endTimeDisplay}</span>
                </div>
                <div
                  className={styles.vodScrubTrack}
                  role="slider"
                  tabIndex={0}
                  aria-label={t('player.seekTimeline', { defaultValue: 'Seek timeline' })}
                  aria-valuemin={0}
                  aria-valuemax={Math.round(windowDuration)}
                  aria-valuenow={Math.round(relativePosition)}
                  onPointerDown={handleVodScrubPointerDown}
                  onPointerMove={handleVodScrubPointerMove}
                  onKeyDown={handleVodScrubKeyDown}
                >
                  <div className={styles.vodScrubFill} style={{ width: seekProgressPercent }}></div>
                  <div className={styles.vodScrubThumb} style={{ left: seekProgressPercent }}></div>
                </div>
              </div>

              <div
                className={[
                  styles.transportControls,
                  useTheaterControlsLayout ? styles.transportControlsTheater : null,
                ].filter(Boolean).join(' ')}
              >
                <div className={styles.seekButtons}>
                  <Button variant="ghost" size="sm" onClick={() => seekBy(-900)} title={t('player.seekBack15m')} aria-label={t('player.seekBack15m')}>
                    -15m
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => seekBy(-60)} title={t('player.seekBack60s')} aria-label={t('player.seekBack60s')}>
                    -60s
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => seekBy(-15)} title={t('player.seekBack15s')} aria-label={t('player.seekBack15s')}>
                    -15s
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
                  {isPlaying ? <PauseGlyph /> : <PlayGlyph />}
                </Button>

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
              </div>

              {hasLiveDvrWindow && (
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
              disabled={startIntentInFlight.current}
            >
              ▶ {t('common.startStream')}
            </Button>
          )}

          {/* DVR Mode Button (Safari Only / Fallback) */}
          {showDvrModeButton && !canToggleFullscreen && (
            <Button size="sm" onClick={enterDVRMode} title={t('player.dvrMode')}>
              DVR
            </Button>
          )}

          <div
            className={[
              styles.utilityControls,
              useTheaterControlsLayout ? styles.utilityControlsTheater : null,
            ].filter(Boolean).join(' ')}
          >
            <PlayerRuntimeMeta
              show={showRuntimePolicyMeta}
              phase={effectiveRuntimePolicyPhase}
              phaseState={runtimePolicyPhaseState}
              phaseLabel={runtimePolicyPhaseLabel}
              hint={runtimePolicyMetaHintLabel}
              theater={useTheaterControlsLayout}
            />
            {prefersDesktopNativeFullscreen && canEnterNativeFullscreen && !isFullscreen && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => enterNativeFullscreen()}
                title={t('player.nativeFullscreenTitle', { defaultValue: 'Open Apple player' })}
              >
                {t('player.nativeFullscreenLabel', { defaultValue: 'Native' })}
              </Button>
            )}

            {canToggleFullscreen && (
              <Button
                variant="ghost"
                size="sm"
                active={isFullscreen}
                onClick={() => void toggleFullscreen()}
                title={isFullscreen
                  ? t('player.exitFullscreenLabel', { defaultValue: 'Exit fullscreen' })
                  : t('player.fullscreenLabel', { defaultValue: 'Fullscreen' })}
                aria-label={isFullscreen
                  ? t('player.exitFullscreenLabel', { defaultValue: 'Exit fullscreen' })
                  : t('player.fullscreenLabel', { defaultValue: 'Fullscreen' })}
              >
                <FullscreenGlyph />
              </Button>
            )}

            {canToggleMute && (
              <div
                className={[
                  styles.volumeControl,
                  useTheaterControlsLayout ? styles.volumeControlTheater : null,
                ].filter(Boolean).join(' ')}
              >
                <Button
                  variant={isMuted ? 'primary' : 'ghost'}
                  size="sm"
                  className={styles.audioToggleButton}
                  onClick={toggleMute}
                  title={audioToggleLabel}
                  aria-label={audioToggleLabel}
                  aria-pressed={!isMuted}
                >
                  <VolumeGlyph muted={isMuted} />
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
                {t('player.pipLabel')}
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
              <Button variant="danger" size="sm" onClick={() => void stopStream()}>
                {t('common.stop')}
              </Button>
            )}
          </div>
        </div>
      )}
        </div>
        {showRecordingWatchLayout && (
          <section
            className={styles.watchLayout}
            aria-label={t('player.watchSurfaceLabel', { defaultValue: 'Recording playback details' })}
          >
            <div className={styles.watchMain}>
              <h1 className={styles.watchTitle}>{recordingWatchTitle}</h1>
              <div className={styles.watchMeta}>
                {recordingDateLabel ? <span>{recordingDateLabel}</span> : null}
                {recordingLengthLabel ? <span>{recordingLengthLabel}</span> : null}
                {requestedStartPositionSeconds > 0 ? (
                  <span className={styles.watchResumeHint}>
                    {t('player.resumeFrom', {
                      defaultValue: 'Resume {{time}}',
                      time: formatClock(requestedStartPositionSeconds),
                    })}
                  </span>
                ) : null}
              </div>
              {recordingDescription ? (
                <p className={styles.watchDescription}>{recordingDescription}</p>
              ) : null}
            </div>
          </section>
        )}
      </div>
      {/* Resume Overlay */}
      {showResumeOverlay && resumeState && (
        <div className={styles.resumeOverlay}>
          <div className={styles.resumeContent}>
            <h3>{t('player.resumeTitle')}</h3>
            <p>{t('player.resumePrompt', { time: formatClock(resumeState.posSeconds) })}</p>
            <div className={styles.resumeActions}>
              <Button
                ref={resumePrimaryActionRef}
                autoFocus
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
