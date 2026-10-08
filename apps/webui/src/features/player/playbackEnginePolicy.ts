import type { HlsConfig } from 'hls.js';
import { hlsTuningForLink, type PlaybackLinkProfile } from './utils/playbackLinkProfile';

export function createHlsRuntimeConfig(link: PlaybackLinkProfile = 'stable'): Partial<HlsConfig> {
  const tuning = hlsTuningForLink(link);
  return {
    debug: false,
    // Own the buffer via ManagedMediaSource on Safari 17.1+. Keeping this
    // pinned prevents a future hls.js default from silently selecting MSE,
    // which breaks the app-owned AV1, AirPlay, and MMS lifecycle.
    preferManagedMediaSource: true,
    enableWorker: true,
    // This engages only for playlists advertising EXT-X-PART. It is a no-op
    // for the stable non-low-latency path.
    lowLatencyMode: tuning.lowLatencyMode,
    backBufferLength: 300,
    maxBufferLength: tuning.maxBufferLength,
    capLevelToPlayerSize: true,
    liveSyncDuration: tuning.liveSyncDuration,
    // Rate-based live catch-up caused visible judder and compressed audio.
    // A multi-hour DVR window makes slow drift safer than playback-rate churn.
    maxLiveSyncPlaybackRate: 1,
    // Broadcast passthrough can contain small DTS gaps. These values let
    // hls.js cross them instead of exhausting its stall recovery.
    maxBufferHole: 1,
    nudgeOffset: 0.2,
    nudgeMaxRetry: 6,
  };
}

export const HLS_STARTUP_POLICY = Object.freeze({
  // Live sessions start with a 6s cushion to prevent buffer stalls on fresh sessions.
  liveBufferTargetSeconds: 6,
  // Counted from MANIFEST_PARSED: 15s covers 3x2s segments growing at ~1s/s in 2s steps.
  liveTimeoutMs: 15_000,
  // Rate-based slow-build caused visible judder and audio micro-skips when restoring to 1.
  slowBuildPlaybackRate: 1,
  slowBuildTargetSeconds: 8,
  slowBuildMaxMs: 150_000,
});

// Transport cadence controls buffer tuning only; codec/mode decisions stay on
// the server. Bound the reserve so an unusual playlist cannot hold startup forever.
export function hlsLiveCadencePolicy(targetDuration: number, link: PlaybackLinkProfile = 'stable') {
  const cadence = Number.isFinite(targetDuration) && targetDuration > 0
    ? Math.min(targetDuration, 20)
    : 0;
  return {
    targetLatencySeconds: Math.max(hlsTuningForLink(link).liveSyncDuration, cadence * 3),
    bufferTargetSeconds: Math.max(HLS_STARTUP_POLICY.liveBufferTargetSeconds, cadence * 2),
    timeoutMs: Math.max(HLS_STARTUP_POLICY.liveTimeoutMs, cadence * 3_000),
  };
}

export const HLS_NETWORK_RETRY_POLICY = Object.freeze({
  maxRetries: 6,
  initialBackoffMs: 1_000,
  backoffCapMs: 30_000,
});

export function hlsNetworkRetryPolicyForLink(link: PlaybackLinkProfile = 'stable') {
  const tuning = hlsTuningForLink(link);
  return {
    maxRetries: tuning.maxNetworkRetries,
    initialBackoffMs: HLS_NETWORK_RETRY_POLICY.initialBackoffMs,
    backoffCapMs: tuning.networkBackoffCapMs,
  };
}

export function hlsNetworkRetryBackoffMs(
  retryCount: number,
  link: PlaybackLinkProfile = 'stable',
): number {
  const policy = hlsNetworkRetryPolicyForLink(link);
  return Math.min(
    policy.initialBackoffMs * Math.pow(2, retryCount),
    policy.backoffCapMs,
  );
}
