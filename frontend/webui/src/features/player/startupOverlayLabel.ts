import type { TFunction } from 'i18next';

import type {
  PlaybackPresentationKind,
  PlaybackTransportKind,
  PlayerStatus,
} from '../../types/v3-player';

export function resolveStartupOverlayLabel(
  status: PlayerStatus,
  fallbackLabel: string,
  profileReason: string | null | undefined,
  presentationKind: PlaybackPresentationKind,
  transportKind: PlaybackTransportKind,
  t: TFunction,
): string {
  if (status !== 'starting' && status !== 'priming' && status !== 'buffering' && status !== 'building') {
    return '';
  }

  switch (profileReason) {
    case 'safari_compat_transcode':
      return t('player.startupHints.safariCompatTranscode', {
        defaultValue: 'Preparing Safari-compatible stream…',
      });
    case 'repair_transcode':
      return t('player.startupHints.repairTranscode', {
        defaultValue: 'Repairing and transcoding stream…',
      });
    case 'transcode_startup':
      return t('player.startupHints.transcodeStartup', {
        defaultValue: 'Preparing transcoded stream…',
      });
    default:
      break;
  }

  switch (presentationKind) {
    case 'vod':
      return transportKind === 'progressive_hls'
        ? t('player.startupHints.recordingProgressiveHls', {
            defaultValue: 'Preparing recording for stable seeking…',
          })
        : t('player.startupHints.recordingStartup', {
            defaultValue: 'Opening recording…',
          });
    case 'live_dvr':
      return t('player.startupHints.liveDvrStartup', {
        defaultValue: 'Preparing live stream with timeshift…',
      });
    case 'direct':
      return t('player.startupHints.directSourceStartup', {
        defaultValue: 'Opening direct source…',
      });
    default:
      return fallbackLabel;
  }
}

export function resolveStartupOverlaySupport(
  profileReason: string | null | undefined,
  presentationKind: PlaybackPresentationKind,
  transportKind: PlaybackTransportKind,
  t: TFunction,
): string {
  switch (profileReason) {
    case 'safari_compat_transcode':
      return t('player.startupSupport.safariCompatTranscode', {
        defaultValue: 'Safari needs a compatible stream variant first. This can take a little longer.',
      });
    case 'repair_transcode':
      return t('player.startupSupport.repairTranscode', {
        defaultValue: 'The incoming stream is being stabilized before playback starts.',
      });
    case 'transcode_startup':
      return t('player.startupSupport.transcodeStartup', {
        defaultValue: 'The stream is being prepared for this device and will start automatically.',
      });
    default:
      break;
  }

  switch (presentationKind) {
    case 'vod':
      return transportKind === 'progressive_hls'
        ? t('player.startupSupport.recordingProgressiveHls', {
            defaultValue: 'This is a finished recording. Safari uses a progressive HLS path here so seeking and fast-forward stay stable.',
          })
        : t('player.startupSupport.recordingDefault', {
            defaultValue: 'This finished recording starts automatically as soon as the first stable segments are ready.',
          });
    case 'live_dvr':
      return t('player.startupSupport.liveDvrDefault', {
        defaultValue: 'The live stream is opened with a seekable window so you can jump within the current buffer.',
      });
    case 'direct':
      return t('player.startupSupport.directSourceDefault', {
        defaultValue: 'The direct source is opened without a live session bridge and starts as soon as the target device accepts it.',
      });
    default:
      return t('player.startupSupport.default', {
        defaultValue: 'Playback starts automatically as soon as the first stable segments are ready.',
      });
  }
}
