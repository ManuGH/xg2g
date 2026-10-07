import type { ContinueWatchingItem } from './api';

/**
 * Normalizes series / show titles to group different episodes or daily recordings of the same show.
 * Examples:
 * - "Café PULS mit PULS 4 Austria News" -> "café puls"
 * - "Café PULS - Frühsport" -> "café puls"
 * - "Tatort: Höllenfahrt" -> "tatort"
 * - "Tagesschau (2026-10-07)" -> "tagesschau"
 * - "Unser Club S01E03" -> "unser club"
 */
export function normalizeSeriesTitle(rawTitle?: string): string {
  if (!rawTitle) return '';
  let s = rawTitle.trim();
  if (!s) return '';

  // Strip date formats: (2026-10-07), (07.10.2026), 2026-10-07
  s = s.replace(/\s*\(?\d{4}[-/.]\d{1,2}[-/.]\d{1,2}\)?/gi, '');
  s = s.replace(/\s*\(?\d{1,2}\.\d{1,2}\.\d{2,4}\)?/gi, '');

  // Strip Season/Episode tokens: S01E02, S1 E2, E05, Ep. 3
  s = s.replace(/\s*\(?S\d+\s*E\d+\)?/gi, '');
  s = s.replace(/\s*\(?Folge\s*\d+\)?/gi, '');
  s = s.replace(/\s*\(?Episode\s*\d+\)?/gi, '');

  // Strip subtitles delimited by colon ": ", dash " - ", en-dash " – ", em-dash " — "
  const colonIdx = s.indexOf(': ');
  if (colonIdx > 2) {
    s = s.slice(0, colonIdx);
  }
  const dashMatch = s.match(/\s+[-–—]\s+/);
  if (dashMatch && typeof dashMatch.index === 'number' && dashMatch.index > 2) {
    s = s.slice(0, dashMatch.index);
  }

  // Strip " mit ..." suffix (common in morning shows/magazines e.g. "Café PULS mit PULS 4...")
  const mitMatch = s.match(/\s+mit\s+.+$/i);
  if (mitMatch && typeof mitMatch.index === 'number' && mitMatch.index > 2) {
    s = s.slice(0, mitMatch.index);
  }

  return s.trim().toLowerCase();
}

/**
 * Checks whether an item is virtually finished (credits rolling, <= 2 min left, or >= 93% progress).
 */
export function isItemVirtuallyFinished(item: ContinueWatchingItem): boolean {
  const duration = item.durationSeconds ?? 0;
  if (duration <= 0) return false;
  const remainingSeconds = duration - item.posSeconds;
  // If less than or equal to 2 minutes remaining, or watched >= 93% of the total duration
  if (remainingSeconds <= 120 || item.posSeconds / duration >= 0.93) {
    return true;
  }
  return false;
}

/**
 * Normalizes progress percent (0-100) for UI display.
 */
export function progressPercent(item: ContinueWatchingItem): number {
  const d = item.durationSeconds ?? 0;
  if (d <= 0) return 0;
  return Math.round(Math.max(0, Math.min(1, item.posSeconds / d)) * 100);
}

/**
 * Resolves a 2-letter monogram for fallback thumbnail generation.
 */
export function resolveItemMonogram(title?: string): string {
  const normalized = String(title || '').trim();
  if (!normalized) return 'REC';
  const words = normalized.split(/\s+/).filter(Boolean);
  if (words.length === 1) {
    const first = words[0];
    return first ? first.slice(0, 2).toUpperCase() : 'REC';
  }
  return words.slice(0, 2).map((w) => w.slice(0, 1).toUpperCase()).join('');
}

/**
 * Filters and deduplicates resume items:
 * - Drops items with posSeconds < 15
 * - Drops virtually finished items (<= 2 min left or >= 93% watched)
 * - Groups by series/show, keeping only the single most recently updated entry
 * - Caps to maxLimit (default 4)
 */
export function filterAndDeduplicateContinueWatching(
  items: ContinueWatchingItem[],
  maxLimit = 4,
): ContinueWatchingItem[] {
  // 1. Initial filter for minimal engagement and unfinished status
  const eligible = items.filter((item) => item.posSeconds >= 15 && !isItemVirtuallyFinished(item));

  // 2. Group by series / show base title
  const groupMap = new Map<string, ContinueWatchingItem>();

  for (const item of eligible) {
    const key = normalizeSeriesTitle(item.title) || item.recordingId;
    const existing = groupMap.get(key);
    if (!existing) {
      groupMap.set(key, item);
      continue;
    }

    // Pick the most recently updated item
    const existingDate = existing.updatedAt ? new Date(existing.updatedAt).getTime() : 0;
    const itemDate = item.updatedAt ? new Date(item.updatedAt).getTime() : 0;

    if (itemDate > existingDate) {
      groupMap.set(key, item);
    } else if (itemDate === existingDate && item.posSeconds > existing.posSeconds) {
      groupMap.set(key, item);
    }
  }

  // 3. Collect and sort by newest updatedAt desc
  const result = Array.from(groupMap.values());
  result.sort((a, b) => {
    const timeA = a.updatedAt ? new Date(a.updatedAt).getTime() : 0;
    const timeB = b.updatedAt ? new Date(b.updatedAt).getTime() : 0;
    return timeB - timeA;
  });

  return result.slice(0, maxLimit);
}
