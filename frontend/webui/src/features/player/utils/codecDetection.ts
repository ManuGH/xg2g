/**
 * Codec Detection Utilities
 *
 * Extracted from V3Player.tsx for testability and reuse.
 * Probes browser codec support via MediaCapabilities, MediaSource, and HTMLVideoElement APIs.
 */

import { detectPlaybackClientFamily } from './playbackClientFamily';

export type PreferredCodec = 'av1' | 'hevc' | 'h264';

export type VideoCodecSignal = {
  codec: PreferredCodec;
  supported: boolean;
  smooth?: boolean;
  powerEfficient?: boolean;
};

let cachedVideoCodecSignals: VideoCodecSignal[] | null = null;

/** Reset cached codecs (for testing). */
export function resetCachedCodecs(): void {
  cachedVideoCodecSignals = null;
}

function isIOSNativeAV1ProbeEnabled(videoEl?: HTMLVideoElement | null): boolean {
  if (!videoEl) return false;
  try {
    return detectPlaybackClientFamily(videoEl) === 'ios_safari_native';
  } catch {
    return false;
  }
}

function isDesktopSafariNativeAV1ProbeEnabled(videoEl?: HTMLVideoElement | null): boolean {
  if (!videoEl) return false;
  try {
    return detectPlaybackClientFamily(videoEl) === 'safari_native';
  } catch {
    return false;
  }
}

type DecodingInfoResult = {
  supported: boolean;
  smooth: boolean;
  powerEfficient: boolean;
};

type MediaCapabilitiesProbeConfig = {
  type: 'media-source' | 'file';
  video: {
    contentType: string;
    width: number;
    height: number;
    bitrate: number;
    framerate: number;
  };
};

type MediaCapabilitiesProbe = {
  decodingInfo?: (config: MediaCapabilitiesProbeConfig) => Promise<Partial<DecodingInfoResult> | null>;
};

function getMediaCapabilitiesProbe(): MediaCapabilitiesProbe | undefined {
  if (typeof navigator === 'undefined') return undefined;
  return (navigator as Navigator & { mediaCapabilities?: MediaCapabilitiesProbe }).mediaCapabilities;
}

function mergeDecodingInfoResult(current: DecodingInfoResult, next?: Partial<DecodingInfoResult> | null): DecodingInfoResult {
  if (!next) return current;
  return {
    supported: current.supported || next.supported === true,
    smooth: current.smooth || next.smooth === true,
    powerEfficient: current.powerEfficient || next.powerEfficient === true,
  };
}

async function decodeInfoForContentType(contentType: string): Promise<DecodingInfoResult> {
  let result: DecodingInfoResult = {
    supported: false,
    smooth: false,
    powerEfficient: false,
  };

  try {
    const mc = getMediaCapabilitiesProbe();
    if (mc?.decodingInfo) {
      const baseVideo = {
        contentType,
        width: 1920,
        height: 1080,
        bitrate: 5_000_000,
        framerate: 30
      };

      try {
        const info = await mc.decodingInfo({ type: 'media-source', video: baseVideo });
        result = mergeDecodingInfoResult(result, info);
      } catch {
        // Some platforms only accept type='file'; try fallback below.
      }

      try {
        const info = await mc.decodingInfo({ type: 'file', video: baseVideo });
        result = mergeDecodingInfoResult(result, info);
      } catch {
        // ignore
      }
    }
  } catch {
    // ignore
  }

  return result;
}

async function detectCodecSignal(
  codec: PreferredCodec,
  contentTypes: string[],
  videoEl?: HTMLVideoElement | null
): Promise<VideoCodecSignal> {
  let aggregated: DecodingInfoResult = {
    supported: false,
    smooth: false,
    powerEfficient: false,
  };

  for (const contentType of contentTypes) {
    aggregated = mergeDecodingInfoResult(aggregated, await decodeInfoForContentType(contentType));
  }

  let supported = aggregated.supported;
  if (!supported) {
    try {
      supported = contentTypes.some((contentType) => typeof MediaSource !== 'undefined' && MediaSource.isTypeSupported(contentType));
    } catch {
      // ignore
    }
  }

  if (!supported) {
    try {
      const video = videoEl || (typeof document !== 'undefined' ? document.createElement('video') : null);
      if (video) {
        supported = contentTypes.some((contentType) => video.canPlayType(contentType) !== '');
      }
    } catch {
      // ignore
    }
  }

  const signal: VideoCodecSignal = {
    codec,
    supported,
  };
  if (aggregated.smooth) {
    signal.smooth = true;
  }
  if (aggregated.powerEfficient) {
    signal.powerEfficient = true;
  }
  return signal;
}

export async function detectVideoCodecSignals(videoEl?: HTMLVideoElement | null): Promise<VideoCodecSignal[]> {
  if (cachedVideoCodecSignals) return cachedVideoCodecSignals;

  const av1Types = ['video/mp4; codecs="av01.0.05M.08"'];
  const hevcTypes = [
    'video/mp4; codecs="hvc1.1.6.L120.90"',
    'video/mp4; codecs="hev1.1.6.L120.90"'
  ];
  const h264Types = ['video/mp4; codecs="avc1.42E01E"'];

  const signals = await Promise.all([
    detectCodecSignal('av1', av1Types, videoEl),
    detectCodecSignal('hevc', hevcTypes, videoEl),
    detectCodecSignal('h264', h264Types, videoEl),
  ]);

  cachedVideoCodecSignals = signals;
  return signals;
}

export async function detectPreferredCodecs(videoEl?: HTMLVideoElement | null): Promise<PreferredCodec[]> {
  const signals = await detectVideoCodecSignals(videoEl);
  const out: PreferredCodec[] = [];
  const signalFor = (codec: PreferredCodec) => signals.find((signal) => signal.codec === codec);
  const av1Signal = signalFor('av1');
  const allowIOSNativeAV1 =
    isIOSNativeAV1ProbeEnabled(videoEl) &&
    (av1Signal?.supported || av1Signal?.smooth);
  const allowDesktopSafariNativeAV1 =
    isDesktopSafariNativeAV1ProbeEnabled(videoEl) &&
    (av1Signal?.supported || av1Signal?.smooth);

  if (av1Signal?.powerEfficient || allowIOSNativeAV1 || allowDesktopSafariNativeAV1) out.push('av1');
  if (signalFor('hevc')?.powerEfficient || signalFor('hevc')?.smooth) out.push('hevc');

  // Always include H.264 as a safe fallback.
  // If the platform surprisingly doesn't report support, keep it anyway: server will still fall back if needed.
  out.push('h264');

  return Array.from(new Set(out));
}
