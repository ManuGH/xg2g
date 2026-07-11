import { afterEach, describe, expect, it, vi } from 'vitest';
import { measurePlaybackNetwork } from './playbackNetworkProbe';

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('measurePlaybackNetwork', () => {
  it('coalesces concurrent probes and reuses the recent result', async () => {
    const fetchMock = vi.fn(async () => new Response(new Uint8Array(512 * 1024), {
      status: 200,
      headers: { 'X-XG2G-Playback-Probe': 'measured' },
    }));
    vi.stubGlobal('fetch', fetchMock);
    vi.spyOn(performance, 'now').mockReturnValueOnce(0).mockReturnValueOnce(100);

    const [first, concurrent] = await Promise.all([
      measurePlaybackNetwork('/api/test-cache'),
      measurePlaybackNetwork('/api/test-cache'),
    ]);
    const cached = await measurePlaybackNetwork('/api/test-cache');

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(first).toEqual({ kind: 'measured', downlinkMbps: 41.94304 });
    expect(concurrent).toEqual(first);
    expect(cached).toEqual(first);
  });
});
