import { describe, expect, it } from 'vitest';
import type { PlaybackInfo, PlaybackTraceOperator } from '../../../client-ts';
import type { CapabilitySnapshot } from '../utils/playbackCapabilities';

import {
  extractPlaybackObservability,
  extractPlaybackTrace,
  mergePlaybackTraceOperator,
  normalizePlaybackWindowKind,
  resolveApiUrl,
  resolveAutoTranscodeCodecs,
  resolveMediaArtworkUrl,
  resolvePlaybackDurationSeconds,
  resolvePlaybackWindowKind,
} from './playerPlaybackModel';

describe('playerPlaybackModel', () => {
  it('extracts trace payloads from nested error/problem wrappers', () => {
    const trace = {
      requestId: 'req-1',
      sessionId: 'session-1',
      stopReason: 'client_stop',
    };

    expect(extractPlaybackTrace({ body: { details: { trace } } })).toEqual(trace);
    expect(extractPlaybackTrace({ requestId: 'req-2' })).toBeNull();
    expect(extractPlaybackTrace(null)).toBeNull();
  });

  it('extracts authoritative session traces that do not repeat requestId inside trace', () => {
    const trace = {
      source: { container: 'ts', videoCodec: 'h264', audioCodec: 'ac3' },
      targetProfileHash: 'hash-session-1',
      ffmpegPlan: {
        inputKind: 'tuner',
        packaging: 'fmp4',
        videoMode: 'transcode',
        videoCodec: 'av1',
      },
    };

    expect(extractPlaybackTrace({ requestId: 'req-session-1', trace })).toEqual(trace);
    expect(extractPlaybackTrace({ requestId: 'req-only' })).toBeNull();
  });

  it('resolves URL helpers relative to the active window', () => {
    expect(resolveMediaArtworkUrl('/picons/channel.png')).toBe('http://localhost:3000/picons/channel.png');
    expect(resolveMediaArtworkUrl('http://example.test/logo.png')).toBe('http://example.test/logo.png');
    expect(resolveMediaArtworkUrl('')).toBeNull();
    expect(resolveApiUrl('http://api.test/base', '/services/now-next')).toBe('http://api.test/base/services/now-next');
  });

  it('resolves auto transcode codecs from advertised codecs before probe signals', () => {
    const snapshot: CapabilitySnapshot = {
      capabilitiesVersion: 3,
      container: ['ts'],
      videoCodecs: ['H264', 'hevc', 'unsupported', 'h264'],
      audioCodecs: ['aac'],
      videoCodecSignals: [
        { codec: 'av1', supported: true, smooth: true, powerEfficient: true },
      ],
    };

    expect(resolveAutoTranscodeCodecs(snapshot)).toEqual(['h264', 'hevc']);
  });

  it('falls back to codec signals and always keeps h264 as the safe floor', () => {
    const snapshot: CapabilitySnapshot = {
      capabilitiesVersion: 3,
      container: ['ts'],
      videoCodecs: [],
      audioCodecs: ['aac'],
      videoCodecSignals: [
        { codec: 'av1', supported: true, smooth: true, powerEfficient: true },
        { codec: 'hevc', supported: true, smooth: true, powerEfficient: false },
      ],
    };

    expect(resolveAutoTranscodeCodecs(snapshot)).toEqual(['av1', 'hevc']);
    expect(resolveAutoTranscodeCodecs({ ...snapshot, videoCodecSignals: [] })).toEqual(['h264']);
    expect(resolveAutoTranscodeCodecs(null)).toEqual([]);
  });

  it('normalizes playback window kinds', () => {
    expect(resolvePlaybackWindowKind('LIVE', false)).toBe('live');
    expect(resolvePlaybackWindowKind('LIVE', true)).toBe('live-dvr');
    expect(resolvePlaybackWindowKind('VOD', false)).toBe('vod');
    expect(resolvePlaybackWindowKind('UNKNOWN', true)).toBe('unknown');
    expect(normalizePlaybackWindowKind('live-dvr')).toBe('live-dvr');
    expect(normalizePlaybackWindowKind('legacy')).toBe('unknown');
  });

  it('merges operator traces without losing fallback runtime details', () => {
    const primary: PlaybackTraceOperator = {
      runtimePolicyPhase: 'cooldown',
      overrideApplied: false,
    };
    const fallback: PlaybackTraceOperator = {
      forcedIntent: 'quality',
      runtimePolicyPhase: 'probing',
      runtimeProbeSuccessStreak: 2,
      overrideApplied: true,
    };

    expect(mergePlaybackTraceOperator(primary, fallback)).toMatchObject({
      forcedIntent: 'quality',
      runtimePolicyPhase: 'cooldown',
      runtimeProbeSuccessStreak: 2,
      overrideApplied: false,
    });
    expect(mergePlaybackTraceOperator(null, null)).toBeNull();
  });

  it('extracts playback observability from backend decision traces', () => {
    const info: PlaybackInfo = {
      requestId: 'req-1',
      sessionId: 'session-1',
      mode: 'hls',
      isSeekable: true,
      durationSeconds: 123,
      decision: {
        mode: 'direct_stream',
        selectedOutputUrl: '/hls/master.m3u8',
        selectedOutputKind: 'hls',
        selected: {
          container: 'ts',
          videoCodec: 'h264',
          audioCodec: 'aac',
        },
        outputs: [],
        constraints: [],
        reasons: [],
        trace: {
          requestId: 'req-1',
          requestProfile: 'copy',
          requestedIntent: 'generic',
          resolvedIntent: 'copy',
          qualityRung: 'copy',
          hostPressureBand: 'low',
          hostOverrideApplied: true,
          targetProfileHash: 'hash-1',
          operator: { forcedIntent: 'copy' },
        },
      },
    };

    expect(extractPlaybackObservability(info, 'hlsjs')).toMatchObject({
      clientPath: 'hlsjs',
      requestProfile: 'copy',
      resolvedIntent: 'copy',
      hostOverrideApplied: true,
      targetProfileHash: 'hash-1',
      selectedOutputKind: 'hls',
    });
    expect(resolvePlaybackDurationSeconds(info)).toBe(123);
    expect(extractPlaybackObservability({ requestId: 'req-2', sessionId: 'session-2', mode: 'hls', isSeekable: false }, null)).toBeNull();
    expect(extractPlaybackObservability({ requestId: 'req-3', sessionId: 'session-3', mode: 'hls', isSeekable: false }, 'native')).toMatchObject({ clientPath: 'native' });
    expect(resolvePlaybackDurationSeconds({ requestId: 'req-4', sessionId: 'session-4', mode: 'hls', isSeekable: false, durationSeconds: 0 })).toBeNull();
  });
});
