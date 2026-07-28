// Pure decision for "what to do when a browser tab returns to the foreground"
// for the V3 player. iOS Safari and desktop browsers suspend the decoder while
// backgrounded and do NOT auto-resume inline <video> on return, so the frame
// stays black/frozen. This picks the recovery action on the hidden->visible
// edge. Kept pure (no DOM/refs) so it is unit-tested; the effect in
// usePlaybackOrchestrator wires the side effects. TV uses its own effect.

export type ForegroundResumeAction = 'retry' | 'play' | 'none';

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
  return 'play';
}
