export function rewriteRecordingTimeshiftUrl(url: string): string {
  if (!url || !url.includes('/playlist.m3u8')) return url;

  try {
    const parsed = new URL(url, 'http://localhost');
    if (!parsed.pathname.startsWith('/api/v3/recordings/') || !parsed.pathname.endsWith('/playlist.m3u8')) {
      return url;
    }
    parsed.pathname = parsed.pathname.replace(/\/playlist\.m3u8$/, '/timeshift.m3u8');
    return parsed.origin === 'http://localhost'
      ? `${parsed.pathname}${parsed.search}${parsed.hash}`
      : parsed.toString();
  } catch {
    return url.replace(/\/playlist\.m3u8(?=([?#]|$))/, '/timeshift.m3u8');
  }
}

export function shouldUseProgressiveRecordingPath({
  streamUrl,
  isSeekable,
  recordingHlsEngine,
  progressiveReady,
}: {
  streamUrl: string;
  isSeekable: boolean;
  recordingHlsEngine: 'native' | 'hlsjs';
  progressiveReady: boolean | null | undefined;
}): boolean {
  if (!isSeekable || recordingHlsEngine !== 'native' || progressiveReady !== true) {
    return false;
  }
  return rewriteRecordingTimeshiftUrl(streamUrl) !== streamUrl;
}
