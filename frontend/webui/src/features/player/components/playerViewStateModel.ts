import type { TFunction } from 'i18next';
import type { AppError } from '../../../types/errors';
import type { PlayerAudioTrack, PlayerStatus, V3PlayerViewState, V3PlayerLabeledValue } from '../../../types/v3-player';
import type { PlaybackTrace as PlaybackTraceContract } from '../../../client-ts';
import type { PlaybackObservability } from '../orchestrator/observabilityFormatters';
import {
  formatBooleanLabel,
  formatExecutionLabel,
  formatQualityRungLabel,
  formatRequestProfileLabel,
  formatTargetProfileSummary,
} from '../orchestrator/observabilityFormatters';

export interface BuildViewStateInput {
  channel: { name?: string; logoUrl?: string | null } | undefined;
  playbackMode: string;
  liveNowPlaying: { title: string | null; desc: string | null };
  onClose?: (() => void) | undefined;
  isIdle: boolean;
  status: PlayerStatus;
  showStats: boolean;
  showPlaybackChrome: boolean;
  isWebKitFullscreenActive: boolean;
  isFullscreen: boolean;
  prefersDesktopNativeFullscreen: boolean;
  supportsNativeFullscreen: boolean;
  mseAv1Readout: string;
  effectiveSessionId: string | null;
  sessionPlaybackTrace: PlaybackTraceContract | null;
  traceId: string;
  effectiveClientPath: string | null;
  effectiveRequestProfile: any;
  effectiveRequestedIntent: any;
  effectiveResolvedIntent: any;
  effectiveQualityRung: any;
  effectiveAudioQualityRung: any;
  effectiveVideoQualityRung: any;
  effectiveDegradedFrom: any;
  effectiveHostPressureBand: string | null;
  effectiveHostOverrideApplied: boolean;
  effectiveForcedIntent: any;
  effectiveOperatorMaxQualityRung: any;
  effectiveOperatorRuleName: string | null;
  effectiveOperatorRuleScope: string | null;
  effectiveClientFallbackDisabled: boolean;
  effectiveOperatorOverrideApplied: boolean;
  sourceProfileSummary: string;
  effectiveTargetProfile: any;
  effectiveTargetProfileHash: string | null;
  ffmpegPlanSummary: string;
  firstFrameLabel: string;
  fallbackSummary: string;
  stopSummary: string;
  hostPressureSummary: string;
  playbackObservability: PlaybackObservability | null;
  showNativeBufferingMask: boolean;
  stats: {
    resolution: string;
    bandwidth: number;
    bufferHealth: number;
    latency: number | null;
    fps: number;
    droppedFrames: number;
    levelIndex: number;
    buffer: number;
  };
  hlsRefCurrent: boolean;
  seekableStart: number;
  seekableEnd: number;
  currentPlaybackTime: number;
  windowDuration: number;
  hasSeekWindow: boolean;
  isCompactTouchLayout: boolean;
  currentPositionDisplay: string;
  dvrPreviewBaseUrl: string | null;
  dvrPreviewSegmentSeconds: number;
  dvrPreviewWindowStartUnix: number | null;
  useMinimalStartupChrome: boolean;
  showStartupOverlay: boolean;
  showSpinnerCard: boolean;
  useNativeBufferingSafeOverlay: boolean;
  overlayStatus: string;
  spinnerLabel: string;
  spinnerSupport: string;
  startupPhaseSteps: Array<{ key: string; label: string; state: 'done' | 'active' | 'pending' }>;
  startupProgressPercent: number;
  startupElapsedSeconds: number;
  autoStart?: boolean;
  error: AppError | null;
  showErrorDetails: boolean;
  effectiveSessionLabel: string;
  isPlaying: boolean;
  ttffMetrics: { ttffMs: number; manifestMs: number; bufferMs: number } | null;
  startTimeDisplay: string;
  endTimeDisplay: string;
  relativePosition: number;
  isLiveMode: boolean;
  isAtLiveEdge: boolean;
  recordingId?: string;
  src?: string;
  sRef: string;
  startIntentInFlightRef: boolean;
  showDvrModeButton: boolean;
  canToggleFullscreen: boolean;
  canEnterNativeFullscreen: boolean;
  canToggleMute: boolean;
  isMuted: boolean;
  canAdjustVolume: boolean;
  volume: number;
  canTogglePiP: boolean;
  isPip: boolean;
  showResumeOverlay: boolean;
  resumeState: { posSeconds: number } | null;
  explicitProfile: string;
  audioTracks: PlayerAudioTrack[];
  activeAudioTrack: number;
  durationSeconds: number | null;
  formatClock: (seconds: number) => string;
  t: TFunction;
}

