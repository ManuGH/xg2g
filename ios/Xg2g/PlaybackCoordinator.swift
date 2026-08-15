// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Everything needed to hand a live stream to AVPlayer.
///
/// The cookie travels beside the URL rather than inside it. `AVURLAsset` takes
/// cookies explicitly via `AVURLAssetHTTPCookiesKey`, so the credential never
/// enters a URL string — which is what keeps it out of access logs, referrers
/// and player caches.
struct LiveStream: Equatable, Sendable {
    let sessionID: String
    let playlistURL: URL
    /// Name, value and path of the playback ticket cookie.
    let ticket: PlaybackTicket
}

struct PlaybackTicket: Equatable, Sendable {
    let name: String
    let value: String
    let path: String
    let expiresAt: Date

    /// The cookie AVFoundation should send. Built here rather than in the view
    /// layer so nothing above this point has to know it exists.
    func httpCookie(for url: URL) -> HTTPCookie? {
        guard let host = url.host else { return nil }
        return HTTPCookie(properties: [
            .name: name,
            .value: value,
            .domain: host,
            .path: path,
            .secure: url.scheme?.lowercased() == "https" ? "TRUE" : "FALSE",
            .expires: expiresAt,
        ])
    }
}

/// Starts live playback: intent → session → ticket → playable stream.
///
/// ## Why playback has its own credential
///
/// A player fetches the playlist and every segment itself, through
/// AVFoundation's own networking. There is no seam at which the app could
/// attach a fresh DPoP proof to a segment request, so the API credential cannot
/// reach the media path — and must not be made to, since the only ways to do
/// that are putting it in a URL or intercepting every segment through a
/// resource loader.
///
/// So the app spends its API credential once, here, to mint a ticket that is
/// good for exactly one session's media and nothing else. That single request
/// *is* DPoP-authorized; everything AVPlayer does afterwards carries only the
/// ticket.
actor PlaybackCoordinator {

    enum Failure: Error, Equatable, Sendable {
        /// The server accepted the intent but named no session.
        case noSessionCreated
        /// The stream URL could not be built inside the configured deployment.
        case unusableStreamURL
        case ticketRefused
    }

    private let address: ServerAddress
    private let api: APIClient
    private let now: @Sendable () -> Date

    init(address: ServerAddress, api: APIClient, now: @escaping @Sendable () -> Date = { Date() }) {
        self.address = address
        self.api = api
        self.now = now
    }

    /// Starts a live session for a channel and returns something playable.
    func startLive(serviceRef: String) async throws -> LiveStream {
        let intent: PlaybackWire.IntentAcceptedResponse = try await api.send(
            APIRequest(
                method: .post,
                path: "intents",
                body: try Self.encode(PlaybackWire.IntentRequest(type: "stream.start", serviceRef: serviceRef)),
                contentType: "application/json"
            )
        )

        let sessionID = intent.sessionID.trimmingCharacters(in: .whitespaces)
        guard !sessionID.isEmpty else { throw Failure.noSessionCreated }

        let ticket = try await mintTicket(for: sessionID)

        guard let playlistURL = playlistURL(for: sessionID) else {
            throw Failure.unusableStreamURL
        }

        return LiveStream(sessionID: sessionID, playlistURL: playlistURL, ticket: ticket)
    }

    /// Ends the session. Best-effort by design: the server also reclaims a
    /// session whose heartbeat stops, so a failure here costs a lease timeout
    /// rather than a stuck tuner.
    func stopLive(sessionID: String) async {
        _ = try? await api.send(
            APIRequest<EmptyResponse>(
                method: .post,
                path: "intents",
                body: try? Self.encode(PlaybackWire.IntentRequest(type: "stream.stop", sessionID: sessionID)),
                contentType: "application/json"
            )
        )
    }

    /// Keeps the lease alive. The server expires a session that stops calling.
    func heartbeat(sessionID: String) async throws {
        _ = try await api.send(
            APIRequest<EmptyResponse>(method: .post, path: "sessions/\(sessionID)/heartbeat")
        )
    }

    // MARK: - Internals

    private func mintTicket(for sessionID: String) async throws -> PlaybackTicket {
        let response: PlaybackWire.TicketResponse
        do {
            response = try await api.send(
                APIRequest(method: .post, path: "sessions/\(sessionID)/playback-ticket")
            )
        } catch {
            throw Failure.ticketRefused
        }

        return PlaybackTicket(
            name: response.cookie,
            value: response.ticket,
            path: response.path,
            expiresAt: now().addingTimeInterval(TimeInterval(response.expiresIn))
        )
    }

    /// The playlist lives under the API base like any other resource, so it is
    /// built the same way and checked against the same containment boundary.
    private func playlistURL(for sessionID: String) -> URL? {
        let url = address.apiBaseURL.appendingPathComponent("sessions/\(sessionID)/hls/index.m3u8")
        guard address.apiScope.contains(url) else { return nil }
        return url
    }

    private static func encode<T: Encodable>(_ value: T) throws -> Data {
        try JSONEncoder().encode(value)
    }
}

// MARK: - Wire

enum PlaybackWire {

    struct IntentRequest: Encodable, Sendable {
        let type: String
        var serviceRef: String?
        var sessionID: String?

        private enum CodingKeys: String, CodingKey {
            case type, serviceRef
            case sessionID = "sessionId"
        }
    }

    struct IntentAcceptedResponse: Decodable, Sendable {
        let sessionID: String
        let status: String

        private enum CodingKeys: String, CodingKey {
            case sessionID = "sessionId"
            case status
        }
    }

    struct TicketResponse: Decodable, Sendable {
        let sessionID: String
        let ticket: String
        let cookie: String
        let path: String
        let expiresIn: Int

        private enum CodingKeys: String, CodingKey {
            case sessionID = "sessionId"
            case ticket, cookie, path, expiresIn
        }
    }
}
