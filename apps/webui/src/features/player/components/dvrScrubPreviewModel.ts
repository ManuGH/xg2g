// Pure helpers for the live-DVR scrub preview (the YouTube-style hover thumbnail).
// The hover position over the slider maps to an offset from the start of the
// seekable window, which the backend turns into the matching keyframe. We round
// the offset to the segment grid so the <img> URL only changes once per segment
// — that keeps requests sparse and lets the browser cache hits between hovers.

export function clampFraction(x: number): number {
  if (Number.isNaN(x)) return 0;
  if (x < 0) return 0;
  if (x > 1) return 1;
  return x;
}

// previewOffsetForFraction maps a 0..1 position along the scrubber to a
// segment-aligned offset (seconds) from the window start.
export function previewOffsetForFraction(
  fraction: number,
  windowDuration: number,
  segmentSeconds: number,
): number {
  const seg = segmentSeconds > 0 ? segmentSeconds : 6;
  const span = windowDuration > 0 ? windowDuration : 0;
  const raw = clampFraction(fraction) * span;
  return Math.max(0, Math.floor(raw / seg) * seg);
}

// dvrPreviewImageUrl appends the offset query the backend reads (?t=seconds).
export function dvrPreviewImageUrl(baseUrl: string, offsetSeconds: number): string {
  const sep = baseUrl.includes('?') ? '&' : '?';
  return `${baseUrl}${sep}t=${Math.max(0, Math.round(offsetSeconds))}`;
}

// formatPreviewClock renders an offset as m:ss / h:mm:ss (window-relative label
// used when there is no wall-clock anchor).
export function formatPreviewClock(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '0:00';
  const total = Math.floor(seconds);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  const pad = (n: number) => n.toString().padStart(2, '0');
  return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
}

// previewHoverLabel is the caption shown above the thumbnail: the wall-clock
// time-of-day when an EPG anchor (windowStartUnix) is known, else the
// window-relative offset.
export function previewHoverLabel(offsetSeconds: number, windowStartUnix: number | null): string {
  if (windowStartUnix && windowStartUnix > 0) {
    const date = new Date((windowStartUnix + offsetSeconds) * 1000);
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false });
  }
  return formatPreviewClock(offsetSeconds);
}
