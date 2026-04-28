import { describe, expect, it } from 'vitest';
import type { PlaybackTargetProfile } from '../../../client-ts';

import {
  formatBooleanLabel,
  formatExecutionLabel,
  formatQualityRungLabel,
  formatTargetProfileSummary,
} from './playerRuntimeMetaFormat';

const targetProfile: PlaybackTargetProfile = {
  container: 'mp4',
  packaging: 'hls',
  video: {
    mode: 'transcode',
    codec: 'h264',
    crf: 23,
    preset: 'veryfast',
    width: 1920,
    height: 1080,
    fps: 25,
  },
  audio: {
    mode: 'transcode',
    codec: 'aac',
    channels: 2,
    bitrateKbps: 192,
    sampleRate: 48000,
  },
  hls: {
    enabled: true,
    segmentContainer: 'fmp4',
    segmentSeconds: 4,
  },
  hwAccel: 'vaapi',
};

describe('playerRuntimeMetaFormat', () => {
  it('formats quality rung ids for display only', () => {
    expect(formatQualityRungLabel(null)).toBe('-');
    expect(formatQualityRungLabel('quality_audio_aac_320_stereo')).toBe('quality audio aac 320 stereo');
  });

  it('formats boolean values as human-readable labels', () => {
    expect(formatBooleanLabel(true)).toBe('yes');
    expect(formatBooleanLabel(false)).toBe('no');
  });

  it('summarizes backend target profile fields without deciding policy', () => {
    expect(formatTargetProfileSummary(targetProfile)).toBe(
      'hls · v:transcode/h264/crf23/veryfast · a:transcode/aac/2ch@192k'
    );
  });

  it('falls back to CPU execution display when no hardware acceleration is reported', () => {
    expect(formatExecutionLabel(null)).toBe('CPU');
    expect(formatExecutionLabel({ ...targetProfile, hwAccel: 'none' })).toBe('CPU');
    expect(formatExecutionLabel(targetProfile)).toBe('VAAPI');
  });
});
