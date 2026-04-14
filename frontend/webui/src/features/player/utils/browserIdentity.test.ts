import { afterEach, describe, expect, it } from "vitest";
import { detectBrowserIdentity } from "./browserIdentity";

const originalUserAgentDescriptor = Object.getOwnPropertyDescriptor(
  window.navigator,
  "userAgent",
);
const originalPlatformDescriptor = Object.getOwnPropertyDescriptor(
  window.navigator,
  "platform",
);
const originalMaxTouchPointsDescriptor = Object.getOwnPropertyDescriptor(
  window.navigator,
  "maxTouchPoints",
);

function setNavigatorStringProperty(
  property: "userAgent" | "platform",
  value: string,
): void {
  Object.defineProperty(window.navigator, property, {
    configurable: true,
    value,
  });
}

function setMaxTouchPoints(value: number): void {
  Object.defineProperty(window.navigator, "maxTouchPoints", {
    configurable: true,
    value,
  });
}

describe("browserIdentity", () => {
  afterEach(() => {
    if (originalUserAgentDescriptor) {
      Object.defineProperty(
        window.navigator,
        "userAgent",
        originalUserAgentDescriptor,
      );
    }
    if (originalPlatformDescriptor) {
      Object.defineProperty(
        window.navigator,
        "platform",
        originalPlatformDescriptor,
      );
    }
    if (originalMaxTouchPointsDescriptor) {
      Object.defineProperty(
        window.navigator,
        "maxTouchPoints",
        originalMaxTouchPointsDescriptor,
      );
    }
  });

  it("classifies macOS Safari separately from mobile WebKit clients", () => {
    setNavigatorStringProperty(
      "userAgent",
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_4) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15",
    );
    setNavigatorStringProperty("platform", "MacIntel");
    setMaxTouchPoints(0);

    expect(detectBrowserIdentity()).toEqual(
      expect.objectContaining({
        platform: "macintel",
        osName: "macos",
        osVersion: "14.4",
        browserName: "safari",
        browserVersion: "17.4",
        platformClass: "macos_safari",
      }),
    );
  });

  it("classifies desktop-style iPadOS as its own WebKit platform class", () => {
    setNavigatorStringProperty(
      "userAgent",
      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1",
    );
    setNavigatorStringProperty("platform", "MacIntel");
    setMaxTouchPoints(5);

    expect(detectBrowserIdentity()).toEqual(
      expect.objectContaining({
        osName: "ipados",
        browserName: "safari",
        browserVersion: "18.0",
        platformClass: "ipados_webkit",
      }),
    );
  });

  it("keeps exact browser branding while grouping iPhone browsers into WebKit playback class", () => {
    setNavigatorStringProperty(
      "userAgent",
      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/135.0.7049.83 Mobile/15E148 Safari/604.1",
    );
    setNavigatorStringProperty("platform", "iPhone");
    setMaxTouchPoints(5);

    expect(detectBrowserIdentity()).toEqual(
      expect.objectContaining({
        osName: "ios",
        osVersion: "17.4",
        browserName: "chrome",
        browserVersion: "135.0.7049.83",
        platformClass: "ios_webkit",
      }),
    );
  });
});
