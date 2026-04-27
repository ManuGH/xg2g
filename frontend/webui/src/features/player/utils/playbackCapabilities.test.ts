import { afterEach, describe, expect, it, vi } from "vitest";
import { gatherPlaybackCapabilities } from "./playbackCapabilities";
import { probeRuntimePlaybackCapabilities } from "./playbackProbe";
import { detectPlaybackClientFamily } from "./playbackClientFamily";

const originalMaxTouchPointsDescriptor = Object.getOwnPropertyDescriptor(
  Navigator.prototype,
  "maxTouchPoints",
) || Object.getOwnPropertyDescriptor(window.navigator, "maxTouchPoints");
const originalPlatformDescriptor = Object.getOwnPropertyDescriptor(
  window.navigator,
  "platform",
);
const originalUserAgentDescriptor = Object.getOwnPropertyDescriptor(
  window.navigator,
  "userAgent",
);

vi.mock("./playbackProbe", () => ({
  probeRuntimePlaybackCapabilities: vi.fn(),
}));

vi.mock("./playbackClientFamily", () => ({
  detectPlaybackClientFamily: vi.fn(),
  normalizePlaybackClientFamily: (value: string | null | undefined) => {
    switch ((value || "").trim().toLowerCase()) {
      case "safari":
      case "safari_native":
        return "safari_native";
      case "ios_safari":
      case "ios_safari_native":
        return "ios_safari_native";
      case "firefox":
      case "firefox_hlsjs":
        return "firefox_hlsjs";
      case "android_tv":
      case "android_tv_browser":
      case "android_tv_hlsjs":
      case "shield_browser":
        return "android_tv_browser";
      case "chromium":
      case "chrome":
      case "edge":
      case "chromium_hlsjs":
        return "chromium_hlsjs";
      default:
        return undefined;
    }
  },
  fallbackPlaybackCapabilitiesForClientFamily: (family: string) => {
    if (family === "android_tv_browser") {
      return {
        deviceType: "android_tv",
        container: ["mp4", "ts", "fmp4"],
        videoCodecs: ["h264"],
        audioCodecs: ["aac", "mp3"],
        hlsEngines: ["hlsjs"],
        preferredHlsEngine: "hlsjs",
      };
    }
    return {
      deviceType: "chromium",
      container: ["mp4", "ts", "fmp4"],
      videoCodecs: ["h264"],
      audioCodecs: ["aac", "mp3"],
      hlsEngines: ["hlsjs"],
      preferredHlsEngine: "hlsjs",
    };
  },
}));

