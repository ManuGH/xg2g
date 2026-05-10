import type { TFunction } from 'i18next';
import type { IntentRequest, PlaybackTrace as PlaybackTraceContract } from '../../../client-ts';
import type { CapabilitySnapshot } from '../utils/playbackCapabilities';
import { resolveAutoTranscodeCodecs } from './observabilityFormatters';
import { extractCapHashFromDecisionToken } from '../utils/playerHelpers';
import type {
  NormalizedPlaybackContract,
  NormalizedPlayablePlaybackContract,
} from '../contracts/normalizedPlaybackTypes';
import type { AppError } from '../../../types/errors';
import type { PlaybackFailureReportOptions } from '../semantics/playbackFailureSemantics';
import { classifyNormalizedContractFailure } from '../semantics/playbackFailureSemantics';
import { buildBlockedContractError } from './contractErrors';
import type { ResumeState } from '../../resume/api';
import type { VodStreamMode } from './playbackTypes';

export interface ContractTelemetryPayload {
  mode: 'normalized';
  kind: NormalizedPlaybackContract['kind'];
  fields: string[];
}

export type ContractSurface = 'live' | 'recording';

const PLAYABLE_TELEMETRY_FIELDS: Record<ContractSurface, string[]> = {
  recording: ['kind', 'playback.mode', 'playback.outputUrl', 'playback.seekable'],
  live: ['kind', 'playback.mode', 'session.decisionToken'],
};

const BLOCKED_TELEMETRY_FIELDS = ['kind', 'failure.kind', 'failure.code'];

export function buildContractConsumedTelemetry(
  contract: NormalizedPlaybackContract,
  surface: ContractSurface,
): ContractTelemetryPayload {
  return {
    mode: 'normalized',
    kind: contract.kind,
    fields: contract.kind === 'playable'
      ? PLAYABLE_TELEMETRY_FIELDS[surface]
      : BLOCKED_TELEMETRY_FIELDS,
  };
}

export interface PreparedFailure {
  appError: AppError;
  options: PlaybackFailureReportOptions;
}

export function buildBlockedContractFailure(
  contract: NormalizedPlaybackContract & { kind: 'blocked' },
  surface: ContractSurface,
  t: TFunction,
): PreparedFailure {
  const blockedFailure = classifyNormalizedContractFailure(contract.failure);
  return {
    appError: buildBlockedContractError(contract, t),
    options: {
      source: 'backend',
      failureClass: blockedFailure.class,
      code: blockedFailure.code,
      retryable: blockedFailure.retryable,
      recoverable: blockedFailure.recoverable,
      terminal: blockedFailure.terminal,
      policyImpact: blockedFailure.policyImpact,
      telemetryContext: surface === 'live'
        ? 'V3Player.live.contract.blocked'
        : 'V3Player.recording.contract.blocked',
      telemetryReason: contract.observability.backendReason ?? contract.failure.code,
    },
  };
}

export function buildAuthDeniedFailure(t: TFunction, status: 401 | 403): PreparedFailure {
  const isUnauthorized = status === 401;
  return {
    appError: {
      title: isUnauthorized ? t('player.authFailed') : t('player.forbidden'),
      status,
      retryable: false,
      code: 'AUTH_DENIED',
    } as AppError,
    options: {
      source: 'backend',
      failureClass: 'auth',
      retryable: false,
      recoverable: false,
      terminal: true,
    },
  };
}

export function buildRecordingGoneFailure(t: TFunction): PreparedFailure {
  return {
    appError: {
      title: t('player.notAvailable'),
      status: 410,
      retryable: false,
      code: 'RECORDING_GONE',
    } as AppError,
    options: {
      source: 'backend',
      failureClass: 'contract',
      retryable: false,
      recoverable: false,
      terminal: true,
      telemetryContext: 'V3Player.recording.contract.blocked',
    },
  };
}

export function buildLeaseBusyFailure(retryAfterSeconds: number, t: TFunction): PreparedFailure {
  const retryHint = retryAfterSeconds > 0
    ? ` ${t('player.retryAfter', { seconds: retryAfterSeconds })}`
    : '';
  return {
    appError: {
      title: `${t('player.leaseBusy')}${retryHint}`,
      status: 409,
      retryable: true,
      code: 'LEASE_BUSY',
    } as AppError,
    options: {
      source: 'backend',
      failureClass: 'contract',
      retryable: true,
      recoverable: false,
      terminal: false,
      telemetryContext: 'V3Player.recording.contract.blocked',
    },
  };
}

