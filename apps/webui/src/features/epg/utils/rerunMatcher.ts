// Rerun and Alternate Broadcasts Matcher
// High-confidence, cross-channel matching engine for movies and series episodes

import type { EpgEvent, EpgChannel } from '../types';

export type ProgrammeClassification = 'movie' | 'series' | 'other';

export type MatchConfidence = 'crid' | 'season_episode' | 'episode_title' | 'movie_metadata';

export interface ExtractedMetadata {
  rawTitle: string;
  normalizedTitle: string;
  mainTitle: string;
  subTitle?: string;
  season?: number;
  episode?: number;
  totalEpisodes?: number;
  year?: number;
  durationMinutes: number;
  isSeries: boolean;
  isMovie: boolean;
  classification: ProgrammeClassification;
}

export interface MatchedBroadcast {
  event: EpgEvent;
  channel?: EpgChannel;
  channelName: string;
  isSameEpisode: boolean;
  confidence: MatchConfidence;
  confidenceLabel?: string;
}

export interface RerunMatchResult {
  programmeType: ProgrammeClassification;
  targetMetadata: ExtractedMetadata;
  sameEpisodeReruns: MatchedBroadcast[];
  otherEpisodes: MatchedBroadcast[];
}

const BROADCAST_NOISE_REGEX = /\b(?:HD|UHD|4K|2K|SD|Live|Direkt|Wdh|Wiederholung|Erstausstrahlung|Neu|Dolby|Stereo|5[.]1)\b/gi;
const FRACTIONAL_EPISODE_REGEX = /\(\s*(\d{1,3})\s*\/\s*(\d{1,3})\s*\)/;
const SEASON_EPISODE_REGEX = /\bS(\d{1,2})[\s._/-]*E(\d{1,3})\b/i;
const COMPACT_SEASON_EPISODE_REGEX = /\b(\d{1,2})x(\d{1,3})\b/i;
const GERMAN_SEASON_EPISODE_REGEX = /Staffel\s*(\d{1,2})[\s,;]+(?:Folge|Episode)\s*(\d{1,3})/i;
const STANDALONE_EPISODE_REGEX = /\b(?:Folge|Episode)\s*(\d{1,3})\b/i;
const PARENTHESIZED_NUMBER_REGEX = /\(\s*(\d{1,4})\s*\)/;

const MOVIE_KEYWORD_REGEX = /\b(?:Spielfilm|Actionfilm|Komödie|Drama|Thriller|Krimi|Horror|Liebesfilm|Zeichentrickfilm|Animationsfilm|Kinofilm|Fernsehfilm|Film|Regie:|Darsteller:|Produktionsland)\b/i;

/**
 * Normalizes text for clean, insensitive comparison:
 * Lowercases, strips noise and punctuation, normalizes spaces, resolves common German diacritics.
 */
