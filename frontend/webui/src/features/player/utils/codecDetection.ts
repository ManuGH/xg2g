/**
 * Codec Detection Utilities
 *
 * Extracted from V3Player.tsx for testability and reuse.
 * Probes browser codec support via MediaCapabilities, MediaSource, and HTMLVideoElement APIs.
 */

export type PreferredCodec = 'av1' | 'hevc' | 'h264';

let cachedPreferredCodecs: PreferredCodec[] | null = null;

/** Reset cached codecs (for testing). */
export function resetCachedCodecs(): void {
  cachedPreferredCodecs = null;
}

export async function detectPreferredCodecs(videoEl?: HTMLVideoElement | null): Promise<PreferredCodec[]> {
  if (cachedPreferredCodecs) return cachedPreferredCodecs;

  const supports = async (contentType: string): Promise<boolean> => {
    try {
      const mc = (navigator as any)?.mediaCapabilities;
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
          if (info?.supported) return true;
        } catch {
          // Some platforms only accept type='file'; try fallback below.
        }

        try {
          const info = await mc.decodingInfo({ type: 'file', video: baseVideo });
          if (info?.supported) return true;
        } catch {
          // ignore
        }
      }
    } catch {
      // ignore
    }

    try {
      if (typeof MediaSource !== 'undefined' && MediaSource.isTypeSupported(contentType)) return true;
    } catch {
      // ignore
    }

    try {
      const v = videoEl || (typeof document !== 'undefined' ? document.createElement('video') : null);
      if (v && v.canPlayType(contentType) !== '') return true;
    } catch {
      // ignore
    }

    return false;
  };

  const supportsAny = async (contentTypes: string[]): Promise<boolean> => {
    const results = await Promise.all(contentTypes.map((ct) => supports(ct)));
    return results.some(Boolean);
  };

  const av1Types = ['video/mp4; codecs="av01.0.05M.08"'];
  const hevcTypes = [
    'video/mp4; codecs="hvc1.1.6.L120.90"',
    'video/mp4; codecs="hev1.1.6.L120.90"'
  ];
  const h264Types = ['video/mp4; codecs="avc1.42E01E"'];

  const out: PreferredCodec[] = [];

  if (await supportsAny(av1Types)) out.push('av1');
  if (await supportsAny(hevcTypes)) out.push('hevc');

  // Always include H.264 as a safe fallback.
  // If the platform surprisingly doesn't report support, keep it anyway: server will still fall back if needed.
  if (out.length === 0) {
    // Still probe H.264 once, but don't block the fallback list on it.
    await supportsAny(h264Types);
  }
  out.push('h264');

  cachedPreferredCodecs = out;
  return out;
}
