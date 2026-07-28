import type { ErrorData } from 'hls.js';
import type { TFunction } from 'i18next';

export interface PlaybackErrorPresentation {
  title: string;
  details: string;
}

export interface MediaElementErrorContext {
  code?: number;
  message?: string | null;
  currentSrc?: string | null;
  readyState: number;
  networkState: number;
  hlsJsActive: boolean;
}

function compactPlaybackUrl(url?: string | null): string {
  if (!url) {
    return 'playback source';
  }

  try {
    const parsed = new URL(url, 'http://localhost');
    const segments = parsed.pathname.split('/').filter(Boolean);
    const lastSegment = segments.length > 0 ? segments[segments.length - 1] : undefined;
    return lastSegment || parsed.pathname || url;
  } catch {
    return url;
  }
}

function extractHttpStatus(data: ErrorData): number | undefined {
  const candidates = [
    (data as { response?: { code?: number; status?: number } }).response?.code,
    (data as { response?: { code?: number; status?: number } }).response?.status,
    (data as { networkDetails?: { status?: number } }).networkDetails?.status,
  ];

  return candidates.find((value): value is number => typeof value === 'number');
}

function buildHttpDetails(prefix: string, status: number | undefined, detail: string | undefined): string {
  const parts = [prefix];

  if (typeof status === 'number') {
    parts.push(`HTTP ${status}`);
  }

  if (detail) {
    parts.push(detail);
  }

  return parts.join(' • ');
}

function isManifestFailure(detail: string): boolean {
  return /(manifest|level|playlist|m3u8)/i.test(detail);
}

function isSegmentFailure(detail: string): boolean {
  return /(frag|segment|m4s|\.ts|keyLoad|fragLoad)/i.test(detail);
}

function isRecordingPlayback(url?: string | null): boolean {
  return typeof url === 'string' && url.includes('/recordings/');
}

export function classifyHlsFatalError(
  data: ErrorData,
  t: TFunction,
  playbackUrl?: string | null
): PlaybackErrorPresentation {
  const detail = typeof data.details === 'string' ? data.details : '';
  const status = extractHttpStatus(data);
  const sourceLabel = compactPlaybackUrl(playbackUrl);

  if (status === 401) {
    return {
      title: t('player.authFailed', { defaultValue: 'Authentication failed' }),
      details: buildHttpDetails(`The request for ${sourceLabel} was rejected.`, status, detail),
    };
  }

  if (status === 403) {
    return {
      title: t('player.playbackDenied', { defaultValue: 'Playback denied' }),
      details: buildHttpDetails(`The server denied access to ${sourceLabel}.`, status, detail),
    };
  }

  if (status === 404 && isRecordingPlayback(playbackUrl)) {
    return {
      title: t('player.recordingNotFound', { defaultValue: 'Recording was not found.' }),
      details: buildHttpDetails(`The requested recording manifest could not be found.`, status, detail),
    };
  }

  if (status === 416 && isManifestFailure(detail)) {
    return {
      title: t('player.playbackManifestRejected', { defaultValue: 'Playback manifest was rejected' }),
      details: buildHttpDetails(`The device rejected the playback manifest ${sourceLabel}.`, status, detail),
    };
  }

  if ((status === 503 || status === 429) && isManifestFailure(detail)) {
    return {
      title: t('player.playlistNotReady', { defaultValue: 'Playlist not ready' }),
      details: buildHttpDetails(`The playback manifest is not ready yet.`, status, detail),
    };
  }

  if (data.type === 'mediaError') {
    return {
      title: t('player.decodeError', { defaultValue: 'This device could not decode the stream' }),
      details: detail
        ? `The media pipeline failed while processing ${sourceLabel} • ${detail}`
        : `The media pipeline failed while processing ${sourceLabel}.`,
    };
  }

  if (isManifestFailure(detail)) {
    return {
      title: t('player.manifestRequestFailed', { defaultValue: 'Playback manifest could not be loaded' }),
      details: buildHttpDetails(`The player could not load ${sourceLabel}.`, status, detail),
    };
  }

  if (isSegmentFailure(detail)) {
    return {
      title: t('player.segmentRequestFailed', { defaultValue: 'Media segment request failed' }),
      details: buildHttpDetails(`The player lost a media segment while streaming ${sourceLabel}.`, status, detail),
    };
  }

  if (data.type === 'networkError') {
    return {
      title: t('player.networkError', { defaultValue: 'Network error' }),
      details: buildHttpDetails(`The playback request for ${sourceLabel} failed.`, status, detail),
    };
  }

  return {
    title: t('player.hlsError', { defaultValue: 'HLS Error' }),
    details: detail
      ? `${sourceLabel} • ${detail}`
      : `Unexpected playback failure while loading ${sourceLabel}.`,
  };
}

export function classifyMediaElementError(
  context: MediaElementErrorContext,
  t: TFunction
): PlaybackErrorPresentation {
  const sourceLabel = compactPlaybackUrl(context.currentSrc);
  const stateSummary = `readyState ${context.readyState}, networkState ${context.networkState}`;
  const message = context.message?.trim() || null;

  switch (context.code) {
    case 2:
      return {
        title: t('player.networkError', { defaultValue: 'Network error' }),
        details: message
          ? `${message} • ${sourceLabel} • ${stateSummary}`
          : `The device lost the connection while loading ${sourceLabel} • ${stateSummary}`,
      };
    case 3:
      return {
        title: t('player.decodeError', { defaultValue: 'This device could not decode the stream' }),
        details: message
          ? `${message} • ${sourceLabel} • ${stateSummary}`
          : `The device could not decode ${sourceLabel} • ${stateSummary}`,
      };
    case 4:
      return {
        title: t('player.sourceNotSupported', { defaultValue: 'This device rejected the stream source' }),
        details: message
          ? `${message} • ${sourceLabel} • ${stateSummary}`
          : `The device rejected ${sourceLabel}. This is usually a manifest, codec, or server-response issue • ${stateSummary}`,
      };
    default:
      return {
        title: t('player.playbackError', { defaultValue: 'Playback error' }),
        details: message
          ? `${message} • ${sourceLabel} • ${stateSummary}`
          : `Unexpected media element failure while loading ${sourceLabel} • ${stateSummary}`,
      };
  }
}
