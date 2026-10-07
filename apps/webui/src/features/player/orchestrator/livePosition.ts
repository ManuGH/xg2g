// Keeps a live DVR position across a native source re-attach. A re-attached
// native HLS source starts at the playlist's EXT-X-START near the live edge;
// the viewer should continue where they were instead (the DVR window spans
// hours). The mark stores the absolute programme date when the browser exposes
// it (Safari getStartDate() with EXT-X-PROGRAM-DATE-TIME), so the position stays
// right even if the playlist window moved in between.

export interface LivePositionMark {
  currentTime: number;
  /** Programme date of media time 0 when the mark was taken (ms), or null. */
  startDateMs: number | null;
}

type DatedMedia = HTMLMediaElement & { getStartDate?: () => Date };

function startDateMs(media: HTMLMediaElement): number | null {
  try {
    const value = (media as DatedMedia).getStartDate?.().getTime();
    return typeof value === 'number' && Number.isFinite(value) ? value : null;
  } catch {
    return null;
  }
}

export function markLivePosition(media: HTMLMediaElement): LivePositionMark | null {
  const currentTime = media.currentTime;
  if (!Number.isFinite(currentTime) || currentTime <= 0) {
    return null;
  }
  return { currentTime, startDateMs: startDateMs(media) };
}

// Slack for timeupdate granularity and the small backward corrections AVPlayer
// reports while it plays on.
export const LIVE_POSITION_FOLLOW_TOLERANCE_MS = 2_000;

function programmeMs(mark: LivePositionMark, reference: LivePositionMark): number {
  return mark.startDateMs !== null && reference.startDateMs !== null
    ? mark.startDateMs + mark.currentTime * 1000
    : mark.currentTime * 1000;
}

// Moves a mark along with playback that continues while the page is hidden
// (iOS lock-screen play). Only continuous progress counts: the position may
// advance by at most the wall time the element has been playing since the
// mark, so a resume that lands a cold player at the live edge or back at the
// window start cannot move it. Returns null when the sample is rejected.
export function followLivePosition(
  mark: LivePositionMark,
  sample: LivePositionMark,
  playingMs: number,
): LivePositionMark | null {
  const progressMs = programmeMs(sample, mark) - programmeMs(mark, sample);
  if (progressMs < -LIVE_POSITION_FOLLOW_TOLERANCE_MS) {
    return null;
  }
  if (progressMs > Math.max(0, playingMs) + LIVE_POSITION_FOLLOW_TOLERANCE_MS) {
    return null;
  }
  return sample;
}

// Media time on the freshly attached source that corresponds to the mark,
// clamped into the seekable window; null when there is nothing to restore.
export function resolveLivePosition(mark: LivePositionMark, media: HTMLMediaElement): number | null {
  let target = mark.currentTime;
  const nowStart = startDateMs(media);
  if (mark.startDateMs !== null && nowStart !== null) {
    target = (mark.startDateMs + mark.currentTime * 1000 - nowStart) / 1000;
  }
  const seekable = media.seekable;
  if (seekable && seekable.length > 0) {
    const start = seekable.start(0);
    const end = seekable.end(seekable.length - 1);
    target = Math.min(Math.max(target, start), end);
  }
  return Number.isFinite(target) ? target : null;
}
