import { describe, it, expect } from 'vitest';
import { formatDvrPositionDisplay } from './dvrPositionDisplay';

// A fake translate that mimics i18next interpolation of the defaultValue, so the
// test asserts the composed wording, not a translation table.
const fakeT = (_key: string, opts: Record<string, unknown>): string => {
  let out = String(opts.defaultValue ?? '');
  for (const [k, v] of Object.entries(opts)) {
    if (k === 'defaultValue') continue;
    out = out.replace(new RegExp(`{{${k}}}`, 'g'), String(v));
  }
  return out;
};

const clock = (s: number): string => {
  const m = Math.floor(s / 60);
  const sec = Math.floor(s % 60);
  return `${m}:${String(sec).padStart(2, '0')}`;
};

describe('formatDvrPositionDisplay', () => {
  it('shows the offset behind live when scrubbed back', () => {
    const out = formatDvrPositionDisplay(
      { isLiveMode: true, isAtLiveEdge: false, behindLiveSeconds: 420, currentTimeDisplay: '14:23' },
      clock,
      fakeT,
    );
    expect(out).toBe('14:23 · 7:00 behind live');
  });

  it('shows Live (no offset) at the live edge — negative control for the offset branch', () => {
    const out = formatDvrPositionDisplay(
      { isLiveMode: true, isAtLiveEdge: true, behindLiveSeconds: 0, currentTimeDisplay: '14:30' },
      clock,
      fakeT,
    );
    expect(out).toBe('14:30 · Live');
    expect(out).not.toMatch(/behind live/);
  });

  it('treats a tiny offset within the edge slack as Live', () => {
    const out = formatDvrPositionDisplay(
      { isLiveMode: true, isAtLiveEdge: false, behindLiveSeconds: 3, currentTimeDisplay: '14:30' },
      clock,
      fakeT,
    );
    expect(out).toBe('14:30 · Live');
  });

  it('for VOD shows the elapsed position only (no live wording)', () => {
    const out = formatDvrPositionDisplay(
      { isLiveMode: false, isAtLiveEdge: false, behindLiveSeconds: 0, currentTimeDisplay: '12:34' },
      clock,
      fakeT,
    );
    expect(out).toBe('12:34');
    expect(out).not.toMatch(/Live/);
  });
});
