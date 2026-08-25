// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// The two byte-level fetches that are not JSON API calls: artwork, and asking
/// whether a playlist has become servable.
///
/// Neither is an `APIRequest` — one returns an image body, the other cares only
/// about a status code — but both are still network access, and both used to be
/// `URLSession.shared` calls sitting in view code. Keeping them here means the
/// transport boundary is a real boundary rather than "JSON goes through the
/// client and everything else goes wherever".
enum MediaFetcher {

    /// The session these fetches share.
    ///
    /// Separate from `HTTPAPIClient.sharedSession` on purpose: artwork and
    /// readiness polling must never inherit, or compete for, the API client's
    /// timeouts and connection budget.
    private static let session: URLSession = {
        let config = URLSessionConfiguration.default
        config.timeoutIntervalForRequest = 10
        config.timeoutIntervalForResource = 20
        config.requestCachePolicy = .returnCacheDataElseLoad
        return URLSession(configuration: config)
    }()

    /// Fetches image bytes, or `nil` when the fetch fails for any reason.
    ///
    /// Callers are decorating a list row or a lock screen; there is nothing for
    /// them to do with a specific failure, so the error is not propagated.
    static func imageData(from url: URL) async -> Data? {
        guard let (data, response) = try? await session.data(from: url) else { return nil }
        if let http = response as? HTTPURLResponse, !(200..<300).contains(http.statusCode) {
            return nil
        }
        return data
    }

    /// Reads an HLS playlist, or `nil` when it is not servable yet.
    ///
    /// The caller decides what "ready" means by looking at the text; the
    /// request itself — method, cache directive, cookie, HTTP/3 opt-out — is a
    /// transport concern and stays here.
    static func playlistText(url: URL, cookie: HTTPCookie?) async -> String? {
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.assumesHTTP3Capable = false
        request.setValue("no-cache", forHTTPHeaderField: "Cache-Control")
        if let cookie {
            request.setValue("\(cookie.name)=\(cookie.value)", forHTTPHeaderField: "Cookie")
        }

        guard let (data, response) = try? await session.data(for: request),
              let http = response as? HTTPURLResponse,
              http.statusCode == 200
        else { return nil }
        return String(data: data, encoding: .utf8)
    }

    /// Polls until `url` answers 200, and reports whether it ever did.
    ///
    /// HLS playback cannot start before the backend has written a playlist, and
    /// AVFoundation gives no useful error for "not yet". The wait belongs to the
    /// transport because the cookie, the method and the retry budget all do.
    static func waitUntilServable(
        url: URL,
        sessionCookie: String?,
        attempts: Int = 10,
        interval: Duration = .milliseconds(800)
    ) async -> Bool {
        var request = URLRequest(url: url)
        request.httpMethod = "HEAD"
        if let sessionCookie {
            request.setValue("xg2g_session=\(sessionCookie)", forHTTPHeaderField: "Cookie")
        }

        for attempt in 0..<max(attempts, 1) {
            if Task.isCancelled { return false }
            if let (_, response) = try? await session.data(for: request),
               let http = response as? HTTPURLResponse,
               http.statusCode == 200 {
                return true
            }
            if attempt < attempts - 1 {
                try? await Task.sleep(for: interval)
            }
        }
        return false
    }
}
