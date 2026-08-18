// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import { describe, expect, it } from 'vitest';
import { computeOnAirProgress, formatClockLabel, toWholeMinutes } from './onAir';

const START_SEC = 1_770_000_000;
const START_MS = START_SEC * 1000;
const HOUR_SEC = 3600;

describe('computeOnAirProgress', () => {
  it('places now inside the programme', () => {
    const progress = computeOnAirProgress(START_SEC, HOUR_SEC, START_MS + 15 * 60 * 1000);
    expect(progress).not.toBeNull();
    expect(progress!.elapsedSec).toBe(15 * 60);
    expect(progress!.remainingSec).toBe(45 * 60);
    expect(progress!.fraction).toBeCloseTo(0.25);
    expect(progress!.endMs).toBe(START_MS + HOUR_SEC * 1000);
  });

  it('clamps a programme that has run past its slot instead of overshooting', () => {
    const progress = computeOnAirProgress(START_SEC, HOUR_SEC, START_MS + 90 * 60 * 1000);
    expect(progress!.fraction).toBe(1);
    expect(progress!.remainingSec).toBe(0);
    expect(progress!.elapsedSec).toBe(HOUR_SEC);
  });

  it('clamps a clock that sits before the programme start', () => {
    const progress = computeOnAirProgress(START_SEC, HOUR_SEC, START_MS - 10 * 60 * 1000);
    expect(progress!.fraction).toBe(0);
    expect(progress!.elapsedSec).toBe(0);
    expect(progress!.remainingSec).toBe(HOUR_SEC);
  });

  // The whole point of the null contract: no timeline is drawn unless the
  // receiver actually reported both anchors.
  it.each([
    ['missing start', undefined, HOUR_SEC],
    ['missing duration', START_SEC, undefined],
    ['zero duration', START_SEC, 0],
    ['negative duration', START_SEC, -60],
    ['non-finite start', Number.NaN, HOUR_SEC],
  ])('returns null for %s', (_label, begin, duration) => {
    expect(computeOnAirProgress(begin as number | undefined, duration as number | undefined, START_MS)).toBeNull();
  });
});

describe('toWholeMinutes', () => {
  it('rounds up so a running programme never reads as 0 min left', () => {
    expect(toWholeMinutes(1)).toBe(1);
    expect(toWholeMinutes(59)).toBe(1);
    expect(toWholeMinutes(60)).toBe(1);
    expect(toWholeMinutes(61)).toBe(2);
  });

  it('reports a finished programme as 0', () => {
    expect(toWholeMinutes(0)).toBe(0);
    expect(toWholeMinutes(-30)).toBe(0);
  });
});

describe('formatClockLabel', () => {
  it('renders hours and minutes', () => {
    expect(formatClockLabel(START_MS)).toMatch(/\d{1,2}[:.]\d{2}/);
  });
});
