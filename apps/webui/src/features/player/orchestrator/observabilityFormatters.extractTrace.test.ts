import { describe, expect, it } from 'vitest';
import { extractPlaybackTrace } from './observabilityFormatters';

describe('extractPlaybackTrace', () => {
  // Regression: GET /sessions/{id} returns a wrapper whose TOP LEVEL carries
  // requestId + sessionId (so it "looks like a trace"), but the executed trace —
  // with targetProfile / ffmpegPlan — lives under `.trace`. Must descend into
  // `.trace` rather than returning the wrapper, else the stats panel stays blank.
  it('descends into .trace for a session wrapper instead of returning the wrapper', () => {
    const sessionResponse = {
      requestId: 'top-level-req',
      sessionId: 'sess-123',
      state: 'READY',
      mode: 'LIVE',
      trace: {
        requestId: 'trace-req',
        sessionId: 'sess-123',
        clientPath: 'native_hls',
        targetProfileHash: 'exec-hash',
        targetProfile: {
          container: 'mpegts',
          packaging: 'ts',
          video: { mode: 'copy', codec: 'h264' },
          audio: { mode: 'transcode', codec: 'aac' },
        },
        ffmpegPlan: { container: 'mpegts', videoMode: 'copy', audioCodec: 'aac' },
      },
    };

    const trace = extractPlaybackTrace(sessionResponse);
    expect(trace).not.toBeNull();
    expect(trace?.targetProfile?.container).toBe('mpegts');
    expect(trace?.targetProfileHash).toBe('exec-hash');
    expect(trace?.ffmpegPlan?.videoMode).toBe('copy');
    // it must be the executed trace, not the wrapper
    expect(trace?.requestId).toBe('trace-req');
  });

  it('returns a bare trace object unchanged', () => {
    const bare = {
      requestId: 'r1',
      sessionId: 's1',
      targetProfile: { container: 'fmp4', packaging: 'fmp4', video: { mode: 'transcode', codec: 'av1' }, audio: { mode: 'transcode', codec: 'aac' } },
    };
    expect(extractPlaybackTrace(bare)?.targetProfile?.container).toBe('fmp4');
  });

  it('returns null for an object with no trace markers', () => {
    expect(extractPlaybackTrace({ foo: 'bar' })).toBeNull();
    expect(extractPlaybackTrace(null)).toBeNull();
  });
});
