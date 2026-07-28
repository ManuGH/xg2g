// Resume-playback recovery for the native-HLS path. When the browser page is
// frozen (tab occluded / another app takes the foreground/fullscreen) and then
// resumes, the inline <video> does not auto-resume: a single play() right after
// the freeze fizzles because the element is still suspended and discards the call,
// so the frame stays black until the user mashes play a few times.
//
// This OBSERVES FIRST and only then intervenes: it watches one window to confirm
// the stream is genuinely stuck before touching the element — never funking a
// play() into a waking element that was about to recover on its own. Once a stall
// is confirmed it nudges play() on a short interval until currentTime actually
// advances (the only proof the decoder accepted the resume), bounded.
//
// Native-path fix only; an app-controlled MSE/ManagedMediaSource engine would
// instead re-init the buffer (flush + re-append + seek-to-live) — out of scope here.

export const RESUME_PROGRESS_EPSILON = 0.01;
export const RESUME_OBSERVE_MS = 400; // watch this long before deciding the stream is stuck
export const RESUME_RECOVERY_INTERVAL_MS = 250;
export const RESUME_RECOVERY_MAX_ATTEMPTS = 8; // ~2s ceiling of nudges

// resumeStreamRecovered reports whether the stream is fine and needs no nudge:
// it ended, or currentTime advanced past a small epsilon (the decoder is presenting
// again). Pure, so both the observe step and the retry-settle step are unit-tested.
export function resumeStreamRecovered(startTime: number, currentTime: number, ended: boolean): boolean {
  return ended || currentTime > startTime + RESUME_PROGRESS_EPSILON;
}

// resumeRecoverySettled decides whether the retry loop should stop: stop once the
// stream recovered, or the bounded attempts are exhausted. A stuck stream with
// attempts remaining returns false — "keep nudging" — which is the entire fix over
// a single play().
export function resumeRecoverySettled(
  startTime: number,
  currentTime: number,
  ended: boolean,
  attempts: number,
  maxAttempts: number,
): boolean {
  return resumeStreamRecovered(startTime, currentTime, ended) || attempts >= maxAttempts;
}

export interface ResumePlaybackRecoveryOptions {
  observeMs?: number;
  intervalMs?: number;
  maxAttempts?: number;
  onBlocked?: (err: unknown) => void;
  // shouldContinue lets the caller keep a USER pause sacred even mid-recovery: if
  // the user pauses while the loop is running, it stops on the next tick. Wire it to
  // !userPauseIntentRef.current. (The first entry is already gated upstream by
  // decideForegroundResume; this protects against a pause DURING the ~2s window.)
  shouldContinue?: () => boolean;
  onFailed?: () => void;
}

// startResumePlaybackRecovery observes first, then nudges play() until the stream
// advances or the bounded attempts run out. Returns a cancel function so a React
// effect can abort it if the page hides again mid-recovery. A clean resume advances
// within the observation window and the element is never touched.
export function startResumePlaybackRecovery(
  video: HTMLVideoElement,
  options: ResumePlaybackRecoveryOptions = {},
): () => void {
  const observeMs = options.observeMs ?? RESUME_OBSERVE_MS;
  const intervalMs = options.intervalMs ?? RESUME_RECOVERY_INTERVAL_MS;
  const maxAttempts = options.maxAttempts ?? RESUME_RECOVERY_MAX_ATTEMPTS;
  let attempts = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let cancelled = false;

  const alive = (): boolean => !cancelled && (options.shouldContinue?.() ?? true);

  const nudge = (): void => {
    if (!alive()) {
      return;
    }
    const startTime = video.currentTime;
    void video.play().catch((err: unknown) => options.onBlocked?.(err));
    timer = setTimeout(() => {
      if (!alive()) {
        return;
      }
      attempts += 1;
      if (resumeRecoverySettled(startTime, video.currentTime, video.ended, attempts, maxAttempts)) {
        if (!resumeStreamRecovered(startTime, video.currentTime, video.ended)) {
          options.onFailed?.();
        }
        return;
      }
      nudge();
    }, intervalMs);
  };

  // Issue an immediate play() first. React's effect lifecycle may clean up the
  // returned cancel before the observation window timer fires (e.g. when the
  // caller transitions status as part of the same render cycle), so the first
  // attempt must not be deferred past cleanup. The initial synchronous call is a
  // free attempt that never counts toward the maxAttempts bound.
  void video.play().catch((err: unknown) => options.onBlocked?.(err));

  // Observe: if the stream didn't recover after the first nudge, keep trying
  // until it advances or attempts are exhausted.
  const observeStart = video.currentTime;
  timer = setTimeout(() => {
    if (!alive()) {
      return;
    }
    if (resumeStreamRecovered(observeStart, video.currentTime, video.ended)) {
      return; // recovered on its own — never touch a waking element
    }
    // First play() didn't take — continue nudging on interval.
    // nudge() tracks its own attempt counter and will stop at maxAttempts.
    nudge();
  }, observeMs);

  return () => {
    cancelled = true;
    if (timer !== undefined) {
      clearTimeout(timer);
    }
  };
}
