import { afterEach, describe, expect, it } from 'vitest';
import {
  detectPlaybackClientFamily,
  normalizePlaybackClientFamily,
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

  it('detects NVIDIA Shield browser as Android TV browser', () => {
    setUserAgent('Mozilla/5.0 (Linux; Android 11; SHIELD Android TV) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36');
    expect(detectPlaybackClientFamily(null)).toBe('android_tv_browser');
  });

  it('detects Fire TV build models as Android TV browsers', () => {
    setUserAgent('Mozilla/5.0 (Linux; Android 11; AFTKRT Build/RS8141) AppleWebKit/537.36 (KHTML, like Gecko) Silk/124.0 Safari/537.36');
    expect(detectPlaybackClientFamily(null)).toBe('android_tv_browser');
  });

  it('detects Vega OS Fire TV build models without Android in the user agent', () => {
    setUserAgent('Mozilla/5.0 (Linux; Vega OS 1.1; AFTCL001) AppleWebKit/537.36 (KHTML, like Gecko) Silk/124.0 Safari/537.36');
    expect(detectPlaybackClientFamily(null)).toBe('android_tv_browser');
  });

  it('normalizes family identity without inventing media capabilities', () => {
    expect(normalizePlaybackClientFamily('safari')).toBe('safari_native');
    expect(normalizePlaybackClientFamily('android_tv_hlsjs')).toBe('android_tv_browser');
    expect(normalizePlaybackClientFamily('unknown')).toBeUndefined();
  });
});
