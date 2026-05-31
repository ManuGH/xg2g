import { describe, expect, it } from 'vitest';
import type { PlaybackTrace as PlaybackTraceContract, PlaybackTraceOperator } from '../../../client-ts';
import type { CapabilitySnapshot } from '../utils/playbackCapabilities';
import type { PlaybackObservability } from './playerPlaybackModel';

import { buildPlayerRuntimeTelemetryModel } from './playerRuntimeTelemetryModel';

const capabilitySnapshot: CapabilitySnapshot = {
  capabilitiesVersion: 3,
  container: ['ts'],
  videoCodecs: ['h264'],
  audioCodecs: ['aac'],
  hlsEngines: ['native', 'hlsjs'],
  preferredHlsEngine: 'native',
};

const observability: PlaybackObservability = {
  clientPath: 'hlsjs',
  requestProfile: 'generic',
  requestedIntent: 'generic',
  resolvedIntent: 'generic',
  qualityRung: 'quality_audio_aac_160_stereo',
  audioQualityRung: null,
  videoQualityRung: null,
  degradedFrom: null,
  hostPressureBand: 'medium',
  hostOverrideApplied: true,
  targetProfileHash: 'observed-hash',
  targetProfile: null,
  operator: null,
  selectedOutputKind: 'hls',
};

