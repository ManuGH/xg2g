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

export type MaxVideoCapability = { width: number; height: number; fps: number };

let cachedMaxVideo: MaxVideoCapability | null | undefined;

/** Reset cached maxVideo (for testing). */
export function resetCachedMaxVideo(): void {
  cachedMaxVideo = undefined;
}

// Probe whether the device can DECODE the given dimensions/framerate for ANY of
// the codec strings. Copy/direct-play only needs decode capability, so we accept
// `supported` and deliberately do NOT require `smooth` or `powerEfficient` — a
// device may not render 4K "smoothly" on its panel yet still decode it perfectly
// (and downscale in HW). Requiring `smooth` is exactly what wrongly capped an
// iPhone 17 Pro at 1080p and forced a needless 4K-HEVC->AV1 transcode.
async function decodesAt(
  contentTypes: string[],
  width: number,
  height: number,
  framerate: number
): Promise<boolean> {
  const mc = getMediaCapabilitiesProbe();
  if (!mc?.decodingInfo) return false;
  const bitrate = Math.max(2_000_000, Math.round(width * height * framerate * 0.1));
  for (const contentType of contentTypes) {
    const video = { contentType, width, height, bitrate, framerate };
    for (const type of ['media-source', 'file'] as const) {
      try {
        const info = await mc.decodingInfo({ type, video });
        if (info?.supported === true) return true;
      } catch {
        // try next type/codec
      }
    }
  }
  return false;
}

// HEVC Main 10 dominates UHD broadcast; probe Main10 first, then Main, AV1, H.264.
const MAX_VIDEO_RUNGS: Array<{ width: number; height: number; types: string[] }> = [
  {
    width: 3840,
    height: 2160,
    types: [
      'video/mp4; codecs="hvc1.2.4.L153.B0"', // HEVC Main10 L5.1
      'video/mp4; codecs="hvc1.1.6.L153.90"', // HEVC Main L5.1
      'video/mp4; codecs="av01.0.12M.10"',
      'video/mp4; codecs="av01.0.12M.08"',
    ],
  },
  {
    width: 1920,
    height: 1080,
    types: [
      'video/mp4; codecs="hvc1.2.4.L123.B0"',
      'video/mp4; codecs="hvc1.1.6.L123.90"',
      'video/mp4; codecs="av01.0.08M.10"',
      'video/mp4; codecs="avc1.640028"',
    ],
  },
  {
    width: 1280,
    height: 720,
    types: [
      'video/mp4; codecs="avc1.64001F"',
      'video/mp4; codecs="hvc1.1.6.L93.90"',
    ],
  },
];

/**
 * Determine the highest resolution the device can DECODE, by probing a ladder
 * (2160 -> 1080 -> 720) at 50/60 fps. Returns undefined when MediaCapabilities
 * can't tell us (the backend then falls back to its client fixture). This is the
 * truthful `maxVideo` the copy/direct-play decision needs — decode capability,
 * not render smoothness.
 */
export async function detectMaxVideo(): Promise<MaxVideoCapability | undefined> {
  if (cachedMaxVideo !== undefined) return cachedMaxVideo ?? undefined;
  if (!getMediaCapabilitiesProbe()?.decodingInfo) {
    cachedMaxVideo = null;
    return undefined;
  }
  for (const rung of MAX_VIDEO_RUNGS) {
    for (const fps of [60, 50]) {
      if (await decodesAt(rung.types, rung.width, rung.height, fps)) {
        cachedMaxVideo = { width: rung.width, height: rung.height, fps };
        return cachedMaxVideo;
      }
    }
  }
  cachedMaxVideo = null;
  return undefined;
}