export function buildPlayerViewState(input: BuildViewStateInput): V3PlayerViewState {
  const { t, formatClock } = input;
  const showVerboseErrorTelemetry = !input.isCompactTouchLayout;
  const audioToggleLabel = input.isMuted ? t('player.unmute') : t('player.mute');
  const audioToggleIcon = input.isMuted ? '🔊' : '🔇';
  const statsTitle = t('player.statsTitle', { defaultValue: 'Technical Stats' });
  const hlsLevelValue = input.hlsRefCurrent ? (input.stats.levelIndex === -1 ? 'Auto' : String(input.stats.levelIndex)) : 'Native / Direct';
  const fullscreenPathValue = input.isWebKitFullscreenActive
    ? 'native-webkit'
    : input.isFullscreen
      ? 'container'
      : input.prefersDesktopNativeFullscreen
        ? 'desktop-webkit-ready'
        : input.supportsNativeFullscreen
          ? 'webkit-available'
          : 'web-only';

  const ttffReadout = input.ttffMetrics
    ? `${(input.ttffMetrics.ttffMs / 1000).toFixed(2)}s (${input.ttffMetrics.manifestMs}ms manifest + ${input.ttffMetrics.bufferMs}ms buffer)`
    : input.status === 'starting' || input.status === 'buffering'
      ? t('player.measuring', { defaultValue: 'Messen…' })
      : '-';

  const statsRows: V3PlayerLabeledValue[] = [
    { label: t('player.ttff', { defaultValue: 'Startzeit (TTFF)' }), value: ttffReadout },
    { label: t('player.av1Mms', { defaultValue: 'AV1/MMS' }), value: input.mseAv1Readout },
    { label: t('common.session', { defaultValue: 'Session' }), value: input.effectiveSessionId || '-' },
    { label: t('common.requestId', { defaultValue: 'Request ID' }), value: input.sessionPlaybackTrace?.requestId || input.traceId },
    { label: t('player.clientPath', { defaultValue: 'Client Path' }), value: input.effectiveClientPath || '-' },
    { label: t('player.requestProfile', { defaultValue: 'Request Profile' }), value: formatRequestProfileLabel(input.effectiveRequestProfile) },
    { label: t('player.requestedIntent', { defaultValue: 'Requested Intent' }), value: formatRequestProfileLabel(input.effectiveRequestedIntent) },
    { label: t('player.resolvedIntent', { defaultValue: 'Resolved Intent' }), value: formatRequestProfileLabel(input.effectiveResolvedIntent) },
    { label: t('player.qualityRung', { defaultValue: 'Quality Rung' }), value: formatQualityRungLabel(input.effectiveQualityRung) },
    { label: t('player.audioQualityRung', { defaultValue: 'Audio Quality Rung' }), value: formatQualityRungLabel(input.effectiveAudioQualityRung) },
    { label: t('player.videoQualityRung', { defaultValue: 'Video Quality Rung' }), value: formatQualityRungLabel(input.effectiveVideoQualityRung) },
    { label: t('player.degradedFrom', { defaultValue: 'Degraded From' }), value: formatRequestProfileLabel(input.effectiveDegradedFrom) },
    { label: t('player.hostPressure', { defaultValue: 'Host Pressure' }), value: input.effectiveHostPressureBand || '-' },
    { label: t('player.hostOverrideApplied', { defaultValue: 'Host Override Applied' }), value: formatBooleanLabel(input.effectiveHostOverrideApplied) },
    { label: t('player.forcedIntent', { defaultValue: 'Forced Intent' }), value: formatRequestProfileLabel(input.effectiveForcedIntent) },
    { label: t('player.operatorMaxQualityRung', { defaultValue: 'Operator Max Quality' }), value: formatQualityRungLabel(input.effectiveOperatorMaxQualityRung) },
    { label: t('player.operatorRuleName', { defaultValue: 'Operator Rule' }), value: input.effectiveOperatorRuleName || '-' },
    { label: t('player.operatorRuleScope', { defaultValue: 'Operator Rule Scope' }), value: input.effectiveOperatorRuleScope || '-' },
    { label: t('player.clientFallbackDisabled', { defaultValue: 'Client Fallback Disabled' }), value: formatBooleanLabel(input.effectiveClientFallbackDisabled) },
    { label: t('player.operatorOverrideApplied', { defaultValue: 'Operator Override Applied' }), value: formatBooleanLabel(input.effectiveOperatorOverrideApplied) },
    { label: t('player.sourceProfile', { defaultValue: 'Source Profile' }), value: input.sourceProfileSummary },
    { label: t('player.outputProfile', { defaultValue: 'Output Profile' }), value: formatTargetProfileSummary(input.effectiveTargetProfile) },
    { label: t('player.profileHash', { defaultValue: 'Profile Hash' }), value: input.effectiveTargetProfileHash || '-' },
    { label: t('player.execution', { defaultValue: 'Execution' }), value: formatExecutionLabel(input.effectiveTargetProfile) },
    { label: t('player.ffmpegPlan', { defaultValue: 'FFmpeg Plan' }), value: input.ffmpegPlanSummary },
    { label: t('player.firstFrame', { defaultValue: 'First Frame' }), value: input.firstFrameLabel },
    { label: t('player.fallbacks', { defaultValue: 'Fallbacks' }), value: input.fallbackSummary },
    { label: t('player.stopReason', { defaultValue: 'Stop' }), value: input.stopSummary },
    { label: t('player.outputKind', { defaultValue: 'Output Kind' }), value: input.playbackObservability?.selectedOutputKind || '-' },
    { label: t('player.resolution'), value: input.stats.resolution },
    { label: t('player.bandwidth'), value: input.stats.bandwidth > 0 ? `${input.stats.bandwidth} kbps` : '-' },
    { label: t('player.bufferHealth'), value: `${input.stats.bufferHealth}s` },
    { label: t('player.latency'), value: input.stats.latency !== null ? `${input.stats.latency}s` : '-' },
    { label: t('player.fps'), value: String(input.stats.fps) },
    { label: t('player.dropped'), value: String(input.stats.droppedFrames) },
    { label: t('player.hlsLevel'), value: hlsLevelValue },
    { label: t('player.segDuration'), value: input.stats.buffer > 0 ? `${input.stats.buffer}s` : '-' },
    { label: t('player.seekableRange', { defaultValue: 'Seekable' }), value: `${formatClock(input.seekableStart)} -> ${formatClock(input.seekableEnd)}` },
    { label: t('player.playhead', { defaultValue: 'Playhead' }), value: formatClock(input.currentPlaybackTime) },
    { label: t('player.seekWindow', { defaultValue: 'Seek Window' }), value: input.hasSeekWindow ? formatClock(input.windowDuration) : '-' },
    { label: t('player.fullscreenPath', { defaultValue: 'Fullscreen Path' }), value: fullscreenPathValue },
  ];

  const errorTelemetryRows: V3PlayerLabeledValue[] = showVerboseErrorTelemetry
    ? [
      input.stopSummary !== '-' ? { label: t('player.stopReason', { defaultValue: 'Stop' }), value: input.stopSummary } : null,
      input.hostPressureSummary !== '-' ? { label: t('player.hostPressure', { defaultValue: 'Host Pressure' }), value: input.hostPressureSummary } : null,
      input.fallbackSummary !== '-' ? { label: t('player.fallbacks', { defaultValue: 'Fallbacks' }), value: input.fallbackSummary } : null,
      input.ffmpegPlanSummary !== '-' ? { label: t('player.ffmpegPlan', { defaultValue: 'FFmpeg Plan' }), value: input.ffmpegPlanSummary } : null,
    ].filter((row): row is V3PlayerLabeledValue => row !== null)
    : [];

  return {
    channelName: input.channel?.name ?? null,
    programmeTitle: input.playbackMode === 'LIVE' ? input.liveNowPlaying.title : (input.channel?.name ?? null),
    programmeDesc: input.playbackMode === 'LIVE' ? input.liveNowPlaying.desc : null,
    useOverlayLayout: Boolean(input.onClose),
    userIdle: input.isIdle,
    showCloseButton: Boolean(input.onClose),
    closeButtonLabel: t('player.closePlayer'),
    showStatsOverlay: input.showStats && input.showPlaybackChrome,
    statsTitle,
    statusLabel: t('player.status'),
    statusChipLabel: t(`player.statusStates.${input.status}`, { defaultValue: input.status }),
    statusChipState: input.status === 'ready' ? 'live' : input.status === 'error' ? 'error' : 'idle',
    statsRows,
    showNativeBufferingMask: input.showNativeBufferingMask,
    hideVideoElement: input.showNativeBufferingMask,
    showStartupBackdrop: input.useMinimalStartupChrome,
    showStartupOverlay: input.showStartupOverlay,
    showSpinnerCard: input.showSpinnerCard,
    channelLogoUrl: input.channel?.logoUrl ?? null,
    useNativeBufferingSafeOverlay: input.useNativeBufferingSafeOverlay,
    overlayStatusLabel: t(`player.statusStates.${input.overlayStatus}`, { defaultValue: input.overlayStatus }),
    overlayStatusState: input.overlayStatus === 'buffering' ? 'live' : 'idle',
    spinnerEyebrow: t('player.startupSurfaceEyebrow', { defaultValue: 'Live startup' }),
    spinnerLabel: input.spinnerLabel,
    spinnerSupport: input.spinnerSupport,
    startupPhaseSteps: input.startupPhaseSteps,
    startupProgressPercent: input.startupProgressPercent,
    startupElapsedLabel: t('player.startupElapsed', {
      defaultValue: 'Wait {{seconds}}s',
      seconds: input.startupElapsedSeconds,
    }),
    showOverlayStopAction: input.useMinimalStartupChrome && !input.onClose,
    overlayStopLabel: t('common.stop'),
    videoClassName: '',
    autoPlay: Boolean(input.autoStart),
    error: input.error,
    showErrorDetails: input.showErrorDetails,
    errorRetryLabel: t('common.retry'),
    errorTelemetryRows,
    errorDetailToggleLabel: input.error?.detail ? (input.showErrorDetails ? t('common.hideDetails') : t('common.showDetails')) : null,
    errorSessionLabel: `${t('common.session')}: ${input.effectiveSessionId || t('common.notAvailable')}`,
    showPlaybackChrome: input.showPlaybackChrome,
    showSeekControls: input.hasSeekWindow,
    seekBack15mLabel: t('player.seekBack15m'),
    seekBack60sLabel: t('player.seekBack60s'),
    seekBack15sLabel: t('player.seekBack15s'),
    seekForward15sLabel: t('player.seekForward15s'),
    seekForward60sLabel: t('player.seekForward60s'),
    seekForward15mLabel: t('player.seekForward15m'),
    playPauseLabel: input.isPlaying ? t('player.pause') : t('player.play'),
    playPauseIcon: input.isPlaying ? '⏸' : '▶',
    ttffBadgeLabel: input.ttffMetrics ? `${(input.ttffMetrics.ttffMs / 1000).toFixed(2)}s` : null,
    ttffTitle: input.ttffMetrics
      ? `TTFF: ${input.ttffMetrics.ttffMs}ms (${input.ttffMetrics.manifestMs}ms manifest + ${input.ttffMetrics.bufferMs}ms decode)`
      : null,
    seekableStart: input.seekableStart,
    seekableEnd: input.seekableEnd,
    startTimeDisplay: input.startTimeDisplay,
    endTimeDisplay: input.endTimeDisplay,
    currentPositionDisplay: input.currentPositionDisplay,
    dvrPreviewBaseUrl: input.dvrPreviewBaseUrl,
    dvrPreviewSegmentSeconds: input.dvrPreviewSegmentSeconds,
    dvrPreviewWindowStartUnix: input.dvrPreviewWindowStartUnix,
    windowDuration: input.windowDuration,
    relativePosition: input.relativePosition,
    isLiveMode: input.isLiveMode,
    isAtLiveEdge: input.isAtLiveEdge,
    liveButtonLabel: t('player.goLive'),
    showServiceInput: !input.hasSeekWindow && !input.channel && !input.recordingId && !input.src,
    serviceRef: input.sRef,
    showManualStartButton: !input.autoStart && !input.src && !input.recordingId,
    manualStartLabel: t('common.startStream'),
    manualStartDisabled: input.startIntentInFlightRef,
    showDvrModeButton: input.showDvrModeButton && !input.canToggleFullscreen,
    dvrModeLabel: t('player.dvrMode'),
    showNativeFullscreenButton: input.prefersDesktopNativeFullscreen && input.canEnterNativeFullscreen && !input.isFullscreen,
    nativeFullscreenTitle: t('player.nativeFullscreenTitle', { defaultValue: 'Open Apple player' }),
    nativeFullscreenLabel: t('player.nativeFullscreenLabel', { defaultValue: 'Native' }),
    showFullscreenButton: input.canToggleFullscreen,
    fullscreenLabel: input.isFullscreen
      ? t('player.exitFullscreenLabel', { defaultValue: 'Exit fullscreen' })
      : t('player.fullscreenLabel', { defaultValue: 'Fullscreen' }),
    fullscreenActive: input.isFullscreen,
    showVolumeControls: input.canToggleMute,
    audioToggleLabel,
    audioToggleIcon,
    audioToggleActive: !input.isMuted,
    canAdjustVolume: input.canAdjustVolume,
    volume: input.isMuted ? 0 : input.volume,
    deviceVolumeHint: t('player.deviceVolumeHint', { defaultValue: 'Use device buttons' }),
    showPipButton: input.canTogglePiP,
    pipTitle: t('player.pipTitle'),
    pipLabel: t('player.pipLabel'),
    pipActive: input.isPip,
    statsLabel: t('player.statsLabel'),
    statsActive: input.showStats,
    showStopButton: !input.onClose,
    stopLabel: t('common.stop'),
    showResumeOverlay: input.showResumeOverlay && Boolean(input.resumeState),
    resumeTitle: t('player.resumeTitle'),
    resumePrompt: input.resumeState
      ? t('player.resumePrompt', { time: formatClock(input.resumeState.posSeconds) })
      : '',
    resumeActionLabel: t('player.resumeAction'),
    startOverLabel: t('player.startOver'),
    resumePositionSeconds: input.resumeState?.posSeconds ?? null,
    explicitProfile: input.explicitProfile,
    audioTracks: input.audioTracks,
    activeAudioTrack: input.activeAudioTrack,
    playback: {
      durationSeconds: input.durationSeconds,
    },
  };
}
