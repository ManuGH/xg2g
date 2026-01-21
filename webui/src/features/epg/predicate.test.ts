
import { describe, it, expect } from 'vitest';
import { isEventVisible } from './epgModel';

describe('EPG Visibility Predicate', () => {
  const now = 1000;
  const to = 2000; // now + 1000s

  it('should exclude past programs that ended before now', () => {
    const event = { start: 500, end: 999 };
    expect(isEventVisible(event, now, to)).toBe(false);
  });

  it('should exclude programs that end exactly at now', () => {
    const event = { start: 500, end: 1000 };
    expect(isEventVisible(event, now, to)).toBe(false);
  });

  it('should include programs overlapping now (started before, ending after now)', () => {
    const event = { start: 500, end: 1500 };
    expect(isEventVisible(event, now, to)).toBe(true);
  });

  it('should include programs starting exactly at now', () => {
    const event = { start: 1000, end: 1500 };
    expect(isEventVisible(event, now, to)).toBe(true);
  });

  it('should include future programs starting before to', () => {
    const event = { start: 1500, end: 2500 };
    expect(isEventVisible(event, now, to)).toBe(true);
  });

  it('should exclude future programs starting at or after to', () => {
    const event1 = { start: 2000, end: 2500 };
    const event2 = { start: 2100, end: 2500 };
    expect(isEventVisible(event1, now, to)).toBe(false); // Exclusive boundary
    expect(isEventVisible(event2, now, to)).toBe(false);
  });

  it('should handle All range (large to value)', () => {
    const maxTo = now + 336 * 3600;
    const veryFutureEvent = { start: now + 300 * 3600, end: now + 301 * 3600 };
    expect(isEventVisible(veryFutureEvent, now, maxTo)).toBe(true);
  });
});
