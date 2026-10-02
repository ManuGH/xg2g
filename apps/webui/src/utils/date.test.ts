import { describe, expect, it } from 'vitest';
import { formatLocalDateOnly } from './date';

describe('formatLocalDateOnly', () => {
  it('formats a date using local year, month, and day', () => {
    const d = new Date(2026, 11, 31, 23, 59, 59); // Dec 31, 2026 local
    expect(formatLocalDateOnly(d)).toBe('2026-12-31');
  });

  it('pads single-digit month and day with zero', () => {
    const d = new Date(2026, 0, 5, 12, 0, 0); // Jan 5, 2026 local
    expect(formatLocalDateOnly(d)).toBe('2026-01-05');
  });

  it('correctly formats dates constructed from UTC strings across timezones', () => {
    // A timestamp saved at local end-of-day
    const dateStr = '2026-12-31';
    const localEndOfDay = new Date(`${dateStr}T23:59:59`);
    const isoString = localEndOfDay.toISOString();

    // Loading it back using local parts round-trips to the same date string
    const loadedDate = new Date(isoString);
    expect(formatLocalDateOnly(loadedDate)).toBe(dateStr);
  });
});
