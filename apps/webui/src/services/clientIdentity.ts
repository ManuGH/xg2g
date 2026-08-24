// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import type { PlaybackClientIdentity } from '../client-ts';

/**
 * What this client is, as two facts the browser actually knows about itself.
 *
 * ## Why this replaced family detection
 *
 * The WebUI used to decide its own `clientFamilyFallback`: it probed
 * `canPlayType`, sniffed the user agent, and returned `safari_native`,
 * `ios_safari_native`, `android_tv_browser`, `firefox_hlsjs` or
 * `chromium_hlsjs` — the identifiers the backend keys playback policy on. So
 * the page chose the policy applied to it, and the native iOS app, sending the
 * same `ios_safari_native`, was handed browser policy for the same reason.
 *
 * Platform and engine are still read from the user agent, because that is where
 * a browser states them. The difference is what is reported: a fact about the
 * runtime rather than a conclusion about how to treat it. The conclusion is the
 * server's, and there is now exactly one place that draws it.
 *
 * This lives beside the client wrapper rather than in a feature module because
 * it is part of what the transport says about itself.
 */

type Platform = PlaybackClientIdentity['platform'];
type BrowserEngine = NonNullable<PlaybackClientIdentity['browserEngine']>;

function currentUserAgent(): string {
  try {
    return navigator.userAgent || '';
  } catch {
    return '';
  }
}

function hasTouchInput(): boolean {
  try {
    return navigator.maxTouchPoints > 1;
  } catch {
    return false;
  }
}

function detectPlatform(userAgent: string): Platform {
  const ua = userAgent.toLowerCase();

  if (/\baft[a-z0-9]+\b/.test(ua) || /fire\s*tv/.test(ua)) {
    return 'android_tv';
  }
  if (/android/.test(ua)) {
    // Android TV browsers are Android too; the TV markers are what separate a
    // living-room device from a phone, and they change the capability ceiling.
    return /(android\s*tv|shield|bravia|smart[-\s]?tv|hbbtv|googletv|chromecast)/.test(ua)
      ? 'android_tv'
      : 'android';
  }
  if (/(iphone|ipod)/.test(ua)) {
    return 'ios';
  }
  if (/ipad/.test(ua)) {
    return 'ipados';
  }
  if (/macintosh|mac os x/.test(ua)) {
    // iPadOS reports a macOS user agent in desktop mode. Touch points are what
    // tell the two apart; a Mac reports at most one.
    return hasTouchInput() ? 'ipados' : 'macos';
  }
  if (/windows/.test(ua)) {
    return 'windows';
  }
  if (/linux|cros|x11/.test(ua)) {
    return 'linux';
  }
  return 'unknown';
}

function detectBrowserEngine(userAgent: string): BrowserEngine {
  const ua = userAgent.toLowerCase();

  if (/firefox|fxios/.test(ua)) {
    return 'gecko';
  }
  // Order matters: every Chromium user agent also contains "safari".
  if (/chrome|chromium|crios|edg\//.test(ua)) {
    return 'blink';
  }
  if (/safari|applewebkit/.test(ua)) {
    return 'webkit';
  }
  return 'unknown';
}

/**
 * The identity this browser reports to the backend.
 *
 * `surface` is always `browser` here — the WebUI is a page. The native clients
 * report `native_app`, which is the distinction the server needs to know
 * whether a capability claim is decoder truth or a media stack's guess.
 */
export function detectPlaybackClientIdentity(): PlaybackClientIdentity {
  const userAgent = currentUserAgent();
  return {
    platform: detectPlatform(userAgent),
    surface: 'browser',
    browserEngine: detectBrowserEngine(userAgent),
  };
}

/**
 * Observable facts about the machine the browser runs on.
 *
 * Not capabilities and not a family: brand, model, OS and version, which the
 * backend records with a decision so an operator can see what it was made for.
 * It lives here because reading them means reading the user agent, and this
 * file is the one place allowed to.
 */
export type BrowserDeviceContext = {
  brand?: string;
  device?: string;
  platform?: string;
  product?: string;
  manufacturer?: string;
  model?: string;
  osName?: string;
  osVersion?: string;
};

