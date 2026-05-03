import type { TelemetryEventType } from '../../../services/TelemetryService';
import type { PlaybackFailure } from '../orchestrator/playbackTypes';
import type { PlaybackAdvisorySignal } from './playbackFailureSemantics';

type TelemetryDescriptor = {
  type: TelemetryEventType;
  payload: Record<string, unknown>;
};

function baseFailurePayload(failure: PlaybackFailure): Record<string, unknown> {
  return {
    class: failure.class,
    code: failure.code,
    message: failure.message,
    status: failure.status,
    source: failure.source,
    retryable: failure.retryable,
    recoverable: failure.recoverable,
    terminal: failure.terminal,
    policyImpact: failure.policyImpact,
    requestId: failure.appError?.requestId ?? null,
    telemetryReason: failure.telemetryReason ?? null,
  };
}

export function mapPlaybackFailureToTelemetryEvents(
  failure: PlaybackFailure,
): TelemetryDescriptor[] {
  const payload = baseFailurePayload(failure);

  switch (failure.class) {
    case 'auth':
      return [
        { type: 'playback_auth_blocked', payload },
        {
          type: 'ui.auth_error',
          payload: {
            status: failure.status,
            code: failure.code,
            requestId: failure.appError?.requestId ?? null,
          },
        },
      ];
    case 'session':
      return [
        { type: 'playback_session_failed', payload },
        {
          type: 'ui.error',
          payload: {
            status: failure.status,
            code: failure.code,
            requestId: failure.appError?.requestId ?? null,
          },
        },
      ];
    case 'contract':
      return [
        { type: 'playback_contract_blocked', payload },
        {
          type: 'ui.failclosed',
          payload: {
            context: failure.telemetryContext ?? 'playback.contract.blocked',
            reason: failure.telemetryReason ?? failure.code,
          },
        },
      ];
    case 'media':
      return [
        { type: 'playback_media_error', payload },
        {
          type: 'ui.error',
          payload: {
            status: failure.status,
            code: failure.code,
            requestId: failure.appError?.requestId ?? null,
          },
        },
      ];
    case 'advisory':
      return [
        { type: 'playback_advisory', payload },
      ];
    default:
      return [];
  }
}

export function mapPlaybackAdvisoryToTelemetryEvents(
  advisory: PlaybackAdvisorySignal,
): TelemetryDescriptor[] {
  return [
    {
      type: 'playback_advisory',
      payload: {
        code: advisory.code,
        message: advisory.message,
        source: advisory.source,
        policyImpact: advisory.policyImpact,
        terminal: advisory.terminal,
      },
    },
  ];
}
