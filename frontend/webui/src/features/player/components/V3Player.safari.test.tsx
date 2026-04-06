
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import V3Player from './V3Player';
import Hls from '../lib/hlsRuntime';

// Mock HLS.js
vi.mock('../lib/hlsRuntime', () => {
  const HlsMock = vi.fn().mockImplementation(function (this: any) {
    return {
      on: vi.fn(),
      loadSource: vi.fn(),
      attachMedia: vi.fn(),
      destroy: vi.fn(),
      recoverMediaError: vi.fn(),
    };
  });

  // Static methods and properties
  (HlsMock as any).isSupported = vi.fn().mockReturnValue(true);
  (HlsMock as any).Events = {
    LEVEL_SWITCHED: 'hlsLevelSwitched',
    MANIFEST_PARSED: 'hlsManifestParsed',
    FRAG_LOADED: 'hlsFragLoaded',
    ERROR: 'hlsError'
  };
  (HlsMock as any).ErrorTypes = { NETWORK_ERROR: 'networkError' };
  (HlsMock as any).ErrorDetails = { MANIFEST_LOAD_ERROR: 'manifestLoadError' };

  return { default: HlsMock };
});

describe('V3Player Safari Logic', () => {
  let userAgentGetter: any;
  let webkitEnterFullscreenDescriptor: PropertyDescriptor | undefined;
  let webkitSupportsPresentationModeDescriptor: PropertyDescriptor | undefined;
  let maxTouchPointsDescriptor: PropertyDescriptor | undefined;
  let requestFullscreenDescriptor: PropertyDescriptor | undefined;
  const requestFullscreen = vi.fn().mockResolvedValue(undefined);
  const webkitEnterFullscreen = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    (Hls as any).isSupported.mockReturnValue(true);
    userAgentGetter = vi.spyOn(window.navigator, 'userAgent', 'get');
    webkitEnterFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, 'webkitEnterFullscreen');
    webkitSupportsPresentationModeDescriptor = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, 'webkitSupportsPresentationMode');
    maxTouchPointsDescriptor = Object.getOwnPropertyDescriptor(window.navigator, 'maxTouchPoints');
    requestFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLDivElement.prototype, 'requestFullscreen');

    Object.defineProperty(HTMLDivElement.prototype, 'requestFullscreen', {
      configurable: true,
      value: requestFullscreen
    });
  });

  afterEach(() => {
    if (webkitEnterFullscreenDescriptor) {
      Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', webkitEnterFullscreenDescriptor);
    } else {
      delete (HTMLVideoElement.prototype as any).webkitEnterFullscreen;
    }
    if (webkitSupportsPresentationModeDescriptor) {
      Object.defineProperty(HTMLVideoElement.prototype, 'webkitSupportsPresentationMode', webkitSupportsPresentationModeDescriptor);
    } else {
      delete (HTMLVideoElement.prototype as any).webkitSupportsPresentationMode;
    }
    if (maxTouchPointsDescriptor) {
      Object.defineProperty(window.navigator, 'maxTouchPoints', maxTouchPointsDescriptor);
    }
    if (requestFullscreenDescriptor) {
      Object.defineProperty(HTMLDivElement.prototype, 'requestFullscreen', requestFullscreenDescriptor);
    } else {
      delete (HTMLDivElement.prototype as any).requestFullscreen;
    }
    vi.restoreAllMocks();
  });

  it('prefers native HLS on desktop Safari when WebKit playback controls are available', () => {
    // Simulate Safari
    userAgentGetter.mockReturnValue('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0 Safari/605.1.15');
    Object.defineProperty(HTMLVideoElement.prototype, 'webkitSupportsPresentationMode', {
      configurable: true,
      value: vi.fn()
    });
    Object.defineProperty(window.navigator, 'maxTouchPoints', {
      configurable: true,
      value: 0
    });

    // Mock video element support
    const originalCreateElement = document.createElement;
    vi.spyOn(document, 'createElement').mockImplementation((tagName) => {
      const el = originalCreateElement.call(document, tagName);
      if (tagName === 'video') {
        (el as any).canPlayType = (type: string) => type === 'application/vnd.apple.mpegurl' ? 'probably' : '';
      }
      return el;
    });

    const { container } = render(<V3Player src="http://example.com/playlist.m3u8" autoStart={true} />);

    expect(Hls).not.toHaveBeenCalled();
    expect(container.querySelector('video')?.getAttribute('src')).toBe('http://example.com/playlist.m3u8');
  });

  it('should initialize HLS.js on Chrome (Non-Safari)', () => {
    // Simulate Chrome
    userAgentGetter.mockReturnValue('Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/90.0.4430.212 Safari/537.36');

    // Mock video element support (Chrome handles HLS via MSE, so native might say 'maybe' or '')
    const originalCreateElement = document.createElement;
    vi.spyOn(document, 'createElement').mockImplementation((tagName) => {
      const el = originalCreateElement.call(document, tagName);
      if (tagName === 'video') {
        (el as any).canPlayType = (_type: string) => ''; // Chrome doesn't play m3u8 natively usually
      }
      return el;
    });

    render(<V3Player src="http://example.com/playlist.m3u8" autoStart={true} />);

    // HLS.js SHOULD be instantiated
    expect(Hls).toHaveBeenCalled();
  });

  it('falls back to native HLS on desktop Safari when hls.js is unsupported', () => {
    userAgentGetter.mockReturnValue('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15');

    Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', {
      configurable: true,
      value: vi.fn()
    });
    Object.defineProperty(window.navigator, 'maxTouchPoints', {
      configurable: true,
      value: 0
    });

    const originalCanPlayType = HTMLMediaElement.prototype.canPlayType;
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation(function (this: HTMLMediaElement, type: string) {
      if (type === 'application/vnd.apple.mpegurl') return 'probably';
      return originalCanPlayType.call(this, type);
    });
    (Hls as any).isSupported.mockReturnValue(false);

    render(<V3Player src="http://example.com/playlist.m3u8" autoStart={true} />);

    expect(Hls).not.toHaveBeenCalled();
  });

  it('uses container fullscreen by default on desktop Safari and keeps native fullscreen as a separate action', async () => {
    userAgentGetter.mockReturnValue('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15');

    Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', {
      configurable: true,
      value: webkitEnterFullscreen
    });
    Object.defineProperty(window.navigator, 'maxTouchPoints', {
      configurable: true,
      value: 0
    });

    const originalCanPlayType = HTMLMediaElement.prototype.canPlayType;
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation(function (this: HTMLMediaElement, type: string) {
      if (type === 'application/vnd.apple.mpegurl') return 'probably';
      return originalCanPlayType.call(this, type);
    });

    render(<V3Player src="http://example.com/playlist.m3u8" autoStart={true} />);

    await waitFor(() => {
      expect(Hls).not.toHaveBeenCalled();
    });

    fireEvent.click(await screen.findByRole('button', { name: /fullscreen/i }));
    expect(requestFullscreen).toHaveBeenCalledTimes(1);
    expect(webkitEnterFullscreen).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: /native/i }));
    expect(webkitEnterFullscreen).toHaveBeenCalledTimes(1);
  });

  it('prefers native HLS on mobile WebKit when native fullscreen controls are available', () => {
    userAgentGetter.mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1');

    Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', {
      configurable: true,
      value: webkitEnterFullscreen
    });
    Object.defineProperty(window.navigator, 'maxTouchPoints', {
      configurable: true,
      value: 5
    });

    const originalCanPlayType = HTMLMediaElement.prototype.canPlayType;
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation(function (this: HTMLMediaElement, type: string) {
      if (type === 'application/vnd.apple.mpegurl') return 'probably';
      return originalCanPlayType.call(this, type);
    });

    const { container } = render(<V3Player src="http://example.com/playlist.m3u8" autoStart={true} />);

    expect(Hls).not.toHaveBeenCalled();
    expect(container.querySelector('video')?.getAttribute('src')).toBe('http://example.com/playlist.m3u8');
  });
});
