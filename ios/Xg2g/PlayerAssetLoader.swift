// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import Foundation

/// Unified asset and options builder for AVPlayer instances in Xg2g.
///
/// Ensures all video streams (Live, DVR recordings, offline) consistently
/// carry required authentication cookies, User-Agent, and origin headers.
enum PlayerAssetLoader {

    static func makeAsset(url: URL, baseURL: URL? = nil, extraHeaders: [String: String] = [:]) -> AVURLAsset {
        let cookieTarget = baseURL ?? url
        var options: [String: Any] = [:]
        var headers = extraHeaders
        if headers["User-Agent"] == nil {
            headers["User-Agent"] = "xg2g-ios/3.0"
        }

        var allCookies = HTTPCookieStorage.shared.cookies(for: cookieTarget) ?? []
        if let streamCookies = HTTPCookieStorage.shared.cookies(for: url), !streamCookies.isEmpty {
            for c in streamCookies where !allCookies.contains(where: { $0.name == c.name }) {
                allCookies.append(c)
            }
        }

        if !allCookies.isEmpty {
            options[AVURLAssetHTTPCookiesKey] = allCookies
            let cookieHeader = allCookies.map { "\($0.name)=\($0.value)" }.joined(separator: "; ")
            headers["Cookie"] = cookieHeader
        }

        options["AVURLAssetHTTPHeaderFieldsKey"] = headers
        return AVURLAsset(url: url, options: options)
    }

    static func makePlayerItem(url: URL, baseURL: URL? = nil, extraHeaders: [String: String] = [:]) -> AVPlayerItem {
        let asset = makeAsset(url: url, baseURL: baseURL, extraHeaders: extraHeaders)
        return AVPlayerItem(asset: asset)
    }
}
