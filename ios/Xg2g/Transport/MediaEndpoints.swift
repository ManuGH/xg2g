// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Every media URL this app can reach, derived from one configured deployment.
///
/// ## Why feature code no longer builds these
///
/// Live playback, the smoother route and the HLS playlist each used to be
/// assembled where they were needed, from a `String` interpolation over an API
/// base — and each of those sites carried a hard-coded staging address as its
/// "fallback" for when no server was configured. That fallback shipped: a build
/// with no address pointed at a machine on somebody's LAN, and looked like it
/// worked wherever that machine happened to answer.
///
/// Constructing them here removes both problems at once. There is one place
/// that knows what an API path looks like, and no address means no URL — the
/// caller gets `nil` and shows the "configure a server" state, which is the
/// truthful answer.
///
/// Everything returned is checked against `apiScope` first, so a path that
/// resolved back out of the API — through a service reference containing dot
/// segments, say — is refused rather than requested.
struct MediaEndpoints: Sendable {

    private let address: ServerAddress

    init(address: ServerAddress) {
        self.address = address
    }

    /// The universal live ingest route for a service reference.
    func liveStream(serviceRef: String) -> URL? {
        apiURL(path: "stream/live", trailing: serviceRef)
    }

    /// The burst-smoothing proxy route, used by the lab A/B path.
    func smoothStream(serviceRef: String) -> URL? {
        apiURL(path: "stream/smooth", trailing: serviceRef)
    }

    /// The HLS playlist for a started session.
    func hlsPlaylist(sessionID: String) -> URL? {
        apiURL(path: "sessions/\(sessionID)/hls", trailing: "index.m3u8")
    }

    /// A recording's direct MP4 body.
    func recordingStream(recordingID: String) -> URL? {
        apiURL(path: "recordings/\(recordingID)", trailing: "stream.mp4")
    }

    /// A recording's HLS playlist, used when the backend negotiated no URL of
    /// its own.
    func recordingPlaylist(recordingID: String) -> URL? {
        apiURL(path: "recordings/\(recordingID)", trailing: "playlist.m3u8")
    }

    /// Resolves a channel logo the catalogue named, or derives the default one.
    ///
    /// The catalogue may hand back an absolute URL, a deployment-relative path,
    /// or nothing at all. Deciding which is a URL question, not a channel
    /// question, and it used to be answered inside the channel decoder — the one
    /// place in the app that knew what "starts with a scheme" meant.
    static func logoURL(raw: String?, serviceRef: String, baseURL: URL?) -> URL? {
        if let raw = raw?.trimmingCharacters(in: .whitespacesAndNewlines), !raw.isEmpty {
            if let absolute = URL(string: raw), absolute.scheme != nil {
                return absolute
            }
            guard let baseURL else { return URL(string: raw) }
            let path = raw.hasPrefix("/") ? String(raw.dropFirst()) : raw
            return baseURL.appendingPathComponent(path)
        }

        guard let baseURL else { return nil }
        let sanitized = serviceRef
            .replacingOccurrences(of: ":", with: "_")
            .trimmingCharacters(in: CharacterSet(charactersIn: "_"))
        return baseURL.appendingPathComponent("logos/\(sanitized).png")
    }

    // MARK: - Internals

    /// Joins one API-relative path and refuses anything that leaves the API.
    ///
    /// `trailing` is appended with `appendingPathComponent`, which percent-encodes
    /// it — a service reference is full of colons, and a raw interpolation would
    /// produce a URL the server reads differently from the client.
    private func apiURL(path: String, trailing: String) -> URL? {
        let trimmed = trailing.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }

        let url = address.apiBaseURL
            .appendingPathComponent(path)
            .appendingPathComponent(trimmed)

        guard address.apiScope.contains(url) else { return nil }
        return url
    }
}