export function buildServiceRefRequiredFailure(t: TFunction): PreparedFailure {
  return {
    appError: {
      title: t('player.serviceRefRequired'),
      retryable: false,
      code: 'SERVICE_REF_REQUIRED',
    } as AppError,
    options: {
      source: 'orchestrator',
      failureClass: 'contract',
      retryable: false,
      recoverable: false,
      terminal: true,
    },
  };
}

export function buildMissingOutputUrlFailure(t: TFunction): PreparedFailure {
  return {
    appError: {
      title: t('player.serverError'),
      detail: 'Normalized recording contract missing outputUrl',
      retryable: false,
      code: 'MISSING_OUTPUT_URL',
    } as AppError,
    options: {
      source: 'backend',
      failureClass: 'contract',
      retryable: false,
      recoverable: false,
      terminal: true,
      telemetryContext: 'V3Player.recording.output_url.missing',
    },
  };
}

export function buildMissingDecisionTokenFailure(t: TFunction): PreparedFailure {
  return {
    appError: {
      title: t('player.serverError'),
      detail: 'Backend live decision missing playbackDecisionToken',
      retryable: false,
      code: 'PLAYBACK_DECISION_TOKEN_MISSING',
    } as AppError,
    options: {
      source: 'backend',
      failureClass: 'contract',
      retryable: false,
      recoverable: false,
      terminal: true,
      telemetryContext: 'V3Player.live.playback_decision_token.missing',
    },
  };
}

export function buildUnsupportedLiveModeFailure(
  liveMode: VodStreamMode,
  t: TFunction,
): PreparedFailure {
  return {
    appError: {
      title: t('player.serverError'),
      detail: `Unsupported live playback mode: ${liveMode}`,
      retryable: false,
      code: 'UNSUPPORTED_LIVE_MODE',
    } as AppError,
    options: {
      source: 'backend',
      failureClass: 'contract',
      retryable: false,
      recoverable: false,
      terminal: true,
      telemetryContext: 'V3Player.live.mode.unsupported',
    },
  };
}

export type LiveEngineDecision =
  | { engine: 'native' | 'hlsjs' }
  | { unsupported: true };

export function resolveLiveEngineFromMode(
  liveMode: VodStreamMode,
  requestCaps: CapabilitySnapshot,
  resolvePreferredHlsEngineForCapabilities: (caps: CapabilitySnapshot) => 'native' | 'hlsjs',
): LiveEngineDecision {
  if (liveMode === 'native_hls') return { engine: 'native' };
  if (liveMode === 'hlsjs') return { engine: 'hlsjs' };
  if (liveMode === 'transcode') {
    return { engine: resolvePreferredHlsEngineForCapabilities(requestCaps) };
  }
  return { unsupported: true };
}

export function buildLiveIntentBody(
  serviceRef: string,
  decisionToken: string,
  requestCaps: CapabilitySnapshot,
  liveMode: NonNullable<VodStreamMode>,
): IntentRequest {
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
  const capHash = extractCapHashFromDecisionToken(decisionToken);
  if (capHash) {
    intentParams.capHash = capHash;
  }

  return {
    type: 'stream.start',
    serviceRef,
    playbackDecisionToken: decisionToken,
    client: requestCaps,
    ...(Object.keys(intentParams).length > 0 ? { params: intentParams } : {}),
  };
}

export function resolveResumeStateFromContract(
  contract: NormalizedPlayablePlaybackContract,
  durationSeconds: number | null,
): ResumeState | null {
  const { resume, playback, media } = contract;
  if (!playback.seekable) return null;
  if (!resume) return null;
  if (resume.posSeconds < 15) return null;
  if (resume.finished) return null;

  const effectiveDuration = resume.durationSeconds || media.durationSeconds || durationSeconds || 0;
  if (effectiveDuration && resume.posSeconds >= effectiveDuration - 10) return null;

  return {
    posSeconds: resume.posSeconds,
    durationSeconds: resume.durationSeconds || undefined,
    finished: resume.finished || undefined,
  };
}

export interface PrepareForAttemptArgs {
  hasActivePlayback: () => boolean;
  teardownActivePlayback: () => Promise<void>;
  clearPlaybackState: () => void;
  hasActiveNativeRequest?: boolean;
}

export function prepareForPlaybackAttempt({
  hasActivePlayback,
  teardownActivePlayback,
  clearPlaybackState,
  hasActiveNativeRequest = false,
}: PrepareForAttemptArgs): Promise<void> | null {
  if (hasActivePlayback() || hasActiveNativeRequest) {
    return teardownActivePlayback();
  }
  clearPlaybackState();
  return null;
}

export type { PlaybackTraceContract };
