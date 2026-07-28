import type { TFunction } from 'i18next';
import { describe, expect, it } from 'vitest';

import { buildPlayerMediaSessionModel } from './playerMediaSessionModel';

const t = ((key: string, options?: Record<string, unknown>) => {
  if (key === 'player.loading') {
    return 'Loading';
  }
  return String(options?.defaultValue ?? key);
}) as unknown as TFunction;

describe('buildPlayerMediaSessionModel', () => {
  it('prefers current live program title while keeping channel as subtitle', () => {
    const model = buildPlayerMediaSessionModel({
      t,
      playbackMode: 'LIVE',
      liveProgramTitle: 'Tagesschau',
      channelName: 'Das Erste HD',
      channelLogoUrl: '/picons/daserste.png',
      normalizedRecordingTitle: '',
      recordingDateLabel: null,
    });

    expect(model).toEqual({
      title: 'Tagesschau',
      subtitle: 'Das Erste HD',
      artworkUrl: 'http://localhost:3000/picons/daserste.png',
    });
  });

  it('falls back to channel and app names for live playback without program metadata', () => {
    expect(buildPlayerMediaSessionModel({
      t,
      playbackMode: 'LIVE',
      liveProgramTitle: null,
      channelName: null,
      channelLogoUrl: null,
      normalizedRecordingTitle: '',
      recordingDateLabel: null,
    })).toEqual({
      title: 'Loading',
      subtitle: 'xg2g',
      artworkUrl: null,
    });
  });

  it('uses recording title with channel subtitle for VOD playback', () => {
    const model = buildPlayerMediaSessionModel({
      t,
      playbackMode: 'VOD',
      liveProgramTitle: 'Ignored live title',
      channelName: 'arte HD',
      channelLogoUrl: 'https://example.test/arte.png',
      normalizedRecordingTitle: 'Concert Night',
      recordingDateLabel: 'Friday',
    });

    expect(model).toEqual({
      title: 'Concert Night',
      subtitle: 'arte HD',
      artworkUrl: 'https://example.test/arte.png',
    });
  });

  it('uses recording date or app name when VOD has no channel subtitle', () => {
    expect(buildPlayerMediaSessionModel({
      t,
      playbackMode: 'VOD',
      liveProgramTitle: null,
      channelName: null,
      channelLogoUrl: '',
      normalizedRecordingTitle: 'Replay',
      recordingDateLabel: '2026-04-25',
    })).toMatchObject({
      title: 'Replay',
      subtitle: '2026-04-25',
      artworkUrl: null,
    });

    expect(buildPlayerMediaSessionModel({
      t,
      playbackMode: 'UNKNOWN',
      liveProgramTitle: null,
      channelName: null,
      channelLogoUrl: null,
      normalizedRecordingTitle: '',
      recordingDateLabel: null,
    })).toMatchObject({
      title: 'Loading',
      subtitle: 'xg2g',
    });
  });
});
