import { isTruthyOverrideValue } from './iosNativeAv1Experiment';

// Runtime opt-in for routing HEVC live sources on Safari through NATIVE WebKit
// HLS (the <video> element loading the m3u8 directly) instead of hls.js/MSE.
//
// Why this exists: hls.js/MSE cannot sustain 4K@50 HEVC Main10 / HLG (e.g.
// RTL UHD) — it stalls in the decode phase and gives up ("hls.js stall recovery
// failed"). Safari decodes HEVC + HLG natively, and an Apple-silicon media
// engine (e.g. M4) hardware-decodes Main10/HLG up to 2160p60. The correct path
// is therefore native HLS with fMP4/hvc1 packaging while KEEPING video copy (no
// transcode, no quality loss). This deviates from the MSE/hls.js default, so it
// is gated and HEVC-only; H.264 stays on hls.js/MSE (seek benefits).
//
// Default OFF, no hostname auto-enable: this must be an explicit, per-device
// opt-in for the device test before any broader rollout.
//
// Enable:  ?xg2g_native_hevc=1  or  localStorage XG2G_NATIVE_HEVC_SAFARI=1
// Kill:    localStorage XG2G_NATIVE_HEVC_SAFARI_KILL=1  (checked first, forces OFF)

const QUERY_PARAM = 'xg2g_native_hevc';
const STORAGE_KEY = 'XG2G_NATIVE_HEVC_SAFARI';
const KILL_STORAGE_KEY = 'XG2G_NATIVE_HEVC_SAFARI_KILL';

export function isNativeHevcSafariKillSwitchOn(): boolean {
  try {
    if (typeof window !== 'undefined' && window.localStorage) {
      return isTruthyOverrideValue(window.localStorage.getItem(KILL_STORAGE_KEY));
    }
  } catch {
    // ignore
  }
  return false;
}

export function isNativeHevcSafariExperimentEnabled(): boolean {
  if (isNativeHevcSafariKillSwitchOn()) {
    return false;
  }
  try {
    const search = typeof window !== 'undefined' ? window.location.search : '';
    const override = new URLSearchParams(search).get(QUERY_PARAM);
    if (override !== null) {
      return isTruthyOverrideValue(override);
    }
  } catch {
    // ignore
  }
  try {
    if (typeof window !== 'undefined' && window.localStorage) {
      const stored = window.localStorage.getItem(STORAGE_KEY);
      if (stored !== null) {
        return isTruthyOverrideValue(stored);
      }
    }
  } catch {
    // ignore
  }
  return false;
}
