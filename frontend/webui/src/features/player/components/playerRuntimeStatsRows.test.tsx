import { describe, expect, it } from 'vitest';
import type { BuildPlayerRuntimeStatsRowsInput } from './playerRuntimeStatsRows';

import { buildPlayerRuntimeStatsRows } from './playerRuntimeStatsRows';

const t: BuildPlayerRuntimeStatsRowsInput['t'] = (key, options) => String(options?.defaultValue ?? key);

const baseInput: BuildPlayerRuntimeStatsRowsInput = {
  t,
  status: 'playing',
  effectiveSessionId: 'session-1',
  requestId: 'request-1',
  traceId: '-',
  effectiveClientPath: 'hlsjs',
  effectiveRequestProfile: 'generic',
  effectiveRequestedIntent: 'generic',
  effectiveResolvedIntent: 'generic',
  effectiveQualityRung: 'quality_audio_aac_320_stereo',
  effectiveAudioQualityRung: null,
  effectiveVideoQualityRung: null,
  effectiveDegradedFrom: null,
  effectiveHostPressureBand: null,
  effectiveHostOverrideApplied: false,
  effectiveForcedIntent: null,
  effectiveOperatorMaxQualityRung: null,
  effectiveRuntimePolicyAction: null,
  runtimePolicyPhaseLabel: '-',
  effectiveRuntimeProbeCandidate: null,
  runtimePolicyReasonsSummary: '-',
  runtimePolicyConstraintsSummary: '-',
  runtimeProbeTrustSummary: '-',
  effectiveRuntimePolicyTimeline: null,
  effectiveOperatorRuleName: null,
  effectiveOperatorRuleScope: null,
  effectiveClientFallbackDisabled: false,
  effectiveOperatorOverrideApplied: false,
  sourceProfileSummary: '-',
  effectiveTargetProfile: null,
  effectiveTargetProfileHash: null,
  ffmpegPlanSummary: '-',
  runtimeDiagnosticsSummary: '-',
  sourceWarningsSummary: '-',
  clientSummary: '-',
  clientDeviceSummary: '-',
  autoCodecSummary: '-',
  firstFrameLabel: '-',
  fallbackSummary: '-',
  stopSummary: '-',
  selectedOutputKind: null,
  playbackWindowKind: 'vod',
  stats: {
    bandwidth: 0,
    resolution: '-',
    fps: 0,
    droppedFrames: 0,
    buffer: 0,
    bufferHealth: 0,
    latency: null,
    levelIndex: -1,
  },
  hasHlsJsEngine: false,
  seekableStart: 0,
  seekableEnd: 120,
  currentPlaybackTime: 30,
  hasSeekWindow: true,
  windowDuration: 120,
  hasLiveDvrWindow: false,
  liveWindowStateLabel: '-',
  isWebKitFullscreenActive: false,
  isFullscreen: false,
  prefersDesktopNativeFullscreen: false,
  supportsNativeFullscreen: true,
  formatClock: (value) => `${value}s`,
};

function rowValue(key: string, overrides: Partial<BuildPlayerRuntimeStatsRowsInput> = {}) {
  const rows = buildPlayerRuntimeStatsRows({
    ...baseInput,
    ...overrides,
    stats: {
      ...baseInput.stats,
      ...overrides.stats,
    },
  });
  return rows.find((row) => row.key === key)?.value;
}

describe('playerRuntimeStatsRows', () => {
  it('derives HLS level display from the active HLS.js engine state', () => {
    expect(rowValue('hls-level', {
      hasHlsJsEngine: true,
      stats: { ...baseInput.stats, levelIndex: -1 },
    })).toBe('Auto');

    expect(rowValue('hls-level', {
      hasHlsJsEngine: true,
      stats: { ...baseInput.stats, levelIndex: 2 },
    })).toBe(2);

    expect(rowValue('hls-level', {
      hasHlsJsEngine: false,
      stats: { ...baseInput.stats, levelIndex: 2 },
    })).toBe('Native / Direct');
  });

  it('surfaces backend runtime diagnostics rows', () => {
    expect(rowValue('runtime-diagnostics', {
      runtimeDiagnosticsSummary: 'frame 6472 · 51.4fps · 1.03x · drop 0 / dup 52',
    })).toBe('frame 6472 · 51.4fps · 1.03x · drop 0 / dup 52');

    expect(rowValue('source-warnings', {
      sourceWarningsSummary: 'corrupt decoded 2 · corrupt decoded frame in stream 0',
    })).toBe('corrupt decoded 2 · corrupt decoded frame in stream 0');
  });

  it('surfaces client and codec decision summaries', () => {
    expect(rowValue('client-truth', {
      clientSummary: 'android_tv_browser · runtime_plus_family · hlsjs · android_tv',
    })).toBe('android_tv_browser · runtime_plus_family · hlsjs · android_tv');

    expect(rowValue('client-device', {
      clientDeviceSummary: 'Amazon AFTKRT · android 11 · linux',
    })).toBe('Amazon AFTKRT · android 11 · linux');

    expect(rowValue('auto-codec', {
      autoCodecSummary: 'av1 <- av1,hevc,h264 · host_aware_bottleneck · host high · bench strong',
    })).toBe('av1 <- av1,hevc,h264 · host_aware_bottleneck · host high · bench strong');
  });
});
