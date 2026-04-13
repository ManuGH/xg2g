import { describe, expect, it } from 'vitest';
import { normalizePlaybackInfo } from './normalizePlaybackInfo';

describe('normalizePlaybackInfo', () => {
  it('normalizes recording hls payloads into native_hls when native is preferred', () => {
    const contract = normalizePlaybackInfo({
      requestId: 'req-recording-native',
      mode: 'hls',
      isSeekable: true,
      decision: {
        mode: 'direct_stream',
        selectedOutputUrl: '/recordings/test/index.m3u8',
      },
    }, {
      surface: 'recording',
      preferredHlsEngine: 'native',
    });

    expect(contract.kind).toBe('playable');
    if (contract.kind !== 'playable') return;
    expect(contract.playback.mode).toBe('native_hls');
    expect(contract.playback.outputUrl).toBe('/recordings/test/index.m3u8');
    expect(contract.playback.seekable).toBe(true);
    expect(contract.session.required).toBe(false);
  });

  it('normalizes live direct_stream payloads into session-backed hlsjs contracts', () => {
    const contract = normalizePlaybackInfo({
      requestId: 'req-live-hlsjs',
      mode: 'direct_stream',
      playbackDecisionToken: 'live-token-1',
      decision: {
        mode: 'direct_stream',
        reasons: ['direct_stream_match'],
      },
    }, {
      surface: 'live',
      preferredHlsEngine: 'hlsjs',
    });

    expect(contract.kind).toBe('playable');
    if (contract.kind !== 'playable') return;
    expect(contract.playback.mode).toBe('hlsjs');
    expect(contract.playback.outputUrl).toBeNull();
    expect(contract.session.required).toBe(true);
    expect(contract.session.decisionToken).toBe('live-token-1');
  });

  it('normalizes direct_mp4 payloads and closes missing seekability to false with an advisory warning', () => {
    const contract = normalizePlaybackInfo({
      requestId: 'req-direct',
      mode: 'direct_mp4',
      decision: {
        mode: 'direct_play',
        selectedOutputUrl: 'https://cdn.example/video.mp4',
      },
    }, {
      surface: 'recording',
      preferredHlsEngine: 'hlsjs',
    });

    expect(contract.kind).toBe('playable');
    if (contract.kind !== 'playable') return;
    expect(contract.playback.mode).toBe('direct_mp4');
    expect(contract.playback.outputUrl).toBe('https://cdn.example/video.mp4');
    expect(contract.playback.seekable).toBe(false);
    expect(contract.advisory.warnings.map((warning) => warning.code)).toContain('missing_seekability_defaulted_false');
  });

  it('keeps transcode as an explicit normalized mode', () => {
    const contract = normalizePlaybackInfo({
      requestId: 'req-transcode',
      mode: 'transcode',
      isSeekable: true,
      decision: {
        mode: 'transcode',
        selectedOutputUrl: '/recordings/test/transcoded.m3u8',
      },
    }, {
      surface: 'recording',
      preferredHlsEngine: 'hlsjs',
    });

    expect(contract.kind).toBe('playable');
    if (contract.kind !== 'playable') return;
    expect(contract.playback.mode).toBe('transcode');
    expect(contract.playback.outputUrl).toBe('/recordings/test/transcoded.m3u8');
  });

  it('fails closed when mode is missing', () => {
    const contract = normalizePlaybackInfo({
      requestId: 'req-no-mode',
      decision: {
        selectedOutputUrl: '/recordings/test/index.m3u8',
      },
    }, {
      surface: 'recording',
      preferredHlsEngine: 'hlsjs',
    });

    expect(contract).toMatchObject({
      kind: 'blocked',
      failure: {
        kind: 'contract',
        code: 'missing_mode',
      },
    });
  });

  it('fails closed when mode is unknown', () => {
    const contract = normalizePlaybackInfo({
      requestId: 'req-bad-mode',
      mode: 'weird_mode',
    }, {
      surface: 'recording',
      preferredHlsEngine: 'hlsjs',
    });

    expect(contract).toMatchObject({
      kind: 'blocked',
      failure: {
        kind: 'contract',
        code: 'invalid_mode',
      },
    });
  });

  it('fails closed when recording outputUrl is missing', () => {
    const contract = normalizePlaybackInfo({
      requestId: 'req-no-url',
      mode: 'hlsjs',
      isSeekable: true,
      decision: {
        mode: 'transcode',
      },
    }, {
      surface: 'recording',
      preferredHlsEngine: 'hlsjs',
    });

    expect(contract).toMatchObject({
      kind: 'blocked',
      failure: {
        kind: 'contract',
        code: 'missing_output_url',
      },
    });
  });

  it('fails closed when live playbackDecisionToken is missing', () => {
    const contract = normalizePlaybackInfo({
      requestId: 'req-live-no-token',
      mode: 'hlsjs',
    }, {
      surface: 'live',
      preferredHlsEngine: 'hlsjs',
    });

    expect(contract).toMatchObject({
      kind: 'blocked',
      failure: {
        kind: 'contract',
        code: 'missing_decision_token',
      },
    });
  });

  it('fails closed when only the legacy url field is present', () => {
    const contract = normalizePlaybackInfo({
      requestId: 'req-legacy-url',
      mode: 'hlsjs',
      isSeekable: true,
      url: '/legacy/output.m3u8',
    }, {
      surface: 'recording',
      preferredHlsEngine: 'hlsjs',
    });

    expect(contract).toMatchObject({
      kind: 'blocked',
      failure: {
        kind: 'contract',
        code: 'missing_output_url',
      },
    });
  });

  it('keeps advisory warnings advisory when the contract is still playable', () => {
    const contract = normalizePlaybackInfo({
      requestId: 'req-legacy-seekable',
      mode: 'hlsjs',
      seekable: false,
      decision: {
        selectedOutputUrl: '/normative/output.m3u8',
      },
    }, {
      surface: 'recording',
      preferredHlsEngine: 'hlsjs',
    });

    expect(contract.kind).toBe('playable');
    if (contract.kind !== 'playable') return;
    expect(contract.advisory.warnings).toHaveLength(1);
    expect(contract.playback.seekable).toBe(false);
  });

  it('keeps backend blocking decisions blocked', () => {
    const contract = normalizePlaybackInfo({
      requestId: 'req-denied',
      mode: 'deny',
      decision: {
        mode: 'deny',
        reasons: ['policy_denies_transcode'],
      },
    }, {
      surface: 'recording',
      preferredHlsEngine: 'hlsjs',
    });

    expect(contract).toMatchObject({
      kind: 'blocked',
      failure: {
        kind: 'unsupported',
        code: 'playback_denied',
      },
    });
  });

  it('turns null and empty objects into controlled blocked contracts', () => {
    const nullContract = normalizePlaybackInfo(null, {
      surface: 'recording',
      preferredHlsEngine: 'hlsjs',
    });
    const emptyContract = normalizePlaybackInfo({}, {
      surface: 'recording',
      preferredHlsEngine: 'hlsjs',
    });

    expect(nullContract).toMatchObject({
      kind: 'blocked',
      failure: { code: 'invalid_payload' },
    });
    expect(emptyContract).toMatchObject({
      kind: 'blocked',
      failure: { code: 'missing_mode' },
    });
  });
});
