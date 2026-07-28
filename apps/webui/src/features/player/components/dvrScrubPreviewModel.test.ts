import { describe, it, expect } from 'vitest';
import {
  clampFraction,
  previewOffsetForFraction,
  dvrPreviewImageUrl,
  formatPreviewClock,
  previewHoverLabel,
} from './dvrScrubPreviewModel';

describe('clampFraction', () => {
  it('clamps into 0..1 and maps NaN to 0', () => {
    expect(clampFraction(-0.5)).toBe(0);
    expect(clampFraction(1.5)).toBe(1);
    expect(clampFraction(0.42)).toBe(0.42);
    expect(clampFraction(Number.NaN)).toBe(0);
  });
});

describe('previewOffsetForFraction', () => {
  it('rounds down to the segment grid', () => {
    // window 300s, 6s segments. fraction 0.5 -> 150s -> floor to 150 (25*6).
    expect(previewOffsetForFraction(0.5, 300, 6)).toBe(150);
    // 0.51 -> 153 -> floor to 150
    expect(previewOffsetForFraction(0.51, 300, 6)).toBe(150);
    // 0.52 -> 156 -> exactly 156
    expect(previewOffsetForFraction(0.52, 300, 6)).toBe(156);
  });

  it('clamps the ends and tolerates bad inputs (negative control)', () => {
    expect(previewOffsetForFraction(-1, 300, 6)).toBe(0);
    expect(previewOffsetForFraction(2, 300, 6)).toBe(300);
    expect(previewOffsetForFraction(0.5, 0, 6)).toBe(0); // no window
    expect(previewOffsetForFraction(0.5, 300, 0)).toBe(150); // seg<=0 falls back to 6
  });
});

describe('dvrPreviewImageUrl', () => {
  it('appends t with the right separator', () => {
    expect(dvrPreviewImageUrl('/api/v3/sessions/s1/hls/preview.jpg', 120))
      .toBe('/api/v3/sessions/s1/hls/preview.jpg?t=120');
    expect(dvrPreviewImageUrl('/x/preview.jpg?v=2', 5.6))
      .toBe('/x/preview.jpg?v=2&t=6');
  });
});

describe('formatPreviewClock', () => {
  it('formats m:ss and h:mm:ss; negative control for bad input', () => {
    expect(formatPreviewClock(5)).toBe('0:05');
    expect(formatPreviewClock(125)).toBe('2:05');
    expect(formatPreviewClock(3725)).toBe('1:02:05');
    expect(formatPreviewClock(-1)).toBe('0:00');
    expect(formatPreviewClock(Number.NaN)).toBe('0:00');
  });
});

describe('previewHoverLabel', () => {
  it('uses window-relative clock without an anchor', () => {
    expect(previewHoverLabel(125, null)).toBe('2:05');
    expect(previewHoverLabel(125, 0)).toBe('2:05');
  });

  it('uses wall-clock time-of-day with an anchor', () => {
    // 2021-01-01T00:00:00Z = 1609459200; +3600s -> 01:00 local. Assert HH:MM shape.
    const label = previewHoverLabel(3600, 1609459200);
    expect(label).toMatch(/^\d{2}:\d{2}$/);
  });
});
