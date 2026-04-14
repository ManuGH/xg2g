import { hasTouchInput } from "./playerHelpers";

type NavigatorWithUAData = Navigator & {
  userAgentData?: {
    brands?: Array<{
      brand?: string;
      version?: string;
    }>;
    platform?: string;
    platformVersion?: string;
  };
};

export type BrowserIdentity = {
  platform: string;
  osName?: string;
  osVersion?: string;
  browserName?: string;
  browserVersion?: string;
  platformClass?: string;
};

function sanitizeString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim().length > 0
    ? value.trim()
    : undefined;
}

function normalizedPlatform(nav: NavigatorWithUAData): string {
  return (
    sanitizeString(nav.userAgentData?.platform) ??
    sanitizeString(nav.platform) ??
    "browser"
  ).toLowerCase();
}

function normalizedPlatformVersion(nav: NavigatorWithUAData): string | undefined {
  return sanitizeString(nav.userAgentData?.platformVersion);
}

function currentUserAgent(): string {
  try {
    // ua-telemetry-only
    return navigator.userAgent || "";
  } catch {
    return "";
  }
}

function inferOSIdentity(
  ua: string,
  platform: string,
  platformVersion?: string,
): Pick<BrowserIdentity, "osName" | "osVersion"> {
  const touchInput = hasTouchInput();
  const tvosMatch = ua.match(/(?:AppleTV|tvOS)[\s/]+([\d_]+)/i);
  if (tvosMatch) {
    return {
      osName: "tvos",
      osVersion: tvosMatch[1]?.replace(/_/g, "."),
    };
  }

  const androidMatch = ua.match(/Android\s+([\d.]+)/i);
  if (androidMatch) {
    return {
      osName: "android",
      osVersion: androidMatch[1],
    };
  }

  const ipadMatch = ua.match(/iPad;.*OS\s+([\d_]+)/i);
  if (ipadMatch) {
    return {
      osName: "ipados",
      osVersion: ipadMatch[1]?.replace(/_/g, "."),
    };
  }

  const iphoneMatch = ua.match(/(?:iPhone|iPod).*OS\s+([\d_]+)/i);
  if (iphoneMatch) {
    return {
      osName: "ios",
      osVersion: iphoneMatch[1]?.replace(/_/g, "."),
    };
  }

  if (/Macintosh/i.test(ua) && touchInput) {
    return {
      osName: "ipados",
      osVersion: platformVersion,
    };
  }

  const macMatch = ua.match(/Mac OS X\s+([\d_]+)/i);
  if (macMatch) {
    return {
      osName: "macos",
      osVersion: macMatch[1]?.replace(/_/g, "."),
    };
  }

  const windowsMatch = ua.match(/Windows NT\s+([\d.]+)/i);
  if (windowsMatch) {
    return {
      osName: "windows",
      osVersion: windowsMatch[1],
    };
  }

  const chromeOSMatch = ua.match(/CrOS\s+[\w_]+\s+([\d.]+)/i);
  if (chromeOSMatch) {
    return {
      osName: "chromeos",
      osVersion: chromeOSMatch[1],
    };
  }

  if (/Linux/i.test(ua) || platform.includes("linux")) {
    return {
      osName: "linux",
      osVersion: platformVersion,
    };
  }

  return {
    osName: "browser",
    osVersion: platformVersion,
  };
}

function inferBrowserIdentity(
  ua: string,
  nav: NavigatorWithUAData,
): Pick<BrowserIdentity, "browserName" | "browserVersion"> {
  const brandList = Array.isArray(nav.userAgentData?.brands)
    ? nav.userAgentData?.brands
    : [];

  const edgeMatch = ua.match(/Edg(?:A|iOS)?\/([\d.]+)/i);
  if (edgeMatch) {
    return { browserName: "edge", browserVersion: edgeMatch[1] };
  }

  const operaMatch = ua.match(/OPR\/([\d.]+)/i);
  if (operaMatch) {
    return { browserName: "opera", browserVersion: operaMatch[1] };
  }

  const firefoxMatch = ua.match(/(?:Firefox|FxiOS)\/([\d.]+)/i);
  if (firefoxMatch) {
    return { browserName: "firefox", browserVersion: firefoxMatch[1] };
  }

  const samsungMatch = ua.match(/SamsungBrowser\/([\d.]+)/i);
  if (samsungMatch) {
    return {
      browserName: "samsunginternet",
      browserVersion: samsungMatch[1],
    };
  }

  const chromeMatch = ua.match(/(?:Chrome|CriOS|Chromium)\/([\d.]+)/i);
  if (chromeMatch) {
    return { browserName: "chrome", browserVersion: chromeMatch[1] };
  }

  const safariMatch = ua.match(/Version\/([\d.]+).*Safari\//i);
  if (safariMatch) {
    return { browserName: "safari", browserVersion: safariMatch[1] };
  }

  for (const brandEntry of brandList) {
    const brand = sanitizeString(brandEntry?.brand)?.toLowerCase() ?? "";
    const version = sanitizeString(brandEntry?.version);
    if (brand.includes("edge")) {
      return { browserName: "edge", browserVersion: version };
    }
    if (brand.includes("opera")) {
      return { browserName: "opera", browserVersion: version };
    }
    if (brand.includes("firefox")) {
      return { browserName: "firefox", browserVersion: version };
    }
    if (brand.includes("chrome") || brand.includes("chromium")) {
      return { browserName: "chrome", browserVersion: version };
    }
  }

  return {};
}

function inferPlatformClass(
  identity: Pick<BrowserIdentity, "osName" | "browserName">,
): string {
  const osName = identity.osName ?? "";
  const browserName = identity.browserName ?? "";

  if (osName === "ios") {
    return "ios_webkit";
  }
  if (osName === "ipados") {
    return "ipados_webkit";
  }
  if (osName === "tvos") {
    return "tvos_webkit";
  }
  if (osName === "macos" && browserName === "safari") {
    return "macos_safari";
  }
  if (browserName === "firefox") {
    return osName === "android" ? "firefox_mobile" : "firefox_desktop";
  }
  if (
    browserName === "chrome" ||
    browserName === "edge" ||
    browserName === "opera" ||
    browserName === "samsunginternet"
  ) {
    return osName === "android" ? "chromium_mobile" : "chromium_desktop";
  }
  return osName === "android" ? "browser_mobile" : "browser_desktop";
}

export function detectBrowserIdentity(): BrowserIdentity {
  const nav = navigator as NavigatorWithUAData;
  const ua = currentUserAgent();
  const platform = normalizedPlatform(nav);
  const osIdentity = inferOSIdentity(ua, platform, normalizedPlatformVersion(nav));
  const browserIdentity = inferBrowserIdentity(ua, nav);

  return {
    platform,
    ...osIdentity,
    ...browserIdentity,
    platformClass: inferPlatformClass({
      osName: osIdentity.osName,
      browserName: browserIdentity.browserName,
    }),
  };
}
