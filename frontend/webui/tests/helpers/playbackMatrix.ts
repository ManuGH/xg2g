import Hls from '../../src/lib/hlsRuntime';
import { vi } from 'vitest';
import { resetCachedCodecs } from '../../src/utils/codecDetection';

export type BrowserFamily = 'safari_native' | 'ios_safari_native' | 'firefox_hlsjs' | 'chromium_hlsjs';

type CapabilityExpectation = {
  container: string[];
  videoCodecs: string[];
  audioCodecs: string[];
  hlsEngines: Array<'native' | 'hlsjs'>;
  preferredHlsEngine: 'native' | 'hlsjs';
};

type TraceExpectation = {
  requestedIntent: string;
  resolvedIntent: string;
  qualityRung: string;
  degradedFrom: string | null;
};

export type BrowserFamilyMatrixCase = {
  id: BrowserFamily;
  liveMode: 'native_hls' | 'hlsjs';
  recordingMode: 'native_hls' | 'hlsjs';
  expectedOverlayClientPath: string;
  capabilities: {
    live: CapabilityExpectation;
    recording: CapabilityExpectation;
  };
  trace: TraceExpectation;
};

type BrowserProbeFixture = BrowserFamilyMatrixCase & {
  userAgent: string;
  supportsNativeHls: boolean;
  supportsAc3: boolean;
  hlsJsSupported: boolean;
  maxTouchPoints: number;
  supportsHevc: boolean;
  desktopPresentationMode: boolean;
  webkitFullscreen: boolean;
};

const browserProbeFixtures: Record<BrowserFamily, BrowserProbeFixture> = {
  safari_native: {
    id: 'safari_native',
    userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_4) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15',
    liveMode: 'native_hls',
    recordingMode: 'native_hls',
    expectedOverlayClientPath: 'native',
    supportsNativeHls: true,
    supportsAc3: true,
    hlsJsSupported: false,
    maxTouchPoints: 0,
    supportsHevc: true,
    desktopPresentationMode: true,
    webkitFullscreen: false,
    capabilities: {
      live: {
        container: ['mp4', 'ts'],
        videoCodecs: ['hevc', 'h264'],
        audioCodecs: ['aac', 'mp3', 'ac3'],
        hlsEngines: ['native'],
        preferredHlsEngine: 'native',
      },
      recording: {
        container: ['mp4', 'ts'],
        videoCodecs: ['hevc', 'h264'],
        audioCodecs: ['aac', 'mp3'],
        hlsEngines: ['native'],
        preferredHlsEngine: 'native',
      },
    },
    trace: {
      requestedIntent: 'quality',
      resolvedIntent: 'quality',
      qualityRung: 'quality_audio_aac_320_stereo',
      degradedFrom: null,
    },
  },
  ios_safari_native: {
    id: 'ios_safari_native',
    userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1',
    liveMode: 'native_hls',
    recordingMode: 'native_hls',
    expectedOverlayClientPath: 'native',
    supportsNativeHls: true,
    supportsAc3: true,
    hlsJsSupported: false,
    maxTouchPoints: 5,
    supportsHevc: true,
    desktopPresentationMode: false,
    webkitFullscreen: true,
    capabilities: {
      live: {
        container: ['mp4', 'ts'],
        videoCodecs: ['hevc', 'h264'],
        audioCodecs: ['aac', 'mp3', 'ac3'],
        hlsEngines: ['native'],
        preferredHlsEngine: 'native',
      },
      recording: {
        container: ['mp4', 'ts'],
        videoCodecs: ['hevc', 'h264'],
        audioCodecs: ['aac', 'mp3'],
        hlsEngines: ['native'],
        preferredHlsEngine: 'native',
      },
    },
    trace: {
      requestedIntent: 'quality',
      resolvedIntent: 'quality',
      qualityRung: 'quality_audio_aac_320_stereo',
      degradedFrom: null,
    },
  },
  firefox_hlsjs: {
    id: 'firefox_hlsjs',
    userAgent: 'Mozilla/5.0 (Macintosh; Intel Mac OS X 14.4; rv:124.0) Gecko/20100101 Firefox/124.0',
    liveMode: 'hlsjs',
    recordingMode: 'hlsjs',
    expectedOverlayClientPath: 'hlsjs',
    supportsNativeHls: false,
    supportsAc3: false,
    hlsJsSupported: true,
    maxTouchPoints: 0,
    supportsHevc: false,
    desktopPresentationMode: false,
    webkitFullscreen: false,
    capabilities: {
      live: {
        container: ['mp4', 'ts', 'fmp4'],
        videoCodecs: ['h264'],
        audioCodecs: ['aac', 'mp3'],
        hlsEngines: ['hlsjs'],
        preferredHlsEngine: 'hlsjs',
      },
      recording: {
        container: ['mp4', 'ts', 'fmp4'],
        videoCodecs: ['h264'],
        audioCodecs: ['aac', 'mp3'],
        hlsEngines: ['hlsjs'],
        preferredHlsEngine: 'hlsjs',
      },
    },
    trace: {
      requestedIntent: 'quality',
      resolvedIntent: 'compatible',
      qualityRung: 'compatible_audio_aac_256_stereo',
      degradedFrom: 'quality',
    },
  },
  chromium_hlsjs: {
    id: 'chromium_hlsjs',
    userAgent: 'Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36',
    liveMode: 'hlsjs',
    recordingMode: 'hlsjs',
    expectedOverlayClientPath: 'hlsjs',
    supportsNativeHls: false,
    supportsAc3: false,
    hlsJsSupported: true,
    maxTouchPoints: 0,
    supportsHevc: false,
    desktopPresentationMode: false,
    webkitFullscreen: false,
    capabilities: {
      live: {
        container: ['mp4', 'ts', 'fmp4'],
        videoCodecs: ['h264'],
        audioCodecs: ['aac', 'mp3'],
        hlsEngines: ['hlsjs'],
        preferredHlsEngine: 'hlsjs',
      },
      recording: {
        container: ['mp4', 'ts', 'fmp4'],
        videoCodecs: ['h264'],
        audioCodecs: ['aac', 'mp3'],
        hlsEngines: ['hlsjs'],
        preferredHlsEngine: 'hlsjs',
      },
    },
    trace: {
      requestedIntent: 'quality',
      resolvedIntent: 'compatible',
      qualityRung: 'compatible_audio_aac_256_stereo',
      degradedFrom: 'quality',
    },
  },
};