describe('buildPlayerRuntimeTelemetryModel', () => {
  it('prefers authoritative session trace values over observational fallbacks', () => {
    const trace: PlaybackTraceContract = {
      requestId: 'req-1',
      sessionId: 'trace-session',
      clientPath: 'native',
      requestProfile: 'copy',
      requestedIntent: 'copy',
      resolvedIntent: 'copy',
      qualityRung: 'copy',
      hostPressureBand: 'low',
      hostOverrideApplied: false,
      targetProfileHash: 'trace-hash',
      fallbackCount: 2,
      lastFallbackReason: 'stall',
      runtimeDiagnostics: {
        frameCount: 6472,
        fps: 51.35,
        dropFrames: 0,
        dupFrames: 52,
        speed: 1.03,
        corruptDecodedFrames: 2,
        lastWarning: 'corrupt decoded frame in stream 0',
      },
      stopClass: 'client',
      stopReason: 'close',
      autoCodecPolicy: 'host_aware_bottleneck',
      autoCodecRequestedCodecs: 'av1,hevc,h264',
      autoCodecSelectedCodec: 'hevc',
      autoCodecPerformanceClass: 'medium',
      autoCodecBenchmarkClass: 'strong',
      client: {
        clientFamily: 'chromium_hlsjs',
        clientCapsSource: 'runtime_plus_family',
        preferredHlsEngine: 'hlsjs',
        deviceType: 'web',
        deviceContext: {
          manufacturer: 'Apple',
          model: 'MacBook Pro M4',
          osName: 'macos',
          osVersion: '15.4',
          platform: 'macintel',
        },
      },
    };
    const operator: PlaybackTraceOperator = {
      forcedIntent: 'copy',
      runtimePolicyReasons: ['probe_ok', ''],
      runtimePolicyConstraints: ['host_low'],
      runtimeProbeSuccessStreak: 3,
      overrideApplied: true,
    };

    const model = buildPlayerRuntimeTelemetryModel({
      sessionPlaybackTrace: trace,
      playbackObservability: observability,
      capabilitySnapshot,
      effectiveOperator: operator,
      sessionId: null,
      nativeSessionId: null,
      nativePlaybackSessionId: null,
    });

    expect(model.effectiveClientPath).toBe('native');
    expect(model.effectiveSessionId).toBe('trace-session');
    expect(model.effectiveRequestProfile).toBe('copy');
    expect(model.effectiveHostPressureBand).toBe('low');
    expect(model.effectiveHostOverrideApplied).toBe(false);
    expect(model.effectiveTargetProfileHash).toBe('trace-hash');
    expect(model.effectiveForcedIntent).toBe('copy');
    expect(model.runtimePolicyReasonsSummary).toBe('probe_ok');
    expect(model.runtimePolicyConstraintsSummary).toBe('host_low');
    expect(model.runtimeProbeTrustSummary).toBe('success 3 / fail 0');
    expect(model.runtimeDiagnosticsSummary).toBe('frame 6472 · 51.4fps · 1.03x · drop 0 / dup 52');
    expect(model.sourceWarningsSummary).toBe('corrupt decoded 2 · corrupt decoded frame in stream 0');
    expect(model.clientSummary).toBe('chromium_hlsjs · runtime_plus_family · hlsjs · web');
    expect(model.clientDeviceSummary).toBe('Apple MacBook Pro M4 · macos 15.4 · macintel');
    expect(model.autoCodecSummary).toBe('hevc <- av1,hevc,h264 · host_aware_bottleneck · host medium · bench strong');
    expect(model.fallbackSummary).toBe('2 · stall');
    expect(model.stopSummary).toBe('client · close');
    expect(model.hostPressureSummary).toBe('low');
  });

  it('keeps request-context preview fallbacks but suppresses predicted output during the startup window', () => {
    // Active/starting session (id known) but GET /sessions trace not yet landed.
    const model = buildPlayerRuntimeTelemetryModel({
      sessionPlaybackTrace: null,
      playbackObservability: observability,
      capabilitySnapshot,
      effectiveOperator: null,
      sessionId: null,
      nativeSessionId: 'native-session',
      nativePlaybackSessionId: 'host-session',
    });

    // Request-context + host condition keep the preview fallback (describe the
    // request, not the output): faithful during startup.
    expect(model.effectiveClientPath).toBe('hlsjs');
    expect(model.effectiveSessionId).toBe('native-session');
    expect(model.effectiveRequestedIntent).toBe('generic');
    expect(model.effectiveResolvedIntent).toBe('generic');
    expect(model.selectedOutputKind).toBe('hls');
    expect(model.runtimePolicyReasonsSummary).toBe('-');
    expect(model.runtimeProbeTrustSummary).toBe('-');
    expect(model.hostPressureSummary).toBe('medium · applied');

    // Execution-OUTPUT fields must NOT show the prediction once a session exists:
    // no executed trace yet -> null (rendered as "—"), never the preview guess.
    expect(model.effectiveQualityRung).toBeNull();
    expect(model.effectiveTargetProfile).toBeNull();
    expect(model.effectiveTargetProfileHash).toBeNull();

    // No session at all: the preview IS the right source for the player panel.
    const previewOnly = buildPlayerRuntimeTelemetryModel({
      sessionPlaybackTrace: null,
      playbackObservability: observability,
      capabilitySnapshot,
      effectiveOperator: null,
      sessionId: null,
      nativeSessionId: null,
      nativePlaybackSessionId: null,
    });
    expect(previewOnly.effectiveQualityRung).toBe('quality_audio_aac_160_stereo');
    expect(previewOnly.effectiveTargetProfileHash).toBe('observed-hash');

    expect(buildPlayerRuntimeTelemetryModel({
      sessionPlaybackTrace: null,
      playbackObservability: null,
      capabilitySnapshot,
      effectiveOperator: null,
      sessionId: null,
      nativeSessionId: null,
      nativePlaybackSessionId: null,
    }).effectiveClientPath).toBe('native (native/hlsjs)');
  });

  it('does not mix stale playback-info rungs into an active session trace', () => {
    const trace: PlaybackTraceContract = {
      requestId: 'req-av1-session',
      sessionId: 'trace-av1-session',
      clientPath: 'native_hls',
      requestProfile: 'quality',
      requestedIntent: 'quality',
      resolvedIntent: 'quality',
      targetProfileHash: 'trace-av1-hash',
      ffmpegPlan: {
        inputKind: 'tuner',
        container: 'fmp4',
        packaging: 'fmp4',
        hwAccel: 'vaapi_encode_only',
        videoMode: 'transcode',
        videoCodec: 'av1',
        audioMode: 'transcode',
        audioCodec: 'aac',
      },
    };

    const model = buildPlayerRuntimeTelemetryModel({
      sessionPlaybackTrace: trace,
      playbackObservability: {
        ...observability,
        requestProfile: 'compatible',
        qualityRung: 'compatible_video_h264_crf23_fast',
        videoQualityRung: 'compatible_video_h264_crf23_fast',
      },
      capabilitySnapshot,
      effectiveOperator: null,
      sessionId: null,
      nativeSessionId: null,
      nativePlaybackSessionId: null,
    });

    expect(model.effectiveRequestProfile).toBe('quality');
    expect(model.effectiveQualityRung).toBeNull();
    expect(model.effectiveVideoQualityRung).toBeNull();
    expect(model.effectiveTargetProfileHash).toBe('trace-av1-hash');
    expect(model.ffmpegPlanSummary).toBe('tuner · fmp4 · v:transcode/av1 · a:transcode/aac · vaapi_encode_only');
  });
});
