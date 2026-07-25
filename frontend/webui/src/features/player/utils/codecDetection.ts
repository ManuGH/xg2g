/**
 * Codec Detection Utilities
 *
 * Extracted from V3Player.tsx for testability and reuse.
 * Probes browser codec support via MediaCapabilities, MediaSource, and HTMLVideoElement APIs.
 */

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
  if (codec === 'hevc' && typeof MediaSource !== 'undefined') {
    try {
      const mseSupported = contentTypes.some((contentType) => MediaSource.isTypeSupported(contentType));
      if (!mseSupported) {
        supported = false;
      }
    } catch {
      supported = false;
    }
  }
  if (!supported && codec !== 'hevc') {
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

  // The client reports FACTS, it does not decide policy. Whether AV1 is a good
  // idea for this device is decided server-side (autocodec/client_av1_policy.go),
  // which has the device tables and host-encoder state to judge it; the raw
  // per-codec verdicts travel alongside in videoCodecSignals.
  //
  // Do NOT gate this on powerEfficient. Safari reports AV1 as `supported` with
  // neither `smooth` nor `powerEfficient` even on M3/M4 Macs that decode AV1 in
  // hardware, so a powerEfficient gate silently deletes av1 from videoCodecs —
  // and once it is gone the server can never select it, no matter what profile
  // the client requests. That gate is what broke AV1 on Apple hardware.
  //
  // The Settings toggles stay meaningful as an explicit user opt-out: switching
  // both off means "never send me AV1".
  let av1HwEnabled = true;
  let av1SwEnabled = false;
  try {
    const hw = window.localStorage.getItem('xg2g.settings.av1HardwareEnabled');
    if (hw !== null) av1HwEnabled = hw === 'true';
    const sw = window.localStorage.getItem('xg2g.settings.av1SoftwareEnabled');
    if (sw !== null) av1SwEnabled = sw === 'true';
  } catch {}

  const av1OptedOut = !av1HwEnabled && !av1SwEnabled;
  if (!av1OptedOut && (av1Signal?.supported || av1Signal?.smooth || av1Signal?.powerEfficient)) {
    out.push('av1');
  }

  if (signalFor('hevc')?.powerEfficient || signalFor('hevc')?.smooth) out.push('hevc');

  // Always include H.264 as a safe fallback.
  // If the platform surprisingly doesn't report support, keep it anyway: server will still fall back if needed.
  out.push('h264');

  return Array.from(new Set(out));
}

export type MaxVideoCapability = { width: number; height: number; fps: number };

let maxVideoPromise: Promise<MaxVideoCapability | undefined> | undefined;

/** Reset cached maxVideo (for testing). */
export function resetCachedMaxVideo(): void {
  maxVideoPromise = undefined;
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
export function detectMaxVideo(): Promise<MaxVideoCapability | undefined> {
  if (maxVideoPromise) return maxVideoPromise;
  maxVideoPromise = (async () => {
    if (!getMediaCapabilitiesProbe()?.decodingInfo) {
      return undefined;
    }
    for (const rung of MAX_VIDEO_RUNGS) {
      for (const fps of [60, 50]) {
        if (await decodesAt(rung.types, rung.width, rung.height, fps)) {
          return { width: rung.width, height: rung.height, fps };
        }
      }
    }
    return undefined;
  })();
  return maxVideoPromise;
}
