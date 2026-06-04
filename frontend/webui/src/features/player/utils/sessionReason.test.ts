import { describe, expect, it } from 'vitest';
import { translatePlaybackReason } from './sessionReason';

// Minimal TFunction stub: resolves the keys we assert, echoes others.
const t = ((key: string) => {
  const table: Record<string, string> = {
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

  it('returns a generic message when nothing is available', () => {
    expect(translatePlaybackReason(undefined, undefined, t)).toBe('Playback failed.');
  });
});
