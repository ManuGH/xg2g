import { afterEach, describe, expect, it, vi } from 'vitest';
import { detectMaxVideo, resetCachedMaxVideo } from './codecDetection';

type DecodeReply = { supported: boolean; smooth?: boolean; powerEfficient?: boolean };

function installMediaCapabilities(reply: (height: number) => DecodeReply): void {
  (navigator as unknown as { mediaCapabilities?: unknown }).mediaCapabilities = {
    decodingInfo: async (cfg: { video: { height: number } }) => reply(cfg.video.height),
  };
}

afterEach(() => {
  resetCachedMaxVideo();
  delete (navigator as unknown as { mediaCapabilities?: unknown }).mediaCapabilities;
  vi.restoreAllMocks();
});

describe('detectMaxVideo', () => {
  it('reports 2160p when the device decodes 4K (highest rung wins)', async () => {
    installMediaCapabilities(() => ({ supported: true, smooth: true }));
    expect(await detectMaxVideo()).toEqual({ width: 3840, height: 2160, fps: 60 });
  });

  it('accepts DECODE capability even when not smooth (the iPhone-17-Pro fix)', async () => {
    // 4K decodes but reports smooth:false — copy/direct only needs decode.
    installMediaCapabilities(() => ({ supported: true, smooth: false, powerEfficient: false }));
    expect(await detectMaxVideo()).toEqual({ width: 3840, height: 2160, fps: 60 });
  });

  it('caps at 1080p when 2160p is not decodable', async () => {
    installMediaCapabilities((height) => ({ supported: height <= 1080 }));
    expect(await detectMaxVideo()).toEqual({ width: 1920, height: 1080, fps: 60 });
  });

  it('returns undefined when MediaCapabilities is unavailable (backend fixture decides)', async () => {
    // no mediaCapabilities installed
    expect(await detectMaxVideo()).toBeUndefined();
  });

  it('returns undefined when nothing decodes', async () => {
    installMediaCapabilities(() => ({ supported: false }));
    expect(await detectMaxVideo()).toBeUndefined();
  });
});
