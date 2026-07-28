import type { PlaybackInfo } from '../../../client-ts';
import type {
  NormalizePlaybackInfoContext,
  NormalizedAdvisoryWarning,
  NormalizedBlockedPlaybackContract,
  NormalizedContractFailure,
  NormalizedPlaybackContract,
  NormalizedPlaybackDecisionObservability,
  NormalizedPlaybackMode,
  NormalizedPlayablePlaybackContract,
} from './normalizedPlaybackTypes';

function asRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' ? value as Record<string, unknown> : null;
}

function readString(value: unknown): string | null {
  return typeof value === 'string' && value.trim().length > 0 ? value.trim() : null;
}

function readBoolean(value: unknown): boolean | null {
  return typeof value === 'boolean' ? value : null;
}

function readNumber(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null;
}

function readStringArray(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((entry): entry is string => typeof entry === 'string' && entry.length > 0)
    : [];
}

function inferMimeType(mode: NormalizedPlaybackMode, outputUrl: string | null): string | null {
  if (mode === 'direct_mp4') {
    return 'video/mp4';
  }
  if (outputUrl?.endsWith('.m3u8')) {
    return 'application/x-mpegURL';
  }
  if (mode === 'native_hls' || mode === 'hlsjs' || mode === 'transcode') {
    return 'application/x-mpegURL';
  }
  return null;
}

function buildBlockedContract(
  failure: NormalizedContractFailure,
  warnings: NormalizedAdvisoryWarning[],
  requestId: string | null,
  backendReason: string | null,
  decision: NormalizedPlaybackDecisionObservability | null,
): NormalizedBlockedPlaybackContract {
  return {
    kind: 'blocked',
    failure,
    observability: {
      requestId,
      backendReason,
      decision,
    },
    advisory: {
      warnings,
    },
  };
}

function buildFailure(
  kind: NormalizedContractFailure['kind'],
  code: string,
  message: string,
  retryable = false,
): NormalizedContractFailure {
  return {
    kind,
    code,
    message,
    retryable,
    terminal: !retryable,
  };
}

function extractResume(record: Record<string, unknown>): NormalizedPlayablePlaybackContract['resume'] {
  const resumeRecord = asRecord(record.resume);
  if (!resumeRecord) {
    return null;
  }

  const posSeconds = readNumber(resumeRecord.posSeconds);
  if (posSeconds === null) {
    return null;
  }

  const durationSeconds = readNumber(resumeRecord.durationSeconds) ?? undefined;
  const finished = readBoolean(resumeRecord.finished) ?? undefined;

  return {
    posSeconds,
    durationSeconds,
    finished,
  };
}

function resolveBackendReason(record: Record<string, unknown>, decisionRecord: Record<string, unknown> | null): string | null {
  const directReason = readString(record.decisionReason) ?? readString(record.reason);
  if (directReason) {
    return directReason;
  }
  const reasons = readStringArray(decisionRecord?.reasons);
  return reasons[0] ?? null;
}

function extractDecisionObservability(
  decisionRecord: Record<string, unknown> | null,
): NormalizedPlaybackDecisionObservability | null {
  if (!decisionRecord) {
    return null;
  }

  const traceRecord = asRecord(decisionRecord.trace);
  const targetProfile = asRecord(traceRecord?.targetProfile)
    ? traceRecord?.targetProfile as NormalizedPlaybackDecisionObservability['targetProfile']
    : null;
  const operator = asRecord(traceRecord?.operator)
    ? traceRecord?.operator as NormalizedPlaybackDecisionObservability['operator']
    : null;
  const selectedOutputKind = readString(decisionRecord.selectedOutputKind);

  return {
    requestProfile: readString(traceRecord?.requestProfile),
    requestedIntent: readString(traceRecord?.requestedIntent),
    resolvedIntent: readString(traceRecord?.resolvedIntent),
    qualityRung: readString(traceRecord?.qualityRung),
    audioQualityRung: readString(traceRecord?.audioQualityRung),
    videoQualityRung: readString(traceRecord?.videoQualityRung),
    degradedFrom: readString(traceRecord?.degradedFrom),
    hostPressureBand: readString(traceRecord?.hostPressureBand),
    hostOverrideApplied: readBoolean(traceRecord?.hostOverrideApplied) ?? false,
    targetProfileHash: readString(traceRecord?.targetProfileHash),
    targetProfile,
    operator,
    selectedOutputKind: selectedOutputKind === 'file' || selectedOutputKind === 'hls'
      ? selectedOutputKind
      : null,
  };
}

function resolveMode(
  rawMode: string | null,
  decisionMode: string | null,
  context: NormalizePlaybackInfoContext,
  warnings: NormalizedAdvisoryWarning[],
): NormalizedPlaybackMode | null {
  if (decisionMode === 'deny') {
    return null;
  }

  switch (rawMode) {
    case 'native_hls':
    case 'hlsjs':
    case 'direct_mp4':
    case 'transcode':
      return rawMode;
    case 'hls':
      warnings.push({
        code: 'legacy_hls_mode_mapped',
        message: `Mapped legacy hls mode to ${context.preferredHlsEngine}.`,
        source: 'normalizer',
      });
      return context.preferredHlsEngine === 'native' ? 'native_hls' : 'hlsjs';
    case 'direct_stream':
      warnings.push({
        code: 'legacy_direct_stream_mode_mapped',
        message: `Mapped legacy direct_stream mode to ${context.preferredHlsEngine}.`,
        source: 'normalizer',
      });
      return context.preferredHlsEngine === 'native' ? 'native_hls' : 'hlsjs';
    default:
      return null;
  }
}

