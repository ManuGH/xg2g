import type {
  PlaybackTargetProfile,
  PlaybackTrace as PlaybackTraceContract,
  PlaybackTraceOperator,
  PlaybackTraceRuntimeReplay,
  PlaybackTraceRuntimeTick,
} from '../../../client-ts';
import type { CapabilitySnapshot } from '../utils/playbackCapabilities';
import type { PlaybackObservability } from './playerPlaybackModel';
import {
  formatAutoCodecSummary,
  formatClientPath,
  formatFallbackSummary,
  formatFfmpegPlanSummary,
  formatFirstFrameLabel,
  formatHostPressureSummary,
  formatRuntimeDiagnosticsSummary,
  formatSourceProfileSummary,
  formatSourceWarningsSummary,
  formatStopSummary,
  formatTraceClientDeviceSummary,
  formatTraceClientSummary,
} from './playerRuntimeTraceFormat';

export interface BuildPlayerRuntimeTelemetryModelInput {
  sessionPlaybackTrace: PlaybackTraceContract | null;
  playbackObservability: PlaybackObservability | null;
  capabilitySnapshot: CapabilitySnapshot | null;
  effectiveOperator: PlaybackTraceOperator | null;
  sessionId: string | null | undefined;
  nativeSessionId: string | null | undefined;
  nativePlaybackSessionId: string | null | undefined;
}

export interface PlayerRuntimeTelemetryModel {
  effectiveClientPath: string | null;
  effectiveSessionId: string | null;
  effectiveRequestProfile: string | null;
  effectiveRequestedIntent: string | null;
  effectiveResolvedIntent: string | null;
  effectiveQualityRung: string | null;
  effectiveAudioQualityRung: string | null;
  effectiveVideoQualityRung: string | null;
  effectiveDegradedFrom: string | null;
  effectiveTargetProfile: PlaybackTargetProfile | null;
  effectiveTargetProfileHash: string | null;
  effectiveHostPressureBand: string | null;
  effectiveHostOverrideApplied: boolean;
  effectiveForcedIntent: string | null;
  effectiveOperatorRuleName: string | null;
  effectiveOperatorRuleScope: string | null;
  effectiveRuntimePolicyAction: string | null;
  effectiveRuntimePolicyConstraints: string[] | null;
  effectiveRuntimePolicyReplay: PlaybackTraceRuntimeReplay | null;
  effectiveRuntimePolicyReasons: string[] | null;
  effectiveRuntimePolicyTimeline: PlaybackTraceRuntimeTick[] | null;
  effectiveRuntimeProbeFailureStreak: number | null;
  effectiveRuntimeProbeSuccessStreak: number | null;
  effectiveClientFallbackDisabled: boolean;
  effectiveOperatorOverrideApplied: boolean;
  runtimePolicyReasonsSummary: string;
  runtimePolicyConstraintsSummary: string;
  runtimeProbeTrustSummary: string;
  sourceProfileSummary: string;
  ffmpegPlanSummary: string;
  runtimeDiagnosticsSummary: string;
  sourceWarningsSummary: string;
  clientSummary: string;
  clientDeviceSummary: string;
  autoCodecSummary: string;
  firstFrameLabel: string;
  fallbackSummary: string;
  stopSummary: string;
  hostPressureSummary: string;
  selectedOutputKind: string | null;
}

