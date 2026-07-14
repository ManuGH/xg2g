import { afterEach, describe, expect, it, vi } from 'vitest';
import { measurePlaybackNetwork } from './playbackNetworkProbe';

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('measurePlaybackNetwork', () => {
  it('coalesces concurrent probes and reuses the recent result', async () => {
    const connectionDescriptor = Object.getOwnPropertyDescriptor(navigator, 'connection');
    const connection = {
      effectiveType: '4g',
      downlink: 50,
      rtt: 20,
      saveData: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    };
    Object.defineProperty(navigator, 'connection', {
      configurable: true,
      value: connection,
    });
    const fetchMock = vi.fn(async () => new Response(new Uint8Array(512 * 1024), {
      status: 200,
      headers: { 'X-XG2G-Playback-Probe': 'measured' },
    }));
    vi.stubGlobal('fetch', fetchMock);
    let now = 0;
    vi.spyOn(performance, 'now').mockImplementation(() => {
      now += 100;
      return now;
    });

    try {
      const [first, concurrent] = await Promise.all([
        measurePlaybackNetwork('/api/test-cache'),
        measurePlaybackNetwork('/api/test-cache'),
      ]);
      const cached = await measurePlaybackNetwork('/api/test-cache');

      expect(fetchMock).toHaveBeenCalledTimes(1);
      expect(first).toEqual({ kind: 'measured', downlinkMbps: 41.94304 });
      expect(concurrent).toEqual(first);
      expect(cached).toEqual(first);
    } finally {
      if (connectionDescriptor) {
        Object.defineProperty(navigator, 'connection', connectionDescriptor);
      } else {
        delete (navigator as Navigator & { connection?: unknown }).connection;
      }
    }
  });

  it('does not reuse positive bandwidth evidence when the browser exposes no network fingerprint', async () => {
    const connectionDescriptor = Object.getOwnPropertyDescriptor(navigator, 'connection');
    delete (navigator as Navigator & { connection?: unknown }).connection;
    const fetchMock = vi.fn(async () => new Response(new Uint8Array(512 * 1024), {
      status: 200,
      headers: { 'X-XG2G-Playback-Probe': 'measured' },
    }));
    vi.stubGlobal('fetch', fetchMock);
    vi.spyOn(performance, 'now')
      .mockReturnValueOnce(0)
      .mockReturnValueOnce(100)
      .mockReturnValueOnce(200)
      .mockReturnValueOnce(300);

    try {
      await measurePlaybackNetwork('/api/test-network-handoff');
      await measurePlaybackNetwork('/api/test-network-handoff');
      expect(fetchMock).toHaveBeenCalledTimes(2);
    } finally {
      if (connectionDescriptor) {
        Object.defineProperty(navigator, 'connection', connectionDescriptor);
      }
    }
  });

  it('invalidates positive evidence when the connection object is replaced with the same fingerprint', async () => {
    const connectionDescriptor = Object.getOwnPropertyDescriptor(navigator, 'connection');
    const connection = () => ({
      effectiveType: '4g',
      downlink: 50,
      rtt: 20,
      saveData: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    });
    Object.defineProperty(navigator, 'connection', {
      configurable: true,
      value: connection(),
    });
    const fetchMock = vi.fn(async () => new Response(new Uint8Array(512 * 1024), {
      status: 200,
      headers: { 'X-XG2G-Playback-Probe': 'measured' },
    }));
    vi.stubGlobal('fetch', fetchMock);
    vi.spyOn(performance, 'now')
      .mockReturnValueOnce(0)
      .mockReturnValueOnce(100)
      .mockReturnValueOnce(200)
      .mockReturnValueOnce(300);

    try {
      await measurePlaybackNetwork('/api/test-connection-identity');
      Object.defineProperty(navigator, 'connection', {
        configurable: true,
        value: connection(),
      });
      await measurePlaybackNetwork('/api/test-connection-identity');
      expect(fetchMock).toHaveBeenCalledTimes(2);
    } finally {
      if (connectionDescriptor) {
        Object.defineProperty(navigator, 'connection', connectionDescriptor);
      } else {
        delete (navigator as Navigator & { connection?: unknown }).connection;
      }
    }
  });

  it('does not let an old probe completion remove the replacement in-flight probe', async () => {
    const connectionDescriptor = Object.getOwnPropertyDescriptor(navigator, 'connection');
    const connection = () => ({
      effectiveType: '4g',
      downlink: 50,
      rtt: 20,
      saveData: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    });
    Object.defineProperty(navigator, 'connection', {
      configurable: true,
      value: connection(),
    });
    let resolveFirst: (response: Response) => void = () => {};
    let resolveSecond: (response: Response) => void = () => {};
    const firstResponse = new Promise<Response>((resolve) => { resolveFirst = resolve; });
    const secondResponse = new Promise<Response>((resolve) => { resolveSecond = resolve; });
    const fetchMock = vi.fn()
      .mockReturnValueOnce(firstResponse)
      .mockReturnValueOnce(secondResponse);
    vi.stubGlobal('fetch', fetchMock);
    vi.spyOn(performance, 'now')
      .mockReturnValueOnce(0)
      .mockReturnValueOnce(100)
      .mockReturnValueOnce(200)
      .mockReturnValueOnce(300);

    try {
      const oldProbe = measurePlaybackNetwork('/api/test-inflight-handoff');
      Object.defineProperty(navigator, 'connection', {
        configurable: true,
        value: connection(),
      });
      const replacementProbe = measurePlaybackNetwork('/api/test-inflight-handoff');

      resolveFirst(new Response(new Uint8Array(512 * 1024), {
        status: 200,
        headers: { 'X-XG2G-Playback-Probe': 'measured' },
      }));
      await expect(oldProbe).resolves.toBeUndefined();

      const coalescedProbe = measurePlaybackNetwork('/api/test-inflight-handoff');
      expect(coalescedProbe).toBe(replacementProbe);
      expect(fetchMock).toHaveBeenCalledTimes(2);

      resolveSecond(new Response(new Uint8Array(512 * 1024), {
        status: 200,
        headers: { 'X-XG2G-Playback-Probe': 'measured' },
      }));
      await expect(replacementProbe).resolves.toEqual({
        kind: 'measured',
        downlinkMbps: 20.97152,
      });
    } finally {
      if (connectionDescriptor) {
        Object.defineProperty(navigator, 'connection', connectionDescriptor);
      } else {
        delete (navigator as Navigator & { connection?: unknown }).connection;
      }
    }
  });
});