describe("gatherPlaybackCapabilities", () => {
  afterEach(() => {
    delete window.__XG2G_HOST__;
    delete window.Xg2gHost;
    vi.restoreAllMocks();
    if (originalUserAgentDescriptor) {
      Object.defineProperty(window.navigator, "userAgent", originalUserAgentDescriptor);
    }
    if (originalPlatformDescriptor) {
      Object.defineProperty(window.navigator, "platform", originalPlatformDescriptor);
    }
    if (originalMaxTouchPointsDescriptor) {
      Object.defineProperty(window.navigator, "maxTouchPoints", originalMaxTouchPointsDescriptor);
    }
  });

  it("prefers native host playback capabilities when the bridge exposes them", async () => {
    window.Xg2gHost = {
      getCapabilitiesJson: () =>
        JSON.stringify({
          platform: "android-tv",
          isTv: true,
          supportsKeepScreenAwake: true,
          supportsHostMediaKeys: true,
          supportsInputFocus: true,
          supportsNativePlayback: true,
        }),
      getPlaybackCapabilitiesJson: () =>
        JSON.stringify({
          capabilitiesVersion: 3,
          container: ["hls", "mpegts", "ts", "mp4"],
          videoCodecs: ["h264"],
          audioCodecs: ["aac", "mp3", "ac3"],
          deviceContext: {
            brand: "google",
            product: "mdarcy",
            device: "foster",
            platform: "android-tv",
            manufacturer: "NVIDIA",
            model: "Shield",
            osName: "Android",
            osVersion: "14",
          },
          networkContext: {
            kind: "ethernet",
            downlinkKbps: 940000,
            metered: false,
            internetValidated: true,
          },
          supportsHls: true,
          supportsRange: true,
          deviceType: "android_tv",
          hlsEngines: ["native"],
          preferredHlsEngine: "native",
          runtimeProbeUsed: false,
          runtimeProbeVersion: 1,
          clientFamilyFallback: "android_tv_native",
          allowTranscode: true,
        }),
    };

    const capabilities = await gatherPlaybackCapabilities("live");

    expect(capabilities).toEqual(
      expect.objectContaining({
        deviceType: "android_tv",
        preferredHlsEngine: "native",
        runtimeProbeUsed: false,
        videoCodecs: ["h264"],
        audioCodecs: ["aac", "mp3", "ac3"],
        deviceContext: expect.objectContaining({
          brand: "google",
          product: "mdarcy",
          device: "foster",
          platform: "android-tv",
          manufacturer: "NVIDIA",
        }),
        networkContext: expect.objectContaining({
          kind: "ethernet",
        }),
      }),
    );
    expect(probeRuntimePlaybackCapabilities).not.toHaveBeenCalled();
  });

  it("falls back to browser runtime probing on non-native hosts", async () => {
    vi.mocked(probeRuntimePlaybackCapabilities).mockResolvedValue({
      version: 2,
      usedRuntimeProbe: true,
      nativeHls: false,
      hlsJs: true,
      preferredHlsEngine: "hlsjs",
      hlsEngines: ["hlsjs"],
      containers: ["mp4", "ts", "fmp4"],
      videoCodecs: ["h264"],
      videoCodecSignals: [{ codec: "h264", supported: true }],
      audioCodecs: ["aac", "mp3"],
      supportsRange: true,
    });
    vi.mocked(detectPlaybackClientFamily).mockReturnValue("chromium_hlsjs");

    const capabilities = await gatherPlaybackCapabilities("live");

    expect(capabilities).toEqual(
      expect.objectContaining({
        deviceType: "web",
        preferredHlsEngine: "hlsjs",
        clientFamilyFallback: "chromium_hlsjs",
        runtimeProbeUsed: true,
        videoCodecs: ["h264"],
        deviceContext: expect.objectContaining({
          platform: expect.any(String),
        }),
        networkContext: expect.objectContaining({
          internetValidated: expect.any(Boolean),
        }),
      }),
    );
    expect(probeRuntimePlaybackCapabilities).toHaveBeenCalledTimes(1);
  });

  it("marks Android TV browsers as Android TV instead of generic web", async () => {
    vi.mocked(probeRuntimePlaybackCapabilities).mockResolvedValue({
      version: 2,
      usedRuntimeProbe: true,
      nativeHls: false,
      hlsJs: true,
      preferredHlsEngine: "hlsjs",
      hlsEngines: ["hlsjs"],
      containers: ["mp4", "ts", "fmp4"],
      videoCodecs: ["h264"],
      videoCodecSignals: [{ codec: "h264", supported: true }],
      audioCodecs: ["aac", "mp3"],
      supportsRange: true,
    });
    vi.mocked(detectPlaybackClientFamily).mockReturnValue("android_tv_browser");

    const capabilities = await gatherPlaybackCapabilities("live");

    expect(capabilities).toEqual(
      expect.objectContaining({
        deviceType: "android_tv",
        preferredHlsEngine: "hlsjs",
        clientFamilyFallback: "android_tv_browser",
        videoCodecs: ["h264"],
      }),
    );
  });

  it("adds Fire TV model hints to browser capability snapshots", async () => {
    vi.mocked(probeRuntimePlaybackCapabilities).mockResolvedValue({
      version: 2,
      usedRuntimeProbe: true,
      nativeHls: false,
      hlsJs: true,
      preferredHlsEngine: "hlsjs",
      hlsEngines: ["hlsjs"],
      containers: ["mp4", "ts", "fmp4"],
      videoCodecs: ["av1", "hevc", "h264"],
      videoCodecSignals: [{ codec: "av1", supported: true, smooth: true }],
      audioCodecs: ["aac", "mp3"],
      supportsRange: true,
    });
    vi.mocked(detectPlaybackClientFamily).mockReturnValue("android_tv_browser");
    Object.defineProperty(navigator, "userAgent", {
      configurable: true,
      value:
        "Mozilla/5.0 (Linux; Android 11; AFTKRT Build/RS8141) AppleWebKit/537.36 (KHTML, like Gecko) Silk/124.0 Safari/537.36",
    });

    const capabilities = await gatherPlaybackCapabilities("live");

    expect(capabilities.deviceContext).toEqual(
      expect.objectContaining({
        brand: "amazon",
        manufacturer: "Amazon",
        product: "AFTKRT",
        device: "firetv",
        model: "AFTKRT",
        osName: "android",
        osVersion: "11",
      }),
    );
  });

  it("adds Xiaomi TV Stick 4K model hints to browser capability snapshots", async () => {
    vi.mocked(probeRuntimePlaybackCapabilities).mockResolvedValue({
      version: 2,
      usedRuntimeProbe: true,
      nativeHls: false,
      hlsJs: true,
      preferredHlsEngine: "hlsjs",
      hlsEngines: ["hlsjs"],
      containers: ["mp4", "ts", "fmp4"],
      videoCodecs: ["av1", "hevc", "h264"],
      videoCodecSignals: [{ codec: "av1", supported: true, smooth: true }],
      audioCodecs: ["aac", "mp3"],
      supportsRange: true,
    });
    vi.mocked(detectPlaybackClientFamily).mockReturnValue("android_tv_browser");
    Object.defineProperty(navigator, "userAgent", {
      configurable: true,
      value:
        "Mozilla/5.0 (Linux; Android 11; MDZ-27-AA Xiaomi TV Stick 4K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36",
    });

    const capabilities = await gatherPlaybackCapabilities("live");

    expect(capabilities.deviceContext).toEqual(
      expect.objectContaining({
        brand: "xiaomi",
        manufacturer: "Xiaomi",
        product: "MDZ-27-AA",
        device: "xiaomi-tv-stick",
        model: "MDZ-27-AA",
        osName: "android",
        osVersion: "11",
      }),
    );
  });

  it("normalizes legacy native client-family aliases from the host bridge", async () => {
    window.Xg2gHost = {
      getCapabilitiesJson: () =>
        JSON.stringify({
          platform: "android",
          supportsNativePlayback: true,
        }),
      getPlaybackCapabilitiesJson: () =>
        JSON.stringify({
          capabilitiesVersion: 3,
          container: ["mp4", "ts"],
          videoCodecs: ["av1", "hevc", "h264"],
          audioCodecs: ["aac", "ac3", "mp3"],
          supportsHls: true,
          supportsRange: true,
          deviceType: "safari",
          hlsEngines: ["native"],
          preferredHlsEngine: "native",
          runtimeProbeUsed: true,
          runtimeProbeVersion: 2,
          clientFamilyFallback: "safari",
          allowTranscode: true,
        }),
    };

    const capabilities = await gatherPlaybackCapabilities("live");

    expect(capabilities).toEqual(
      expect.objectContaining({
        clientFamilyFallback: "safari_native",
        preferredHlsEngine: "native",
        videoCodecs: ["av1", "hevc", "h264"],
      }),
    );
  });

  it("marks desktop-mode iPadOS Safari as iPadOS instead of macOS", async () => {
    vi.mocked(probeRuntimePlaybackCapabilities).mockResolvedValue({
      version: 2,
      usedRuntimeProbe: true,
      nativeHls: true,
      hlsJs: false,
      preferredHlsEngine: "native",
      hlsEngines: ["native"],
      containers: ["mp4", "ts", "fmp4"],
      videoCodecs: ["av1", "hevc", "h264"],
      videoCodecSignals: [{ codec: "av1", supported: true, smooth: true }],
      audioCodecs: ["aac", "mp3", "ac3"],
      supportsRange: true,
    });
    vi.mocked(detectPlaybackClientFamily).mockReturnValue("ios_safari_native");
    Object.defineProperty(navigator, "userAgent", {
      configurable: true,
      value:
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15",
    });
    Object.defineProperty(navigator, "platform", {
      configurable: true,
      value: "MacIntel",
    });
    Object.defineProperty(navigator, "maxTouchPoints", {
      configurable: true,
      value: 5,
    });

    const capabilities = await gatherPlaybackCapabilities("live");

    expect(capabilities.deviceContext).toEqual(
      expect.objectContaining({
        osName: "ipados",
        osVersion: undefined,
        platform: "macintel",
      }),
    );
  });

  it("drops frozen desktop Safari macOS 10.15.7 so runtime AV1 can drive policy", async () => {
    vi.mocked(probeRuntimePlaybackCapabilities).mockResolvedValue({
      version: 2,
      usedRuntimeProbe: true,
      nativeHls: true,
      hlsJs: false,
      preferredHlsEngine: "native",
      hlsEngines: ["native"],
      containers: ["mp4", "ts", "fmp4"],
      videoCodecs: ["av1", "hevc", "h264"],
      videoCodecSignals: [{ codec: "av1", supported: true, smooth: true }],
      audioCodecs: ["aac", "mp3", "ac3"],
      supportsRange: true,
    });
    vi.mocked(detectPlaybackClientFamily).mockReturnValue("safari_native");
    Object.defineProperty(navigator, "userAgent", {
      configurable: true,
      value:
        "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.4 Safari/605.1.15",
    });
    Object.defineProperty(navigator, "platform", {
      configurable: true,
      value: "MacIntel",
    });
    Object.defineProperty(navigator, "maxTouchPoints", {
      configurable: true,
      value: 0,
    });

    const capabilities = await gatherPlaybackCapabilities("live");

    expect(capabilities.deviceContext).toEqual(
      expect.objectContaining({
        osName: "macos",
        osVersion: undefined,
        platform: "macintel",
      }),
    );
    expect(capabilities.videoCodecs).toEqual(["av1", "hevc", "h264"]);
    expect(capabilities.videoCodecSignals).toEqual([
      { codec: "av1", supported: true, smooth: true },
    ]);
  });
});
