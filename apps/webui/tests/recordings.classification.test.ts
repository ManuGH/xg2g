import { describe, it, expect } from 'vitest';
import {
  classifyRecording,
  extractSeriesTitle,
  groupRecordings,
  normalizeTitle,
  type RecordingCategory,
} from '../src/features/recordings/classification';
import type { RecordingItem } from '../src/client-ts';

describe('recordings classification & grouping', () => {
  describe('normalizeTitle', () => {
    it('normalizes accents, casing and whitespace', () => {
      expect(normalizeTitle('Café  PULS ')).toBe('cafe puls');
      expect(normalizeTitle('Fußball')).toBe('fussball');
      expect(normalizeTitle('ZIB 2: Spezial')).toBe('zib 2: spezial');
    });
  });

  describe('extractSeriesTitle', () => {
    it('normalizes Cafe Puls variations to Café PULS', () => {
      expect(extractSeriesTitle('Café PULS mit PULS 4 Aktuell')).toBe('Café PULS');
      expect(extractSeriesTitle('Café PULS - Das Magazin')).toBe('Café PULS');
      expect(extractSeriesTitle('Café PULS vom 29.09.2026')).toBe('Café PULS');
      expect(extractSeriesTitle('Cafe Puls')).toBe('Café PULS');
    });

    it('extracts series title before colon or dash', () => {
      expect(extractSeriesTitle('Tatort: Schaukelstuhl')).toBe('Tatort');
      expect(extractSeriesTitle('Universum: Wilde Arktis')).toBe('Universum');
      expect(extractSeriesTitle('Polizeiruf 110: Der Fall')).toBe('Polizeiruf 110');
    });

    it('extracts series title before season and episode tokens', () => {
      expect(extractSeriesTitle('The Big Bang Theory - Staffel 4, Folge 12')).toBe('The Big Bang Theory');
      expect(extractSeriesTitle('Die Simpsons - S12E04 - Lisa als Baumschützerin')).toBe('Die Simpsons');
      expect(extractSeriesTitle('Dark S01E01 Secrets')).toBe('Dark');
    });

    it('matches configured series rule keywords', () => {
      expect(extractSeriesTitle('Café PULS Frühfernsehen', ['Café PULS'])).toBe('Café PULS');
      expect(extractSeriesTitle('Bergdoktor - Neue Staffel', ['Bergdoktor'])).toBe('Bergdoktor');
    });
  });

  describe('classifyRecording', () => {
    it('classifies sports accurately', () => {
      const rec1: RecordingItem = {
        recordingId: 'r1',
        title: 'Bundesliga: Sturm Graz - Rapid Wien',
        status: 'completed',
        durationSeconds: 6000,
      };
      expect(classifyRecording(rec1)).toBe('sport');

      const rec2: RecordingItem = {
        recordingId: 'r2',
        title: 'Formel 1: Großer Preis von Österreich',
        status: 'completed',
        durationSeconds: 7200,
      };
      expect(classifyRecording(rec2)).toBe('sport');

      const rec3: RecordingItem = {
        recordingId: 'r3',
        title: 'UEFA Champions League Live',
        status: 'completed',
        durationSeconds: 6500,
      };
      expect(classifyRecording(rec3)).toBe('sport');
    });

    it('classifies series accurately based on show patterns, keywords and tokens', () => {
      const rec1: RecordingItem = {
        recordingId: 'r1',
        title: 'Café PULS mit PULS 4 Aktuell',
        status: 'completed',
        durationSeconds: 3600,
      };
      expect(classifyRecording(rec1)).toBe('series');

      const rec2: RecordingItem = {
        recordingId: 'r2',
        title: 'Die Simpsons - S12E04',
        status: 'completed',
        durationSeconds: 1500,
      };
      expect(classifyRecording(rec2)).toBe('series');

      const rec3: RecordingItem = {
        recordingId: 'r3',
        title: 'Tatort: Schaukelstuhl',
        status: 'completed',
        durationSeconds: 5400,
      };
      expect(classifyRecording(rec3)).toBe('series');

      const rec4: RecordingItem = {
        recordingId: 'r4',
        title: 'Custom Show',
        description: 'Neue Folge der Fernsehserie.',
        status: 'completed',
        durationSeconds: 2700,
      };
      expect(classifyRecording(rec4)).toBe('series');
    });

    it('classifies movies accurately based on movie keywords or duration >= 70 min', () => {
      const rec1: RecordingItem = {
        recordingId: 'r1',
        title: 'Inception',
        description: 'Ein packender Spielfilm von Christopher Nolan.',
        status: 'completed',
        durationSeconds: 8800,
      };
      expect(classifyRecording(rec1)).toBe('movies');

      const rec2: RecordingItem = {
        recordingId: 'r2',
        title: 'Ein unbekannter Thriller',
        status: 'completed',
        durationSeconds: 5000,
      };
      expect(classifyRecording(rec2)).toBe('movies');
    });
  });

  describe('groupRecordings', () => {
    it('groups multiple episodes of Café PULS into a single series group', () => {
      const recordings: RecordingItem[] = [
        {
          recordingId: 'cp1',
          title: 'Café PULS',
          beginUnixSeconds: 1727600000,
          durationSeconds: 3600,
          status: 'completed',
        },
        {
          recordingId: 'cp2',
          title: 'Café PULS mit PULS 4 Aktuell',
          beginUnixSeconds: 1727686400,
          durationSeconds: 3600,
          status: 'completed',
        },
        {
          recordingId: 'cp3',
          title: 'Café PULS - Das Magazin',
          beginUnixSeconds: 1727772800,
          durationSeconds: 1800,
          status: 'completed',
        },
        {
          recordingId: 'movie1',
          title: 'Inception',
          description: 'Spielfilm',
          beginUnixSeconds: 1727500000,
          durationSeconds: 8800,
          status: 'completed',
        },
        {
          recordingId: 'sport1',
          title: 'Formel 1: Rennen',
          beginUnixSeconds: 1727550000,
          durationSeconds: 7200,
          status: 'completed',
        },
      ];

      const { seriesGroups, classifiedMap } = groupRecordings(recordings);

      expect(seriesGroups).toHaveLength(1);
      const cpGroup = seriesGroups[0];
      expect(cpGroup.seriesTitle).toBe('Café PULS');
      expect(cpGroup.episodes).toHaveLength(3);
      expect(cpGroup.latestBeginUnixSeconds).toBe(1727772800);
      expect(cpGroup.totalDurationSeconds).toBe(9000);

      // Verify individual classification in map
      expect(classifiedMap.get('cp1')).toBe('series');
      expect(classifiedMap.get('cp2')).toBe('series');
      expect(classifiedMap.get('cp3')).toBe('series');
      expect(classifiedMap.get('movie1')).toBe('movies');
      expect(classifiedMap.get('sport1')).toBe('sport');
    });

    it('upgrades recurring titles (>= 2) to series even without explicit genre', () => {
      const recordings: RecordingItem[] = [
        {
          recordingId: 'show1',
          title: 'Gartenlust - Folge 1',
          beginUnixSeconds: 1000,
          status: 'completed',
        },
        {
          recordingId: 'show2',
          title: 'Gartenlust - Folge 2',
          beginUnixSeconds: 2000,
          status: 'completed',
        },
      ];

      const { seriesGroups, classifiedMap } = groupRecordings(recordings);

      expect(seriesGroups).toHaveLength(1);
      expect(seriesGroups[0].seriesTitle).toBe('Gartenlust');
      expect(seriesGroups[0].episodes).toHaveLength(2);
      expect(classifiedMap.get('show1')).toBe('series');
      expect(classifiedMap.get('show2')).toBe('series');
    });

    it('does NOT upgrade repeated movie broadcasts to series without positive series evidence', () => {
      const recordings: RecordingItem[] = [
        {
          recordingId: 'm1',
          title: 'Inception',
          description: 'Spielfilm von Christopher Nolan',
          beginUnixSeconds: 1000,
          durationSeconds: 8800,
          status: 'completed',
        },
        {
          recordingId: 'm2',
          title: 'Inception',
          description: 'Spielfilm von Christopher Nolan (Wdh.)',
          beginUnixSeconds: 5000,
          durationSeconds: 8800,
          status: 'completed',
        },
      ];

      const { seriesGroups, classifiedMap } = groupRecordings(recordings);

      expect(seriesGroups).toHaveLength(0);
      expect(classifiedMap.get('m1')).toBe('movies');
      expect(classifiedMap.get('m2')).toBe('movies');
    });

    it('upgrades movies to series if an explicit series rule keyword is configured', () => {
      const recordings: RecordingItem[] = [
        {
          recordingId: 'm1',
          title: 'James Bond 007 - Skyfall',
          description: 'Actionfilm',
          beginUnixSeconds: 1000,
          durationSeconds: 8400,
          status: 'completed',
        },
        {
          recordingId: 'm2',
          title: 'James Bond 007 - Spectre',
          description: 'Actionfilm',
          beginUnixSeconds: 5000,
          durationSeconds: 8600,
          status: 'completed',
        },
      ];

      const { seriesGroups, classifiedMap } = groupRecordings(recordings, ['James Bond 007']);

      expect(seriesGroups).toHaveLength(1);
      expect(seriesGroups[0].seriesTitle).toBe('James Bond 007');
      expect(classifiedMap.get('m1')).toBe('series');
      expect(classifiedMap.get('m2')).toBe('series');
    });
  });
});
