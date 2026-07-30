import { describe, expect, it } from 'vitest';
import {
  createHlsRuntimeConfig,
  hlsNetworkRetryBackoffMs,
  HLS_NETWORK_RETRY_POLICY,
  HLS_STARTUP_POLICY,
} from './playbackEnginePolicy';

describe('playbackEnginePolicy', () => {
  it('preserves the production HLS runtime tuning contract', () => {
    expect(createHlsRuntimeConfig()).toEqual({
      debug: false,
      preferManagedMediaSource: true,
      enableWorker: true,
      lowLatencyMode: true,
      backBufferLength: 300,
      maxBufferLength: 60,
      capLevelToPlayerSize: true,
      liveSyncDuration: 12,
      maxLiveSyncPlaybackRate: 1,
      maxBufferHole: 1,
      nudgeOffset: 0.2,
      nudgeMaxRetry: 6,
    });
  });

  it('returns a fresh HLS configuration for every engine instance', () => {
    const first = createHlsRuntimeConfig();
    const second = createHlsRuntimeConfig();

    expect(first).not.toBe(second);
    expect(first).toEqual(second);
  });

  it('preserves live startup buffering policy', () => {
    expect(HLS_STARTUP_POLICY).toEqual({
      bufferTargetSeconds: 4.5,
      timeoutMs: 6_000,
      slowBuildPlaybackRate: 0.955,
      slowBuildTargetSeconds: 8,
      slowBuildMaxMs: 150_000,
    });
  });

  it('preserves retry count and exponential backoff with its cap', () => {
    expect(HLS_NETWORK_RETRY_POLICY.maxRetries).toBe(6);
    expect([0, 1, 2, 3, 4, 5, 6].map((retryCount) => hlsNetworkRetryBackoffMs(retryCount))).toEqual([
      1_000,
      2_000,
      4_000,
      8_000,
      16_000,
      30_000,
      30_000,
    ]);
  });
});
