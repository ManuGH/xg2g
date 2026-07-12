import { describe, expect, it } from 'vitest';
import { formatSessionTimelineEvent, sessionTimeline } from './sessionTimeline';

describe('sessionTimeline', () => {
  it('anchors events to the attempt start and clears on a new attempt', () => {
    sessionTimeline.beginAttempt(1);
    sessionTimeline.record('manifest_loaded');
    sessionTimeline.record('first_frame');

    let events = sessionTimeline.getSnapshot();
    expect(events.map((e) => e.kind)).toEqual(['attempt_started', 'manifest_loaded', 'first_frame']);
    expect(events.every((e) => e.epoch === 1)).toBe(true);
    expect(events[0]?.atMs).toBe(0);

    sessionTimeline.beginAttempt(2);
    events = sessionTimeline.getSnapshot();
    expect(events).toHaveLength(1);
    expect(events[0]?.epoch).toBe(2);
  });

  it('drops events outside an attempt', () => {
    sessionTimeline.beginAttempt(3);
    sessionTimeline.endAttempt('user_stop');
    const lengthAfterStop = sessionTimeline.getSnapshot().length;

    sessionTimeline.record('stall', 'late event from torn-down session');
    const snapshot = sessionTimeline.getSnapshot();
    expect(snapshot).toHaveLength(lengthAfterStop);
    const lastEvent = snapshot[snapshot.length - 1];
    expect(lastEvent?.kind).toBe('stopped');
    expect(lastEvent?.detail).toBe('user_stop');
  });

  it('notifies subscribers and supports unsubscribe', () => {
    let notified = 0;
    const unsubscribe = sessionTimeline.subscribe(() => {
      notified += 1;
    });
    sessionTimeline.beginAttempt(4);
    sessionTimeline.record('stall');
    expect(notified).toBe(2);
    unsubscribe();
    sessionTimeline.record('stall');
    expect(notified).toBe(2);
  });

  it('serializes compactly for telemetry', () => {
    sessionTimeline.beginAttempt(5);
    sessionTimeline.record('recovery_started', 'decode attempt 1');
    const described = sessionTimeline.describe();
    expect(described[0]).toMatch(/^T\+0\.00s attempt_started$/);
    expect(described[1]).toMatch(/^T\+\d+\.\d{2}s recovery_started \(decode attempt 1\)$/);
  });

  it('formats events with seconds precision', () => {
    expect(formatSessionTimelineEvent({
      kind: 'first_frame',
      atMs: 2310,
      wallClockMs: 0,
      epoch: 1,
    })).toBe('T+2.31s first_frame');
  });
});
