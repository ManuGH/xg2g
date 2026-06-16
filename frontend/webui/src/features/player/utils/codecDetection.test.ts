import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  detectMaxVideo,
  detectPreferredCodecs,
  detectVideoCodecSignals,
  resetCachedCodecs,
} from './codecDetection';

describe('codecDetection', () => {
  const originalMediaCapabilities = (navigator as any).mediaCapabilities;
  const originalLocation = window.location;

  beforeEach(() => {
    resetCachedCodecs();
    vi.restoreAllMocks();
    // Note: codecDetection does not touch localStorage; the cache it owns is
    // reset via resetCachedCodecs() above. (A vestigial window.localStorage
    // .clear() here threw under Node 26, whose native experimental localStorage
    // is undefined unless --localstorage-file is passed.)
  });

  afterEach(() => {
    (navigator as any).mediaCapabilities = originalMediaCapabilities;
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: originalLocation,
    });
  });

  it('marks av1 as power efficient and hevc as smooth when MediaCapabilities reports it', async () => {
    (navigator as any).mediaCapabilities = {
      decodingInfo: vi.fn().mockImplementation(async ({ video }: { video?: { contentType?: string } }) => {
        const contentType = video?.contentType ?? '';
        if (contentType.includes('av01')) {
          return { supported: true, smooth: true, powerEfficient: true };
        }
        if (contentType.includes('hvc1') || contentType.includes('hev1')) {
          return { supported: true, smooth: true, powerEfficient: false };
        }
        if (contentType.includes('avc1')) {
          return { supported: true, smooth: true, powerEfficient: true };
        }
        return { supported: false, smooth: false, powerEfficient: false };
      })
    };

    const signals = await detectVideoCodecSignals();
    const preferred = await detectPreferredCodecs();

    expect(signals).toEqual([
      { codec: 'av1', supported: true, smooth: true, powerEfficient: true },
      { codec: 'hevc', supported: true, smooth: true },
      { codec: 'h264', supported: true, smooth: true, powerEfficient: true },
    ]);
    expect(preferred).toEqual(['av1', 'hevc', 'h264']);
  });

  it('falls back to HTMLVideoElement support when MediaCapabilities is unavailable', async () => {
    (navigator as any).mediaCapabilities = undefined;

    const video = document.createElement('video');
    vi.spyOn(video, 'canPlayType').mockImplementation((type: string) => {
      if (type.includes('avc1')) return 'probably';
      return '';
    });

    const signals = await detectVideoCodecSignals(video);
    const preferred = await detectPreferredCodecs(video);

    expect(signals).toEqual([
      { codec: 'av1', supported: false },
      { codec: 'hevc', supported: false },
      { codec: 'h264', supported: true },
    ]);
    expect(preferred).toEqual(['h264']);
  });

  it('allows ios native av1 when the runtime probe reports support', async () => {
    (navigator as any).mediaCapabilities = {
      decodingInfo: vi.fn().mockImplementation(async ({ video }: { video?: { contentType?: string } }) => {
        const contentType = video?.contentType ?? '';
        if (contentType.includes('av01')) {
          return { supported: true, smooth: true, powerEfficient: false };
        }
        if (contentType.includes('hvc1') || contentType.includes('hev1')) {
          return { supported: true, smooth: true, powerEfficient: true };
        }
        if (contentType.includes('avc1')) {
          return { supported: true, smooth: true, powerEfficient: true };
        }
        return { supported: false, smooth: false, powerEfficient: false };
      })
    };

    const video = document.createElement('video') as HTMLVideoElement & { webkitEnterFullscreen?: () => void };
    video.webkitEnterFullscreen = vi.fn();
    vi.spyOn(video, 'canPlayType').mockImplementation((type: string) => {
      if (type === 'application/vnd.apple.mpegurl') return 'probably';
      if (type.includes('av01')) return 'probably';
      if (type.includes('hvc1') || type.includes('hev1')) return 'probably';
      if (type.includes('avc1')) return 'probably';
      return '';
    });
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(
      'Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Mobile/15E148 Safari/604.1'
    );
    Object.defineProperty(navigator, 'maxTouchPoints', {
      configurable: true,
      value: 5,
    });

    const preferred = await detectPreferredCodecs(video);

    expect(preferred).toEqual(['av1', 'hevc', 'h264']);
  });

  it('allows desktop safari native av1 when Safari reports decode support', async () => {
    (navigator as any).mediaCapabilities = {
      decodingInfo: vi.fn().mockImplementation(async ({ video }: { video?: { contentType?: string } }) => {
        const contentType = video?.contentType ?? '';
        if (contentType.includes('av01')) {
          return { supported: true, smooth: false, powerEfficient: false };
        }
        if (contentType.includes('hvc1') || contentType.includes('hev1')) {
          return { supported: true, smooth: true, powerEfficient: true };
        }
        if (contentType.includes('avc1')) {
          return { supported: true, smooth: true, powerEfficient: true };
        }
        return { supported: false, smooth: false, powerEfficient: false };
      })
    };

    const video = document.createElement('video');
    vi.spyOn(video, 'canPlayType').mockImplementation((type: string) => {
      if (type === 'application/vnd.apple.mpegurl') return 'probably';
      if (type.includes('av01')) return 'probably';
      if (type.includes('hvc1') || type.includes('hev1')) return 'probably';
      if (type.includes('avc1')) return 'probably';
      return '';
    });
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(
      'Mozilla/5.0 (Macintosh; Intel Mac OS X 15_4) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Safari/605.1.15'
    );
    Object.defineProperty(navigator, 'maxTouchPoints', {
      configurable: true,
      value: 0,
    });

    const preferred = await detectPreferredCodecs(video);

    expect(preferred).toEqual(['av1', 'hevc', 'h264']);
  });

  it('keeps the ios native av1 query override compatible with supported-or-smooth probes', async () => {
    (navigator as any).mediaCapabilities = {
      decodingInfo: vi.fn().mockImplementation(async ({ video }: { video?: { contentType?: string } }) => {
        const contentType = video?.contentType ?? '';
        if (contentType.includes('av01')) {
          return { supported: true, smooth: true, powerEfficient: false };
        }
        if (contentType.includes('hvc1') || contentType.includes('hev1')) {
          return { supported: true, smooth: true, powerEfficient: true };
        }
        if (contentType.includes('avc1')) {
          return { supported: true, smooth: true, powerEfficient: true };
        }
        return { supported: false, smooth: false, powerEfficient: false };
      })
    };

    const video = document.createElement('video') as HTMLVideoElement & { webkitEnterFullscreen?: () => void };
    video.webkitEnterFullscreen = vi.fn();
    vi.spyOn(video, 'canPlayType').mockImplementation((type: string) => {
      if (type === 'application/vnd.apple.mpegurl') return 'probably';
      if (type.includes('av01')) return 'probably';
      if (type.includes('hvc1') || type.includes('hev1')) return 'probably';
      if (type.includes('avc1')) return 'probably';
      return '';
    });
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(
      'Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Mobile/15E148 Safari/604.1'
    );
    Object.defineProperty(navigator, 'maxTouchPoints', {
      configurable: true,
      value: 5,
    });
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: {
        ...originalLocation,
        search: '?xg2g_ios_native_av1=1',
      },
    });

    const preferred = await detectPreferredCodecs(video);

    expect(preferred).toEqual(['av1', 'hevc', 'h264']);
  });

  it('allows ios native av1 automatically on the staging host aliases', async () => {
    (navigator as any).mediaCapabilities = {
      decodingInfo: vi.fn().mockImplementation(async ({ video }: { video?: { contentType?: string } }) => {
        const contentType = video?.contentType ?? '';
        if (contentType.includes('av01')) {
          return { supported: true, smooth: true, powerEfficient: false };
        }
        if (contentType.includes('hvc1') || contentType.includes('hev1')) {
          return { supported: true, smooth: true, powerEfficient: true };
        }
        if (contentType.includes('avc1')) {
          return { supported: true, smooth: true, powerEfficient: true };
        }
        return { supported: false, smooth: false, powerEfficient: false };
      })
    };

    const video = document.createElement('video') as HTMLVideoElement & { webkitEnterFullscreen?: () => void };
    video.webkitEnterFullscreen = vi.fn();
    vi.spyOn(video, 'canPlayType').mockImplementation((type: string) => {
      if (type === 'application/vnd.apple.mpegurl') return 'probably';
      if (type.includes('av01')) return 'probably';
      if (type.includes('hvc1') || type.includes('hev1')) return 'probably';
      if (type.includes('avc1')) return 'probably';
      return '';
    });
    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(
      'Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Mobile/15E148 Safari/604.1'
    );
    Object.defineProperty(navigator, 'maxTouchPoints', {
      configurable: true,
      value: 5,
    });
    for (const hostname of ['tv.example.com', 'tv.example.net']) {
      resetCachedCodecs();
      Object.defineProperty(window, 'location', {
        configurable: true,
        value: {
          ...originalLocation,
          hostname,
          host: hostname,
          search: '',
        },
      });

      const preferred = await detectPreferredCodecs(video);
      expect(preferred).toEqual(['av1', 'hevc', 'h264']);
    }
  });

  it('recomputes preferred codecs when an ios native probe follows a cached non-native probe', async () => {
    (navigator as any).mediaCapabilities = {
      decodingInfo: vi.fn().mockImplementation(async ({ video }: { video?: { contentType?: string } }) => {
        const contentType = video?.contentType ?? '';
        if (contentType.includes('av01')) {
          return { supported: true, smooth: true, powerEfficient: false };
        }
        if (contentType.includes('hvc1') || contentType.includes('hev1')) {
          return { supported: true, smooth: true, powerEfficient: true };
        }
        if (contentType.includes('avc1')) {
          return { supported: true, smooth: true, powerEfficient: true };
        }
        return { supported: false, smooth: false, powerEfficient: false };
      })
    };

    vi.spyOn(navigator, 'userAgent', 'get').mockReturnValue(
      'Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Mobile/15E148 Safari/604.1'
    );
    Object.defineProperty(navigator, 'maxTouchPoints', {
      configurable: true,
      value: 5,
    });
    Object.defineProperty(window, 'location', {
      configurable: true,
      value: {
        ...originalLocation,
        hostname: 'tv.example.com',
        host: 'tv.example.com',
        search: '',
      },
    });

    const warmedPreferred = await detectPreferredCodecs(null);
    expect(warmedPreferred).toEqual(['hevc', 'h264']);

    const video = document.createElement('video') as HTMLVideoElement & { webkitEnterFullscreen?: () => void };
    video.webkitEnterFullscreen = vi.fn();
    vi.spyOn(video, 'canPlayType').mockImplementation((type: string) => {
      if (type === 'application/vnd.apple.mpegurl') return 'probably';
      if (type.includes('av01')) return 'probably';
      if (type.includes('hvc1') || type.includes('hev1')) return 'probably';
      if (type.includes('avc1')) return 'probably';
      return '';
    });

    const preferred = await detectPreferredCodecs(video);
    expect(preferred).toEqual(['av1', 'hevc', 'h264']);
  });

  it('detectMaxVideo reports a 2160p ceiling when the device decodes 4K smoothly', async () => {
    (navigator as any).mediaCapabilities = {
      decodingInfo: vi.fn().mockImplementation(async ({ video }: { video?: { contentType?: string } }) => {
        const contentType = video?.contentType ?? '';
        if (contentType.includes('hvc1') || contentType.includes('hev1') || contentType.includes('avc1')) {
          return { supported: true, smooth: true, powerEfficient: true };
        }
        return { supported: false, smooth: false, powerEfficient: false };
      }),
    };

    const max = await detectMaxVideo();
    expect(max).toEqual({ width: 3840, height: 2160 });
  });

  it('detectMaxVideo caps at 1080p when 4K is supported but not smooth', async () => {
    (navigator as any).mediaCapabilities = {
      decodingInfo: vi.fn().mockImplementation(async ({ video }: { video?: { contentType?: string; height?: number } }) => {
        const contentType = video?.contentType ?? '';
        const height = video?.height ?? 0;
        const isHwCodec = contentType.includes('hvc1') || contentType.includes('hev1') || contentType.includes('avc1');
        if (isHwCodec && height <= 1080) {
          return { supported: true, smooth: true, powerEfficient: true };
        }
        if (isHwCodec) {
          // 4K rung: decoder accepts it but cannot sustain it.
          return { supported: true, smooth: false, powerEfficient: false };
        }
        return { supported: false, smooth: false, powerEfficient: false };
      }),
    };

    const max = await detectMaxVideo();
    expect(max).toEqual({ width: 1920, height: 1080 });
  });

  it('detectMaxVideo returns null when MediaCapabilities is unavailable (no guessing)', async () => {
    (navigator as any).mediaCapabilities = undefined;
    const max = await detectMaxVideo();
    expect(max).toBeNull();
  });
});
