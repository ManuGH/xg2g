import Hls from '../lib/hlsRuntime';
import {
  detectPreferredCodecs,
  detectVideoCodecSignals,
  type PreferredCodec,
  type VideoCodecSignal
} from './codecDetection';
import { shouldPreferNativeWebKitHls } from './playerHelpers';

export type RuntimePlaybackProbeScope = 'live' | 'recording';
export type RuntimeHlsEngine = 'native' | 'hlsjs';

export type RuntimePlaybackProbe = {
  version: 2;
  usedRuntimeProbe: boolean;
  nativeHls: boolean;
  hlsJs: boolean;
  preferredHlsEngine: RuntimeHlsEngine | null;
  hlsEngines: RuntimeHlsEngine[];
  containers: string[];
  videoCodecs: PreferredCodec[];
  videoCodecSignals: VideoCodecSignal[];
  audioCodecs: string[];
  supportsRange: boolean;
};

function dedupeStrings<T extends string>(values: T[]): T[] {
  return Array.from(new Set(values));
}

function probeNativeHls(videoEl: HTMLVideoElement | null): boolean {
  if (!videoEl) return false;
  try {
    return videoEl.canPlayType('application/vnd.apple.mpegurl') !== '';
  } catch {
    return false;
  }
}

function probeAc3(videoEl: HTMLVideoElement | null): boolean {
  if (!videoEl) return false;
  try {
    return videoEl.canPlayType('audio/mp4; codecs="ac-3"') !== '';
  } catch {
    return false;
  }
}

export async function probeRuntimePlaybackCapabilities(
  videoEl: HTMLVideoElement | null,
  scope: RuntimePlaybackProbeScope = 'live'
): Promise<RuntimePlaybackProbe> {
  const [preferredCodecs, videoCodecSignals] = await Promise.all([
    detectPreferredCodecs(videoEl),
    detectVideoCodecSignals(videoEl),
  ]);
  const hlsJsSupported = Hls.isSupported();
  const nativeHls = probeNativeHls(videoEl);
  const supportsAc3 = scope === 'live' && probeAc3(videoEl);
  const preferNativeHls = shouldPreferNativeWebKitHls(videoEl, hlsJsSupported);
  const preferredHlsEngine: RuntimeHlsEngine | null =
    hlsJsSupported && !preferNativeHls ? 'hlsjs' :
      nativeHls ? 'native' :
        hlsJsSupported ? 'hlsjs' : null;

  const hlsEngines: RuntimeHlsEngine[] = [];
  if (preferredHlsEngine) {
    hlsEngines.push(preferredHlsEngine);
  }
  if (nativeHls && preferredHlsEngine !== 'native') {
    hlsEngines.push('native');
  }
  if (hlsJsSupported && preferredHlsEngine !== 'hlsjs' && !preferNativeHls) {
    hlsEngines.push('hlsjs');
  }

  const containers = ['mp4', 'ts'];
  if (hlsJsSupported && !preferNativeHls) {
    containers.push('fmp4');
  }

  const audioCodecs = ['aac', 'mp3'];
  if (supportsAc3) {
    audioCodecs.push('ac3');
  }

  return {
    version: 2,
    usedRuntimeProbe: true,
    nativeHls,
    hlsJs: hlsJsSupported,
    preferredHlsEngine,
    hlsEngines: dedupeStrings(hlsEngines),
    containers: dedupeStrings(containers),
    videoCodecs: dedupeStrings(preferredCodecs),
    videoCodecSignals,
    audioCodecs: dedupeStrings(audioCodecs),
    supportsRange: true,
  };
}
