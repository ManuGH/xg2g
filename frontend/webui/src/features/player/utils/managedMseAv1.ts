// Capability probe for the hls.js + ManagedMediaSource migration gate.
//
// THE question this answers (and nothing else): does *this* browser expose our
// AV1 fMP4 stream through (Managed)MediaSource, so a JS player (hls.js) could
// drive it? This is independent of native AV1 decode — Safari can play AV1 via
// native HLS while still refusing av01 through MSE. isTypeSupported is the only
// authoritative source; web research about Chrome/Firefox/YouTube does not apply.
//
// Pure + synchronous + dependency-free. Safe to call anywhere (SSR-guarded).

export interface ManagedMseAv1Support {
  hasManagedMediaSource: boolean;
  hasMediaSource: boolean;
  // Our transcode is av1_vaapi -profile:v main (Profile 0), 10-bit (p010le).
  // Level digit is resolution/fps-dependent; test the 1080p candidates (4.0/4.1).
  av1_10bit_l40: boolean; // av01.0.08M.10
  av1_10bit_l41: boolean; // av01.0.09M.10
  av1_8bit_l40: boolean;  // av01.0.08M.08 (signal only; our stream is 10-bit)
}

type MseLike = { isTypeSupported?: (mime: string) => boolean } | undefined;

function resolveMseConstructor(): MseLike {
  if (typeof window === 'undefined') return undefined;
  const w = window as unknown as { ManagedMediaSource?: MseLike; MediaSource?: MseLike };
  // hls.js prefers ManagedMediaSource when present (iOS 17.1+/Safari 17.1+),
  // so probe the same constructor it would actually use.
  return w.ManagedMediaSource ?? w.MediaSource;
}

function mseIsTypeSupported(mime: string): boolean {
  try {
    const ctor = resolveMseConstructor();
    return typeof ctor?.isTypeSupported === 'function' ? ctor.isTypeSupported(mime) : false;
  } catch {
    return false;
  }
}

let cachedSupport: ManagedMseAv1Support | null = null;

export function getManagedMseAv1Support(): ManagedMseAv1Support {
  if (cachedSupport) return cachedSupport;

  const w = typeof window !== 'undefined'
    ? (window as unknown as { ManagedMediaSource?: unknown; MediaSource?: unknown })
    : undefined;
  cachedSupport = {
    hasManagedMediaSource: typeof w?.ManagedMediaSource !== 'undefined',
    hasMediaSource: typeof w?.MediaSource !== 'undefined',
    av1_10bit_l40: mseIsTypeSupported('video/mp4; codecs="av01.0.08M.10"'),
    av1_10bit_l41: mseIsTypeSupported('video/mp4; codecs="av01.0.09M.10"'),
    av1_8bit_l40: mseIsTypeSupported('video/mp4; codecs="av01.0.08M.08"'),
  };
  return cachedSupport;
}

// One-line summary for the Stats panel — paste-free device gate readout.
export function formatManagedMseAv1(s: ManagedMseAv1Support): string {
  if (!s.hasManagedMediaSource && !s.hasMediaSource) return 'no MSE';
  const src = s.hasManagedMediaSource ? 'MMS' : 'MSE';
  if (s.av1_10bit_l40 || s.av1_10bit_l41) return `${src}+AV1/10bit OK`;
  if (s.av1_8bit_l40) return `${src}, AV1/10bit NO (8bit ok)`;
  return `${src}, AV1 NO`;
}
