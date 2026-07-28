import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { buildLiveIntentBody, getStoredDvrWindowSec } from './startupHelpers';

describe('buildLiveIntentBody & getStoredDvrWindowSec', () => {
  const dummyCaps = { videoCodecs: ['h264'] } as any;

  beforeEach(() => {
    window.localStorage.clear();
  });

  afterEach(() => {
    window.localStorage.clear();
  });

  it('includes dvr_window_sec=0 when stored setting is live_only', () => {
    window.localStorage.setItem('xg2g.settings.dvrMode', 'live_only');
    expect(getStoredDvrWindowSec()).toBe(0);

    const intent = buildLiveIntentBody('1:0:1:1:0:0:0:0:0:0:', 'token-123', dummyCaps, 'native_hls');
    expect(intent.params?.dvr_window_sec).toBe('0');
  });

  it('includes dvr_window_sec=7200 when stored setting is 2h', () => {
    window.localStorage.setItem('xg2g.settings.dvrMode', '2h');
    expect(getStoredDvrWindowSec()).toBe(7200);

    const intent = buildLiveIntentBody('1:0:1:1:0:0:0:0:0:0:', 'token-123', dummyCaps, 'native_hls');
    expect(intent.params?.dvr_window_sec).toBe('7200');
  });

  it('allows explicit override over stored setting', () => {
    window.localStorage.setItem('xg2g.settings.dvrMode', '2h');
    const intent = buildLiveIntentBody('1:0:1:1:0:0:0:0:0:0:', 'token-123', dummyCaps, 'native_hls', 0);
    expect(intent.params?.dvr_window_sec).toBe('0');
  });
});
