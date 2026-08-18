// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

/**
 * Where the current programme stands on its own clock.
 *
 * Every field is derived from what /receiver/current already reports
 * (`now.beginTimestamp`, `now.durationSec`). Nothing here estimates: if the
 * receiver does not tell us when the programme started or how long it runs,
 * this returns null and the caller shows no timeline at all. A progress bar
 * that guesses is worse than no progress bar, because the operator cannot see
 * that it is guessing.
 */
export type OnAirProgress = {
  /** Unix ms of the programme start, as reported. */
  startMs: number;
  /** Unix ms of the programme end, derived from start + duration. */
  endMs: number;
  elapsedSec: number;
  remainingSec: number;
  /** Position of "now" inside the programme, clamped to 0..1. */
  fraction: number;
};

function isUsable(value: number | undefined): value is number {
  return typeof value === 'number' && Number.isFinite(value) && value > 0;
}

export function computeOnAirProgress(
  beginTimestampSec: number | undefined,
  durationSec: number | undefined,
  nowMs: number,
): OnAirProgress | null {
  if (!isUsable(beginTimestampSec) || !isUsable(durationSec)) {
    return null;
  }

  const startMs = beginTimestampSec * 1000;
  const endMs = startMs + durationSec * 1000;

  // A programme that has run past its slot still reads as "on air" on the
  // receiver until the box switches over, so clamp rather than reject: the bar
  // parks at the end instead of overshooting or disappearing.
  const rawElapsedSec = (nowMs - startMs) / 1000;
  const elapsedSec = Math.min(Math.max(rawElapsedSec, 0), durationSec);

  return {
    startMs,
    endMs,
    elapsedSec: Math.round(elapsedSec),
    remainingSec: Math.round(durationSec - elapsedSec),
    fraction: elapsedSec / durationSec,
  };
}

/** Unix ms to a local HH:MM label. */
export function formatClockLabel(ms: number): string {
  return new Date(ms).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

/**
 * The same label for EPG data, which carries Unix *seconds*.
 *
 * Returns an empty string for a missing or zero timestamp: an event whose
 * start or end the receiver never reported should render no time at all,
 * not "01:00" from the epoch. The guard names those two cases rather than
 * testing falsiness, so the contract the tests assert is the contract the
 * code states - a caller reading this should not have to work out which
 * values of a `number` happen to be falsy.
 */
export function formatClockLabelFromSeconds(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds === 0) return '';
  return formatClockLabel(seconds * 1000);
}

/**
 * Whole minutes, rounded up while any part of the minute remains. "0 min left"
 * on a programme that is still running would be a lie the operator can see on
 * screen; the last 59 seconds read as "1 min".
 */
export function toWholeMinutes(seconds: number): number {
  return Math.ceil(Math.max(seconds, 0) / 60);
}
