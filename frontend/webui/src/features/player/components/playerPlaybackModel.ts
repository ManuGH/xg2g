import type {
  PlaybackInfo,
  PlaybackTrace as PlaybackTraceContract,
  PlaybackTraceOperator,
  PlaybackTargetProfile,
} from '../../../client-ts';
import type { CapabilitySnapshot } from '../utils/playbackCapabilities';

export type PlaybackObservability = {
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

export type PlaybackWindowKind = 'live' | 'live-dvr' | 'vod' | 'unknown';

export function extractPlaybackTrace(value: unknown): PlaybackTraceContract | null {
  if (!value || typeof value !== 'object') {
    return null;
  }

  const record = value as Record<string, unknown>;
  const hasTraceMarker =
    'sessionId' in record ||
    'source' in record ||
    'targetProfileHash' in record ||
    'targetProfile' in record ||
    'ffmpegPlan' in record ||
    'stopReason' in record ||
    'stopClass' in record;
  if (hasTraceMarker) {
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

export function resolveMediaArtworkUrl(url: string | null | undefined): string | null {
  if (!url) return null;
  try {
    return new URL(url, typeof window !== 'undefined' ? window.location.href : 'http://localhost').toString();
  } catch {
    return null;
  }
}

export function resolveApiUrl(base: string, path: string): string {
  const normalizedBase = base.endsWith('/') ? base : `${base}/`;
  const normalizedPath = path.startsWith('/') ? path.slice(1) : path;
  return new URL(normalizedPath, new URL(normalizedBase, typeof window !== 'undefined' ? window.location.href : 'http://localhost')).toString();
}

export function resolveAutoTranscodeCodecs(snapshot: CapabilitySnapshot | null): string[] {
  if (!snapshot) return [];

  // The live stream-info request already carries the probed, policy-shaped
  // client codec list. Reusing it for the start intent keeps both requests in
  // sync and avoids silently dropping relaxed iOS-native AV1.
  const advertisedCodecs = Array.isArray(snapshot.videoCodecs)
    ? Array.from(
        new Set(
          snapshot.videoCodecs
            .map((codec) => codec.trim().toLowerCase())
            .filter((codec): codec is 'av1' | 'hevc' | 'h264' => (
              codec === 'av1' || codec === 'hevc' || codec === 'h264'
            ))
        )
      )
    : [];
  if (advertisedCodecs.length > 0) {
    return advertisedCodecs;
  }

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

export function resolvePlaybackWindowKind(
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

export function normalizePlaybackWindowKind(value: string | null | undefined): PlaybackWindowKind {
  switch (value) {
    case 'live':
    case 'live-dvr':
    case 'vod':
      return value;
    default:
      return 'unknown';
  }
}

export function mergePlaybackTraceOperator(
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
    runtimePolicyReplay: primary?.runtimePolicyReplay ?? fallback?.runtimePolicyReplay,
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

export function extractPlaybackObservability(
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

export function resolvePlaybackDurationSeconds(playbackInfo: PlaybackInfo): number | null {
  if (typeof playbackInfo.durationSeconds === 'number' && playbackInfo.durationSeconds > 0) {
    return playbackInfo.durationSeconds;
  }
  return null;
}
