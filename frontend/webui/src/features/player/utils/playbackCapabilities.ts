import type { PlaybackCapabilities as PlaybackCapabilitiesContract } from "../../../client-ts";
import {
  getNativePlaybackCapabilities,
  type HostEnvironment,
  resolveHostEnvironment,
} from "../../../lib/hostBridge";
import { detectBrowserIdentity } from "./browserIdentity";
import { detectPlaybackClientFamily } from "./playbackClientFamily";
import {
  probeRuntimePlaybackCapabilities,
  type RuntimePlaybackProbeScope,
} from "./playbackProbe";

export type CapabilitySnapshot = Pick<
  PlaybackCapabilitiesContract,
  | "capabilitiesVersion"
  | "container"
  | "videoCodecs"
  | "audioCodecs"
  | "deviceType"
  | "supportsHls"
  | "supportsRange"
  | "allowTranscode"
  | "runtimeProbeUsed"
  | "runtimeProbeVersion"
  | "clientFamilyFallback"
  | "videoCodecSignals"
> & {
  deviceContext?: {
    brand?: string;
    device?: string;
    platform?: string;
    product?: string;
    manufacturer?: string;
    model?: string;
    browserName?: string;
    browserVersion?: string;
    osName?: string;
    osVersion?: string;
    platformClass?: string;
    sdkInt?: number;
  };
  networkContext?: {
    kind?: string;
    downlinkKbps?: number;
    metered?: boolean;
    internetValidated?: boolean;
  };
  hlsEngines?: string[];
  preferredHlsEngine?: string;
  maxVideo?: PlaybackCapabilitiesContract["maxVideo"];
};

function sanitizeStringArray(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) {
    return undefined;
  }

  const strings = value.filter(
    (entry): entry is string =>
      typeof entry === "string" && entry.trim().length > 0,
  );
  return strings.length > 0 ? Array.from(new Set(strings)) : undefined;
}

function sanitizeString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim().length > 0
    ? value.trim()
    : undefined;
}

function sanitizeNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function inferNativePlatformClass(
  environment: HostEnvironment,
  deviceContext?: CapabilitySnapshot["deviceContext"],
): string | undefined {
  if (deviceContext?.platformClass) {
    return deviceContext.platformClass;
  }
  if (deviceContext?.osName?.toLowerCase() === "tvos") {
    return "tvos_native_host";
  }
  if (environment.platform === "android-tv") {
    return "android_tv_native_host";
  }
  if (environment.platform === "android") {
    return "android_native_host";
  }
  return undefined;
}

function sanitizeNativePlaybackCapabilities(
  value: unknown,
  environment: HostEnvironment,
): CapabilitySnapshot | null {
  if (!value || typeof value !== "object") {
    return null;
  }

  const record = value as Record<string, unknown>;
  const container = sanitizeStringArray(record.container);
  const videoCodecs = sanitizeStringArray(record.videoCodecs);
  const audioCodecs = sanitizeStringArray(record.audioCodecs);
  if (!container || !videoCodecs || !audioCodecs) {
    return null;
  }

  const hlsEngines = sanitizeStringArray(record.hlsEngines);
  const maxVideo =
    record.maxVideo && typeof record.maxVideo === "object"
      ? {
          width: sanitizeNumber((record.maxVideo as Record<string, unknown>).width),
          height: sanitizeNumber((record.maxVideo as Record<string, unknown>).height),
          fps: sanitizeNumber((record.maxVideo as Record<string, unknown>).fps),
        }
      : undefined;
  const deviceContext =
    record.deviceContext && typeof record.deviceContext === "object"
      ? {
          brand: sanitizeString((record.deviceContext as Record<string, unknown>).brand),
          device: sanitizeString((record.deviceContext as Record<string, unknown>).device),
          platform: sanitizeString((record.deviceContext as Record<string, unknown>).platform),
          product: sanitizeString((record.deviceContext as Record<string, unknown>).product),
          manufacturer: sanitizeString((record.deviceContext as Record<string, unknown>).manufacturer),
          model: sanitizeString((record.deviceContext as Record<string, unknown>).model),
          browserName: sanitizeString((record.deviceContext as Record<string, unknown>).browserName),
          browserVersion: sanitizeString((record.deviceContext as Record<string, unknown>).browserVersion),
          osName: sanitizeString((record.deviceContext as Record<string, unknown>).osName),
          osVersion: sanitizeString((record.deviceContext as Record<string, unknown>).osVersion),
          platformClass: sanitizeString((record.deviceContext as Record<string, unknown>).platformClass),
          sdkInt: sanitizeNumber((record.deviceContext as Record<string, unknown>).sdkInt),
        }
      : undefined;
  if (deviceContext) {
    deviceContext.platformClass = inferNativePlatformClass(environment, deviceContext);
  }
  const networkContext =
    record.networkContext && typeof record.networkContext === "object"
      ? {
          kind: sanitizeString((record.networkContext as Record<string, unknown>).kind),
          downlinkKbps: sanitizeNumber((record.networkContext as Record<string, unknown>).downlinkKbps),
          metered:
            typeof (record.networkContext as Record<string, unknown>).metered === "boolean"
              ? ((record.networkContext as Record<string, unknown>).metered as boolean)
              : undefined,
          internetValidated:
            typeof (record.networkContext as Record<string, unknown>).internetValidated === "boolean"
              ? ((record.networkContext as Record<string, unknown>).internetValidated as boolean)
              : undefined,
        }
      : undefined;

  return {
    capabilitiesVersion:
      typeof record.capabilitiesVersion === "number"
        ? record.capabilitiesVersion
        : 3,
    container,
    videoCodecs,
    audioCodecs,
    videoCodecSignals: Array.isArray(record.videoCodecSignals)
      ? (record.videoCodecSignals as CapabilitySnapshot["videoCodecSignals"])
      : undefined,
    maxVideo,
    supportsHls: record.supportsHls === true,
    supportsRange: record.supportsRange === true,
    deviceType:
      typeof record.deviceType === "string" ? record.deviceType : undefined,
    hlsEngines,
    preferredHlsEngine:
      typeof record.preferredHlsEngine === "string"
        ? record.preferredHlsEngine
        : undefined,
    runtimeProbeUsed: record.runtimeProbeUsed === true,
    runtimeProbeVersion:
      typeof record.runtimeProbeVersion === "number"
        ? record.runtimeProbeVersion
        : undefined,
    clientFamilyFallback:
      typeof record.clientFamilyFallback === "string"
        ? record.clientFamilyFallback
        : undefined,
    allowTranscode:
      typeof record.allowTranscode === "boolean"
        ? record.allowTranscode
        : undefined,
    deviceContext,
    networkContext,
  };
}

