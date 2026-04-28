import type { PlaybackTargetProfile, PlaybackTraceRuntimeTick } from '../../../client-ts';
import type { PlayerStats, PlayerStatus } from '../../../types/v3-player';

import type { PlayerRuntimeMetaPanelRow } from './PlayerRuntimeMeta';
import type { PlaybackWindowKind } from './playerPlaybackModel';
import {
  formatBooleanLabel,
  formatExecutionLabel,
  formatQualityRungLabel,
  formatTargetProfileSummary,
} from './playerRuntimeMetaFormat';
import {
  formatRequestProfileLabel,
  formatRuntimeTimelineEntry,
} from './playerRuntimeTraceFormat';
import styles from './V3Player.module.css';

type Translate = (key: string, options?: Record<string, unknown>) => string;

export interface BuildPlayerRuntimeStatsRowsInput {
  t: Translate;
  status: PlayerStatus;
  effectiveSessionId: string | null;
  requestId: string | null | undefined;
  traceId: string;
  effectiveClientPath: string | null;
  effectiveRequestProfile: string | null;
  effectiveRequestedIntent: string | null;
  effectiveResolvedIntent: string | null;
  effectiveQualityRung: string | null;
  effectiveAudioQualityRung: string | null;
  effectiveVideoQualityRung: string | null;
  effectiveDegradedFrom: string | null;
  effectiveHostPressureBand: string | null;
  effectiveHostOverrideApplied: boolean;
  effectiveForcedIntent: string | null;
  effectiveOperatorMaxQualityRung: string | null;
  effectiveRuntimePolicyAction: string | null;
  runtimePolicyPhaseLabel: string;
  effectiveRuntimeProbeCandidate: string | null;
  runtimePolicyReasonsSummary: string;
  runtimePolicyConstraintsSummary: string;
  runtimeProbeTrustSummary: string;
  effectiveRuntimePolicyTimeline: PlaybackTraceRuntimeTick[] | null;
  effectiveOperatorRuleName: string | null;
  effectiveOperatorRuleScope: string | null;
  effectiveClientFallbackDisabled: boolean;
  effectiveOperatorOverrideApplied: boolean;
  sourceProfileSummary: string;
  effectiveTargetProfile: PlaybackTargetProfile | null;
  effectiveTargetProfileHash: string | null;
  ffmpegPlanSummary: string;
  runtimeDiagnosticsSummary: string;
  sourceWarningsSummary: string;
  clientSummary: string;
  clientDeviceSummary: string;
  autoCodecSummary: string;
  firstFrameLabel: string;
  fallbackSummary: string;
  stopSummary: string;
  selectedOutputKind: string | null | undefined;
  playbackWindowKind: PlaybackWindowKind;
  stats: PlayerStats;
  hasHlsJsEngine: boolean;
  seekableStart: number;
  seekableEnd: number;
  currentPlaybackTime: number;
  hasSeekWindow: boolean;
  windowDuration: number;
  hasLiveDvrWindow: boolean;
  liveWindowStateLabel: string;
  isWebKitFullscreenActive: boolean;
  isFullscreen: boolean;
  prefersDesktopNativeFullscreen: boolean;
  supportsNativeFullscreen: boolean;
  formatClock: (value: number) => string;
}

export function buildPlayerRuntimeStatsRows({
  t,
  status,
  effectiveSessionId,
  requestId,
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
  effectiveRuntimePolicyAction,
  runtimePolicyPhaseLabel,
  effectiveRuntimeProbeCandidate,
  runtimePolicyReasonsSummary,
  runtimePolicyConstraintsSummary,
  runtimeProbeTrustSummary,
  effectiveRuntimePolicyTimeline,
  effectiveOperatorRuleName,
  effectiveOperatorRuleScope,
  effectiveClientFallbackDisabled,
  effectiveOperatorOverrideApplied,
  sourceProfileSummary,
  effectiveTargetProfile,
  effectiveTargetProfileHash,
  ffmpegPlanSummary,
  runtimeDiagnosticsSummary,
  sourceWarningsSummary,
  clientSummary,
  clientDeviceSummary,
  autoCodecSummary,
  firstFrameLabel,
  fallbackSummary,
  stopSummary,
  selectedOutputKind,
  playbackWindowKind,
  stats,
  hasHlsJsEngine,
  seekableStart,
  seekableEnd,
  currentPlaybackTime,
  hasSeekWindow,
  windowDuration,
  hasLiveDvrWindow,
  liveWindowStateLabel,
  isWebKitFullscreenActive,
  isFullscreen,
  prefersDesktopNativeFullscreen,
  supportsNativeFullscreen,
  formatClock,
}: BuildPlayerRuntimeStatsRowsInput): PlayerRuntimeMetaPanelRow[] {
  const runtimePolicyTimelineSummaryEntries =
    effectiveRuntimePolicyTimeline?.slice(-6).reverse().map((entry) => ({
      key: `${entry.tickAt}-${entry.policyAction ?? 'hold'}-${entry.plannedTransition ?? 'noop'}`,
      value: formatRuntimeTimelineEntry(entry),
    })) ?? [];

  return [
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
    { key: 'request-id', label: t('common.requestId', { defaultValue: 'Request ID' }), value: requestId || traceId },
    { key: 'client-path', label: t('player.clientPath', { defaultValue: 'Client Path' }), value: effectiveClientPath || '-' },
    { key: 'client-truth', label: t('player.clientTruth', { defaultValue: 'Client Truth' }), value: clientSummary },
    { key: 'client-device', label: t('player.clientDevice', { defaultValue: 'Client Device' }), value: clientDeviceSummary },
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
    { key: 'auto-codec', label: t('player.autoCodec', { defaultValue: 'Auto Codec' }), value: autoCodecSummary },
    { key: 'ffmpeg-plan', label: t('player.ffmpegPlan', { defaultValue: 'FFmpeg Plan' }), value: ffmpegPlanSummary },
    { key: 'runtime-diagnostics', label: t('player.runtimeDiagnostics', { defaultValue: 'Runtime Diagnostics' }), value: runtimeDiagnosticsSummary },
    { key: 'source-warnings', label: t('player.sourceWarnings', { defaultValue: 'Source Warnings' }), value: sourceWarningsSummary },
    { key: 'first-frame', label: t('player.firstFrame', { defaultValue: 'First Frame' }), value: firstFrameLabel },
    { key: 'fallbacks', label: t('player.fallbacks', { defaultValue: 'Fallbacks' }), value: fallbackSummary },
    { key: 'stop-reason', label: t('player.stopReason', { defaultValue: 'Stop' }), value: stopSummary },
    { key: 'output-kind', label: t('player.outputKind', { defaultValue: 'Output Kind' }), value: selectedOutputKind || '-' },
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
      value: hasHlsJsEngine ? (stats.levelIndex === -1 ? 'Auto' : stats.levelIndex) : 'Native / Direct',
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
  ];
}
