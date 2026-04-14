import { describe, expect, it } from 'vitest';

import { resolveStartupOverlayLabel, resolveStartupOverlaySupport } from './startupOverlayLabel';

describe('resolveStartupOverlayLabel', () => {
  const t = (key: string) => {
    const messages: Record<string, string> = {
      'player.startupHints.safariCompatTranscode': 'Preparing Safari-compatible stream…',
      'player.startupHints.repairTranscode': 'Repairing and transcoding stream…',
      'player.startupHints.transcodeStartup': 'Preparing transcoded stream…',
      'player.startupSupport.safariCompatTranscode': 'Safari needs a compatible stream variant first. This can take a little longer.',
      'player.startupSupport.default': 'Playback starts automatically as soon as the first stable segments are ready.',
    };
    return messages[key] ?? key;
  };

  it('returns the Safari-specific hint when the backend flags Safari compatibility transcoding', () => {
    expect(resolveStartupOverlayLabel('priming', 'Preparing playback…', 'safari_compat_transcode', 'unknown', 'unknown', t as any)).toBe(
      'Preparing Safari-compatible stream…',
    );
  });

  it('falls back to the generic label when no specific profile reason exists', () => {
    expect(resolveStartupOverlayLabel('starting', 'Starting…', null, 'unknown', 'unknown', t as any)).toBe('Starting…');
  });

  it('returns the Safari-specific support copy for the startup card', () => {
    expect(resolveStartupOverlaySupport('safari_compat_transcode', 'unknown', 'unknown', t as any)).toBe(
      'Safari needs a compatible stream variant first. This can take a little longer.',
    );
  });

  it('returns the generic support copy when no specific profile reason exists', () => {
    expect(resolveStartupOverlaySupport(null, 'unknown', 'unknown', t as any)).toBe(
      'Playback starts automatically as soon as the first stable segments are ready.',
    );
  });
});
