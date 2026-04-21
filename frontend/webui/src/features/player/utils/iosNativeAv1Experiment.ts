const IOS_NATIVE_AV1_RELAXED_QUERY_PARAM = 'xg2g_ios_native_av1';
const IOS_NATIVE_AV1_RELAXED_STORAGE_KEY = 'XG2G_IOS_NATIVE_AV1';
const IOS_NATIVE_AV1_RELAXED_HOSTNAMES = new Set([
  'xg2g.home.matrixcentral.de',
  'xg2g2.home.matrixcentral.de',
  'xg2g.home.matrixcental.de',
  'xg2g2.home.matrixcental.de',
]);

export function isTruthyOverrideValue(value: string | null | undefined): boolean {
  if (typeof value !== 'string') return false;
  switch (value.trim().toLowerCase()) {
    case '1':
    case 'true':
    case 'on':
    case 'yes':
    case 'enabled':
      return true;
    default:
      return false;
  }
}

export function hasIOSNativeAV1ExperimentOverride(): boolean {
  try {
    const search = typeof window !== 'undefined' ? window.location.search : '';
    const override = new URLSearchParams(search).get(IOS_NATIVE_AV1_RELAXED_QUERY_PARAM);
    if (override !== null) {
      return isTruthyOverrideValue(override);
    }
  } catch {
    // ignore
  }

  try {
    if (typeof window !== 'undefined' && window.localStorage) {
      const storedOverride = window.localStorage.getItem(IOS_NATIVE_AV1_RELAXED_STORAGE_KEY);
      if (storedOverride !== null) {
        return isTruthyOverrideValue(storedOverride);
      }
    }
  } catch {
    // ignore
  }

  try {
    const hostname = typeof window !== 'undefined' ? window.location.hostname.toLowerCase() : '';
    return IOS_NATIVE_AV1_RELAXED_HOSTNAMES.has(hostname);
  } catch {
    return false;
  }
}
