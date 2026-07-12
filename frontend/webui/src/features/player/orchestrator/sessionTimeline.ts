// Session timeline: a structured, per-attempt chronicle of what the player
// actually did — attempt started, contract resolved, transcoder phases,
// manifest, first frame, stalls, recoveries, fallbacks, stop. It exists so a
// misbehaving session can be diagnosed from one ordered event list instead of
// correlating scattered debug logs (see the lease-expiry and phantom-fix
// incidents). Pure module + tiny store; React reads it via useSyncExternalStore.

export type SessionTimelineEventKind =
  | 'attempt_started'
  | 'contract_resolved'
  | 'session_phase'
  | 'manifest_loaded'
  | 'first_frame'
  | 'stall'
  | 'recovery_started'
  | 'recovery_succeeded'
  | 'recovery_failed'
  | 'auto_profile_fallback'
  | 'failure'
  | 'advisory'
  | 'stopped';

export interface SessionTimelineEvent {
  kind: SessionTimelineEventKind;
  /** Milliseconds since the current attempt started. */
  atMs: number;
  /** Wall-clock timestamp for cross-referencing with backend logs. */
  wallClockMs: number;
  /** Playback epoch the event belongs to. */
  epoch: number;
  detail?: string;
}

const MAX_EVENTS = 80;

type Listener = () => void;

class SessionTimelineStore {
  private events: SessionTimelineEvent[] = [];
  private snapshot: readonly SessionTimelineEvent[] = [];
  private attemptStartedAt: number | null = null;
  private currentEpoch = 0;
  private listeners = new Set<Listener>();

  /** Start a fresh attempt: clears prior events, anchors t0. */
  beginAttempt(epoch: number): void {
    this.attemptStartedAt = performance.now();
    this.currentEpoch = epoch;
    this.events = [];
    this.pushEvent('attempt_started');
  }

  record(kind: SessionTimelineEventKind, detail?: string): void {
    if (this.attemptStartedAt === null) {
      return; // No attempt in flight; late events from torn-down sessions are noise.
    }
    this.pushEvent(kind, detail);
  }

  /** Marks the attempt finished; subsequent record() calls are dropped until beginAttempt. */
  endAttempt(detail?: string): void {
    if (this.attemptStartedAt === null) {
      return;
    }
    this.pushEvent('stopped', detail);
    this.attemptStartedAt = null;
  }

  getSnapshot = (): readonly SessionTimelineEvent[] => this.snapshot;

  subscribe = (listener: Listener): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  /** Compact serialization for telemetry payloads / error reports. */
  describe(maxEvents = MAX_EVENTS): string[] {
    return this.snapshot
      .slice(-maxEvents)
      .map((event) => formatSessionTimelineEvent(event));
  }

  private pushEvent(kind: SessionTimelineEventKind, detail?: string): void {
    const now = performance.now();
    const atMs = this.attemptStartedAt === null || kind === 'attempt_started' ? 0 : Math.max(0, now - this.attemptStartedAt);
    this.events.push({
      kind,
      atMs: Math.round(atMs),
      wallClockMs: Date.now(),
      epoch: this.currentEpoch,
      detail,
    });
    if (this.events.length > MAX_EVENTS) {
      this.events = this.events.slice(-MAX_EVENTS);
    }
    this.snapshot = [...this.events];
    this.listeners.forEach((listener) => listener());
  }
}

export function formatSessionTimelineEvent(event: SessionTimelineEvent): string {
  const seconds = (event.atMs / 1000).toFixed(2);
  const suffix = event.detail ? ` (${event.detail})` : '';
  return `T+${seconds}s ${event.kind}${suffix}`;
}

export const sessionTimeline = new SessionTimelineStore();
