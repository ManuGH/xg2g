// Counts what the per-session warning dedupe hides.
//
// reportPlaybackWarning reports a warning code once per session (or once per
// repeat window), which keeps the server log readable but makes a stream that
// stutters every few seconds look identical to one that hiccuped once at start.
// This tracker counts every occurrence, so the render heartbeat can say how
// often and for how long playback stalled, and how often hls.js had to jump a
// hole or nudge the playhead, between two beats.

export interface PlaybackStallCounters {
  /** Entries into buffering from a real 'waiting'/'stalled' while playing. */
  stalls: number;
  /** Time spent in those stalls, including one still in progress. */
  stallMs: number;
  /** hls.js bufferSeekOverHole: the playhead jumped a gap in the buffer. */
  holeJumps: number;
  /** hls.js bufferNudgeOnStall: the playhead was nudged past a stuck point. */
  nudges: number;
  /** hls.js bufferStalledError: playback ran out of buffered media. */
  stallErrors: number;
  /** hls.js bufferAppendError: a segment could not be appended. */
  appendErrors: number;
}

export interface PlaybackStallTracker {
  /** Starts counting for sessionId if it is not the session being counted. */
  ensureSession(sessionId: string): void;
  /** Playback entered buffering. A stall already in progress is not counted twice. */
  stallStarted(): void;
  /** Playback resumed; closes the stall in progress, if any. */
  stallEnded(): void;
  /** One non-fatal hls.js error detail; details that are not buffer stalls are ignored. */
  hlsNonFatal(details: unknown): void;
  /** Counters for sessionId, or zeros when a different session is being counted. */
  snapshot(sessionId: string): PlaybackStallCounters;
}

export function emptyStallCounters(): PlaybackStallCounters {
  return { stalls: 0, stallMs: 0, holeJumps: 0, nudges: 0, stallErrors: 0, appendErrors: 0 };
}

export function createPlaybackStallTracker(now: () => number = () => performance.now()): PlaybackStallTracker {
  let session: string | null = null;
  let counters = emptyStallCounters();
  let stallStartedAt: number | null = null;

  return {
    ensureSession(sessionId) {
      if (session === sessionId) {
        return;
      }
      session = sessionId;
      counters = emptyStallCounters();
      stallStartedAt = null;
    },
    stallStarted() {
      if (stallStartedAt !== null) {
        return;
      }
      stallStartedAt = now();
      counters.stalls += 1;
    },
    stallEnded() {
      if (stallStartedAt === null) {
        return;
      }
      counters.stallMs += Math.max(0, now() - stallStartedAt);
      stallStartedAt = null;
    },
    hlsNonFatal(details) {
      switch (details) {
        case 'bufferSeekOverHole':
          counters.holeJumps += 1;
          break;
        case 'bufferNudgeOnStall':
          counters.nudges += 1;
          break;
        case 'bufferStalledError':
          counters.stallErrors += 1;
          break;
        case 'bufferAppendError':
          counters.appendErrors += 1;
          break;
        default:
          break;
      }
    },
    snapshot(sessionId) {
      if (session !== sessionId) {
        return emptyStallCounters();
      }
      const ongoing = stallStartedAt === null ? 0 : Math.max(0, now() - stallStartedAt);
      return { ...counters, stallMs: counters.stallMs + ongoing };
    },
  };
}