function inferBrowserDeviceContext(): CapabilitySnapshot["deviceContext"] {
  const identity = detectBrowserIdentity();

  return {
    platform: identity.platform,
    browserName: identity.browserName,
    browserVersion: identity.browserVersion,
    osName: identity.osName,
    osVersion: identity.osVersion,
    platformClass: identity.platformClass,
  };
}

function inferBrowserNetworkContext(): CapabilitySnapshot["networkContext"] {
  const nav = navigator as Navigator & {
    connection?: {
      effectiveType?: string;
      downlink?: number;
      saveData?: boolean;
    };
  };

  return {
    kind: nav.connection?.effectiveType || "unknown",
    downlinkKbps:
      typeof nav.connection?.downlink === "number"
        ? Math.round(nav.connection.downlink * 1000)
        : undefined,
    metered:
      typeof nav.connection?.saveData === "boolean"
        ? nav.connection.saveData
        : undefined,
    internetValidated:
      typeof navigator.onLine === "boolean" ? navigator.onLine : undefined,
  };
}

export async function gatherPlaybackCapabilities(
  scope: RuntimePlaybackProbeScope = "live",
  videoEl: HTMLVideoElement | null = null,
): Promise<CapabilitySnapshot> {
  const environment = resolveHostEnvironment();
  if (environment.supportsNativePlayback) {
    const nativeCapabilities = sanitizeNativePlaybackCapabilities(
      getNativePlaybackCapabilities(),
      environment,
    );
    if (nativeCapabilities) {
      return nativeCapabilities;
    }
  }

  const probe = await probeRuntimePlaybackCapabilities(videoEl, scope);
  const clientFamilyFallback = detectPlaybackClientFamily(videoEl);

  return {
    capabilitiesVersion: 3,
    container: probe.containers,
    videoCodecs: probe.videoCodecs,
    videoCodecSignals: probe.videoCodecSignals,
    audioCodecs: probe.audioCodecs,
    hlsEngines: probe.hlsEngines.length > 0 ? probe.hlsEngines : undefined,
    preferredHlsEngine: probe.preferredHlsEngine ?? undefined,
    supportsHls: probe.hlsEngines.length > 0,
    supportsRange: probe.supportsRange,
    allowTranscode: true,
    deviceType: "web",
    deviceContext: inferBrowserDeviceContext(),
    networkContext: inferBrowserNetworkContext(),
    runtimeProbeUsed: probe.usedRuntimeProbe,
    runtimeProbeVersion: probe.version,
    clientFamilyFallback,
  };
}
