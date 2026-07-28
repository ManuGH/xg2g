import type { TFunction } from 'i18next';
import { describe, expect, it } from 'vitest';

import type { BuildPlayerSurfaceLayoutModelInput } from './playerSurfaceLayoutModel';
import { buildPlayerSurfaceLayoutModel } from './playerSurfaceLayoutModel';

const t = ((key: string, options?: Record<string, unknown>) => {
  const message = String(options?.defaultValue ?? key);
  return message.replace('{{seconds}}', String(options?.seconds ?? ''));
}) as unknown as TFunction;

const baseInput: BuildPlayerSurfaceLayoutModelInput = {
  t,
  uiSurface: {
    width: 1280,
    heightClass: 'comfortable',
    inputMode: 'fine',
    orientation: 'landscape',
  },
  isCompactTouchLayout: false,
  isRecordingPageLayout: false,
  isFullscreen: false,
  hasSeekWindow: true,
  hasTouchPlaybackInput: false,
  useOverlayShell: false,
  hasLiveDvrWindow: false,
  playbackMode: 'LIVE',
  sessionWindowKind: 'unknown',
  liveWindowLiveEdge: null,
  seekableStart: 0,
  seekableEnd: 120,
  currentPlaybackTime: 120,
  isAtLiveEdge: true,
};

type BuildOverrides = Partial<Omit<BuildPlayerSurfaceLayoutModelInput, 'uiSurface'>> & {
  uiSurface?: Partial<BuildPlayerSurfaceLayoutModelInput['uiSurface']>;
};

function build(overrides: BuildOverrides = {}) {
  return buildPlayerSurfaceLayoutModel({
    ...baseInput,
    ...overrides,
    uiSurface: {
      ...baseInput.uiSurface,
      ...overrides.uiSurface,
    },
  });
}

describe('buildPlayerSurfaceLayoutModel', () => {
  it('uses theater controls for recording page playback outside fullscreen', () => {
    const model = build({
      isRecordingPageLayout: true,
      hasSeekWindow: true,
    });

    expect(model.useTheaterControlsLayout).toBe(true);
    expect(build({ isRecordingPageLayout: true, isFullscreen: true }).useTheaterControlsLayout).toBe(false);
  });

  it('guards inline live DVR scrubbing on touch overlay surfaces', () => {
    const model = build({
      hasTouchPlaybackInput: true,
      useOverlayShell: true,
      hasLiveDvrWindow: true,
      isCompactTouchLayout: true,
    });

    expect(model.useLiveDvrTouchFullscreenGuard).toBe(true);
    expect(model.disableInlineLiveDvrScrub).toBe(true);
    expect(model.useMinimalTouchInlineChrome).toBe(true);
  });

  it('derives compact and tight surfaces from viewport state', () => {
    const smallLandscape = build({
      uiSurface: {
        width: 680,
        orientation: 'landscape',
      },
    });
    const coarseShort = build({
      uiSurface: {
        inputMode: 'coarse',
        heightClass: 'short',
      },
    });

    expect(smallLandscape.useCompactSurface).toBe(true);
    expect(smallLandscape.useTightSurface).toBe(true);
    expect(coarseShort.useCompactSurface).toBe(true);
    expect(coarseShort.useTightSurface).toBe(true);
    expect(build({ uiSurface: { width: 1190 } }).useTheaterStackSurface).toBe(true);
  });

  it('prefers session playback window kind and formats live DVR state', () => {
    expect(build({ playbackMode: 'VOD', sessionWindowKind: 'live-dvr' }).playbackWindowKind).toBe('live-dvr');
    expect(build({ playbackMode: 'VOD' }).mobileInlinePlaybackLabel).toBe('Replay');
    expect(build({ playbackMode: 'LIVE', hasLiveDvrWindow: true }).mobileInlinePlaybackLabel).toBe('DVR');
    expect(build({ playbackMode: 'UNKNOWN' }).mobileInlinePlaybackLabel).toBeNull();

    expect(build({
      hasLiveDvrWindow: true,
      currentPlaybackTime: 0,
      seekableStart: 10,
    }).liveWindowStateLabel).toBe('Window ready');

    expect(build({
      hasLiveDvrWindow: true,
      currentPlaybackTime: 88,
      liveWindowLiveEdge: 120,
      isAtLiveEdge: false,
    }).liveWindowStateLabel).toBe('32s behind live');

    expect(build({
      hasLiveDvrWindow: true,
      currentPlaybackTime: 120,
      isAtLiveEdge: true,
    }).liveWindowStateLabel).toBe('At live edge');
  });
});