export function inferBrowserDeviceContext(): BrowserDeviceContext {
  const nav = navigator as Navigator & {
    userAgentData?: {
      platform?: string;
      platformVersion?: string;
    };
    maxTouchPoints?: number;
  };
  const ua = navigator.userAgent;
  const platform = nav.userAgentData?.platform || navigator.platform || "browser";
  const platformVersion =
    typeof nav.userAgentData?.platformVersion === "string"
      ? nav.userAgentData.platformVersion.trim()
      : undefined;

  const isIPadOSDesktopUA =
    /Mac OS X/i.test(ua) && /Macintosh/i.test(ua) && (nav.maxTouchPoints ?? 0) > 1;
  let osName = "browser";
  let osVersion: string | undefined;
  const patterns: Array<[RegExp, string]> = [
    [/Android\s+([\d.]+)/i, "android"],
    [/Fire\s*OS\s+([\d.]+)/i, "fireos"],
    [/Vega\s*OS(?:\s+version:?)?\s*([\d.]+)/i, "vegaos"],
    [/(?:iPhone|iPad|CPU (?:iPhone )?OS)\s+([\d_]+)/i, "ios"],
    [/Windows NT\s+([\d.]+)/i, "windows"],
    [/Mac OS X\s+([\d_]+)/i, "macos"],
    [/CrOS\s+[\w_]+\s+([\d.]+)/i, "chromeos"],
  ];
  for (const [pattern, candidate] of patterns) {
    const match = ua.match(pattern);
    if (match) {
      osName = candidate;
      osVersion = match[1]?.replace(/_/g, ".");
      break;
    }
  }
  if (isIPadOSDesktopUA) {
    osName = "ipados";
  }
  if (osName === "browser" && /Linux/i.test(ua)) {
    osName = "linux";
  }
  if (!osVersion && platformVersion) {
    osVersion = platformVersion;
  }
  if (isFrozenWebKitMacOSVersion(ua, osName, osVersion, String(platform))) {
    osVersion = undefined;
  }

  return {
    ...inferBrowserDeviceHints(ua),
    platform: String(platform).toLowerCase(),
    osName,
    osVersion,
  };
}

function inferBrowserDeviceHints(userAgent: string): Partial<BrowserDeviceContext> {
  const fireTVModel = userAgent.match(/\b(AFT[A-Z0-9]+)\b/i)?.[1];
  if (fireTVModel) {
    return {
      brand: "amazon",
      manufacturer: "Amazon",
      product: fireTVModel.toUpperCase(),
      device: "firetv",
      model: fireTVModel.toUpperCase(),
    };
  }

  if (/shield\s+android\s+tv/i.test(userAgent)) {
    return {
      brand: "nvidia",
      manufacturer: "NVIDIA",
      device: "shield",
      model: "SHIELD Android TV",
    };
  }

  const xiaomiModel =
    userAgent.match(/\b(MDZ-[A-Z0-9-]+)\b/i)?.[1] ||
    userAgent.match(/\b(MiTV-[A-Z0-9-]+)\b/i)?.[1];
  if (xiaomiModel || /xiaomi\s+tv\s+stick\s+4k|mi\s+tv\s+stick\s+4k/i.test(userAgent)) {
    return {
      brand: "xiaomi",
      manufacturer: "Xiaomi",
      product: xiaomiModel,
      device: "xiaomi-tv-stick",
      model: xiaomiModel || "Xiaomi TV Stick 4K",
    };
  }

  return {};
}

function isFrozenWebKitMacOSVersion(
  userAgent: string,
  osName: string,
  osVersion: string | undefined,
  platform: string,
): boolean {
  return (
    (osName === "macos" || osName === "ipados") &&
    osVersion === "10.15.7" &&
    /Safari\/605\.1\.15/i.test(userAgent) &&
    !/(Chrome|Chromium|Edg|OPR|Firefox)\//i.test(userAgent) &&
    String(platform).toLowerCase() === "macintel"
  );
}
