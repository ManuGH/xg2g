import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import {
  detectPreferredCodecs,
  detectVideoCodecSignals,
  resetCachedCodecs,
} from './codecDetection';

describe('codecDetection', () => {
  const originalMediaCapabilities = (navigator as any).mediaCapabilities;

  beforeEach(() => {
    resetCachedCodecs();
    vi.restoreAllMocks();
  });

  afterEach(() => {
    (navigator as any).mediaCapabilities = originalMediaCapabilities;
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
});
