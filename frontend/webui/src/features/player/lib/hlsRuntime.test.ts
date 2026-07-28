import { describe, expect, it } from 'vitest';

import Hls from './hlsRuntime';

// This asserts a property of the hls.js distribution, which is unusual for a unit
// test and deliberate here: it is the integration contract the live pipeline depends
// on, and violating it produces silent failure rather than an error.
//
// The backend serves live multi-audio as HLS alternate renditions — a video-only
// variant plus EXT-X-MEDIA audio groups. hls.js only loads those renditions when both
// controllers are present:
//
//   altAudioEnabled = !!(config.audioStreamController && config.audioTrackController)
//
// The `hls.js/light` build ships both as `undefined`, so hls.js reports
// altAudio: false, never requests the audio rendition playlist, and playback runs
// silently with a video-only variant. No error, no console warning, no failed request
// — on 2026-07-26 that cost an afternoon, because the server side looked perfect:
// the audio segments were produced and never fetched.
//
// Import through ./hlsRuntime, not through 'hls.js' directly. Importing the package
// would assert something about the dependency while leaving the actual regression —
// switching hlsRuntime back to the light build — undetected.
describe('hlsRuntime', () => {
  it('exposes the alternate-audio controllers, without which live audio is silent', () => {
    expect(Hls.DefaultConfig.audioStreamController).toBeDefined();
    expect(Hls.DefaultConfig.audioTrackController).toBeDefined();
  });
});
