import type {
  PlaybackClientSnapshot,
  PlaybackSourceProfile,
  PlaybackTrace as PlaybackTraceContract,
  PlaybackTraceFfmpegPlan,
  PlaybackTraceRuntimeDiagnostics,
  PlaybackTraceRuntimeTick,
} from '../../../client-ts';
import type { ChipState } from '../../../components/ui/StatusChip';
import type { CapabilitySnapshot } from '../utils/playbackCapabilities';

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

export function formatRuntimeDiagnosticsSummary(diagnostics: PlaybackTraceRuntimeDiagnostics | null | undefined): string {
  if (!diagnostics) return '-';
  const frame = typeof diagnostics.frameCount === 'number' ? `frame ${diagnostics.frameCount}` : null;
  const fps = typeof diagnostics.fps === 'number' && diagnostics.fps > 0
    ? `${diagnostics.fps.toFixed(1)}fps`
    : null;
  const speed = typeof diagnostics.speed === 'number' && diagnostics.speed > 0
    ? `${diagnostics.speed.toFixed(2)}x`
    : null;
  const drops =
    typeof diagnostics.dropFrames === 'number' || typeof diagnostics.dupFrames === 'number'
      ? `drop ${diagnostics.dropFrames ?? 0} / dup ${diagnostics.dupFrames ?? 0}`
      : null;
  return [frame, fps, speed, drops].filter(Boolean).join(' · ') || '-';
}

export function formatSourceWarningsSummary(diagnostics: PlaybackTraceRuntimeDiagnostics | null | undefined): string {
  if (!diagnostics) return '-';
  const corrupt = typeof diagnostics.corruptDecodedFrames === 'number' && diagnostics.corruptDecodedFrames > 0
    ? `corrupt decoded ${diagnostics.corruptDecodedFrames}`
    : null;
  const warning = diagnostics.lastWarning || null;
  return [corrupt, warning].filter(Boolean).join(' · ') || '-';
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

export function formatTraceClientSummary(
  client: PlaybackClientSnapshot | null | undefined,
  fallbackSnapshot: CapabilitySnapshot | null,
): string {
  const family = client?.clientFamily || fallbackSnapshot?.clientFamilyFallback || null;
  const source =
    client?.clientCapsSource ||
    (fallbackSnapshot?.runtimeProbeUsed
      ? 'runtime'
      : fallbackSnapshot?.clientFamilyFallback
        ? 'family_fallback'
        : null);
  const engine = client?.preferredHlsEngine || fallbackSnapshot?.preferredHlsEngine || null;
  const deviceType = client?.deviceType || fallbackSnapshot?.deviceType || null;
  return [family, source, engine, deviceType].filter(Boolean).join(' · ') || '-';
}

export function formatTraceClientDeviceSummary(
  client: PlaybackClientSnapshot | null | undefined,
  fallbackSnapshot: CapabilitySnapshot | null,
): string {
  const ctx = client?.deviceContext || fallbackSnapshot?.deviceContext || null;
  if (!ctx) return '-';
  const maker = ctx.manufacturer || ctx.brand || null;
  const model = ctx.model || ctx.product || ctx.device || null;
  const device = [maker, model].filter(Boolean).join(' ') || null;
  const os = [ctx.osName || null, ctx.osVersion || null].filter(Boolean).join(' ') || null;
  return [device, os, ctx.platform || null].filter(Boolean).join(' · ') || '-';
}

export function formatAutoCodecSummary(trace: PlaybackTraceContract | null | undefined): string {
  if (!trace) return '-';
  const selected = trace.autoCodecSelectedCodec || null;
  const requested = trace.autoCodecRequestedCodecs || null;
  const selection = selected && requested ? `${selected} <- ${requested}` : selected || requested;
  const host = trace.autoCodecPerformanceClass ? `host ${trace.autoCodecPerformanceClass}` : null;
  const bench = trace.autoCodecBenchmarkClass ? `bench ${trace.autoCodecBenchmarkClass}` : null;
  return [selection, trace.autoCodecPolicy || null, host, bench].filter(Boolean).join(' · ') || '-';
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

export function formatRuntimePolicyPhaseLabel(value: string | null | undefined): string {
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

export function resolveRuntimePolicyPhaseState(value: string | null | undefined): ChipState {
  switch (value) {
    case 'probing':
      return 'pending';
    case 'cooldown':
    case 'recovering':
      return 'warning';
    case 'probe_regressed':
    case 'degraded':
      return 'error';
    case 'stable':
      return 'success';
    default:
      return 'idle';
  }
}

export function formatRuntimeTimelineTime(value: string | null | undefined): string {
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

export function formatRuntimeTimelineEntry(entry: PlaybackTraceRuntimeTick): string {
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

export function resolveRuntimePolicyMetaHint(
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
