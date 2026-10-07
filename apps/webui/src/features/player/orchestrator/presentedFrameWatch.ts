import { readPlaybackFrameCounters } from '../playbackRenderProbe';

// How long a stream that kept playing while the page was hidden gets to show
// moving pictures again before the foreground recovery re-attaches it. AVPlayer
// stops fetching video while the page is hidden and resumes at the next
// segment (2 s) once it is visible.
export const LIVE_FOREGROUND_FRAME_GRACE_MS = 2_500;

const FRAME_COUNTER_POLL_MS = 250;

type FrameCallbackVideo = HTMLVideoElement & {
  requestVideoFrameCallback?: (callback: (now: number, metadata: { mediaTime?: number }) => void) => number;
  cancelVideoFrameCallback?: (handle: number) => void;
};

// Calls onMissing unless the element presents moving pictures within
// timeoutMs. A single presented frame is not enough: a frozen layer can
// re-present its last frame, so the media time has to advance between two
// frames. Without frame callbacks or frame counters there is nothing to
// observe and onMissing runs right away. Returns a cancel function.
export function watchForMovingPictures(
  video: HTMLVideoElement,
  timeoutMs: number,
  onMissing: () => void,
): () => void {
  const el = video as FrameCallbackVideo;
  let settled = false;
  let frameHandle: number | null = null;
  let pollTimer: number | null = null;

  const stop = () => {
    settled = true;
    window.clearTimeout(deadline);
    if (pollTimer !== null) {
      window.clearInterval(pollTimer);
      pollTimer = null;
    }
    if (frameHandle !== null && typeof el.cancelVideoFrameCallback === 'function') {
      el.cancelVideoFrameCallback(frameHandle);
    }
    frameHandle = null;
  };

  const deadline = window.setTimeout(() => {
    if (settled) return;
    stop();
    onMissing();
  }, timeoutMs);

  if (typeof el.requestVideoFrameCallback === 'function') {
    let firstMediaTime: number | null = null;
    let frames = 0;
    const onFrame = (_now: number, metadata: { mediaTime?: number }) => {
      frameHandle = null;
      if (settled) return;
      frames += 1;
      const mediaTime = typeof metadata?.mediaTime === 'number' ? metadata.mediaTime : null;
      if (firstMediaTime === null) {
        firstMediaTime = mediaTime ?? Number.NEGATIVE_INFINITY;
      } else if (frames >= 2 && (mediaTime === null || mediaTime > firstMediaTime)) {
        stop();
        return;
      }
      frameHandle = el.requestVideoFrameCallback!(onFrame);
    };
    frameHandle = el.requestVideoFrameCallback(onFrame);
    return stop;
  }

  const initial = readPlaybackFrameCounters(video).totalFrames;
  if (initial === null) {
    stop();
    onMissing();
    return stop;
  }
  pollTimer = window.setInterval(() => {
    const total = readPlaybackFrameCounters(video).totalFrames;
    if (total !== null && total >= initial + 2) {
      stop();
    }
  }, FRAME_COUNTER_POLL_MS);
  return stop;
}
