import type { RuntimePlaybackProbeScope } from './playbackProbe';
import { hasTouchInput, shouldForceNativeMobileHls } from './playerHelpers';

export type PlaybackClientFamily =
  | 'safari_native'
  | 'ios_safari_native'
  | 'firefox_hlsjs'
  | 'chromium_hlsjs';

type PlaybackClientFamilyCapabilities = {
  deviceType: 'safari' | 'ios_safari' | 'firefox' | 'chromium';
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
