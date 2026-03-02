
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render } from '@testing-library/react';
import V3Player from './V3Player';
import Hls from 'hls.js';

// Mock HLS.js
vi.mock('hls.js', () => {
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

  beforeEach(() => {
    vi.clearAllMocks();
    userAgentGetter = vi.spyOn(window.navigator, 'userAgent', 'get');
    webkitEnterFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, 'webkitEnterFullscreen');
  });

  afterEach(() => {
    if (webkitEnterFullscreenDescriptor) {
      Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', webkitEnterFullscreenDescriptor);
    } else {
      // eslint-disable-next-line @typescript-eslint/no-dynamic-delete
      delete (HTMLVideoElement.prototype as any).webkitEnterFullscreen;
    }
    vi.restoreAllMocks();
  });

  it('initializes HLS.js in auto mode when HLS.js is supported', () => {
    // Simulate Safari
    userAgentGetter.mockReturnValue('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0 Safari/605.1.15');

    // Mock video element support
    const originalCreateElement = document.createElement;
    vi.spyOn(document, 'createElement').mockImplementation((tagName) => {
      const el = originalCreateElement.call(document, tagName);
      if (tagName === 'video') {
        (el as any).canPlayType = (type: string) => type === 'application/vnd.apple.mpegurl' ? 'probably' : '';
      }
      return el;
    });

    render(<V3Player src="http://example.com/playlist.m3u8" autoStart={true} />);

    // Auto mode prefers HLS.js when available.
    expect(Hls).toHaveBeenCalled();
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

  it('should prefer native HLS on mobile WebKit video elements', () => {
    userAgentGetter.mockReturnValue('Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1');

    Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', {
      configurable: true,
      value: vi.fn()
    });

    const originalCanPlayType = HTMLMediaElement.prototype.canPlayType;
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation(function (this: HTMLMediaElement, type: string) {
      if (type === 'application/vnd.apple.mpegurl') return 'probably';
      return originalCanPlayType.call(this, type);
    });

    render(<V3Player src="http://example.com/playlist.m3u8" autoStart={true} />);

    // Mobile WebKit path should avoid hls.js and use native HLS.
    expect(Hls).not.toHaveBeenCalled();
  });
});
