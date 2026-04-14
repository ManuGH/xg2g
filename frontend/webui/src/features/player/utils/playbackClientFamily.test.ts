import { afterEach, describe, expect, it } from 'vitest';
import {
  detectPlaybackClientFamily,
  fallbackPlaybackCapabilitiesForClientFamily,
} from './playbackClientFamily';

const originalUserAgentDescriptor = Object.getOwnPropertyDescriptor(window.navigator, 'userAgent');

function setUserAgent(value: string): void {
  Object.defineProperty(window.navigator, 'userAgent', {
    configurable: true,
    value,
  });
}

describe('playbackClientFamily', () => {
  afterEach(() => {
    if (originalUserAgentDescriptor) {
      Object.defineProperty(window.navigator, 'userAgent', originalUserAgentDescriptor);
    }
  });

  it('detects desktop safari family for native hls on macos', () => {
    setUserAgent('Mozilla/5.0 (Macintosh; Intel Mac OS X 14_4) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15');
    const video = document.createElement('video');
    video.canPlayType = ((contentType: string) => (
      contentType === 'application/vnd.apple.mpegurl' ? 'probably' : ''
    )) as typeof video.canPlayType;

    expect(detectPlaybackClientFamily(video)).toBe('safari_native');
  });

  it('detects ios safari family from touch-capable webkit clients', () => {
    setUserAgent('Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1');
    const video = document.createElement('video') as HTMLVideoElement & { webkitEnterFullscreen?: () => void };
    video.canPlayType = ((contentType: string) => (
      contentType === 'application/vnd.apple.mpegurl' ? 'probably' : ''
    )) as typeof video.canPlayType;
    video.webkitEnterFullscreen = () => {};

    expect(detectPlaybackClientFamily(video)).toBe('ios_safari_native');
  });

  it('detects firefox hls.js family from the user agent', () => {
    setUserAgent('Mozilla/5.0 (Macintosh; Intel Mac OS X 14.4; rv:124.0) Gecko/20100101 Firefox/124.0');
    expect(detectPlaybackClientFamily(null)).toBe('firefox_hlsjs');
  });

  it('keeps iPhone WebKit browsers in the native mobile family even without a video element', () => {
    setUserAgent('Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/135.0.7049.83 Mobile/15E148 Safari/604.1');
    expect(detectPlaybackClientFamily(null)).toBe('ios_safari_native');
  });

  it('keeps family fallback capability variants scoped by playback type', () => {
    const live = fallbackPlaybackCapabilitiesForClientFamily('safari_native', 'live');
    const recording = fallbackPlaybackCapabilitiesForClientFamily('safari_native', 'recording');

    expect(live.audioCodecs).toContain('ac3');
    expect(recording.audioCodecs).not.toContain('ac3');
    expect(live.deviceType).toBe('safari');
    expect(recording.preferredHlsEngine).toBe('native');
  });
});
