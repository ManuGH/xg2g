
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

  beforeEach(() => {
    userAgentGetter = vi.spyOn(window.navigator, 'userAgent', 'get');
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('should NOT initialize HLS.js on Safari (Native Playback)', () => {
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

    // HLS.js should NOT be instantiated
    expect(Hls).not.toHaveBeenCalled();
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
});