export function buildPlayerRuntimeTelemetryModel({
  sessionPlaybackTrace,
  playbackObservability,
  capabilitySnapshot,
  effectiveOperator,
  sessionId,
  nativeSessionId,
  nativePlaybackSessionId,
}: BuildPlayerRuntimeTelemetryModelInput): PlayerRuntimeTelemetryModel {
  const hasSessionTrace = sessionPlaybackTrace !== null;
  const effectiveClientPath =
    sessionPlaybackTrace?.clientPath ||
    (!hasSessionTrace ? playbackObservability?.clientPath : null) ||
    formatClientPath(capabilitySnapshot);
  const effectiveSessionId =
    sessionId ||
    nativeSessionId ||
    nativePlaybackSessionId ||
    sessionPlaybackTrace?.sessionId ||
    null;
  const effectiveRequestProfile =
    sessionPlaybackTrace?.requestProfile ??
    (!hasSessionTrace ? playbackObservability?.requestProfile : null) ??
    null;
  const effectiveRequestedIntent =
    sessionPlaybackTrace?.requestedIntent ??
    (!hasSessionTrace ? playbackObservability?.requestedIntent : null) ??
    effectiveRequestProfile;
  const effectiveResolvedIntent =
    sessionPlaybackTrace?.resolvedIntent ??
    (!hasSessionTrace ? playbackObservability?.resolvedIntent : null) ??
    null;
  const effectiveQualityRung =
    sessionPlaybackTrace?.qualityRung ??
    (!hasSessionTrace ? playbackObservability?.qualityRung : null) ??
    null;
  const effectiveAudioQualityRung =
    sessionPlaybackTrace?.audioQualityRung ??
    (!hasSessionTrace ? playbackObservability?.audioQualityRung : null) ??
    null;
  const effectiveVideoQualityRung =
    sessionPlaybackTrace?.videoQualityRung ??
    (!hasSessionTrace ? playbackObservability?.videoQualityRung : null) ??
    null;
  const effectiveDegradedFrom =
    sessionPlaybackTrace?.degradedFrom ??
    (!hasSessionTrace ? playbackObservability?.degradedFrom : null) ??
    null;
  const effectiveTargetProfile =
    sessionPlaybackTrace?.targetProfile ??
    (!hasSessionTrace ? playbackObservability?.targetProfile : null) ??
    null;
  const effectiveTargetProfileHash =
    sessionPlaybackTrace?.targetProfileHash ??
    (!hasSessionTrace ? playbackObservability?.targetProfileHash : null) ??
    null;
  const effectiveHostPressureBand =
    sessionPlaybackTrace?.hostPressureBand ??
    (!hasSessionTrace ? playbackObservability?.hostPressureBand : null) ??
    null;
  const effectiveHostOverrideApplied =
    sessionPlaybackTrace?.hostOverrideApplied ??
    (!hasSessionTrace ? playbackObservability?.hostOverrideApplied : null) ??
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
  const sourceProfileSummary = formatSourceProfileSummary(sessionPlaybackTrace?.source);
  const ffmpegPlanSummary = formatFfmpegPlanSummary(sessionPlaybackTrace?.ffmpegPlan);
  const runtimeDiagnosticsSummary = formatRuntimeDiagnosticsSummary(sessionPlaybackTrace?.runtimeDiagnostics);
  const sourceWarningsSummary = formatSourceWarningsSummary(sessionPlaybackTrace?.runtimeDiagnostics);
  const clientSummary = formatTraceClientSummary(sessionPlaybackTrace?.client, capabilitySnapshot);
  const clientDeviceSummary = formatTraceClientDeviceSummary(sessionPlaybackTrace?.client, capabilitySnapshot);
  const autoCodecSummary = formatAutoCodecSummary(sessionPlaybackTrace);
  const firstFrameLabel = formatFirstFrameLabel(sessionPlaybackTrace?.firstFrameAtMs);
  const fallbackSummary = formatFallbackSummary(sessionPlaybackTrace);
  const stopSummary = formatStopSummary(sessionPlaybackTrace);
  const hostPressureSummary = formatHostPressureSummary(effectiveHostPressureBand, effectiveHostOverrideApplied);

  return {
    effectiveClientPath,
    effectiveSessionId,
    effectiveRequestProfile,
    effectiveRequestedIntent,
    effectiveResolvedIntent,
    effectiveQualityRung,
    effectiveAudioQualityRung,
    effectiveVideoQualityRung,
    effectiveDegradedFrom,
    effectiveTargetProfile,
    effectiveTargetProfileHash,
    effectiveHostPressureBand,
    effectiveHostOverrideApplied,
    effectiveForcedIntent,
    effectiveOperatorRuleName,
    effectiveOperatorRuleScope,
    effectiveRuntimePolicyAction,
    effectiveRuntimePolicyConstraints,
    effectiveRuntimePolicyReplay,
    effectiveRuntimePolicyReasons,
    effectiveRuntimePolicyTimeline,
    effectiveRuntimeProbeFailureStreak,
    effectiveRuntimeProbeSuccessStreak,
    effectiveClientFallbackDisabled,
    effectiveOperatorOverrideApplied,
    runtimePolicyReasonsSummary,
    runtimePolicyConstraintsSummary,
    runtimeProbeTrustSummary,
    sourceProfileSummary,
    ffmpegPlanSummary,
    runtimeDiagnosticsSummary,
    sourceWarningsSummary,
    clientSummary,
    clientDeviceSummary,
    autoCodecSummary,
    firstFrameLabel,
    fallbackSummary,
    stopSummary,
    hostPressureSummary,
    selectedOutputKind: playbackObservability?.selectedOutputKind ?? null,
  };
}
