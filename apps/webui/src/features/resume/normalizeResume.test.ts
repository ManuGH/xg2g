import { describe, expect, it } from 'vitest';
import {
  normalizeSeriesTitle,
  isItemVirtuallyFinished,
  progressPercent,
  resolveItemMonogram,
  filterAndDeduplicateContinueWatching,
} from './normalizeResume';
import type { ContinueWatchingItem } from './api';

describe('normalizeResume', () => {
  describe('normalizeSeriesTitle', () => {
    it('normalizes show titles with Austrian/German magazine suffixes', () => {
      expect(normalizeSeriesTitle('Café PULS mit PULS 4 Austria News')).toBe('café puls');
      expect(normalizeSeriesTitle('Café PULS mit PULS 4 Aktuell')).toBe('café puls');
      expect(normalizeSeriesTitle('Café PULS')).toBe('café puls');
    });

    it('normalizes show titles with subtitles or episode colons', () => {
      expect(normalizeSeriesTitle('Tatort: Höllenfahrt')).toBe('tatort');
      expect(normalizeSeriesTitle('Tatort: Murot und das Gesetz des Karmas')).toBe('tatort');
    });

    it('normalizes titles with dash separators', () => {
      expect(normalizeSeriesTitle('ZIB 2 - Das Magazin')).toBe('zib 2');
      expect(normalizeSeriesTitle('Report – Spezial')).toBe('report');
    });

    it('normalizes titles with season/episode markings and dates', () => {
      expect(normalizeSeriesTitle('Unser Club S01E03')).toBe('unser club');
      expect(normalizeSeriesTitle('Tagesschau (2026-10-07)')).toBe('tagesschau');
    });

    it('handles empty or missing titles gracefully', () => {
      expect(normalizeSeriesTitle('')).toBe('');
      expect(normalizeSeriesTitle(undefined)).toBe('');
    });
  });

  describe('isItemVirtuallyFinished', () => {
    it('returns true when remaining time is 2 minutes or less', () => {
      const item: ContinueWatchingItem = {
        recordingId: 'rec-1',
        title: 'Angel Has Fallen',
        posSeconds: 7140,
        durationSeconds: 7200, // 60s remaining
      };
      expect(isItemVirtuallyFinished(item)).toBe(true);
    });

    it('returns true when over 93% has been watched', () => {
      const item: ContinueWatchingItem = {
        recordingId: 'rec-2',
        title: 'Café PULS',
        posSeconds: 5700,
        durationSeconds: 6000, // 95% watched
      };
      expect(isItemVirtuallyFinished(item)).toBe(true);
    });

    it('returns false when substantial portion is still unwatched', () => {
      const item: ContinueWatchingItem = {
        recordingId: 'rec-3',
        title: 'Tatort',
        posSeconds: 1800,
        durationSeconds: 5400, // 33% watched, 60m remaining
      };
      expect(isItemVirtuallyFinished(item)).toBe(false);
    });

    it('returns false when duration is unknown or zero', () => {
      const item: ContinueWatchingItem = {
        recordingId: 'rec-4',
        title: 'Live Recording',
        posSeconds: 300,
        durationSeconds: 0,
      };
      expect(isItemVirtuallyFinished(item)).toBe(false);
    });
  });

  describe('progressPercent', () => {
    it('calculates progress accurately', () => {
      expect(progressPercent({ recordingId: '1', posSeconds: 50, durationSeconds: 100 })).toBe(50);
      expect(progressPercent({ recordingId: '1', posSeconds: 0, durationSeconds: 100 })).toBe(0);
      expect(progressPercent({ recordingId: '1', posSeconds: 100, durationSeconds: 0 })).toBe(0);
    });
  });

  describe('resolveItemMonogram', () => {
    it('generates 2-letter monograms correctly', () => {
      expect(resolveItemMonogram('Café PULS')).toBe('CP');
      expect(resolveItemMonogram('Tatort')).toBe('TA');
      expect(resolveItemMonogram('')).toBe('REC');
    });
  });

  describe('filterAndDeduplicateContinueWatching', () => {
    it('deduplicates multiple episodes of the same show and keeps the latest', () => {
      const items: ContinueWatchingItem[] = [
        {
          recordingId: 'rec-old',
          title: 'Café PULS mit PULS 4 Aktuell',
          posSeconds: 200,
          durationSeconds: 3600,
          updatedAt: '2026-10-06T08:00:00Z',
        },
        {
          recordingId: 'rec-new',
          title: 'Café PULS mit PULS 4 Austria News',
          posSeconds: 1200,
          durationSeconds: 3600,
          updatedAt: '2026-10-07T08:00:00Z',
        },
        {
          recordingId: 'rec-older',
          title: 'Café PULS',
          posSeconds: 500,
          durationSeconds: 3600,
          updatedAt: '2026-10-05T08:00:00Z',
        },
      ];

      const result = filterAndDeduplicateContinueWatching(items, 4);
      expect(result).toHaveLength(1);
      expect(result[0]?.recordingId).toBe('rec-new');
    });

    it('drops items below posSeconds threshold (< 15s) and virtually finished items', () => {
      const items: ContinueWatchingItem[] = [
        {
          recordingId: 'rec-accidental',
          title: 'Briefly Opened',
          posSeconds: 8,
          durationSeconds: 3600,
        },
        {
          recordingId: 'rec-finished',
          title: 'Angel Has Fallen',
          posSeconds: 7150,
          durationSeconds: 7200, // 50s left -> virtually finished!
        },
        {
          recordingId: 'rec-valid',
          title: 'Unser Club',
          posSeconds: 600,
          durationSeconds: 1800,
          updatedAt: '2026-10-07T09:00:00Z',
        },
      ];

      const result = filterAndDeduplicateContinueWatching(items, 4);
      expect(result).toHaveLength(1);
      expect(result[0]?.recordingId).toBe('rec-valid');
    });

    it('caps output to maximum limit (default 4 items)', () => {
      const items: ContinueWatchingItem[] = [
        { recordingId: '1', title: 'Show A', posSeconds: 100, durationSeconds: 1000, updatedAt: '2026-10-07T01:00:00Z' },
        { recordingId: '2', title: 'Show B', posSeconds: 100, durationSeconds: 1000, updatedAt: '2026-10-07T02:00:00Z' },
        { recordingId: '3', title: 'Show C', posSeconds: 100, durationSeconds: 1000, updatedAt: '2026-10-07T03:00:00Z' },
        { recordingId: '4', title: 'Show D', posSeconds: 100, durationSeconds: 1000, updatedAt: '2026-10-07T04:00:00Z' },
        { recordingId: '5', title: 'Show E', posSeconds: 100, durationSeconds: 1000, updatedAt: '2026-10-07T05:00:00Z' },
        { recordingId: '6', title: 'Show F', posSeconds: 100, durationSeconds: 1000, updatedAt: '2026-10-07T06:00:00Z' },
      ];

      const result = filterAndDeduplicateContinueWatching(items, 4);
      expect(result).toHaveLength(4);
      // Newest should be first: 6, 5, 4, 3
      expect(result.map((r) => r.recordingId)).toEqual(['6', '5', '4', '3']);
    });
  });
});
