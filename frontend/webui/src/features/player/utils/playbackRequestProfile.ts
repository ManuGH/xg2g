import { getNativePlaybackCapabilities, resolveHostEnvironment } from '../../../lib/hostBridge';

export type PlaybackRequestProfile = 'repair';
export type PlaybackProfileSelection = 'auto' | PlaybackRequestProfile;

export function normalizePlaybackProfileSelection(value: unknown): PlaybackProfileSelection {
  const normalized = typeof value === 'string' ? value.trim().toLowerCase() : '';
  // The client may request the conservative repair path. Everything else,
  // including values persisted by older builds, returns to evidence-driven
  // automatic planning. Internal encoder profile ids are planner outputs.
  return normalized === 'repair' ? 'repair' : 'auto';
}

export function resolvePlaybackProfileForPreflight(
  selection: unknown,
): PlaybackRequestProfile | undefined {
  const normalized = normalizePlaybackProfileSelection(selection);
  return normalized === 'repair' ? normalized : undefined;
}

export type PlaybackClientDeviceContext = {
  brand?: string;
  product?: string;
  device?: string;
  manufacturer?: string;
  model?: string;
  osName?: string;
  osVersion?: string;
  sdkInt?: number;
};

export type PlaybackClientNetworkContext = {
  kind: string;
  effectiveType?: string;
  downlinkMbps?: number;
  rttMs?: number;
  saveData?: boolean;
  metered?: boolean;
  internetValidated?: boolean;
};

export type PlaybackClientContext = {
  platform: string;
  isTv: boolean;
  isNativePlayback: boolean;
  device?: PlaybackClientDeviceContext;
  network?: PlaybackClientNetworkContext;
};

type BrowserNetworkInformation = {
  effectiveType?: string;
  downlink?: number;
  rtt?: number;
  saveData?: boolean;
};

type NavigatorWithNetwork = Navigator & {
  connection?: BrowserNetworkInformation;
  userAgentData?: {
    platform?: string;
  };
};

function sanitizeString(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim().length > 0 ? value.trim() : undefined;
}

function sanitizeNumber(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined;
}

function parseBrowserOs(userAgent: string, platform: string | undefined): PlaybackClientDeviceContext {
  const ua = userAgent.toLowerCase();
  const platformToken = (platform || '').toLowerCase();

  const androidMatch = ua.match(/android\s+([\d.]+)/i);
  if (androidMatch) {
    return { osName: 'android', osVersion: androidMatch[1] };
  }

  const iosMatch = ua.match(/os\s+([\d_]+)\s+like\s+mac\s+os\s+x/i);
  if (iosMatch) {
    return { osName: 'ios', osVersion: iosMatch[1]?.replace(/_/g, '.') };
  }

  const macMatch = ua.match(/mac\s+os\s+x\s+([\d_]+)/i);
  if (macMatch || platformToken.includes('mac')) {
    return { osName: 'macos', osVersion: macMatch?.[1]?.replace(/_/g, '.') };
  }

  const windowsMatch = ua.match(/windows\s+nt\s+([\d.]+)/i);
  if (windowsMatch || platformToken.includes('win')) {
    return { osName: 'windows', osVersion: windowsMatch?.[1] };
  }

  if (ua.includes('cros')) {
    return { osName: 'chromeos' };
  }

  if (platformToken.includes('linux') || ua.includes('linux')) {
    return { osName: 'linux' };
  }

  return {};
}

function estimateBandwidthFromResources(): number | undefined {
  if (typeof performance === 'undefined' || typeof performance.getEntriesByType !== 'function') {
    return undefined;
  }
  
  try {
    const resources = performance.getEntriesByType('resource') as PerformanceResourceTiming[];
    let totalBytes = 0;
    let totalTimeMs = 0;
    
    for (const r of resources) {
      if (r.transferSize && r.transferSize > 0 && r.responseEnd > r.responseStart) {
        const duration = r.responseEnd - r.responseStart;
        if (duration > 10) {
          totalBytes += r.transferSize;
          totalTimeMs += duration;
        }
      }
    }

    if (totalBytes > 50000 && totalTimeMs > 50) {
      const bytesPerMs = totalBytes / totalTimeMs;
      const bitsPerSec = bytesPerMs * 1000 * 8;
      return bitsPerSec / 1000000;
    }
  } catch {
    // Ignore errors
  }
  
  return undefined;
}

