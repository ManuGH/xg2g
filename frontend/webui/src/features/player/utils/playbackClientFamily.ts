import type { RuntimePlaybackProbeScope } from './playbackProbe';
import { hasTouchInput, shouldForceNativeMobileHls } from './playerHelpers';

export type PlaybackClientFamily =
  | 'safari_native'
  | 'ios_safari_native'
  | 'firefox_hlsjs'
  | 'android_tv_browser'
  | 'chromium_hlsjs';

export function normalizePlaybackClientFamily(
  value: string | null | undefined,
): PlaybackClientFamily | undefined {
  switch ((value || '').trim().toLowerCase()) {
    case 'safari':
    case 'safari_native':
      return 'safari_native';
    case 'ios_safari':
    case 'ios_safari_native':
      return 'ios_safari_native';
    case 'firefox':
    case 'firefox_hlsjs':
      return 'firefox_hlsjs';
    case 'android_tv':
    case 'android_tv_browser':
    case 'android_tv_hlsjs':
    case 'shield_browser':
      return 'android_tv_browser';
    case 'chromium':
    case 'chrome':
    case 'edge':
    case 'chromium_hlsjs':
      return 'chromium_hlsjs';
    default:
      return undefined;
  }
}

type PlaybackClientFamilyCapabilities = {
  deviceType: 'safari' | 'ios_safari' | 'firefox' | 'android_tv' | 'chromium';
  container: string[];
  videoCodecs: string[];
  audioCodecs: string[];
  hlsEngines: Array<'native' | 'hlsjs'>;
  preferredHlsEngine: 'native' | 'hlsjs';
};

const PLAYBACK_CLIENT_FAMILY_CAPABILITIES: Record<
  PlaybackClientFamily,
  Record<RuntimePlaybackProbeScope, PlaybackClientFamilyCapabilities>
> = {
  safari_native: {
    live: {
      deviceType: 'safari',
      container: ['mp4', 'ts'],
      videoCodecs: ['hevc', 'h264'],
      audioCodecs: ['aac', 'mp3', 'ac3'],
      hlsEngines: ['native'],
      preferredHlsEngine: 'native',
    },
    recording: {
      deviceType: 'safari',
      container: ['mp4', 'ts'],
      videoCodecs: ['hevc', 'h264'],
      audioCodecs: ['aac', 'mp3'],
      hlsEngines: ['native'],
      preferredHlsEngine: 'native',
    },
  },
  ios_safari_native: {
    live: {
      deviceType: 'ios_safari',
      container: ['mp4', 'ts'],
      videoCodecs: ['hevc', 'h264'],
      audioCodecs: ['aac', 'mp3', 'ac3'],
      hlsEngines: ['native'],
      preferredHlsEngine: 'native',
    },
    recording: {
      deviceType: 'ios_safari',
      container: ['mp4', 'ts'],
      videoCodecs: ['hevc', 'h264'],
      audioCodecs: ['aac', 'mp3'],
      hlsEngines: ['native'],
      preferredHlsEngine: 'native',
    },
  },
  firefox_hlsjs: {
    live: {
      deviceType: 'firefox',
      container: ['mp4', 'ts', 'fmp4'],
      videoCodecs: ['h264'],
      audioCodecs: ['aac', 'mp3'],
      hlsEngines: ['hlsjs'],
      preferredHlsEngine: 'hlsjs',
    },
    recording: {
      deviceType: 'firefox',
      container: ['mp4', 'ts', 'fmp4'],
      videoCodecs: ['h264'],
      audioCodecs: ['aac', 'mp3'],
      hlsEngines: ['hlsjs'],
      preferredHlsEngine: 'hlsjs',
    },
  },
  android_tv_browser: {
    live: {
      deviceType: 'android_tv',
      container: ['mp4', 'ts', 'fmp4'],
      videoCodecs: ['h264'],
      audioCodecs: ['aac', 'mp3'],
      hlsEngines: ['hlsjs'],
      preferredHlsEngine: 'hlsjs',
    },
    recording: {
      deviceType: 'android_tv',
      container: ['mp4', 'ts', 'fmp4'],
      videoCodecs: ['h264'],
      audioCodecs: ['aac', 'mp3'],
      hlsEngines: ['hlsjs'],
      preferredHlsEngine: 'hlsjs',
    },
  },
  chromium_hlsjs: {
    live: {
      deviceType: 'chromium',
      container: ['mp4', 'ts', 'fmp4'],
      videoCodecs: ['h264'],
      audioCodecs: ['aac', 'mp3'],
      hlsEngines: ['hlsjs'],
      preferredHlsEngine: 'hlsjs',
    },
    recording: {
      deviceType: 'chromium',
      container: ['mp4', 'ts', 'fmp4'],
      videoCodecs: ['h264'],
      audioCodecs: ['aac', 'mp3'],
      hlsEngines: ['hlsjs'],
      preferredHlsEngine: 'hlsjs',
    },
  },
};

function currentUserAgent(): string {
  try {
    return navigator.userAgent || '';
  } catch {
    return '';
  }
}

function isFirefoxUserAgent(): boolean {
  return /firefox/i.test(currentUserAgent());
}

function isIOSUserAgent(): boolean {
  const ua = currentUserAgent();
  return /(iphone|ipad|ipod)/i.test(ua) || (/macintosh/i.test(ua) && hasTouchInput());
}

function isAndroidTVUserAgent(): boolean {
  const ua = currentUserAgent();
  return (
    /\baft[a-z0-9]+\b/i.test(ua) ||
    /fire\s*tv/i.test(ua) ||
    (/android/i.test(ua) && /(android\s*tv|shield|bravia|smart[-\s]?tv|hbbtv|googletv|chromecast)/i.test(ua))
  );
}

export function detectPlaybackClientFamily(
  videoEl: HTMLVideoElement | null
): PlaybackClientFamily {
  if (videoEl) {
    try {
      const nativeHls = videoEl.canPlayType('application/vnd.apple.mpegurl') !== '';
      if (nativeHls) {
        if (shouldForceNativeMobileHls(videoEl) || isIOSUserAgent()) {
          return 'ios_safari_native';
        }
        return 'safari_native';
      }
    } catch {
      // Fall back to UA-based families below.
    }
  }

  if (isAndroidTVUserAgent()) {
    return 'android_tv_browser';
  }

  return isFirefoxUserAgent() ? 'firefox_hlsjs' : 'chromium_hlsjs';
}

export function fallbackPlaybackCapabilitiesForClientFamily(
  family: PlaybackClientFamily,
  scope: RuntimePlaybackProbeScope
): PlaybackClientFamilyCapabilities {
  const capabilitySet = PLAYBACK_CLIENT_FAMILY_CAPABILITIES[family][scope];
  return {
    deviceType: capabilitySet.deviceType,
    container: [...capabilitySet.container],
    videoCodecs: [...capabilitySet.videoCodecs],
    audioCodecs: [...capabilitySet.audioCodecs],
    hlsEngines: [...capabilitySet.hlsEngines],
    preferredHlsEngine: capabilitySet.preferredHlsEngine,
  };
}
