// Resume-playback recovery for the native-HLS path. When the browser page is
// frozen (tab occluded / another app takes the foreground/fullscreen) and then
// resumes, the inline <video> does not auto-resume: a single play() right after
// the freeze often fizzles because the element is still suspended and discards the
// call, so the frame stays black until the user mashes play a few times. This
// automates exactly that — retry play() on a short interval until currentTime
// actually advances (the only proof the decoder accepted the resume), bounded.
//
// This is the native-path fix. An app-controlled MSE/ManagedMediaSource engine
// would instead re-init the buffer (flush + re-append + seek-to-live); that is the
// general recovery path and is intentionally out of scope here.

export const RESUME_PROGRESS_EPSILON = 0.01;
export const RESUME_RECOVERY_INTERVAL_MS = 250;
export const RESUME_RECOVERY_MAX_ATTEMPTS = 8; // ~2s ceiling

// resumeRecoverySettled decides whether the retry loop should stop. It settles once
// the stream is demonstrably advancing (recovered), the media ended, or the bounded
// attempts are exhausted. A stuck stream with attempts remaining returns false —
// "keep nudging" — which is the entire fix over the prior single play(). Pure, so it
// is unit-tested.
export function resumeRecoverySettled(
  startTime: number,
  currentTime: number,
  ended: boolean,
  attempts: number,
  maxAttempts: number,
): boolean {
  const advancing = currentTime > startTime + RESUME_PROGRESS_EPSILON;
  return advancing || ended || attempts >= maxAttempts;
}

export interface ResumePlaybackRecoveryOptions {
  intervalMs?: number;
  maxAttempts?: number;
  onBlocked?: (err: unknown) => void;
}

// startResumePlaybackRecovery nudges video.play() until the stream advances or the
// bounded attempts run out, then stops. Returns a cancel function so a React effect
// can abort it if the page goes hidden again mid-recovery. Harmless on a clean
// resume: the first attempt's play() resumes, currentTime advances within one
// interval, and resumeRecoverySettled stops the loop after a single play() — so it
// does not disturb ordinary tab-switches that never lost the decoder.
export function startResumePlaybackRecovery(
  video: HTMLVideoElement,
  options: ResumePlaybackRecoveryOptions = {},
): () => void {
  const intervalMs = options.intervalMs ?? RESUME_RECOVERY_INTERVAL_MS;
  const maxAttempts = options.maxAttempts ?? RESUME_RECOVERY_MAX_ATTEMPTS;
  let attempts = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let cancelled = false;

  const attempt = (): void => {
    if (cancelled) {
      return;
    }
    const startTime = video.currentTime;
    void video.play().catch((err: unknown) => options.onBlocked?.(err));
    timer = setTimeout(() => {
      if (cancelled) {
        return;
      }
      attempts += 1;
      if (resumeRecoverySettled(startTime, video.currentTime, video.ended, attempts, maxAttempts)) {
        return;
      }
      attempt();
    }, intervalMs);
  };

  attempt();

  return () => {
    cancelled = true;
    if (timer !== undefined) {
      clearTimeout(timer);
    }
  };
}
