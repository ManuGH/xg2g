import { describe, expect, it } from 'vitest';

import {
  resolveSeekKeyAction,
  resolveSeekProgressPercent,
  resolveSeekTargetFromPointer,
} from './playerSeekModel';

describe('playerSeekModel', () => {
  it('formats clamped seek progress percentages', () => {
    expect(resolveSeekProgressPercent(30, 120)).toBe('25%');
    expect(resolveSeekProgressPercent(-10, 120)).toBe('0%');
    expect(resolveSeekProgressPercent(180, 120)).toBe('100%');
    expect(resolveSeekProgressPercent(30, 0)).toBe('0%');
  });

  it('resolves pointer position into a clamped seek target', () => {
    expect(resolveSeekTargetFromPointer({
      clientX: 60,
      trackLeft: 10,
      trackWidth: 100,
      seekableStart: 20,
      windowDuration: 200,
    })).toBe(120);

    expect(resolveSeekTargetFromPointer({
      clientX: -20,
      trackLeft: 10,
      trackWidth: 100,
      seekableStart: 20,
      windowDuration: 200,
    })).toBe(20);

    expect(resolveSeekTargetFromPointer({
      clientX: 240,
      trackLeft: 10,
      trackWidth: 100,
      seekableStart: 20,
      windowDuration: 200,
    })).toBe(220);
  });

  it('returns null for invalid pointer seek geometry', () => {
    expect(resolveSeekTargetFromPointer({
      clientX: 60,
      trackLeft: 10,
      trackWidth: 0,
      seekableStart: 20,
      windowDuration: 200,
    })).toBeNull();

    expect(resolveSeekTargetFromPointer({
      clientX: 60,
      trackLeft: 10,
      trackWidth: 100,
      seekableStart: 20,
      windowDuration: 0,
    })).toBeNull();
  });

  it('maps keyboard scrub actions only when a seek window exists', () => {
    expect(resolveSeekKeyAction('ArrowLeft', 120)).toEqual({ type: 'seekBy', seconds: -15 });
    expect(resolveSeekKeyAction('ArrowRight', 120)).toEqual({ type: 'seekBy', seconds: 15 });
    expect(resolveSeekKeyAction('Home', 120)).toEqual({ type: 'seekToStart' });
    expect(resolveSeekKeyAction('End', 120)).toEqual({ type: 'seekToEnd' });
    expect(resolveSeekKeyAction('PageUp', 120)).toBeNull();
    expect(resolveSeekKeyAction('ArrowRight', 0)).toBeNull();
  });
});
