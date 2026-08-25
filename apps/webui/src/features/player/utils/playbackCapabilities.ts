import type { PlaybackCapabilities as PlaybackCapabilitiesContract } from "../../../client-ts";
import {
  getNativePlaybackCapabilities,
  resolveHostEnvironment,
} from "../../../lib/hostBridge";
import {
  detectPlaybackClientIdentity,
  inferBrowserDeviceContext,
} from "../../../services/clientIdentity";
import {
  probeRuntimePlaybackCapabilities,
  type RuntimePlaybackProbeScope,
} from "./playbackProbe";

/**
 * Reads a stored identity back, or re-detects it.
 *
 * A persisted snapshot predating the identity field has none; detecting again
 * is right, because the identity describes the runtime reading it, not the one
 * that wrote it.
 */
function readClientIdentity(
  record: Record<string, unknown>,
): CapabilitySnapshot["clientIdentity"] {
  const stored = record.clientIdentity;
  if (stored && typeof stored === "object") {
    const candidate = stored as Record<string, unknown>;
    if (typeof candidate.platform === "string" && typeof candidate.surface === "string") {
      return candidate as CapabilitySnapshot["clientIdentity"];
    }
  }
  return detectPlaybackClientIdentity();
}

export type CapabilitySnapshot = Pick<
  PlaybackCapabilitiesContract,
  | "capabilitiesVersion"
  | "container"
  | "videoCodecs"
  | "audioCodecs"
  | "clientIdentity"
  | "supportsHls"
  | "supportsRange"
  | "allowTranscode"
  | "runtimeProbeUsed"
  | "runtimeProbeVersion"
  | "videoCodecSignals"
> & {
  deviceContext?: {
    brand?: string;
    device?: string;
    platform?: string;
    product?: string;
    manufacturer?: string;
    model?: string;
    osName?: string;
    osVersion?: string;
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

function sanitizeNativePlaybackCapabilities(
  value: unknown,
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
          width:
            typeof (record.maxVideo as Record<string, unknown>).width ===
            "number"
              ? ((record.maxVideo as Record<string, unknown>).width as number)
              : undefined,
          height:
            typeof (record.maxVideo as Record<string, unknown>).height ===
            "number"
              ? ((record.maxVideo as Record<string, unknown>).height as number)
              : undefined,
          fps:
            typeof (record.maxVideo as Record<string, unknown>).fps === "number"
              ? ((record.maxVideo as Record<string, unknown>).fps as number)
              : undefined,
        }
      : undefined;
  const deviceContext =
    record.deviceContext && typeof record.deviceContext === "object"
      ? {
          brand:
            typeof (record.deviceContext as Record<string, unknown>).brand ===
            "string"
              ? ((record.deviceContext as Record<string, unknown>)
                  .brand as string)
              : undefined,
          device:
            typeof (record.deviceContext as Record<string, unknown>).device ===
            "string"
              ? ((record.deviceContext as Record<string, unknown>)
                  .device as string)
              : undefined,
          platform:
            typeof (record.deviceContext as Record<string, unknown>)
              .platform === "string"
              ? ((record.deviceContext as Record<string, unknown>)
                  .platform as string)
              : undefined,
          product:
            typeof (record.deviceContext as Record<string, unknown>)
              .product === "string"
              ? ((record.deviceContext as Record<string, unknown>)
                  .product as string)
              : undefined,
          manufacturer:
            typeof (record.deviceContext as Record<string, unknown>)
              .manufacturer === "string"
              ? ((record.deviceContext as Record<string, unknown>)
                  .manufacturer as string)
              : undefined,
          model:
            typeof (record.deviceContext as Record<string, unknown>).model ===
            "string"
              ? ((record.deviceContext as Record<string, unknown>)
                  .model as string)
              : undefined,
          osName:
            typeof (record.deviceContext as Record<string, unknown>).osName ===
            "string"
              ? ((record.deviceContext as Record<string, unknown>)
                  .osName as string)
              : undefined,
          osVersion:
            typeof (record.deviceContext as Record<string, unknown>)
              .osVersion === "string"
              ? ((record.deviceContext as Record<string, unknown>)
                  .osVersion as string)
              : undefined,
          sdkInt:
            typeof (record.deviceContext as Record<string, unknown>).sdkInt ===
            "number"
              ? ((record.deviceContext as Record<string, unknown>)
                  .sdkInt as number)
              : undefined,
        }
      : undefined;
  const networkContext =
    record.networkContext && typeof record.networkContext === "object"
      ? {
          kind:
            typeof (record.networkContext as Record<string, unknown>).kind ===
            "string"
              ? ((record.networkContext as Record<string, unknown>)
                  .kind as string)
              : undefined,
          downlinkKbps:
            typeof (record.networkContext as Record<string, unknown>)
              .downlinkKbps === "number"
              ? ((record.networkContext as Record<string, unknown>)
                  .downlinkKbps as number)
              : undefined,
          metered:
            typeof (record.networkContext as Record<string, unknown>)
              .metered === "boolean"
              ? ((record.networkContext as Record<string, unknown>)
                  .metered as boolean)
              : undefined,
          internetValidated:
            typeof (record.networkContext as Record<string, unknown>)
              .internetValidated === "boolean"
              ? ((record.networkContext as Record<string, unknown>)
                  .internetValidated as boolean)
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
    clientIdentity: readClientIdentity(record),
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
    allowTranscode:
      typeof record.allowTranscode === "boolean"
        ? record.allowTranscode
        : undefined,
    deviceContext,
    networkContext,
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
    );
    if (nativeCapabilities) {
      return nativeCapabilities;
    }
  }

  const probe = await probeRuntimePlaybackCapabilities(videoEl, scope);

  return {
    capabilitiesVersion: 3,
    clientIdentity: detectPlaybackClientIdentity(),
    container: probe.containers,
    videoCodecs: probe.videoCodecs,
    videoCodecSignals: probe.videoCodecSignals,
    audioCodecs: probe.audioCodecs,
    maxVideo: probe.maxVideo,
    hlsEngines: probe.hlsEngines.length > 0 ? probe.hlsEngines : undefined,
    preferredHlsEngine: probe.preferredHlsEngine ?? undefined,
    supportsHls: probe.hlsEngines.length > 0,
    supportsRange: probe.supportsRange,
    allowTranscode: true,
    deviceContext: inferBrowserDeviceContext(),
    networkContext: inferBrowserNetworkContext(),
    runtimeProbeUsed: probe.usedRuntimeProbe,
    runtimeProbeVersion: probe.version,
  };
}
