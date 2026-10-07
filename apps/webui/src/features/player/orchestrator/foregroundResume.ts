// Pure decision for "what to do when a browser tab returns to the foreground"
// for the V3 player. iOS Safari and desktop browsers suspend the decoder while
// backgrounded and do NOT auto-resume inline <video> on return, so the frame
// stays black/frozen. This picks the recovery action on the hidden->visible
// edge. Kept pure (no DOM/refs) so it is unit-tested; the effect in
// usePlaybackOrchestrator wires the side effects. TV uses its own effect.

export type ForegroundResumeAction = 'retry' | 'reattach' | 'play' | 'none';

// iOS Safari pauses inline video whenever the page goes to the background
// (WebKit interruptions "EnteringBackground" / "SuspendedUnderLock") and releases
// the video render resource. Resuming a live native-HLS stream with play() then
// keeps the clock and audio running over a black video layer, or seeks into a
// cold pipeline (seek cancelled, video restarting at segment 0). Past this short
// a gap a live stream re-attaches its source (continuing at the viewer's DVR
// position); only momentary hides keep the play() nudge.
export const LIVE_FOREGROUND_REATTACH_MS = 3_000;

export interface ForegroundResumeInput {
  /** True only on a genuine hidden -> visible transition (not mount/initial). */
  wasHidden: boolean;
  /** The video is in picture-in-picture, i.e. never really backgrounded. */
  isPiP: boolean;
  /** Current player status. 'error' means the heartbeat reaped the session. */
  status: string;
  /** The user deliberately paused — never auto-resume. */
  userPaused: boolean;
  /** Status is terminal (stopped/idle/error). */
  hasTerminal: boolean;
  /** How long the page was hidden, in wall-clock ms (0 when unknown). */
  hiddenMs?: number;
  /** The stream is live (not VOD / recording). */
  isLive?: boolean;
  /** The native (browser-owned) HLS engine is attached. */
  isNative?: boolean;
}

export function decideForegroundResume(input: ForegroundResumeInput): ForegroundResumeAction {
  if (!input.wasHidden) {
    return 'none';
  }
  if (input.isPiP) {
    return 'none';
  }
  // A reaped session (heartbeat 410/404 surfaced as status 'error') must be
  // handled BEFORE the terminal bail — 'error' is terminal, but here we want to
  // re-establish it, not give up.
  if (input.status === 'error') {
    return 'retry';
  }
  if (input.userPaused || input.hasTerminal) {
    return 'none';
  }
  if (input.isLive && input.isNative && (input.hiddenMs ?? 0) >= LIVE_FOREGROUND_REATTACH_MS) {
    return 'reattach';
  }
  return 'play';
}
