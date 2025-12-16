// SPDX-License-Identifier: MIT

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
