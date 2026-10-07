import { describe, expect, it } from 'vitest';
import { markLivePosition, resolveLivePosition } from './livePosition';

function media(opts: { currentTime: number; startDateMs?: number; seekable?: [number, number] }): HTMLMediaElement {
  const el = document.createElement('video');
  Object.defineProperty(el, 'currentTime', { configurable: true, value: opts.currentTime });
  if (opts.startDateMs !== undefined) {
    Object.defineProperty(el, 'getStartDate', { configurable: true, value: () => new Date(opts.startDateMs!) });
  }
  const range = opts.seekable;
  Object.defineProperty(el, 'seekable', {
    configurable: true,
    value: range ? { length: 1, start: () => range[0], end: () => range[1] } : { length: 0, start: () => 0, end: () => 0 },
  });
  return el;
}

describe('live DVR position across a re-attach', () => {
  it('marks nothing before playback has a position', () => {
    expect(markLivePosition(media({ currentTime: 0 }))).toBeNull();
  });

  it('restores the same media time when the timeline origin is unchanged', () => {
    const mark = markLivePosition(media({ currentTime: 120, startDateMs: 1_000_000, seekable: [0, 400] }))!;
    expect(resolveLivePosition(mark, media({ currentTime: 394, startDateMs: 1_000_000, seekable: [0, 430] }))).toBe(120);
  });

  it('follows the programme date when the playlist window moved', () => {
    const mark = markLivePosition(media({ currentTime: 120, startDateMs: 1_000_000 }))!;
    // The new attachment's media time 0 is 30 s later in programme time.
    expect(resolveLivePosition(mark, media({ currentTime: 0, startDateMs: 1_030_000, seekable: [0, 400] }))).toBe(90);
  });

  it('clamps into the seekable DVR window', () => {
    const mark = markLivePosition(media({ currentTime: 50, startDateMs: 1_000_000 }))!;
    expect(resolveLivePosition(mark, media({ currentTime: 0, startDateMs: 1_100_000, seekable: [10, 300] }))).toBe(10);
  });

  it('falls back to the raw media time without a programme date', () => {
    const mark = markLivePosition(media({ currentTime: 75 }))!;
    expect(mark.startDateMs).toBeNull();
    expect(resolveLivePosition(mark, media({ currentTime: 0, seekable: [0, 300] }))).toBe(75);
  });
});