function resolveSeekable(record: Record<string, unknown>, warnings: NormalizedAdvisoryWarning[]): boolean {
  const authoritative = readBoolean(record.isSeekable);
  if (authoritative !== null) {
    return authoritative;
  }

  const legacy = readBoolean(record.seekable);
  if (legacy !== null) {
    warnings.push({
      code: 'legacy_seekable_field',
      message: 'Using deprecated seekable field as the seekability source of truth.',
      source: 'normalizer',
    });
    return legacy;
  }

  warnings.push({
    code: 'missing_seekability_defaulted_false',
    message: 'Seekability was missing and has been normalized closed to false.',
    source: 'normalizer',
  });
  return false;
}

function resolveOutputUrl(
  decisionRecord: Record<string, unknown> | null,
): string | null {
  const selected = readString(decisionRecord?.selectedOutputUrl);
  if (selected) {
    return selected;
  }

  return null;
}

function hasBlockingDecision(decisionMode: string | null, rawMode: string | null): boolean {
  return decisionMode === 'deny' || rawMode === 'deny';
}

function isUsableOutputUrl(value: string | null): boolean {
  return Boolean(value && (value.startsWith('/') || value.startsWith('http://') || value.startsWith('https://')));
}

export function normalizePlaybackInfo(
  raw: unknown,
  context: NormalizePlaybackInfoContext,
): NormalizedPlaybackContract {
  const record = asRecord(raw);
  if (!record) {
    return buildBlockedContract(
      buildFailure('contract', 'invalid_payload', 'Playback contract payload was not an object.'),
      [],
      null,
      null,
      null,
    );
  }

  const decisionRecord = asRecord(record.decision);
  const warnings: NormalizedAdvisoryWarning[] = [];
  const requestId = readString(record.requestId);
  const backendReason = resolveBackendReason(record, decisionRecord);
  const decisionObservability = extractDecisionObservability(decisionRecord);
  const rawMode = readString(record.mode) ?? readString(decisionRecord?.mode);
  const decisionMode = readString(decisionRecord?.mode);

  if (!rawMode) {
    return buildBlockedContract(
      buildFailure('contract', 'missing_mode', 'Backend decision missing mode.'),
      warnings,
      requestId,
      backendReason,
      decisionObservability,
    );
  }

  if (hasBlockingDecision(decisionMode, rawMode)) {
    return buildBlockedContract(
      buildFailure('unsupported', 'playback_denied', 'Playback denied.'),
      warnings,
      requestId,
      backendReason,
      decisionObservability,
    );
  }

  const mode = resolveMode(rawMode, decisionMode, context, warnings);
  if (!mode) {
    return buildBlockedContract(
      buildFailure('contract', 'invalid_mode', `Unsupported backend playback mode: ${rawMode}`),
      warnings,
      requestId,
      backendReason,
      decisionObservability,
    );
  }

  if (context.surface === 'live' && mode === 'direct_mp4') {
    return buildBlockedContract(
      buildFailure('unsupported', 'unsupported_live_mode', 'Unsupported live playback mode: direct_mp4'),
      warnings,
      requestId,
      backendReason,
      decisionObservability,
    );
  }

  const outputUrl = resolveOutputUrl(decisionRecord);
  if (context.surface === 'recording' && !isUsableOutputUrl(outputUrl)) {
    return buildBlockedContract(
      buildFailure('contract', outputUrl ? 'invalid_output_url' : 'missing_output_url', outputUrl
        ? 'Backend decision returned an invalid selectedOutputUrl.'
        : 'Backend decision missing selectedOutputUrl.'),
      warnings,
      requestId,
      backendReason,
      decisionObservability,
    );
  }

  const sessionRequired = context.surface === 'live';
  const decisionToken = readString(record.playbackDecisionToken);
  if (sessionRequired && !decisionToken) {
    return buildBlockedContract(
      buildFailure('contract', 'missing_decision_token', 'Backend live decision missing playbackDecisionToken.'),
      warnings,
      requestId,
      backendReason,
      decisionObservability,
    );
  }

  const seekable = resolveSeekable(record, warnings);
  const sessionId = readString(record.sessionId);

  return {
    kind: 'playable',
    playback: {
      mode,
      outputUrl,
      seekable,
      live: sessionRequired,
      autoplayAllowed: true,
    },
    session: {
      required: sessionRequired,
      sessionId,
      expiresAt: null,
      decisionToken,
    },
    media: {
      mimeType: inferMimeType(mode, outputUrl),
      durationSeconds: readNumber(record.durationSeconds),
      startUnix: readNumber(record.startUnix),
      liveEdgeUnix: readNumber(record.liveEdgeUnix),
    },
    resume: extractResume(record),
    observability: {
      requestId,
      backendReason,
      decision: decisionObservability,
    },
    advisory: {
      warnings,
    },
  };
}

export function normalizeTypedPlaybackInfo(
  raw: PlaybackInfo,
  context: NormalizePlaybackInfoContext,
): NormalizedPlaybackContract {
  return normalizePlaybackInfo(raw, context);
}
