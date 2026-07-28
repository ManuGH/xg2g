import type { TFunction } from 'i18next';
import type {
  NormalizedBlockedPlaybackContract,
  NormalizedPlayablePlaybackContract,
} from '../contracts/normalizedPlaybackTypes';
import type { PlaybackContractState } from './playbackTypes';
import type { AppError } from '../../../types/errors';

export function buildContractState(
  kind: PlaybackContractState['kind'],
  contract: NormalizedPlayablePlaybackContract,
  streamUrl: string | null,
): PlaybackContractState {
  return {
    kind,
    requestId: contract.observability.requestId,
    mode: contract.playback.mode,
    streamUrl,
    canSeek: contract.playback.seekable,
    live: contract.playback.live,
    autoplayAllowed: contract.playback.autoplayAllowed,
    sessionRequired: contract.session.required,
    sessionId: contract.session.sessionId,
    expiresAt: contract.session.expiresAt,
    decisionToken: contract.session.decisionToken,
    durationSeconds: contract.media.durationSeconds,
    startUnix: contract.media.startUnix,
    mimeType: contract.media.mimeType,
  };
}

export function resolveContractFailureTitle(
  contract: NormalizedBlockedPlaybackContract,
  t: TFunction,
): string {
  switch (contract.failure.kind) {
    case 'auth':
      return t('player.authFailed');
    case 'session':
    case 'unavailable':
      return t('player.notAvailable');
    case 'unsupported':
      return contract.failure.code === 'playback_denied'
        ? t('player.playbackDenied')
        : t('player.serverError');
    case 'contract':
    default:
      return t('player.serverError');
  }
}

export function buildBlockedContractError(
  contract: NormalizedBlockedPlaybackContract,
  t: TFunction,
): AppError {
  const title = resolveContractFailureTitle(contract, t);
  const detailParts = [
    contract.failure.message,
    contract.observability.backendReason,
    contract.observability.requestId ? `requestId=${contract.observability.requestId}` : null,
  ].filter(Boolean);

  return {
    title,
    detail: detailParts.length > 0 ? detailParts.join(' · ') : undefined,
    retryable: contract.failure.retryable,
    code: contract.failure.code,
    requestId: contract.observability.requestId ?? undefined,
  };
}
