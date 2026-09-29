// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import type { RecordingItem } from '../../client-ts';

export type RecordingCategory = 'all' | 'movies' | 'series' | 'sport';

export interface SeriesGroup {
  seriesTitle: string;
  normalizedKey: string;
  episodes: RecordingItem[];
  latestBeginUnixSeconds: number;
  totalDurationSeconds: number;
}

export function normalizeTitle(text?: string): string {
  if (!text) return '';
  return text
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase()
    .replace(/ß/g, 'ss')
    .replace(/\s+/g, ' ')
    .trim();
}

const SPORT_PATTERNS = [
  /\b(sport|sports|sportschau|sportstudio)\b/i,
  /\b(fussball|fußball|football|soccer)\b/i,
  /\b(bundesliga|premier\s*league|la\s*liga|serie\s*a|ligue\s*1)\b/i,
  /\b(champions\s*league|europa\s*league|conference\s*league)\b/i,
  /\b(dfb-pokal|dfb\s*pokal|öfb-cup|öfb\s*cup)\b/i,
  /\b(wm\s*20\d\d|em\s*20\d\d|weltmeisterschaft|europameisterschaft)\b/i,
  /\b(formel\s*1|formula\s*1|f1|motogp|motorsport|grand\s*prix)\b/i,
  /\b(tennis|atp|wta|wimbledon|us\s*open|roland\s*garros|australian\s*open)\b/i,
  /\b(ski|ski\s*alpin|skispringen|biathlon|langlauf|slalom|abfahrt|nordische)\b/i,
  /\b(olympia|olympiade|olympics|olympische)\b/i,
  /\b(eishockey|ice\s*hockey|nhl|del)\b/i,
  /\b(basketball|nba)\b/i,
  /\b(nfl|super\s*bowl|american\s*football)\b/i,
  /\b(radsport|tour\s*de\s*france|giro\s*d'italia|vuelta)\b/i,
  /\b(darts|boxen|boxing|ufc|mma|wrestling)\b/i,
  /\b(golf|pga)\b/i,
  /\b(handball)\b/i,
];

const EPISODE_PATTERNS = [
  /\bS\d+\s*E\d+\b/i,               // S01E05, S1E2
  /\b\d+x\d+\b/i,                    // 1x05
  /\b(Staffel|Season)\s+\d+\b/i,    // Staffel 1
  /\b(Folge|Episode)\s+\d+\b/i,     // Folge 12
  /\bTeil\s+\d+\b/i,                // Teil 2
  /\(\d+\/\d+\)/,                   // (1/10)
];

const SERIES_GENRE_PATTERNS = [
  /\b(serie|fernsehserie|tv-serie|soap|sitcom|daily|telenovela)\b/i,
  /\b(magazin|nachrichten|journal|talkshow|reportage)\b/i,
];

const KNOWN_SERIES_PREFIXES = [
  'cafe puls',
  'café puls',
  'tatort',
  'polizeiruf 110',
  'der alte',
  'soko',
  'zib',
  'tagesschau',
  'tagesthemen',
  'heute-journal',
  'heute journal',
  'puls 24 news',
  'puls 4 news',
  'universum',
  'terra x',
  'heimatleuchten',
  'die simpsons',
  'the simpsons',
  'the big bang theory',
  'grey\'s anatomy',
  'greys anatomy',
  'gzsz',
  'gute zeiten, schlechte zeiten',
  'rote rosen',
  'sturm der liebe',
  'in aller freundschaft',
];

const MOVIE_PATTERNS = [
  /\b(spielfilm|film|movie|kino|kinofilm)\b/i,
  /\b(thriller|komödie|comedy|drama|actionfilm|liebesfilm|krimi|horror|western)\b/i,
  /\b(science\s*fiction|sci-fi|fantasy|abenteuerfilm|animationsfilm|zeichentrickfilm)\b/i,
];

export function extractSeriesTitle(title?: string, knownSeriesKeywords: string[] = []): string {
  const rawTitle = String(title || '').trim();
  if (!rawTitle) return '';

  const normTitle = normalizeTitle(rawTitle);

  // 1. Check knownSeriesKeywords first (e.g. configured Series Rules)
  if (knownSeriesKeywords && knownSeriesKeywords.length > 0) {
    for (const kw of knownSeriesKeywords) {
      const normKw = normalizeTitle(kw);
      if (normKw && (normTitle === normKw || normTitle.startsWith(normKw + ' ') || normTitle.startsWith(normKw + ':') || normTitle.startsWith(normKw + '-'))) {
        return kw.trim();
      }
    }
  }

  // 2. Check built-in known series prefixes
  for (const prefix of KNOWN_SERIES_PREFIXES) {
    if (normTitle === prefix || normTitle.startsWith(prefix + ' ') || normTitle.startsWith(prefix + ':') || normTitle.startsWith(prefix + '-')) {
      if (prefix.includes('puls') || prefix.includes('cafe')) {
        return 'Café PULS';
      }
      return rawTitle.slice(0, prefix.length).trim();
    }
  }

  // 3. Strip episodic tokens (S01E02, Staffel X, Folge Y, etc.)
  let cleaned = rawTitle.replace(/\s*[-–—:]?\s*(S\d+\s*E\d+|Staffel\s+\d+|Folge\s+\d+|Episode\s+\d+|\d+x\d+|Teil\s+\d+|\(\d+\/\d+\)).*$/i, '');

  // 4. Strip date tokens (vom DD.MM.YYYY, DD.MM.YYYY, YYYY-MM-DD, etc.)
  cleaned = cleaned.replace(/\s*[-–—:]?\s*(vom\s+\d{1,2}\.\d{1,2}\.?(\d{2,4})?|\d{1,2}\.\d{1,2}\.\d{2,4}|\d{4}-\d{2}-\d{2}).*$/i, '');

  // 5. If title contains a colon ':' or dash ' - ', e.g. "Tatort: Der Fall", strip subtitle
  const colonMatch = cleaned.match(/^([^:]+):/);
  const colonPrefix = colonMatch?.[1]?.trim();
  if (colonPrefix && colonPrefix.length >= 3) {
    cleaned = colonPrefix;
  }

  const dashMatch = cleaned.match(/^([^-–—]+)\s+[-–—]\s+/);
  const dashPrefix = dashMatch?.[1]?.trim();
  if (dashPrefix && dashPrefix.length >= 3) {
    cleaned = dashPrefix;
  }

  return cleaned.trim() || rawTitle;
}

export function classifyRecording(
  rec: RecordingItem,
  knownSeriesKeywords: string[] = []
): 'movies' | 'series' | 'sport' | 'other' {
  const title = rec.title || '';
  const desc = rec.description || '';
  const combined = `${title} ${desc}`;
  const normTitle = normalizeTitle(title);
  const normCombined = normalizeTitle(combined);

  // 1. Sport Check
  if (SPORT_PATTERNS.some((p) => p.test(normCombined))) {
    return 'sport';
  }

  // 2. Known Series Rules Check
  if (knownSeriesKeywords.length > 0) {
    for (const kw of knownSeriesKeywords) {
      const normKw = normalizeTitle(kw);
      if (normKw && (normTitle === normKw || normTitle.includes(normKw))) {
        return 'series';
      }
    }
  }

  // 3. Known Series Prefixes Check
  if (KNOWN_SERIES_PREFIXES.some((prefix) => normTitle === prefix || normTitle.startsWith(prefix + ' ') || normTitle.startsWith(prefix + ':') || normTitle.startsWith(prefix + '-'))) {
    return 'series';
  }

  // 4. Episodic Tokens Check
  if (EPISODE_PATTERNS.some((p) => p.test(combined))) {
    return 'series';
  }

  // 5. Series Genre Tokens Check
  if (SERIES_GENRE_PATTERNS.some((p) => p.test(combined))) {
    return 'series';
  }

  // 6. Movie Check
  if (MOVIE_PATTERNS.some((p) => p.test(normCombined))) {
    return 'movies';
  }

  // 7. Duration Check: >= 70 minutes (4200 seconds) without episodic markers -> Movie
  const duration = rec.durationSeconds ?? rec.resume?.durationSeconds ?? 0;
  if (duration >= 4200) {
    return 'movies';
  }

  return 'other';
}

export function groupRecordings(
  recordings: RecordingItem[],
  knownSeriesKeywords: string[] = []
): {
  seriesGroups: SeriesGroup[];
  classifiedMap: Map<string, 'movies' | 'series' | 'sport' | 'other'>;
} {
  const classifiedMap = new Map<string, 'movies' | 'series' | 'sport' | 'other'>();
  const seriesGroupsByKey = new Map<string, SeriesGroup>();

  // Pass 1: compute initial classification and series titles
  for (const rec of recordings) {
    const recId = rec.recordingId || `${rec.title}-${rec.beginUnixSeconds}`;
    const initialClass = classifyRecording(rec, knownSeriesKeywords);
    classifiedMap.set(recId, initialClass);

    const seriesTitle = extractSeriesTitle(rec.title, knownSeriesKeywords);
    const normalizedKey = normalizeTitle(seriesTitle);

    if (!normalizedKey) continue;

    const existingGroup = seriesGroupsByKey.get(normalizedKey);
    if (existingGroup) {
      existingGroup.episodes.push(rec);
      const recBegin = rec.beginUnixSeconds || 0;
      if (recBegin > existingGroup.latestBeginUnixSeconds) {
        existingGroup.latestBeginUnixSeconds = recBegin;
      }
      existingGroup.totalDurationSeconds += (rec.durationSeconds ?? rec.resume?.durationSeconds ?? 0);
    } else {
      seriesGroupsByKey.set(normalizedKey, {
        seriesTitle,
        normalizedKey,
        episodes: [rec],
        latestBeginUnixSeconds: rec.beginUnixSeconds || 0,
        totalDurationSeconds: rec.durationSeconds ?? rec.resume?.durationSeconds ?? 0,
      });
    }
  }

  // Pass 2: If a group has >= 2 episodes, ensure all member episodes are classified as 'series'
  for (const group of seriesGroupsByKey.values()) {
    if (group.episodes.length >= 2) {
      for (const rec of group.episodes) {
        const recId = rec.recordingId || `${rec.title}-${rec.beginUnixSeconds}`;
        const currentClass = classifiedMap.get(recId);
        if (currentClass !== 'sport') {
          classifiedMap.set(recId, 'series');
        }
      }
    }
  }

  // Pass 3: Filter seriesGroups for actual series (either classified as series, or >= 2 episodes)
  const validSeriesGroups: SeriesGroup[] = [];
  for (const group of seriesGroupsByKey.values()) {
    const isSeries = group.episodes.length >= 2 || group.episodes.some((rec) => {
      const recId = rec.recordingId || `${rec.title}-${rec.beginUnixSeconds}`;
      return classifiedMap.get(recId) === 'series';
    });

    if (isSeries) {
      // Sort episodes inside the group newest first by default
      group.episodes.sort((a, b) => (b.beginUnixSeconds || 0) - (a.beginUnixSeconds || 0));
      validSeriesGroups.push(group);
    }
  }

  // Sort series groups by latest recording timestamp
  validSeriesGroups.sort((a, b) => b.latestBeginUnixSeconds - a.latestBeginUnixSeconds);

  return {
    seriesGroups: validSeriesGroups,
    classifiedMap,
  };
}
