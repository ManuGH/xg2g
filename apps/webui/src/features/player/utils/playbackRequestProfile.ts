import { getNativePlaybackCapabilities, resolveHostEnvironment } from '../../../lib/hostBridge';
import type { CapabilitySnapshot } from './playbackCapabilities';

export type PlaybackRequestProfile = 'direct' | 'quality' | 'compatible' | 'repair' | 'bandwidth';
export type PlaybackProfileSelection = 'auto' | 'direct' | 'quality' | 'compatible' | 'repair';

export function normalizePlaybackProfileSelection(value: unknown): PlaybackProfileSelection {
  const normalized = typeof value === 'string' ? value.trim().toLowerCase() : '';
  switch (normalized) {
    case 'direct':
    case 'copy':
    case 'passthrough':
      return 'direct';
    case 'quality':
    case 'compatible':
    case 'repair':
      return normalized;
    default:
      // Internal encoder-profile ids from older UI builds are intentionally
      // discarded. The planner accepts public playback intents only.
      return 'auto';
  }
}

export function resolvePlaybackProfileForPreflight(
  selection: unknown,
  automaticProfile?: PlaybackRequestProfile,
): PlaybackRequestProfile | undefined {
  const normalized = normalizePlaybackProfileSelection(selection);
  return normalized === 'auto' ? automaticProfile : normalized;
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

const MODERN_VIDEO_CODECS = ['av1', 'hevc', 'h264'] as const;

function supportsHighQualityPlayback(capabilities: CapabilitySnapshot): boolean {
  const modernCodecs = MODERN_VIDEO_CODECS.filter((codec) => capabilities.videoCodecs.includes(codec));
  if (modernCodecs.length === 0) {
    return false;
  }

  // Media Capabilities verdicts (decodingInfo) beat static codec lists: when
  // every modern codec the client offers carries an explicit smooth=false
  // signal, the device decodes but cannot keep up — don't request quality.
  const signals = capabilities.videoCodecSignals;
  if (signals && signals.length > 0) {
    const verdicts = modernCodecs
      .map((codec) => signals.find((signal) => signal.codec === codec))
      .filter((signal): signal is NonNullable<typeof signal> => signal != null && signal.smooth !== undefined);
    if (verdicts.length > 0 && verdicts.every((signal) => signal.smooth === false)) {
      return false;
    }
  }

  if (!capabilities.maxVideo) {
    return true;
  }

  const width = capabilities.maxVideo.width ?? 0;
  const height = capabilities.maxVideo.height ?? 0;
  return width >= 1920 && height >= 1080;
}

// Profile selection is a step function over measured downlink, and the encoder
// output carries a single rendition — there is no ABR ladder to absorb a
// bandwidth change, so every profile change costs a fresh encoder session. On
// mobile the measurement walks across the step edges continuously (a train
// swings between ~5 and ~40 Mbit within a minute), which turns ordinary
// cellular jitter into repeated transcode restarts. Two mechanisms keep that
// quiet without making the decision less truthful:
//
//   * hysteresis — moving to a different rung needs a clearly better (or
//     clearly worse) measurement than staying on the current one, so a sample
//     sitting on an edge never flips the decision back and forth;
//   * a downgrade hold — once playback has actually died of link starvation,
//     stay on 'bandwidth' for a cooldown no matter what the next probe claims.
//     The probe is a 512 KB burst; it cannot see the dead spot that just killed
//     the stream.
//
// The declared signals (save-data, 2g/3g, metered) get no hysteresis: they are
// statements of intent from the client, not noisy measurements.

/** Below this the link cannot carry any rung but the safe one. */
const MINIMUM_VIABLE_MBPS = 6;
/** Downlink under this selects 'bandwidth'... */
const BANDWIDTH_ENTER_MBPS = 15;
/** ...and it takes this much to climb back out of it. */
const BANDWIDTH_EXIT_MBPS = 20;
/** Downlink at or above this selects 'quality'... */
const QUALITY_ENTER_MBPS = 35;
/** ...and it takes a drop below this to give it up. */
const QUALITY_EXIT_MBPS = 28;

/** How long a starvation failure pins the automatic choice to 'bandwidth'. */
export const NETWORK_DOWNGRADE_HOLD_MS = 10 * 60 * 1000;

/**
 * Carries the automatic resolver's memory across attempts. Held by the
 * orchestrator for the lifetime of a player instance; the resolver stays a
 * function of (context, capabilities, memory) with no hidden module state.
 */
export interface AutomaticProfileMemory {
  /** The rung the previous automatic resolution settled on. */
  lastProfile: PlaybackRequestProfile | undefined;
  /** Epoch ms until which 'bandwidth' is forced; 0 when no hold is active. */
  bandwidthHoldUntilMs: number;
}

export function createAutomaticProfileMemory(): AutomaticProfileMemory {
  return { lastProfile: undefined, bandwidthHoldUntilMs: 0 };
}

/**
 * Record that playback died because the link could not carry the stream. Pins
 * the automatic resolution to 'bandwidth' for the cooldown.
 */
export function noteNetworkStarvation(
  memory: AutomaticProfileMemory,
  nowMs: number = Date.now(),
): void {
  memory.bandwidthHoldUntilMs = nowMs + NETWORK_DOWNGRADE_HOLD_MS;
  memory.lastProfile = 'bandwidth';
}

/** Drop any active hold — used when a new viewing intent starts. */
export function clearNetworkStarvationHold(memory: AutomaticProfileMemory): void {
  memory.bandwidthHoldUntilMs = 0;
}

// Network kinds that may be promoted to 'quality'. 'lan' and 'measured' are
// verdicts of the server-side probe and outrank anything the browser guesses;
// without them a probed client could only ever be demoted to 'bandwidth',
// never promoted.
const QUALITY_ELIGIBLE_NETWORK_KINDS: ReadonlySet<string> = new Set([
  'lan',
  'measured',
  'ethernet',
  'wifi',
  'browser',
  'other',
]);

export function resolvePlaybackRequestProfile(
  context: PlaybackClientContext,
  capabilities: CapabilitySnapshot,
  _scope: 'live' | 'recording',
  memory?: AutomaticProfileMemory,
  nowMs: number = Date.now(),
): PlaybackRequestProfile | undefined {
  const network = context.network;
  if (network?.kind === 'offline') {
    return undefined;
  }

  const settle = (profile: PlaybackRequestProfile | undefined) => {
    if (memory) {
      memory.lastProfile = profile;
    }
    return profile;
  };

  if (memory && memory.bandwidthHoldUntilMs > nowMs) {
    return settle('bandwidth');
  }

  const downlinkMbps = typeof network?.downlinkMbps === 'number' ? network.downlinkMbps : undefined;

  if (
    network?.saveData
    || network?.effectiveType === 'slow-2g'
    || network?.effectiveType === '2g'
    || (downlinkMbps !== undefined && downlinkMbps < MINIMUM_VIABLE_MBPS)
  ) {
    return settle('bandwidth');
  }

  if (
    network?.kind === 'cellular'
    || network?.metered
    || network?.effectiveType === '3g'
  ) {
    return settle('bandwidth');
  }

  const previous = memory?.lastProfile;

  if (downlinkMbps !== undefined) {
    const bandwidthCeiling = previous === 'bandwidth' ? BANDWIDTH_EXIT_MBPS : BANDWIDTH_ENTER_MBPS;
    if (downlinkMbps < bandwidthCeiling) {
      return settle('bandwidth');
    }
  }

  const qualityFloor = previous === 'quality' ? QUALITY_EXIT_MBPS : QUALITY_ENTER_MBPS;
  if (
    supportsHighQualityPlayback(capabilities)
    && !network?.saveData
    && !network?.metered
    && (network == null || QUALITY_ELIGIBLE_NETWORK_KINDS.has(network.kind))
    && (downlinkMbps === undefined || downlinkMbps >= qualityFloor)
  ) {
    return settle('quality');
  }

  return settle(undefined);
}

export function buildPlaybackProfileHeaders(profile?: PlaybackRequestProfile): Record<string, string> {
  if (!profile) {
    return {};
  }
  return {
    'X-XG2G-Profile': profile,
  };
}
