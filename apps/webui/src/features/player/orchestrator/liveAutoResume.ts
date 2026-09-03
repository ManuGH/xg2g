// Pure decision for "what to do when Live TV pauses without user intent."
// In Live TV, playback must never silently freeze on a frame when Safari or macOS
// pauses the media element (e.g. window occlusion, CoreAudio device route changes,
// or App Nap). If the user did not deliberately pause, we re-assert playback.
// Kept pure so the state machine branching is 100% unit-tested.

export type LiveAutoResumeAction = 'play' | 'none';

export interface LiveAutoResumeInput {
  /** True only when the player is currently in LIVE broadcast mode. */
  isLiveMode: boolean;
  /** Whether the underlying <video> element is currently paused. */
  isPaused: boolean;
  /** The user deliberately clicked pause or stop — keep sacred, never auto-resume. */
  userPaused: boolean;
  /** Status is terminal (stopped/idle/error) or in teardown. */
  hasTerminal: boolean;
}

export function decideLiveAutoResume(input: LiveAutoResumeInput): LiveAutoResumeAction {
  if (!input.isLiveMode) {
    return 'none';
  }
  if (!input.isPaused) {
    return 'none';
  }
  if (input.userPaused || input.hasTerminal) {
    return 'none';
  }
  return 'play';
}
