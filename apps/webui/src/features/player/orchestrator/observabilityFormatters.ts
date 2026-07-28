import type {
  PlaybackSourceProfile,
  PlaybackTrace as PlaybackTraceContract,
  PlaybackTraceFfmpegPlan,
  PlaybackTraceOperator,
  PlaybackTargetProfile,
} from '../../../client-ts';
import type { CapabilitySnapshot } from '../utils/playbackCapabilities';
import type { NormalizedPlaybackDecisionObservability } from '../contracts/normalizedPlaybackTypes';

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

export function formatSourceProfileSummary(source: PlaybackSourceProfile | null | undefined): string {
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

export function formatFfmpegPlanSummary(plan: PlaybackTraceFfmpegPlan | null | undefined): string {
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

export function formatFirstFrameLabel(firstFrameAtMs: number | null | undefined): string {
  if (!firstFrameAtMs || firstFrameAtMs <= 0) return '-';
  return new Date(firstFrameAtMs).toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  });
}

export function formatFallbackSummary(trace: PlaybackTraceContract | null | undefined): string {
  if (!trace) return '-';
  const count = typeof trace.fallbackCount === 'number' ? trace.fallbackCount : 0;
  const lastReason = trace.lastFallbackReason || null;
  if (count <= 0 && !lastReason) return '-';
  return [count > 0 ? String(count) : null, lastReason].filter(Boolean).join(' · ');
}

export function formatStopSummary(trace: PlaybackTraceContract | null | undefined): string {
  if (!trace) return '-';
  return [trace.stopClass || null, trace.stopReason || null].filter(Boolean).join(' · ') || '-';
}

export function formatHostPressureSummary(hostPressureBand: string | null, hostOverrideApplied: boolean): string {
  if (!hostPressureBand) return '-';
  return hostOverrideApplied ? `${hostPressureBand} · applied` : hostPressureBand;
}

export function extractPlaybackTrace(value: unknown): PlaybackTraceContract | null {
  if (!value || typeof value !== 'object') {
    return null;
  }

  const record = value as Record<string, unknown>;
  // A session/response wrapper nests the executed trace under `.trace`. Descend
  // FIRST: the wrapper itself also carries a top-level requestId + sessionId,
  // which would otherwise match the "looks like a trace" heuristic below and
  // return the wrapper (missing targetProfile / ffmpegPlan) — the bug that left
  // the live stats panel blank for the controller (native HLS) snapshot path.
  if ('trace' in record && record.trace) {
    const nested = extractPlaybackTrace(record.trace);
    if (nested) {
      return nested;
    }
  }
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

  if ('body' in record) {
    return extractPlaybackTrace(record.body);
  }
  if ('details' in record) {
    return extractPlaybackTrace(record.details);
  }

  return null;
}

export function formatClientPath(snapshot: CapabilitySnapshot | null): string {
  if (!snapshot) return '-';
  const preferred = snapshot.preferredHlsEngine ?? '-';
  const engines = snapshot.hlsEngines?.length ? snapshot.hlsEngines.join('/') : null;
  return engines ? `${preferred} (${engines})` : preferred;
}

export function formatRequestProfileLabel(profile: string | null): string {
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

export function resolveAutoTranscodeCodecs(snapshot: CapabilitySnapshot | null): string[] {
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

export function formatQualityRungLabel(rung: string | null): string {
  if (!rung) return '-';
  return rung.split('_').join(' ');
}

export function formatBooleanLabel(value: boolean): string {
  return value ? 'yes' : 'no';
}

export function formatTargetProfileSummary(target: PlaybackTargetProfile | null): string {
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

export function formatExecutionLabel(target: PlaybackTargetProfile | null): string {
  if (!target?.hwAccel || target.hwAccel === 'none') {
    return 'CPU';
  }
  return target.hwAccel.toUpperCase();
}

export function resolvePlaybackObservability(
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
