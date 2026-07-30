export const PLAYBACK_WARNING_CODE_WAITING = 101;
export const PLAYBACK_WARNING_CODE_STALLED = 102;
export const PLAYBACK_WARNING_CODE_DECODER_RECOVERY = 103;
export const PLAYBACK_WARNING_CODE_NETWORK_RETRY = 104;
export const PLAYBACK_WARNING_CODE_HLS_NONFATAL = 105;
export const PLAYBACK_WARNING_CODE_HLS_LEADGAP = 106;

const PLAYBACK_INFO_CODE_RECOVERED_BUFFERING = 211;
const PLAYBACK_INFO_CODE_RECOVERED_NETWORK = 212;
const PLAYBACK_INFO_CODE_RECOVERED_DECODER = 213;

// hls.js self-recovers from these non-fatal events. They still describe a
// user-visible live stall or rough cold-start and therefore belong in telemetry.
const NON_FATAL_STALL_DETAILS = new Set([
  'bufferStalledError',
  'bufferSeekOverHole',
  'bufferNudgeOnStall',
  'bufferAppendError',
]);

export interface PlaybackRecoveryInfo {
  code: number;
  message: string;
}

export function playbackRecoveryInfoForWarning(code: number): PlaybackRecoveryInfo | null {
  switch (code) {
    case PLAYBACK_WARNING_CODE_WAITING:
    case PLAYBACK_WARNING_CODE_STALLED:
      return { code: PLAYBACK_INFO_CODE_RECOVERED_BUFFERING, message: 'recovered_buffering' };
    case PLAYBACK_WARNING_CODE_NETWORK_RETRY:
      return { code: PLAYBACK_INFO_CODE_RECOVERED_NETWORK, message: 'recovered_network' };
    case PLAYBACK_WARNING_CODE_DECODER_RECOVERY:
      return { code: PLAYBACK_INFO_CODE_RECOVERED_DECODER, message: 'recovered_decoder' };
    default:
      return null;
  }
}

export function isNonFatalHlsStallDetail(details: unknown): boolean {
  return typeof details === 'string' && NON_FATAL_STALL_DETAILS.has(details);
}

export function extractHlsHttpStatus(data: unknown): number | undefined {
  const statusCarrier = (data && typeof data === 'object' ? data : {}) as {
    response?: { code?: unknown; status?: unknown };
    networkDetails?: { status?: unknown };
  };
  const candidates = [
    statusCarrier.response?.code,
    statusCarrier.response?.status,
    statusCarrier.networkDetails?.status,
  ];

  return candidates.find((value): value is number => typeof value === 'number');
}
