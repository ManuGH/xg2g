import type { TFunction } from 'i18next';

import type { PlayerStatus } from '../../types/v3-player';

export function resolveStartupOverlayLabel(
  status: PlayerStatus,
  fallbackLabel: string,
  profileReason: string | null | undefined,
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
      return fallbackLabel;
  }
}

export function resolveStartupOverlaySupport(
  profileReason: string | null | undefined,
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
      return t('player.startupSupport.default', {
        defaultValue: 'Playback starts automatically as soon as the first stable segments are ready.',
      });
  }
}
