import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import Hls from '../lib/hlsRuntime';
import { probeRuntimePlaybackCapabilities } from './playbackProbe';
import { resetCachedCodecs } from './codecDetection';

vi.mock('../lib/hlsRuntime', () => {
  const HlsMock = vi.fn();
  (HlsMock as any).isSupported = vi.fn().mockReturnValue(true);
  return { default: HlsMock };
});

const originalMaxTouchPointsDescriptor = Object.getOwnPropertyDescriptor(navigator, 'maxTouchPoints');
const originalUserAgentDescriptor = Object.getOwnPropertyDescriptor(navigator, 'userAgent');
const originalWebkitSupportsPresentationModeDescriptor = Object.getOwnPropertyDescriptor(
  HTMLVideoElement.prototype,
  'webkitSupportsPresentationMode'
);

describe('probeRuntimePlaybackCapabilities', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    resetCachedCodecs();
  });

  afterEach(() => {
    if (originalMaxTouchPointsDescriptor) {
      Object.defineProperty(navigator, 'maxTouchPoints', originalMaxTouchPointsDescriptor);
    }
    if (originalUserAgentDescriptor) {
      Object.defineProperty(navigator, 'userAgent', originalUserAgentDescriptor);
    }

    if (originalWebkitSupportsPresentationModeDescriptor) {
      Object.defineProperty(
        HTMLVideoElement.prototype,
        'webkitSupportsPresentationMode',
        originalWebkitSupportsPresentationModeDescriptor
      );
    } else {
      // eslint-disable-next-line @typescript-eslint/no-dynamic-delete
      delete (HTMLVideoElement.prototype as any).webkitSupportsPresentationMode;
    }
  });

  it('prefers native HLS when hls.js is unavailable and keeps live ac3 support', async () => {
    vi.mocked(Hls.isSupported).mockReturnValue(false);

    const video = document.createElement('video');
    vi.spyOn(video, 'canPlayType').mockImplementation((type: string) => {
      if (type === 'application/vnd.apple.mpegurl') return 'probably';
      if (type === 'audio/mp4; codecs="ac-3"') return 'probably';
      if (type.includes('avc1')) return 'probably';
      return '';
    });

    const probe = await probeRuntimePlaybackCapabilities(video, 'live');

    expect(probe.version).toBe(2);
    expect(probe.usedRuntimeProbe).toBe(true);
    expect(probe.nativeHls).toBe(true);
    expect(probe.hlsJs).toBe(false);
    expect(probe.preferredHlsEngine).toBe('native');
    expect(probe.hlsEngines).toEqual(['native']);
    expect(probe.containers).toEqual(['mp4', 'ts']);
    expect(probe.audioCodecs).toEqual(['aac', 'mp3', 'ac3']);
    expect(probe.videoCodecs).toContain('h264');
    expect(probe.videoCodecSignals).toEqual([
      { codec: 'av1', supported: false },
      { codec: 'hevc', supported: false },
      { codec: 'h264', supported: true },
    ]);
  });

  it('uses hls.js plus fmp4 on non-native clients and preserves recording ac3 support', async () => {
    vi.mocked(Hls.isSupported).mockReturnValue(true);

    const video = document.createElement('video');
    vi.spyOn(video, 'canPlayType').mockImplementation((type: string) => {
      if (type === 'audio/mp4; codecs="ac-3"') return 'probably';
      if (type.includes('avc1')) return 'probably';
      return '';
    });

    const probe = await probeRuntimePlaybackCapabilities(video, 'recording');

    expect(probe.nativeHls).toBe(false);
    expect(probe.hlsJs).toBe(true);
    expect(probe.preferredHlsEngine).toBe('hlsjs');
    expect(probe.hlsEngines).toEqual(['hlsjs']);
    expect(probe.containers).toEqual(['mp4', 'ts', 'fmp4']);
    expect(probe.audioCodecs).toEqual(['aac', 'mp3', 'ac3']);
    expect(probe.videoCodecs).toEqual(['h264']);
    expect(probe.videoCodecSignals).toEqual([
      { codec: 'av1', supported: false },
      { codec: 'hevc', supported: false },
      { codec: 'h264', supported: true },
    ]);
  });

  it('prefers hls.js on desktop WebKit when hls.js is available', async () => {
    vi.mocked(Hls.isSupported).mockReturnValue(true);
    Object.defineProperty(navigator, 'maxTouchPoints', {
      configurable: true,
      value: 0,
    });
    Object.defineProperty(HTMLVideoElement.prototype, 'webkitSupportsPresentationMode', {
      configurable: true,
      value: vi.fn(),
    });

    const video = document.createElement('video');
    vi.spyOn(video, 'canPlayType').mockImplementation((type: string) => {
      if (type === 'application/vnd.apple.mpegurl') return 'probably';
      if (type.includes('avc1')) return 'probably';
      return '';
    });

    const probe = await probeRuntimePlaybackCapabilities(video, 'live');

    expect(probe.nativeHls).toBe(true);
    expect(probe.hlsJs).toBe(true);
    expect(probe.preferredHlsEngine).toBe('hlsjs');
    expect(probe.hlsEngines).toEqual(['hlsjs', 'native']);
    expect(probe.containers).toEqual(['mp4', 'ts', 'fmp4']);
    expect(probe.videoCodecSignals[2]).toEqual({ codec: 'h264', supported: true });
  });

  it('prefers native HLS for desktop WebKit recordings even when hls.js is available', async () => {
    vi.mocked(Hls.isSupported).mockReturnValue(true);
    Object.defineProperty(navigator, 'maxTouchPoints', {
      configurable: true,
      value: 0,
    });
    Object.defineProperty(navigator, 'userAgent', {
      configurable: true,
      value:
        'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_4) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15',
    });
    Object.defineProperty(HTMLVideoElement.prototype, 'webkitSupportsPresentationMode', {
      configurable: true,
      value: vi.fn(),
    });

    const video = document.createElement('video');
    vi.spyOn(video, 'canPlayType').mockImplementation((type: string) => {
      if (type === 'application/vnd.apple.mpegurl') return 'probably';
      if (type === 'audio/mp4; codecs="ac-3"') return 'probably';
      if (type.includes('avc1')) return 'probably';
      return '';
    });

    const probe = await probeRuntimePlaybackCapabilities(video, 'recording');

    expect(probe.nativeHls).toBe(true);
    expect(probe.hlsJs).toBe(true);
    expect(probe.preferredHlsEngine).toBe('native');
    expect(probe.hlsEngines).toEqual(['native']);
    expect(probe.containers).toEqual(['mp4', 'ts']);
    expect(probe.audioCodecs).toEqual(['aac', 'mp3']);
  });

  it('prefers native HLS on touch WebKit even when hls.js is available', async () => {
    vi.mocked(Hls.isSupported).mockReturnValue(true);
    Object.defineProperty(navigator, 'maxTouchPoints', {
      configurable: true,
      value: 5,
    });

    const video = document.createElement('video') as HTMLVideoElement & {
      webkitEnterFullscreen?: () => void;
    };
    video.webkitEnterFullscreen = vi.fn();
    vi.spyOn(video, 'canPlayType').mockImplementation((type: string) => {
      if (type === 'application/vnd.apple.mpegurl') return 'probably';
      if (type.includes('avc1')) return 'probably';
      return '';
    });

    const probe = await probeRuntimePlaybackCapabilities(video, 'live');

    expect(probe.nativeHls).toBe(true);
    expect(probe.hlsJs).toBe(true);
    expect(probe.preferredHlsEngine).toBe('native');
    expect(probe.hlsEngines).toEqual(['native']);
    expect(probe.containers).toEqual(['mp4', 'ts']);
    expect(probe.videoCodecSignals[2]).toEqual({ codec: 'h264', supported: true });
  });
});
