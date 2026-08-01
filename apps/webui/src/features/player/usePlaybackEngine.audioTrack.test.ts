import { describe, expect, it } from 'vitest';
import { selectInitialHlsAudioTrack } from './usePlaybackEngine';

describe('selectInitialHlsAudioTrack', () => {
  it('selects the declared default when HLS.js has not selected an audio rendition', () => {
    expect(
      selectInitialHlsAudioTrack(-1, [
        { id: 4 },
        { id: 7, default: true },
      ]),
    ).toBe(7);
  });

  it('falls back to the first rendition when no default is declared', () => {
    expect(selectInitialHlsAudioTrack(-1, [{ id: 3 }, { id: 8 }])).toBe(3);
  });

  it('preserves an already selected rendition', () => {
    expect(selectInitialHlsAudioTrack(8, [{ id: 3 }, { id: 8, default: true }])).toBeNull();
  });

  it('does nothing when the playlist has no audio renditions', () => {
    expect(selectInitialHlsAudioTrack(-1, [])).toBeNull();
  });
});
