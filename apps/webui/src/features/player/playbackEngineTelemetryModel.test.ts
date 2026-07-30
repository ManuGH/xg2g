import { describe, expect, it } from 'vitest';
import {
  extractHlsHttpStatus,
  isNonFatalHlsStallDetail,
  playbackRecoveryInfoForWarning,
  PLAYBACK_WARNING_CODE_DECODER_RECOVERY,
  PLAYBACK_WARNING_CODE_NETWORK_RETRY,
  PLAYBACK_WARNING_CODE_STALLED,
  PLAYBACK_WARNING_CODE_WAITING,
} from './playbackEngineTelemetryModel';

describe('playbackEngineTelemetryModel', () => {
  it.each([
    [PLAYBACK_WARNING_CODE_WAITING, 211, 'recovered_buffering'],
    [PLAYBACK_WARNING_CODE_STALLED, 211, 'recovered_buffering'],
    [PLAYBACK_WARNING_CODE_NETWORK_RETRY, 212, 'recovered_network'],
    [PLAYBACK_WARNING_CODE_DECODER_RECOVERY, 213, 'recovered_decoder'],
  ])('maps warning code %i to its recovery event', (warningCode, recoveryCode, message) => {
    expect(playbackRecoveryInfoForWarning(warningCode)).toEqual({
      code: recoveryCode,
      message,
    });
  });

  it('does not invent recovery telemetry for informational or unknown warning codes', () => {
    expect(playbackRecoveryInfoForWarning(105)).toBeNull();
    expect(playbackRecoveryInfoForWarning(999)).toBeNull();
  });

  it.each([
    'bufferStalledError',
    'bufferSeekOverHole',
    'bufferNudgeOnStall',
    'bufferAppendError',
  ])('recognizes %s as a self-recovering HLS stall detail', (details) => {
    expect(isNonFatalHlsStallDetail(details)).toBe(true);
  });

  it.each(['fragLoadError', '', undefined, null, 102])(
    'does not classify %s as a self-recovering HLS stall detail',
    (details) => {
      expect(isNonFatalHlsStallDetail(details)).toBe(false);
    },
  );

  it('reads HLS HTTP status fields in the existing priority order', () => {
    expect(extractHlsHttpStatus({
      response: { code: 401, status: 402 },
      networkDetails: { status: 403 },
    })).toBe(401);
    expect(extractHlsHttpStatus({
      response: { status: 404 },
      networkDetails: { status: 405 },
    })).toBe(404);
    expect(extractHlsHttpStatus({
      networkDetails: { status: 503 },
    })).toBe(503);
  });

  it('ignores missing and non-numeric HLS HTTP status fields', () => {
    expect(extractHlsHttpStatus({ response: { code: '401' } })).toBeUndefined();
    expect(extractHlsHttpStatus({})).toBeUndefined();
    expect(extractHlsHttpStatus(null)).toBeUndefined();
  });
});