export const browserFamilyMatrixCases: BrowserFamilyMatrixCase[] = Object.values(browserProbeFixtures).map((fixture) => ({
  id: fixture.id,
  liveMode: fixture.liveMode,
  recordingMode: fixture.recordingMode,
  expectedOverlayClientPath: fixture.expectedOverlayClientPath,
  capabilities: fixture.capabilities,
  trace: fixture.trace,
}));

export function browserFamilyMatrixCase(id: BrowserFamily): BrowserFamilyMatrixCase {
  return browserProbeFixtures[id];
}

type Restorer = () => void;

function restoreProperty(target: object, key: string, descriptor: PropertyDescriptor | undefined): void {
  if (descriptor) {
    Object.defineProperty(target, key, descriptor);
    return;
  }
  // eslint-disable-next-line @typescript-eslint/no-dynamic-delete
  delete (target as Record<string, unknown>)[key];
}

export function applyBrowserFamilyMatrix(id: BrowserFamily): Restorer {
  const fixture = browserProbeFixtures[id];
  const restore: Restorer[] = [];

  const maxTouchPointsDescriptor = Object.getOwnPropertyDescriptor(window.navigator, 'maxTouchPoints');
  const userAgentDescriptor = Object.getOwnPropertyDescriptor(window.navigator, 'userAgent');
  const mediaCapabilitiesDescriptor = Object.getOwnPropertyDescriptor(window.navigator, 'mediaCapabilities');
  const webkitEnterFullscreenDescriptor = Object.getOwnPropertyDescriptor(HTMLVideoElement.prototype, 'webkitEnterFullscreen');
  const webkitSupportsPresentationModeDescriptor = Object.getOwnPropertyDescriptor(
    HTMLVideoElement.prototype,
    'webkitSupportsPresentationMode'
  );

  resetCachedCodecs();

  Object.defineProperty(window.navigator, 'maxTouchPoints', {
    configurable: true,
    value: fixture.maxTouchPoints,
  });
  restore.push(() => restoreProperty(window.navigator, 'maxTouchPoints', maxTouchPointsDescriptor));

  Object.defineProperty(window.navigator, 'userAgent', {
    configurable: true,
    value: fixture.userAgent,
  });
  restore.push(() => restoreProperty(window.navigator, 'userAgent', userAgentDescriptor));

  Object.defineProperty(window.navigator, 'mediaCapabilities', {
    configurable: true,
    value: {
      decodingInfo: vi.fn(async (config: { video?: { contentType?: string } }) => {
        const contentType = String(config?.video?.contentType ?? '');
        if (contentType.includes('av01')) {
          return { supported: false };
        }
        if (contentType.includes('hvc1') || contentType.includes('hev1')) {
          return { supported: fixture.supportsHevc };
        }
        if (contentType.includes('avc1')) {
          return { supported: true };
        }
        return { supported: false };
      }),
    },
  });
  restore.push(() => restoreProperty(window.navigator, 'mediaCapabilities', mediaCapabilitiesDescriptor));

  if (fixture.webkitFullscreen) {
    Object.defineProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', {
      configurable: true,
      value: vi.fn(),
    });
  } else {
    restoreProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', undefined);
  }
  restore.push(() => restoreProperty(HTMLVideoElement.prototype, 'webkitEnterFullscreen', webkitEnterFullscreenDescriptor));

  if (fixture.desktopPresentationMode) {
    Object.defineProperty(HTMLVideoElement.prototype, 'webkitSupportsPresentationMode', {
      configurable: true,
      value: vi.fn(() => true),
    });
  } else {
    restoreProperty(HTMLVideoElement.prototype, 'webkitSupportsPresentationMode', undefined);
  }
  restore.push(() =>
    restoreProperty(HTMLVideoElement.prototype, 'webkitSupportsPresentationMode', webkitSupportsPresentationModeDescriptor)
  );

  const canPlayTypeSpy = vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation((contentType: string) => {
    if (contentType === 'application/vnd.apple.mpegurl') {
      return fixture.supportsNativeHls ? 'probably' : '';
    }
    if (contentType === 'audio/mp4; codecs="ac-3"') {
      return fixture.supportsAc3 ? 'probably' : '';
    }
    if (contentType.includes('hvc1') || contentType.includes('hev1')) {
      return fixture.supportsHevc ? 'probably' : '';
    }
    if (contentType.includes('avc1')) {
      return 'probably';
    }
    return '';
  });
  restore.push(() => canPlayTypeSpy.mockRestore());

  const hlsSupportSpy = vi.spyOn(Hls, 'isSupported').mockReturnValue(fixture.hlsJsSupported);
  restore.push(() => hlsSupportSpy.mockRestore());

  return () => {
    while (restore.length > 0) {
      const fn = restore.pop();
      fn?.();
    }
    resetCachedCodecs();
  };
}