export function cleanNormalize(text?: string): string {
  if (!text) return '';
  return text
    .replace(BROADCAST_NOISE_REGEX, ' ')
    .toLowerCase()
    .replace(/[äàáâ]/g, 'ae')
    .replace(/[öòóô]/g, 'oe')
    .replace(/[üùúû]/g, 'ue')
    .replace(/ß/g, 'ss')
    .replace(/[^\w\s]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
}

/**
 * Extracts production year (1900-2099) from title or description.
 */
function extractYear(title: string, desc: string): number | undefined {
  // 1. Direct parenthesized 4-digit year: (1986), (2022)
  const parenMatch = title.match(PARENTHESIZED_NUMBER_REGEX) || desc.match(PARENTHESIZED_NUMBER_REGEX);
  if (parenMatch && parenMatch[1]) {
    const val = parseInt(parenMatch[1], 10);
    if (val >= 1900 && val <= 2099) {
      return val;
    }
  }

  // 2. Year following country code or genre: "USA 1986", "D 2021", "Österreich 2024", "Jahr: 1995"
  const countryYearMatch = (desc + ' ' + title).match(
    /(?:USA|D|GB|F|Österreich|Deutschland|Italien|Spanien|Jahr:?|vom:?|aus dem Jahr)\s*(?:,\s*)?(19\d{2}|20\d{2})\b/i
  );
  if (countryYearMatch && countryYearMatch[1]) {
    const val = parseInt(countryYearMatch[1], 10);
    if (val >= 1900 && val <= 2099) {
      return val;
    }
  }

  return undefined;
}

/**
 * Analyzes and extracts structural metadata from an EPG event.
 */
export function extractMetadata(event: EpgEvent): ExtractedMetadata {
  const rawTitle = (event.title || '').trim();
  const desc = (event.desc || '').trim();
  const durationSeconds = Math.max(0, event.end - event.start);
  const durationMinutes = Math.round(durationSeconds / 60);

  // 1. Extract year
  const year = extractYear(rawTitle, desc);

  // 2. Extract season / episode numbers from title and desc
  let season: number | undefined;
  let episode: number | undefined;
  let totalEpisodes: number | undefined;

  const seMatch = rawTitle.match(SEASON_EPISODE_REGEX) || desc.match(SEASON_EPISODE_REGEX);
  if (seMatch && seMatch[1] && seMatch[2]) {
    season = parseInt(seMatch[1], 10);
    episode = parseInt(seMatch[2], 10);
  } else {
    const compactMatch = rawTitle.match(COMPACT_SEASON_EPISODE_REGEX) || desc.match(COMPACT_SEASON_EPISODE_REGEX);
    if (compactMatch && compactMatch[1] && compactMatch[2]) {
      season = parseInt(compactMatch[1], 10);
      episode = parseInt(compactMatch[2], 10);
    } else {
      const deMatch = rawTitle.match(GERMAN_SEASON_EPISODE_REGEX) || desc.match(GERMAN_SEASON_EPISODE_REGEX);
      if (deMatch && deMatch[1] && deMatch[2]) {
        season = parseInt(deMatch[1], 10);
        episode = parseInt(deMatch[2], 10);
      } else {
        const fracMatch = rawTitle.match(FRACTIONAL_EPISODE_REGEX) || desc.match(FRACTIONAL_EPISODE_REGEX);
        if (fracMatch && fracMatch[1] && fracMatch[2]) {
          episode = parseInt(fracMatch[1], 10);
          totalEpisodes = parseInt(fracMatch[2], 10);
        } else {
          const epOnlyMatch = rawTitle.match(STANDALONE_EPISODE_REGEX) || desc.match(STANDALONE_EPISODE_REGEX);
          if (epOnlyMatch && epOnlyMatch[1]) {
            episode = parseInt(epOnlyMatch[1], 10);
          } else {
            const numMatch = rawTitle.match(PARENTHESIZED_NUMBER_REGEX);
            if (numMatch && numMatch[1]) {
              const val = parseInt(numMatch[1], 10);
              if (val > 0 && val < 1900) {
                episode = val;
              }
            }
          }
        }
      }
    }
  }

  // 3. Separate main title from subtitle / episode name
  let mainTitle = rawTitle;
  let subTitle: string | undefined;

  // Remove bracketed year/episode from main title: "Top Gun (1986)" -> "Top Gun"
  mainTitle = mainTitle.replace(PARENTHESIZED_NUMBER_REGEX, ' ');
  mainTitle = mainTitle.replace(FRACTIONAL_EPISODE_REGEX, ' ');
  mainTitle = mainTitle.replace(SEASON_EPISODE_REGEX, ' ');
  mainTitle = mainTitle.replace(BROADCAST_NOISE_REGEX, ' ');
  mainTitle = mainTitle.replace(/\s+/g, ' ').trim();

  // Split on subtitle separators: " - ", " – ", ": "
  const splitMatch = mainTitle.split(/\s+(?:–|-)\s+|:\s+/);
  if (splitMatch.length > 1 && splitMatch[0] && splitMatch[0].trim().length >= 3) {
    mainTitle = splitMatch[0].trim();
    subTitle = splitMatch.slice(1).join(' - ').trim();
  }

  // Also check if description starts with an explicit episode title: 'Staffel X, Folge Y: "Titel"'
  if (!subTitle && desc) {
    const descEpTitleMatch = desc.match(/(?:Folge|Episode)\s*\d+[:\s]+[„"']([^"'\n\r]+)[“"']/i);
    if (descEpTitleMatch && descEpTitleMatch[1]) {
      subTitle = descEpTitleMatch[1].trim();
    }
  }

  // 4. Classification (Series vs Movie vs Other)
  const hasEpisodeMarker = season !== undefined || episode !== undefined || totalEpisodes !== undefined;
  const hasMovieKeywords = MOVIE_KEYWORD_REGEX.test(desc) || MOVIE_KEYWORD_REGEX.test(rawTitle);

  let isSeries = false;
  let isMovie = false;
  let classification: ProgrammeClassification;

  if (hasEpisodeMarker) {
    isSeries = true;
    classification = 'series';
  } else if (!hasEpisodeMarker && durationMinutes >= 65 && durationMinutes <= 250 && (year !== undefined || hasMovieKeywords)) {
    isMovie = true;
    classification = 'movie';
  } else if (!hasEpisodeMarker && durationMinutes >= 75 && durationMinutes <= 240) {
    // Typical feature-length duration without episode markers
    isMovie = true;
    classification = 'movie';
  } else {
    classification = 'other';
  }

  return {
    rawTitle,
    normalizedTitle: cleanNormalize(rawTitle),
    mainTitle,
    subTitle,
    season,
    episode,
    totalEpisodes,
    year,
    durationMinutes,
    isSeries,
    isMovie,
    classification,
  };
}

/**
 * Finds reliable, verified reruns and alternate broadcasts across all channels for an event.
 */
export function findRerunsAndBroadcasts(
  targetEvent: EpgEvent,
  candidateEvents: EpgEvent[],
  channels: EpgChannel[],
  currentTime: number = Math.floor(Date.now() / 1000)
): RerunMatchResult {
  const targetMeta = extractMetadata(targetEvent);
  const targetNormMain = cleanNormalize(targetMeta.mainTitle);

  const channelMap = new Map<string, EpgChannel>();
  for (const ch of channels) {
    if (ch.serviceRef) channelMap.set(ch.serviceRef, ch);
    if (ch.id) channelMap.set(ch.id, ch);
  }

  const resolveChannelName = (serviceRef: string): string => {
    const ch = channelMap.get(serviceRef);
    return ch?.name || serviceRef;
  };

  const sameEpisodeList: MatchedBroadcast[] = [];
  const otherEpisodesList: MatchedBroadcast[] = [];
  const seenKeys = new Set<string>();

  for (const candidate of candidateEvents) {
    // 1. Skip exact same instance
    if (candidate.serviceRef === targetEvent.serviceRef && candidate.start === targetEvent.start) {
      continue;
    }

    // 2. Skip past broadcasts (which have already completed)
    if (candidate.end <= currentTime) {
      continue;
    }

    // 3. Deduplicate (same channel & start time)
    const key = `${candidate.serviceRef}:${candidate.start}`;
    if (seenKeys.has(key)) {
      continue;
    }
    seenKeys.add(key);

    const candMeta = extractMetadata(candidate);
    const candNormMain = cleanNormalize(candMeta.mainTitle);

    // 4. Strict Main Title Check
    // "top gun" must NEVER match "top gun maverick"
    if (targetNormMain !== candNormMain) {
      continue;
    }

    const durationDiffMinutes = Math.abs(candMeta.durationMinutes - targetMeta.durationMinutes);
    const channel = channelMap.get(candidate.serviceRef);
    const channelName = resolveChannelName(candidate.serviceRef);

    // =========================================================================
    // CASE A: SERIES
    // =========================================================================
    if (targetMeta.classification === 'series' || candMeta.classification === 'series') {
      let isExactEpisode = false;
      let confidence: MatchConfidence = 'season_episode';
      let confidenceLabel: string | undefined;

      // Check SxxExx match
      if (
        targetMeta.season !== undefined &&
        targetMeta.episode !== undefined &&
        candMeta.season !== undefined &&
        candMeta.episode !== undefined
      ) {
        if (targetMeta.season === candMeta.season && targetMeta.episode === candMeta.episode) {
          isExactEpisode = true;
          confidence = 'season_episode';
          confidenceLabel = `Staffel ${targetMeta.season}, Folge ${targetMeta.episode}`;
        }
      } else if (
        targetMeta.episode !== undefined &&
        candMeta.episode !== undefined &&
        targetMeta.episode === candMeta.episode
      ) {
        // Episode numbers match
        isExactEpisode = true;
        confidence = 'season_episode';
        confidenceLabel = `Folge ${targetMeta.episode}`;
      } else if (
        targetMeta.subTitle &&
        candMeta.subTitle &&
        cleanNormalize(targetMeta.subTitle).length >= 4 &&
        cleanNormalize(targetMeta.subTitle) === cleanNormalize(candMeta.subTitle)
      ) {
        // Identical episode title
        isExactEpisode = true;
        confidence = 'episode_title';
        confidenceLabel = `„${targetMeta.subTitle}“`;
      }

      // Check duration reasonableness for episode (within ±20 minutes)
      if (isExactEpisode && durationDiffMinutes <= 20) {
        sameEpisodeList.push({
          event: candidate,
          channel,
          channelName,
          isSameEpisode: true,
          confidence,
          confidenceLabel,
        });
      } else if (!isExactEpisode && durationDiffMinutes <= 25) {
        // Another episode of the same series
        otherEpisodesList.push({
          event: candidate,
          channel,
          channelName,
          isSameEpisode: false,
          confidence: 'season_episode',
          confidenceLabel: candMeta.season && candMeta.episode
            ? `Staffel ${candMeta.season}, Folge ${candMeta.episode}`
            : candMeta.episode
              ? `Folge ${candMeta.episode}`
              : candMeta.subTitle
                ? `„${candMeta.subTitle}“`
                : undefined,
        });
      }
      continue;
    }

    // =========================================================================
    // CASE B: MOVIE (FILM)
    // =========================================================================
    if (targetMeta.classification === 'movie') {
      // If candidate has explicit episode markers (e.g. S01E02), it's not the same film
      if (candMeta.isSeries) {
        continue;
      }

      // Year check: if both have an extracted year, they MUST match!
      // (Prevents Top Gun 1986 vs Top Gun 2022)
      if (targetMeta.year && candMeta.year && targetMeta.year !== candMeta.year) {
        continue;
      }

      // Runtime check: TV broadcast runtimes vary slightly with commercial blocks,
      // but should be within ±25 minutes or 20%
      if (durationDiffMinutes > 25) {
        continue;
      }

      // Verified movie rerun!
      sameEpisodeList.push({
        event: candidate,
        channel,
        channelName,
        isSameEpisode: true,
        confidence: 'movie_metadata',
        confidenceLabel: targetMeta.year ? `${targetMeta.year}` : undefined,
      });
      continue;
    }

    // =========================================================================
    // CASE C: OTHER (Negative rule: "Nur Titel gleich: nicht anzeigen")
    // =========================================================================
    // If it's not a verified episode rerun and not a verified movie, do NOT show
    // simple title collisions (e.g. daily news or generic recurrent programs).
    if (
      targetMeta.subTitle &&
      candMeta.subTitle &&
      cleanNormalize(targetMeta.subTitle).length >= 5 &&
      cleanNormalize(targetMeta.subTitle) === cleanNormalize(candMeta.subTitle) &&
      durationDiffMinutes <= 10
    ) {
      sameEpisodeList.push({
        event: candidate,
        channel,
        channelName,
        isSameEpisode: true,
        confidence: 'episode_title',
        confidenceLabel: `„${targetMeta.subTitle}“`,
      });
    }
  }

  // Sort chronologically by start time
  sameEpisodeList.sort((a, b) => a.event.start - b.event.start);
  otherEpisodesList.sort((a, b) => a.event.start - b.event.start);

  return {
    programmeType: targetMeta.classification,
    targetMetadata: targetMeta,
    sameEpisodeReruns: sameEpisodeList,
    otherEpisodes: otherEpisodesList,
  };
}
