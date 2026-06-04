import { describe, expect, it } from 'vitest';
import { translatePlaybackReason } from './sessionReason';

// Minimal TFunction stub: resolves the keys we assert, echoes others.
const t = ((key: string) => {
  const table: Record<string, string> = {
    'player.reason.R_CANCELLED': 'The session was cancelled.',
    'player.reason.R_UPSTREAM_CORRUPT': 'This channel is currently unavailable or has no video.',
    'player.reason.R_UPSTREAM_SCRAMBLED': 'This channel is encrypted and could not be descrambled.',
    'player.reason.unknown': 'Playback failed.',
  };
  return table[key] ?? key;
}) as unknown as Parameters<typeof translatePlaybackReason>[2];

describe('translatePlaybackReason', () => {
  it('maps a known reason code to localized text, never the raw token', () => {
    const out = translatePlaybackReason('R_UPSTREAM_SCRAMBLED', 'upstream stream is scrambled', t);
    expect(out).toBe('This channel is encrypted and could not be descrambled.');
    expect(out).not.toContain('R_UPSTREAM_SCRAMBLED');
  });

  it('falls back to the server reasonDetail for an untranslated code', () => {
    expect(translatePlaybackReason('R_SOME_NEW_CODE', 'a specific server explanation', t)).toBe(
      'a specific server explanation',
    );
  });

  it('falls back to the raw code when no detail is available', () => {
    expect(translatePlaybackReason('R_SOME_NEW_CODE', undefined, t)).toBe('R_SOME_NEW_CODE');
  });

  it('maps additionally added codes (R_CANCELLED, R_UPSTREAM_CORRUPT)', () => {
    expect(translatePlaybackReason('R_CANCELLED', undefined, t)).toBe('The session was cancelled.');
    expect(translatePlaybackReason('R_UPSTREAM_CORRUPT', undefined, t)).toBe(
      'This channel is currently unavailable or has no video.',
    );
  });

  it('maps R_UPSTREAM_CORRUPT and never leaks the raw token', () => {
    const out = translatePlaybackReason('R_UPSTREAM_CORRUPT', 'upstream corrupt', t);
    expect(out).toBe('This channel is currently unavailable or has no video.');
    expect(out).not.toContain('R_UPSTREAM_CORRUPT');
  });

  it('returns a generic message when nothing is available', () => {
    expect(translatePlaybackReason(undefined, undefined, t)).toBe('Playback failed.');
  });
});
