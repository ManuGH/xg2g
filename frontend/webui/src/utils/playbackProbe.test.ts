import { beforeEach, describe, expect, it, vi } from 'vitest';
import Hls from '../lib/hlsRuntime';
import { probeRuntimePlaybackCapabilities } from './playbackProbe';
import { resetCachedCodecs } from './codecDetection';

vi.mock('../lib/hlsRuntime', () => {
  const HlsMock = vi.fn();
  (HlsMock as any).isSupported = vi.fn().mockReturnValue(true);
  return { default: HlsMock };
});

describe('probeRuntimePlaybackCapabilities', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    resetCachedCodecs();
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

    expect(probe.version).toBe(1);
    expect(probe.usedRuntimeProbe).toBe(true);
    expect(probe.nativeHls).toBe(true);
    expect(probe.hlsJs).toBe(false);
    expect(probe.preferredHlsEngine).toBe('native');
    expect(probe.hlsEngines).toEqual(['native']);
    expect(probe.containers).toEqual(['mp4', 'ts']);
    expect(probe.audioCodecs).toEqual(['aac', 'mp3', 'ac3']);
    expect(probe.videoCodecs).toContain('h264');
  });

  it('uses hls.js plus fmp4 on non-native clients and strips recording ac3', async () => {
    vi.mocked(Hls.isSupported).mockReturnValue(true);

    const video = document.createElement('video');
    vi.spyOn(video, 'canPlayType').mockImplementation((type: string) => {
      if (type.includes('avc1')) return 'probably';
      return '';
    });

    const probe = await probeRuntimePlaybackCapabilities(video, 'recording');

    expect(probe.nativeHls).toBe(false);
    expect(probe.hlsJs).toBe(true);
    expect(probe.preferredHlsEngine).toBe('hlsjs');
    expect(probe.hlsEngines).toEqual(['hlsjs']);
    expect(probe.containers).toEqual(['mp4', 'ts', 'fmp4']);
    expect(probe.audioCodecs).toEqual(['aac', 'mp3']);
    expect(probe.videoCodecs).toEqual(['h264']);
  });

  it('prefers hls.js on modern WebKit even when native HLS is also available', async () => {
    vi.mocked(Hls.isSupported).mockReturnValue(true);

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
  });
});
