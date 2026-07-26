import { describe, expect, it } from 'vitest';
import {
  buildPlaybackProfileHeaders,
  normalizePlaybackProfileSelection,
  resolvePlaybackProfileForPreflight,
} from './playbackRequestProfile';

describe('buildPlaybackProfileHeaders', () => {
  it('emits the profile header only for the repair escape hatch', () => {
    expect(buildPlaybackProfileHeaders()).toEqual({});
    expect(buildPlaybackProfileHeaders('repair')).toEqual({
      'X-XG2G-Profile': 'repair',
    });
  });
});

describe('planner-bound profile selection', () => {
  it('keeps repair as the only client override', () => {
    expect(normalizePlaybackProfileSelection('repair')).toBe('repair');
  });

  it.each([
    'copy',
    'direct',
    'passthrough',
    'quality',
    'compatible',
    'high',
    'low',
    'bandwidth',
    'av1_hw',
    'hevc_hw',
    'h264_fmp4',
    'safari_hevc_hw',
  ])('migrates legacy client value %s back to auto', (value) => {
    expect(normalizePlaybackProfileSelection(value)).toBe('auto');
  });

  it('binds repair and sends no profile for automatic planning', () => {
    expect(resolvePlaybackProfileForPreflight('repair')).toBe('repair');
    expect(resolvePlaybackProfileForPreflight('auto')).toBeUndefined();
    expect(resolvePlaybackProfileForPreflight('bandwidth')).toBeUndefined();
  });
});
