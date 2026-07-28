import { describe, expect, it } from 'vitest';
import type {
  PlaybackSourceProfile,
  PlaybackTrace as PlaybackTraceContract,
  PlaybackTraceFfmpegPlan,
  PlaybackTraceRuntimeTick,
} from '../../../client-ts';
import type { CapabilitySnapshot } from '../utils/playbackCapabilities';

import {
  formatAutoCodecSummary,
  formatClientPath,
  formatFallbackSummary,
  formatFfmpegPlanSummary,
  formatFirstFrameLabel,
  formatHostPressureSummary,
  formatRequestProfileLabel,
  formatRuntimePolicyPhaseLabel,
  formatRuntimeTimelineEntry,
  formatRuntimeTimelineTime,
  formatSourceProfileSummary,
  formatStopSummary,
  formatTraceClientDeviceSummary,
  formatTraceClientSummary,
  resolveRuntimePolicyMetaHint,
  resolveRuntimePolicyPhaseState,
} from './playerRuntimeTraceFormat';

describe('playerRuntimeTraceFormat', () => {
  it('formats source and ffmpeg trace summaries for display', () => {
    const source: PlaybackSourceProfile = {
      container: 'ts',
      videoCodec: 'h264',
      audioCodec: 'aac',
      width: 1920,
      height: 1080,
      fps: 25,
      audioChannels: 2,
      audioBitrateKbps: 192,
    };
    const plan: PlaybackTraceFfmpegPlan = {
      inputKind: 'recording',
      container: 'mp4',
      packaging: 'hls',
      videoMode: 'transcode',
      videoCodec: 'h264',
      audioMode: 'transcode',
      audioCodec: 'aac',
      hwAccel: 'vaapi',
    };

    expect(formatSourceProfileSummary(source)).toBe('ts · h264 · 1920x1080 · 25fps · a:aac/2ch/@192k');
    expect(formatFfmpegPlanSummary(plan)).toBe('recording · hls · v:transcode/h264 · a:transcode/aac · vaapi');
  });

  it('formats fallback, stop and host pressure summaries', () => {
    const trace: PlaybackTraceContract = {
      requestId: 'req_1',
      fallbackCount: 2,
      lastFallbackReason: 'probe_regressed',
      stopClass: 'client',
      stopReason: 'user_stop',
    };

    expect(formatFallbackSummary(null)).toBe('-');
    expect(formatFallbackSummary(trace)).toBe('2 · probe_regressed');
    expect(formatStopSummary(trace)).toBe('client · user_stop');
    expect(formatHostPressureSummary(null, false)).toBe('-');
    expect(formatHostPressureSummary('high', true)).toBe('high · applied');
  });

  it('formats client path and request profile aliases', () => {
    const snapshot: CapabilitySnapshot = {
      capabilitiesVersion: 3,
      container: ['ts'],
      videoCodecs: ['h264'],
      audioCodecs: ['aac'],
      deviceType: 'desktop',
      supportsHls: true,
      supportsRange: true,
      allowTranscode: true,
      runtimeProbeUsed: false,
      runtimeProbeVersion: 1,
      clientFamilyFallback: 'none',
      videoCodecSignals: [],
      hlsEngines: ['native', 'hlsjs'],
      preferredHlsEngine: 'native',
    };

    expect(formatClientPath(null)).toBe('-');
    expect(formatClientPath(snapshot)).toBe('native (native/hlsjs)');
    expect(formatRequestProfileLabel('generic')).toBe('compatible');
    expect(formatRequestProfileLabel('low')).toBe('bandwidth');
    expect(formatRequestProfileLabel('copy')).toBe('direct');
    expect(formatRequestProfileLabel(null)).toBe('-');
  });

  it('formats client truth and auto-codec summaries from backend trace', () => {
    const trace: PlaybackTraceContract = {
      requestId: 'req-av1',
      autoCodecPolicy: 'host_aware_bottleneck',
      autoCodecRequestedCodecs: 'av1,hevc,h264',
      autoCodecSelectedCodec: 'av1',
      autoCodecPerformanceClass: 'high',
      autoCodecBenchmarkClass: 'strong',
      client: {
        clientFamily: 'android_tv_browser',
        clientCapsSource: 'runtime_plus_family',
        preferredHlsEngine: 'hlsjs',
        deviceType: 'android_tv',
        deviceContext: {
          manufacturer: 'Amazon',
          model: 'AFTKRT',
          osName: 'android',
          osVersion: '11',
          platform: 'linux',
        },
      },
    };

    expect(formatTraceClientSummary(trace.client, null)).toBe('android_tv_browser · runtime_plus_family · hlsjs · android_tv');
    expect(formatTraceClientDeviceSummary(trace.client, null)).toBe('Amazon AFTKRT · android 11 · linux');
    expect(formatAutoCodecSummary(trace)).toBe('av1 <- av1,hevc,h264 · host_aware_bottleneck · host high · bench strong');
  });

  it('formats runtime policy phase labels and states', () => {
    expect(formatRuntimePolicyPhaseLabel('probing')).toBe('Probing');
    expect(formatRuntimePolicyPhaseLabel('probe_regressed')).toBe('Probe regressed');
    expect(formatRuntimePolicyPhaseLabel('unknown')).toBe('-');
    expect(resolveRuntimePolicyPhaseState('probing')).toBe('pending');
    expect(resolveRuntimePolicyPhaseState('cooldown')).toBe('warning');
    expect(resolveRuntimePolicyPhaseState('degraded')).toBe('error');
    expect(resolveRuntimePolicyPhaseState('stable')).toBe('success');
    expect(resolveRuntimePolicyPhaseState(null)).toBe('idle');
  });

  it('formats runtime timeline entries without deriving policy', () => {
    const entry: PlaybackTraceRuntimeTick = {
      tickAt: 'not-a-date',
      confidenceState: 'high_confidence',
      policyAction: 'probe_regressed',
      plannedTransition: 'quality_audio',
      executedTransition: 'fallback_transcode',
      activeStep: 'segment_probe',
      probeState: 'failed_probe',
      blockers: ['cpu_hot', 'bandwidth_low'],
    };

    expect(formatRuntimeTimelineTime(null)).toBe('-');
    expect(formatRuntimeTimelineTime('not-a-date')).toBe('-');
    expect(formatRuntimeTimelineEntry(entry)).toBe(
      '- · high confidence · probe regressed · plan:quality audio · run:fallback transcode · step:segment probe · probe:failed probe · block:cpu_hot/bandwidth_low'
    );
  });

  it('resolves only display hints for runtime meta', () => {
    expect(resolveRuntimePolicyMetaHint('probing', 'quality_audio', 'quality_video')).toBe('quality_audio');
    expect(resolveRuntimePolicyMetaHint('cooldown', 'quality_audio', 'quality_video')).toBe('quality_video');
    expect(resolveRuntimePolicyMetaHint('stable', 'quality_audio', 'quality_video')).toBeNull();
    expect(formatFirstFrameLabel(null)).toBe('-');
    expect(formatFirstFrameLabel(-1)).toBe('-');
  });
});
