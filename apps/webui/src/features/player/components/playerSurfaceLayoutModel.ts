import type { TFunction } from 'i18next';

import type { UiSurfaceState } from '../../../lib/uiSurface';
import type { PlaybackWindowKind } from './playerPlaybackModel';
import { resolvePlaybackWindowKind } from './playerPlaybackModel';

type PlaybackMode = 'LIVE' | 'VOD' | 'UNKNOWN';

export interface BuildPlayerSurfaceLayoutModelInput {
  t: TFunction;
  uiSurface: Pick<UiSurfaceState, 'width' | 'heightClass' | 'inputMode' | 'orientation'>;
  isCompactTouchLayout: boolean;
  isRecordingPageLayout: boolean;
  isFullscreen: boolean;
  hasSeekWindow: boolean;
  hasTouchPlaybackInput: boolean;
  useOverlayShell: boolean;
  hasLiveDvrWindow: boolean;
  playbackMode: PlaybackMode;
  sessionWindowKind: PlaybackWindowKind;
  liveWindowLiveEdge: number | null | undefined;
  seekableStart: number;
  seekableEnd: number;
  currentPlaybackTime: number;
  isAtLiveEdge: boolean;
}

export interface PlayerSurfaceLayoutModel {
  useTheaterControlsLayout: boolean;
  useLiveDvrTouchFullscreenGuard: boolean;
  useMinimalTouchInlineChrome: boolean;
  useTheaterStackSurface: boolean;
  useCompactSurface: boolean;
  useTightSurface: boolean;
  disableInlineLiveDvrScrub: boolean;
  playbackWindowKind: PlaybackWindowKind;
  mobileInlinePlaybackLabel: string | null;
  liveWindowStateLabel: string;
}

export function buildPlayerSurfaceLayoutModel({
  t,
  uiSurface,
  isCompactTouchLayout,
  isRecordingPageLayout,
  isFullscreen,
  hasSeekWindow,
  hasTouchPlaybackInput,
  useOverlayShell,
  hasLiveDvrWindow,
  playbackMode,
  sessionWindowKind,
  liveWindowLiveEdge,
  seekableStart,
  seekableEnd,
  currentPlaybackTime,
  isAtLiveEdge,
}: BuildPlayerSurfaceLayoutModelInput): PlayerSurfaceLayoutModel {
  const useTheaterControlsLayout = Boolean(isRecordingPageLayout && !isFullscreen && hasSeekWindow);
  const useLiveDvrTouchFullscreenGuard = Boolean(
    hasTouchPlaybackInput &&
    useOverlayShell &&
    hasLiveDvrWindow &&
    !isFullscreen
  );
  const useMinimalTouchInlineChrome = Boolean(
    useOverlayShell &&
    !useTheaterControlsLayout &&
    !isFullscreen &&
    (isCompactTouchLayout || useLiveDvrTouchFullscreenGuard)
  );
  const useTheaterStackSurface = uiSurface.width < 1220;
  const useCompactSurface =
    uiSurface.width < 768 ||
    (uiSurface.inputMode === 'coarse' && uiSurface.heightClass !== 'comfortable');
  const useTightSurface =
    (uiSurface.width < 768 && uiSurface.orientation === 'landscape') ||
    uiSurface.heightClass !== 'comfortable';
  const inferredPlaybackWindowKind = resolvePlaybackWindowKind(playbackMode, hasLiveDvrWindow);
  const playbackWindowKind = sessionWindowKind !== 'unknown' ? sessionWindowKind : inferredPlaybackWindowKind;
  const mobileInlinePlaybackLabel = playbackWindowKind === 'live-dvr'
    ? t('player.mobilePlaybackWindowBadge.dvr', { defaultValue: 'DVR' })
    : playbackWindowKind === 'live'
      ? t('player.mobilePlaybackWindowBadge.live', { defaultValue: 'Live' })
      : playbackWindowKind === 'vod'
        ? t('player.mobilePlaybackWindowBadge.vod', { defaultValue: 'Replay' })
        : null;
  const liveWindowEdge = liveWindowLiveEdge ?? seekableEnd;
  const liveWindowLagSeconds = Math.max(0, Math.round(liveWindowEdge - currentPlaybackTime));
  const hasLiveWindowPlayhead = hasLiveDvrWindow && currentPlaybackTime >= Math.max(0, seekableStart - 1);
  const liveWindowStateLabel = !hasLiveDvrWindow
    ? '-'
    : !hasLiveWindowPlayhead
      ? t('player.liveWindowReady', { defaultValue: 'Window ready' })
      : isAtLiveEdge
        ? t('player.liveWindowAtEdge', { defaultValue: 'At live edge' })
        : t('player.liveWindowBehindEdge', {
            defaultValue: '{{seconds}}s behind live',
            seconds: liveWindowLagSeconds,
          });

  return {
    useTheaterControlsLayout,
    useLiveDvrTouchFullscreenGuard,
    useMinimalTouchInlineChrome,
    useTheaterStackSurface,
    useCompactSurface,
    useTightSurface,
    disableInlineLiveDvrScrub: useLiveDvrTouchFullscreenGuard,
    playbackWindowKind,
    mobileInlinePlaybackLabel,
    liveWindowStateLabel,
  };
}