function gatherBrowserNetworkContext(): PlaybackClientNetworkContext | undefined {
  if (typeof navigator === 'undefined') {
    return undefined;
  }

  const nav = navigator as NavigatorWithNetwork;
  const connection = nav.connection;
  const isOnline = navigator.onLine !== false;
  
  const downlinkMbps = connection && typeof connection.downlink === 'number'
    ? connection.downlink
    : estimateBandwidthFromResources();

  if (!connection) {
    return isOnline ? { kind: 'browser', downlinkMbps: sanitizeNumber(downlinkMbps) } : { kind: 'offline' };
  }

  return {
    kind: isOnline ? 'browser' : 'offline',
    effectiveType: sanitizeString(connection.effectiveType),
    downlinkMbps: sanitizeNumber(downlinkMbps),
    rttMs: sanitizeNumber(connection.rtt),
    saveData: connection.saveData === true,
  };
}

function gatherNativeClientContext(): PlaybackClientContext | null {
  const environment = resolveHostEnvironment();
  const raw = getNativePlaybackCapabilities();
  if (!raw || typeof raw !== 'object') {
    return null;
  }

  const record = raw as Record<string, unknown>;
  const deviceRecord = record.deviceContext && typeof record.deviceContext === 'object'
    ? record.deviceContext as Record<string, unknown>
    : null;
  const networkRecord = record.networkContext && typeof record.networkContext === 'object'
    ? record.networkContext as Record<string, unknown>
    : null;

  return {
    platform: sanitizeString(deviceRecord?.platform) || environment.platform,
    isTv: environment.isTv,
    isNativePlayback: environment.supportsNativePlayback,
    device: deviceRecord ? {
      brand: sanitizeString(deviceRecord.brand),
      product: sanitizeString(deviceRecord.product),
      device: sanitizeString(deviceRecord.device),
      manufacturer: sanitizeString(deviceRecord.manufacturer),
      model: sanitizeString(deviceRecord.model),
      osName: sanitizeString(deviceRecord.osName),
      osVersion: sanitizeString(deviceRecord.osVersion),
      sdkInt: sanitizeNumber(deviceRecord.sdkInt),
    } : undefined,
    network: networkRecord ? {
      kind: sanitizeString(networkRecord.kind) || 'unknown',
      downlinkMbps: typeof networkRecord.downlinkKbps === 'number' ? networkRecord.downlinkKbps / 1000 : undefined,
      metered: networkRecord.metered === true,
      internetValidated: typeof networkRecord.internetValidated === 'boolean' ? networkRecord.internetValidated : undefined,
    } : undefined,
  };
}

export function gatherPlaybackClientContext(): PlaybackClientContext {
  const nativeContext = gatherNativeClientContext();
  if (nativeContext) {
    return nativeContext;
  }

  const environment = resolveHostEnvironment();
  const nav = typeof navigator !== 'undefined' ? navigator as NavigatorWithNetwork : null;
  const platform = sanitizeString(nav?.userAgentData?.platform)
    || sanitizeString(nav?.platform)
    || environment.platform;

  return {
    platform,
    isTv: environment.isTv,
    isNativePlayback: environment.supportsNativePlayback,
    device: nav ? parseBrowserOs(nav.userAgent || '', platform) : undefined,
    network: gatherBrowserNetworkContext(),
  };
}

export function buildPlaybackProfileHeaders(profile?: PlaybackRequestProfile): Record<string, string> {
  if (profile !== 'repair') {
    return {};
  }
  return {
    'X-XG2G-Profile': profile,
  };
}
