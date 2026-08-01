
import { describe, it, expect } from 'vitest';
import { channelMatchesQuery, isEventVisible } from './epgModel';

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

describe('channelMatchesQuery', () => {
  const channels = [
    { id: 'a', name: 'ORF 1', number: '1' },
    { id: 'b', name: 'ORF 2', number: '2' },
    { id: 'c', name: 'ServusTV HD', number: '3' },
    { id: 'd', name: 'Sky Cinema', number: '301' },
  ];
  const matches = (query: string) =>
    channels.filter((c) => channelMatchesQuery(c, query)).map((c) => c.name);

  it('matches channel names case-insensitively, anywhere in the name', () => {
    expect(matches('orf')).toEqual(['ORF 1', 'ORF 2']);
    expect(matches('tv')).toEqual(['ServusTV HD']);
  });

  // Prefix, not substring: typing "3" offers 3 and 301, not every number
  // containing a 3 somewhere.
  it('matches channel numbers by prefix', () => {
    expect(matches('3')).toEqual(['ServusTV HD', 'Sky Cinema']);
    expect(matches('30')).toEqual(['Sky Cinema']);
    expect(matches('1')).toEqual(['ORF 1']);
  });

  it('returns everything for an empty or whitespace query', () => {
    expect(matches('')).toHaveLength(4);
    expect(matches('   ')).toHaveLength(4);
  });

  it('returns nothing when no channel matches', () => {
    expect(matches('zdf')).toEqual([]);
  });

  it('falls back to the id when a channel has no name', () => {
    expect(channelMatchesQuery({ id: 'orf1.at', number: null }, 'orf1')).toBe(true);
  });
});
