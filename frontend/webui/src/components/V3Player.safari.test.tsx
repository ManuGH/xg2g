
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render } from '@testing-library/react';
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

  beforeEach(() => {
    vi.clearAllMocks();
    (Hls as any).isSupported.mockReturnValue(true);
    userAgentGetter = vi.spyOn(window.navigator, 'userAgent', 'get');
    webkitEnterFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, 'webkitEnterFullscreen');
    webkitSupportsPresentationModeDescriptor = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, 'webkitSupportsPresentationMode');
    maxTouchPointsDescriptor = Object.getOwnPropertyDescriptor(window.navigator, 'maxTouchPoints');
  });

  afterEach(() => {
    if (webkitEnterFullscreenDescriptor) {
      Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', webkitEnterFullscreenDescriptor);
    } else {
      // eslint-disable-next-line @typescript-eslint/no-dynamic-delete
      delete (HTMLVideoElement.prototype as any).webkitEnterFullscreen;
    }
    if (webkitSupportsPresentationModeDescriptor) {
      Object.defineProperty(HTMLVideoElement.prototype, 'webkitSupportsPresentationMode', webkitSupportsPresentationModeDescriptor);
    } else {
      // eslint-disable-next-line @typescript-eslint/no-dynamic-delete
      delete (HTMLVideoElement.prototype as any).webkitSupportsPresentationMode;
    }
    if (maxTouchPointsDescriptor) {
      Object.defineProperty(window.navigator, 'maxTouchPoints', maxTouchPointsDescriptor);
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

  it('prefers native HLS on mobile WebKit when native fullscreen controls are available', () => {
    userAgentGetter.mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1');

    Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', {
      configurable: true,
      value: vi.fn()
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
