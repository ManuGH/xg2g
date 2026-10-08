import { describe, it, expect } from 'vitest';
import {
  cleanNormalize,
  extractMetadata,
  findRerunsAndBroadcasts,
} from './rerunMatcher';
import type { EpgEvent, EpgChannel } from '../types';

describe('rerunMatcher', () => {
  const dummyChannels: EpgChannel[] = [
    { serviceRef: '1:0:19:132F:3EF:1:C00000:0:0:0:', name: 'ORF1 HD' },
    { serviceRef: '1:0:19:1330:3EF:1:C00000:0:0:0:', name: 'ORF2 HD' },
    { serviceRef: '1:0:16:332D:3EB:1:C00000:0:0:0:', name: 'PULS 4 Austria' },
    { serviceRef: '1:0:16:445D:453:1:C00000:0:0:0:', name: 'ProSieben Austria' },
    { serviceRef: '1:0:16:4460:453:1:C00000:0:0:0:', name: 'Kabel Eins Austria' },
    { serviceRef: '1:0:19:2B66:3F3:1:C00000:0:0:0:', name: 'ZDF HD' },
  ];

  const now = 1791396000; // Reference timestamp: ~20:00

  describe('cleanNormalize & extractMetadata', () => {
    it('normalizes german umlauts and broadcast tags', () => {
      expect(cleanNormalize('Top Gun [HD] (Wdh.)')).toBe('top gun');
      expect(cleanNormalize('Über die Dächer von Nizza')).toBe('ueber die daecher von nizza');
    });

    it('extracts year and classifies movie correctly', () => {
      const event: EpgEvent = {
        serviceRef: '1:0:16:332D:3EB:1:C00000:0:0:0:',
        title: 'Top Gun (1986)',
        desc: 'Actionfilm, USA 1986. Mit Tom Cruise, Kelly McGillis. Regie: Tony Scott.',
        start: now,
        end: now + 6600, // 110 min
      };
      const meta = extractMetadata(event);
      expect(meta.classification).toBe('movie');
      expect(meta.year).toBe(1986);
      expect(meta.mainTitle).toBe('Top Gun');
      expect(meta.durationMinutes).toBe(110);
    });

    it('extracts season/episode and classifies series correctly', () => {
      const event: EpgEvent = {
        serviceRef: '1:0:16:445D:453:1:C00000:0:0:0:',
        title: 'Monk - Mr. Monk und die Hellseherin',
        desc: 'Staffel 3, Folge 7. Mr. Monk und die Hellseherin. Comedy-Krimi, USA 2004.',
        start: now,
        end: now + 3000, // 50 min
      };
      const meta = extractMetadata(event);
      expect(meta.classification).toBe('series');
      expect(meta.season).toBe(3);
      expect(meta.episode).toBe(7);
      expect(meta.mainTitle).toBe('Monk');
      expect(meta.subTitle).toBe('Mr. Monk und die Hellseherin');
    });
  });

  describe('Movie reruns (Filme)', () => {
    const targetMovie: EpgEvent = {
      serviceRef: '1:0:16:332D:3EB:1:C00000:0:0:0:', // PULS 4
      title: 'Top Gun (1986)',
      desc: 'Actionfilm, USA 1986. Mit Tom Cruise. Regie: Tony Scott.',
      start: now + 3600, // in 1h
      end: now + 3600 + 6600, // 110 min
    };

    it('finds valid reruns on same channel and cross-channel', () => {
      const candidates: EpgEvent[] = [
        // Rerun on PULS 4 next day
        {
          serviceRef: '1:0:16:332D:3EB:1:C00000:0:0:0:',
          title: 'Top Gun',
          desc: 'Actionfilm, USA 1986. Tony Scott inszeniert Tom Cruise.',
          start: now + 86400,
          end: now + 86400 + 6300, // 105 min
        },
        // Rerun on Kabel Eins in 3 days
        {
          serviceRef: '1:0:16:4460:453:1:C00000:0:0:0:',
          title: 'Top Gun (1986)',
          desc: 'Actionfilm (1986). Mit Tom Cruise, Val Kilmer.',
          start: now + 259200,
          end: now + 259200 + 6900, // 115 min
        },
      ];

      const res = findRerunsAndBroadcasts(targetMovie, candidates, dummyChannels, now);
      expect(res.programmeType).toBe('movie');
      expect(res.sameEpisodeReruns).toHaveLength(2);
      expect(res.sameEpisodeReruns[0]?.channelName).toBe('PULS 4 Austria');
      expect(res.sameEpisodeReruns[1]?.channelName).toBe('Kabel Eins Austria');
    });

    it('strictly rejects sequels like "Top Gun: Maverick"', () => {
      const candidates: EpgEvent[] = [
        {
          serviceRef: '1:0:16:445D:453:1:C00000:0:0:0:', // ProSieben
          title: 'Top Gun: Maverick (2022)',
          desc: 'Actionfilm, USA 2022. Nach mehr als 30 Jahren kehrt Pete Mitchell zurück.',
          start: now + 86400,
          end: now + 86400 + 7800, // 130 min
        },
      ];

      const res = findRerunsAndBroadcasts(targetMovie, candidates, dummyChannels, now);
      expect(res.sameEpisodeReruns).toHaveLength(0);
      expect(res.otherEpisodes).toHaveLength(0);
    });

    it('strictly rejects movies with conflicting production years', () => {
      const candidates: EpgEvent[] = [
        {
          serviceRef: '1:0:16:445D:453:1:C00000:0:0:0:',
          title: 'Top Gun (2024)',
          desc: 'Dokumentation (2024).',
          start: now + 86400,
          end: now + 86400 + 6600,
        },
      ];

      const res = findRerunsAndBroadcasts(targetMovie, candidates, dummyChannels, now);
      expect(res.sameEpisodeReruns).toHaveLength(0);
    });

    it('strictly rejects wildly differing runtimes (> 25 min difference)', () => {
      const candidates: EpgEvent[] = [
        {
          serviceRef: '1:0:16:332D:3EB:1:C00000:0:0:0:',
          title: 'Top Gun',
          desc: 'Kurzclip / Making of.',
          start: now + 86400,
          end: now + 86400 + 900, // only 15 min!
        },
      ];

      const res = findRerunsAndBroadcasts(targetMovie, candidates, dummyChannels, now);
      expect(res.sameEpisodeReruns).toHaveLength(0);
    });
  });

  describe('Series reruns (Serien)', () => {
    const targetEpisode: EpgEvent = {
      serviceRef: '1:0:16:445D:453:1:C00000:0:0:0:', // ProSieben
      title: 'Monk - Mr. Monk und die Hellseherin',
      desc: 'Staffel 3, Folge 7: Mr. Monk und die Hellseherin. USA 2004.',
      start: now + 1800,
      end: now + 1800 + 3000, // 50 min
    };

    it('distinguishes between exact same episode and other upcoming episodes', () => {
      const candidates: EpgEvent[] = [
        // Rerun of the exact same episode on PULS 4
        {
          serviceRef: '1:0:16:332D:3EB:1:C00000:0:0:0:',
          title: 'Monk (S03E07)',
          desc: 'Mr. Monk und die Hellseherin. Comedy-Krimi.',
          start: now + 86400,
          end: now + 86400 + 3000,
        },
        // Next episode of the series (S03E08)
        {
          serviceRef: '1:0:16:445D:453:1:C00000:0:0:0:',
          title: 'Monk (S03E08)',
          desc: 'Staffel 3, Folge 8: Mr. Monk und der andere Detektiv.',
          start: now + 172800,
          end: now + 172800 + 3000,
        },
      ];

      const res = findRerunsAndBroadcasts(targetEpisode, candidates, dummyChannels, now);
      expect(res.programmeType).toBe('series');
      expect(res.sameEpisodeReruns).toHaveLength(1);
      expect(res.sameEpisodeReruns[0]?.event.title).toBe('Monk (S03E07)');
      expect(res.sameEpisodeReruns[0]?.channelName).toBe('PULS 4 Austria');

      expect(res.otherEpisodes).toHaveLength(1);
      expect(res.otherEpisodes[0]?.event.title).toBe('Monk (S03E08)');
      expect(res.otherEpisodes[0]?.channelName).toBe('ProSieben Austria');
    });
  });

  describe('Negative Rule ("Nur Titel gleich: nicht anzeigen")', () => {
    it('does not display recurring generic news with just title match', () => {
      const targetNews: EpgEvent = {
        serviceRef: '1:0:19:2B66:3F3:1:C00000:0:0:0:',
        title: 'heute',
        desc: 'Nachrichten des Tages mit aktuellen Berichten.',
        start: now,
        end: now + 900, // 15 min
      };

      const candidates: EpgEvent[] = [
        {
          serviceRef: '1:0:19:2B66:3F3:1:C00000:0:0:0:',
          title: 'heute',
          desc: 'Nachrichten des Tages.',
          start: now + 86400,
          end: now + 86400 + 900,
        },
      ];

      const res = findRerunsAndBroadcasts(targetNews, candidates, dummyChannels, now);
      expect(res.sameEpisodeReruns).toHaveLength(0);
      expect(res.otherEpisodes).toHaveLength(0);
    });

    it('excludes past broadcasts from future reruns list', () => {
      const targetMovie: EpgEvent = {
        serviceRef: '1:0:16:332D:3EB:1:C00000:0:0:0:',
        title: 'Top Gun (1986)',
        desc: 'Actionfilm, USA 1986.',
        start: now + 3600,
        end: now + 10200,
      };

      const candidates: EpgEvent[] = [
        {
          serviceRef: '1:0:16:332D:3EB:1:C00000:0:0:0:',
          title: 'Top Gun (1986)',
          desc: 'Gestern gelaufen.',
          start: now - 86400,
          end: now - 79800, // in the past!
        },
      ];

      const res = findRerunsAndBroadcasts(targetMovie, candidates, dummyChannels, now);
      expect(res.sameEpisodeReruns).toHaveLength(0);
    });
  });
});
