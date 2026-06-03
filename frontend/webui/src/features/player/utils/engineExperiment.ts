import { isTruthyOverrideValue } from './iosNativeAv1Experiment';

// Runtime opt-in for routing Safari through hls.js + ManagedMediaSource instead
// of native HLS. Stage 0 ships this flag but DOES NOT consume it yet — the
// engine selection is untouched, so behaviour is identical with the flag on or
// off until the Stage 1 wiring lands. Default OFF, no hostname auto-enable
// (unlike iosNativeAv1Experiment): this must be an explicit, per-device A/B.
//
// Enable:  ?xg2g_hlsjs_safari=1  or  localStorage XG2G_HLSJS_SAFARI=1
// Kill:    localStorage XG2G_HLSJS_SAFARI_KILL=1  (checked first, forces OFF)

const QUERY_PARAM = 'xg2g_hlsjs_safari';
const STORAGE_KEY = 'XG2G_HLSJS_SAFARI';
const KILL_STORAGE_KEY = 'XG2G_HLSJS_SAFARI_KILL';

export function isHlsJsSafariKillSwitchOn(): boolean {
  try {
    if (typeof window !== 'undefined' && window.localStorage) {
      return isTruthyOverrideValue(window.localStorage.getItem(KILL_STORAGE_KEY));
    }
  } catch {
    // ignore
  }
  return false;
}

export function isHlsJsSafariExperimentEnabled(): boolean {
  if (isHlsJsSafariKillSwitchOn()) {
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
