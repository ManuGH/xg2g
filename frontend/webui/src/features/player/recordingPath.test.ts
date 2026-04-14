import { describe, expect, it } from 'vitest';

import { rewriteRecordingTimeshiftUrl, shouldUseProgressiveRecordingPath } from './recordingPath';

describe('recordingPath', () => {
  it('rewrites recording playlists to timeshift playlists', () => {
    expect(
      rewriteRecordingTimeshiftUrl('/api/v3/recordings/abc123/playlist.m3u8?variant=test')
    ).toBe('/api/v3/recordings/abc123/timeshift.m3u8?variant=test');
  });

  it('uses the progressive path only when the backend marks it ready', () => {
    expect(shouldUseProgressiveRecordingPath({
      streamUrl: '/api/v3/recordings/abc123/playlist.m3u8',
      isSeekable: true,
      recordingHlsEngine: 'native',
      progressiveReady: true,
    })).toBe(true);

    expect(shouldUseProgressiveRecordingPath({
      streamUrl: '/api/v3/recordings/abc123/playlist.m3u8',
      isSeekable: true,
      recordingHlsEngine: 'native',
      progressiveReady: false,
    })).toBe(false);
  });

  it('never uses the progressive path for non-native or non-seekable playback', () => {
    expect(shouldUseProgressiveRecordingPath({
      streamUrl: '/api/v3/recordings/abc123/playlist.m3u8',
      isSeekable: false,
      recordingHlsEngine: 'native',
      progressiveReady: true,
    })).toBe(false);

    expect(shouldUseProgressiveRecordingPath({
      streamUrl: '/api/v3/recordings/abc123/playlist.m3u8',
      isSeekable: true,
      recordingHlsEngine: 'hlsjs',
      progressiveReady: true,
    })).toBe(false);
  });
});
