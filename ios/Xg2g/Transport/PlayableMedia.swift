// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Something a player can be handed: a URL, and the cookies that authorize it.
///
/// The player layer receives this and nothing else. It does not learn what the
/// cookie is called, what scheme the deployment uses, or what an API path looks
/// like — all three used to be spelled out in view code, in three different
/// places, each with its own idea of how to repair a server address typed
/// without a scheme.
struct PlayableMedia: Sendable {
    let url: URL
    let cookies: [HTTPCookie]
}

/// Turns what the backend said into something AVFoundation can open.
///
/// The backend answers `stream-info` with a URL that may be absolute or
/// relative to the deployment root. Resolving it is a transport decision: it
/// needs the deployment address, it must stay inside the API scope, and it is
/// where the fallback path lives for the case where the backend said nothing.
enum RecordingPlayback {

    /// The session cookie name. Named once, here, because it is a wire detail.
    static let sessionCookieName = "xg2g_session"

    /// Resolves a recording into a playable handle.
    ///
    /// - Parameters:
    ///   - negotiatedPath: what `stream-info` returned, absolute or relative.
    ///     `nil` falls back to the canonical playlist path.
    ///   - sessionCookie: the media session credential, when one was obtained.
    static func resolve(
        address: ServerAddress,
        recordingID: String,
        negotiatedPath: String?,
        sessionCookie: String?
    ) -> PlayableMedia? {
        guard let url = resolveURL(address: address, recordingID: recordingID, negotiatedPath: negotiatedPath) else {
            return nil
        }
        return PlayableMedia(url: url, cookies: cookies(for: url, sessionCookie: sessionCookie))
    }

    /// The cookies a media request must carry.
    ///
    /// Root-scoped: HLS segments live under paths the playlist chooses, and a
    /// cookie scoped to the playlist's own path would not reach them.
    static func cookies(for url: URL, sessionCookie: String?) -> [HTTPCookie] {
        guard let sessionCookie, let host = url.host else { return [] }
        var properties: [HTTPCookiePropertyKey: Any] = [
            .name: sessionCookieName,
            .value: sessionCookie,
            .domain: host,
            .path: "/",
            .expires: Date().addingTimeInterval(86400),
        ]
        if url.scheme?.lowercased() == "https" {
            properties[.secure] = "TRUE"
        }
        return HTTPCookie(properties: properties).map { [$0] } ?? []
    }

    private static func resolveURL(
        address: ServerAddress,
        recordingID: String,
        negotiatedPath: String?
    ) -> URL? {
        guard let path = negotiatedPath?.trimmingCharacters(in: .whitespacesAndNewlines), !path.isEmpty else {
            return MediaEndpoints(address: address).recordingPlaylist(recordingID: recordingID)
        }

        // An absolute URL is only honoured when it belongs to this deployment.
        // A backend that named somebody else's host would otherwise send the
        // player, and the session cookie, wherever it liked.
        if let absolute = URL(string: path), absolute.scheme != nil {
            return address.contains(absolute) ? absolute : nil
        }

        let relative = path.hasPrefix("/") ? String(path.dropFirst()) : path
        guard let resolved = URL(string: relative, relativeTo: address.rootURL)?.absoluteURL,
              address.contains(resolved)
        else { return nil }
        return resolved
    }
}
