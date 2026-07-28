import { describe, expect, it } from 'vitest';

import {
  resolveRuntimePolicyErrorSupport,
  resolveRuntimePolicyStartupSupport,
  resolveStartupOverlayLabel,
  resolveStartupOverlaySupport,
} from './startupOverlayLabel';

describe('resolveStartupOverlayLabel', () => {
  const t = (key: string) => {
    const messages: Record<string, string> = {
      'player.startupHints.safariCompatTranscode': 'Preparing Safari-compatible stream…',
      'player.startupHints.repairTranscode': 'Repairing and transcoding stream…',
      'player.startupHints.transcodeStartup': 'Preparing transcoded stream…',
      'player.startupHints.recovering': 'Reconnecting the stream…',
      'player.startupSupport.safariCompatTranscode': 'Safari needs a compatible stream variant first. This can take a little longer.',
      'player.startupSupport.default': 'Playback starts automatically as soon as the first stable segments are ready.',
      'player.startupSupport.recovering': 'The stream hit a snag and is being restored automatically.',
      'player.runtimePolicySupport.startup.probing': 'Testing {{profile}} briefly. If it stays stable, playback will continue there.',
      'player.runtimePolicySupport.error.cooldown': 'A short cooldown is active before another profile change is allowed.',
    };
    return messages[key] ?? key;
  };

  it('returns the Safari-specific hint when the backend flags Safari compatibility transcoding', () => {
    expect(resolveStartupOverlayLabel('priming', 'Preparing playback…', 'safari_compat_transcode', t as any)).toBe(
      'Preparing Safari-compatible stream…',
    );
  });

  it('falls back to the generic label when no specific profile reason exists', () => {
    expect(resolveStartupOverlayLabel('starting', 'Starting…', null, t as any)).toBe('Starting…');
  });

  it('returns the Safari-specific support copy for the startup card', () => {
    expect(resolveStartupOverlaySupport('safari_compat_transcode', t as any)).toBe(
      'Safari needs a compatible stream variant first. This can take a little longer.',
    );
  });

  it('returns the generic support copy when no specific profile reason exists', () => {
    expect(resolveStartupOverlaySupport(null, t as any)).toBe(
      'Playback starts automatically as soon as the first stable segments are ready.',
    );
  });

  // Regression: the `recovering` state (decode/stall reattach — the dominant
  // HEVC failure path) previously fell through the gate and returned '',
  // leaving the user with a frozen/black frame and no overlay text.
  it('returns an explicit hint for the recovering state instead of an empty label', () => {
    expect(resolveStartupOverlayLabel('recovering', 'Recovering…', null, t as any)).toBe(
      'Reconnecting the stream…',
    );
  });

  it('prefers the repair-transcode hint when recovering into a repair profile', () => {
    expect(resolveStartupOverlayLabel('recovering', 'Recovering…', 'repair_transcode', t as any)).toBe(
      'Repairing and transcoding stream…',
    );
  });

  it('returns recovering-specific support copy for the recovering state', () => {
    expect(resolveStartupOverlaySupport(null, t as any, 'recovering')).toBe(
      'The stream hit a snag and is being restored automatically.',
    );
  });
});

describe('runtime policy support copy', () => {
  const t = (key: string, options?: Record<string, unknown>) => {
    const messages: Record<string, string> = {
      'player.runtimePolicySupport.startup.probing': 'Testing {{profile}} briefly. If it stays stable, playback will continue there.',
      'player.runtimePolicySupport.error.cooldown': 'A short cooldown is active before another profile change is allowed.',
      'player.runtimePolicySupport.genericProfile': 'the current profile',
    };
    const message = messages[key] ?? key;
    return message.replace('{{profile}}', String(options?.profile ?? ''));
  };

  it('returns startup support for probing with the hinted profile', () => {
    expect(resolveRuntimePolicyStartupSupport('probing', 'compatible', t as any)).toBe(
      'Testing compatible briefly. If it stays stable, playback will continue there.',
    );
  });

  it('returns an empty startup support string for stable state', () => {
    expect(resolveRuntimePolicyStartupSupport('stable', 'compatible', t as any)).toBe('');
  });

  it('returns error support for cooldown state', () => {
    expect(resolveRuntimePolicyErrorSupport('cooldown', null, t as any)).toBe(
      'A short cooldown is active before another profile change is allowed.',
    );
  });
});
