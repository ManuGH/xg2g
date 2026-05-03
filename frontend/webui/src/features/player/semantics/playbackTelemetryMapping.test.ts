import { describe, expect, it } from 'vitest';
import { mapPlaybackAdvisoryToTelemetryEvents, mapPlaybackFailureToTelemetryEvents } from './playbackTelemetryMapping';
import type { PlaybackFailure } from '../orchestrator/playbackTypes';

function buildFailure(overrides: Partial<PlaybackFailure>): PlaybackFailure {
  return {
    class: 'contract',
    code: 'missing_output_url',
    message: 'Backend decision missing selectedOutputUrl.',
    terminal: true,
    retryable: false,
    recoverable: false,
    userVisible: true,
    policyImpact: 'blocked',
    source: 'backend',
    messageKey: null,
    appError: {
      title: 'Server error',
      retryable: false,
      code: 'missing_output_url',
      requestId: 'req-1',
    },
    status: null,
    telemetryContext: 'V3Player.recording.contract.blocked',
    telemetryReason: null,
    ...overrides,
  };
}

describe('playbackTelemetryMapping', () => {
  it('maps contract failures to semantic and legacy fail-closed telemetry', () => {
    const events = mapPlaybackFailureToTelemetryEvents(buildFailure({}));

    expect(events).toEqual([
      expect.objectContaining({ type: 'playback_contract_blocked' }),
      expect.objectContaining({
        type: 'ui.failclosed',
        payload: {
          context: 'V3Player.recording.contract.blocked',
          reason: 'missing_output_url',
        },
      }),
    ]);
  });

  it('maps session failures separately from media failures', () => {
    const sessionEvents = mapPlaybackFailureToTelemetryEvents(buildFailure({
      class: 'session',
      code: 'SESSION_EXPIRED',
      message: 'Session expired',
      retryable: true,
      recoverable: true,
      terminal: false,
      source: 'native-host',
      telemetryContext: null,
    }));
    const mediaEvents = mapPlaybackFailureToTelemetryEvents(buildFailure({
      class: 'media',
      code: 'MEDIA_ELEMENT_ERROR',
      message: 'Playback error',
      retryable: true,
      recoverable: true,
      terminal: false,
      source: 'media-element',
      telemetryContext: null,
    }));

    expect(sessionEvents[0]?.type).toBe('playback_session_failed');
    expect(mediaEvents[0]?.type).toBe('playback_media_error');
    expect(sessionEvents[0]?.type).not.toBe(mediaEvents[0]?.type);
  });

  it('keeps advisories advisory in telemetry', () => {
    const events = mapPlaybackAdvisoryToTelemetryEvents({
      class: 'advisory',
      code: 'legacy_seekable_field',
      message: 'Using deprecated seekable field as the seekability source of truth.',
      source: 'normalizer',
      terminal: false,
      retryable: false,
      recoverable: false,
      userVisible: false,
      policyImpact: 'none',
    });

    expect(events).toEqual([
      {
        type: 'playback_advisory',
        payload: {
          code: 'legacy_seekable_field',
          message: 'Using deprecated seekable field as the seekability source of truth.',
          source: 'normalizer',
          policyImpact: 'none',
          terminal: false,
        },
      },
    ]);
  });
});
