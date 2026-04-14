import { afterEach, describe, expect, it, vi } from "vitest";
import { gatherPlaybackCapabilities } from "./playbackCapabilities";
import { probeRuntimePlaybackCapabilities } from "./playbackProbe";
import { detectPlaybackClientFamily } from "./playbackClientFamily";

vi.mock("./playbackProbe", () => ({
  probeRuntimePlaybackCapabilities: vi.fn(),
}));

vi.mock("./playbackClientFamily", () => ({
  detectPlaybackClientFamily: vi.fn(),
}));

describe("gatherPlaybackCapabilities", () => {
  afterEach(() => {
    delete window.__XG2G_HOST__;
    delete window.Xg2gHost;
    vi.restoreAllMocks();
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
          platformClass: "android_tv_native_host",
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
});
