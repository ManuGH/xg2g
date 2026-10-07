// Pure decision for "what to do when a browser tab returns to the foreground"
// for the V3 player. iOS Safari and desktop browsers suspend the decoder while
// backgrounded and do NOT auto-resume inline <video> on return, so the frame
// stays black/frozen. This picks the recovery action on the hidden->visible
// edge. Kept pure (no DOM/refs) so it is unit-tested; the effect in
// usePlaybackOrchestrator wires the side effects. TV uses its own effect.

export type ForegroundResumeAction = 'retry' | 'reattach' | 'play' | 'none';

// iOS Safari pauses inline video when the screen locks (WebKit interruption
// "SuspendedUnderLock") and freezes the page. Resuming a live native-HLS stream
// at the minutes-old position afterwards seeks into a cold AVPlayer pipeline that
// often fails (seek cancelled, video track restarting at segment 0 while audio
// plays on). Past this long a gap a live stream rejoins the live edge instead.
export const LIVE_FOREGROUND_REATTACH_MS = 20_000;

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
  /** How long the page was hidden, in ms (0 when unknown). */
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
