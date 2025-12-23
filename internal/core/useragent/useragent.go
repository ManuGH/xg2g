// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package useragent

import "strings"

// IsSafariBrowser detects Safari on macOS/iOS.
// Safari has "Safari/" and "AppleWebKit/", but not "Chrome/".
func IsSafariBrowser(userAgent string) bool {
	ua := userAgent
	hasSafari := strings.Contains(ua, "Safari/")
	hasChrome := strings.Contains(ua, "Chrome/") || strings.Contains(ua, "Chromium/")
	hasWebKit := strings.Contains(ua, "AppleWebKit/")
	return hasWebKit && hasSafari && !hasChrome
}

// IsNativeAppleClient detects native Apple clients (AVFoundation/WebKit HLS stack).
func IsNativeAppleClient(userAgent string) bool {
	ua := userAgent
	return strings.Contains(ua, "AppleCoreMedia") ||
		strings.Contains(ua, "CFNetwork") ||
		strings.Contains(ua, "VideoToolbox")
}

// IsIOSLike is a broad check for iOS/iPadOS devices and Apple network stacks.
func IsIOSLike(userAgent string) bool {
	ua := userAgent
	return strings.Contains(ua, "iPhone") ||
		strings.Contains(ua, "iPad") ||
		strings.Contains(ua, "iOS") ||
		IsNativeAppleClient(ua)
}
