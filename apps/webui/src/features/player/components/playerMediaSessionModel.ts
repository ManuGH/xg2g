import type { TFunction } from 'i18next';

import { resolveMediaArtworkUrl } from './playerPlaybackModel';

type PlaybackMode = 'LIVE' | 'VOD' | 'UNKNOWN';

export interface BuildPlayerMediaSessionModelInput {
  t: TFunction;
  playbackMode: PlaybackMode;
  liveProgramTitle: string | null | undefined;
  channelName: string | null | undefined;
  channelLogoUrl: string | null | undefined;
  normalizedRecordingTitle: string;
  recordingDateLabel: string | null | undefined;
}

export interface PlayerMediaSessionModel {
  title: string;
  subtitle: string;
  artworkUrl: string | null;
}

export function buildPlayerMediaSessionModel({
  t,
  playbackMode,
  liveProgramTitle,
  channelName,
  channelLogoUrl,
  normalizedRecordingTitle,
  recordingDateLabel,
}: BuildPlayerMediaSessionModelInput): PlayerMediaSessionModel {
  const appName = t('common.appName', { defaultValue: 'xg2g' });
  const loadingTitle = t('player.loading');

  if (playbackMode === 'LIVE') {
    return {
      title: liveProgramTitle || channelName || loadingTitle,
      subtitle: channelName || appName,
      artworkUrl: resolveMediaArtworkUrl(channelLogoUrl || null),
    };
  }

  return {
    title: normalizedRecordingTitle || channelName || loadingTitle,
    subtitle: channelName && normalizedRecordingTitle
      ? channelName
      : recordingDateLabel || appName,
    artworkUrl: resolveMediaArtworkUrl(channelLogoUrl || null),
  };
}
