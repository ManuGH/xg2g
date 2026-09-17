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

    /// Root-scoped cookie ensures media segments under any path inherit the ticket.
    func rootCookie(for url: URL) -> HTTPCookie? {
        guard let host = url.host else { return nil }
        return HTTPCookie(properties: [
            .name: name,
            .value: value,
            .domain: host,
            .path: "/",
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
    func startLive(serviceRef: String, qualityPreference: String = "auto") async throws -> LiveStream {
        let allowTranscode = qualityPreference != "passthrough"
        let intentName: String
        var profileParam: String?
        switch qualityPreference {
        case "passthrough":
            intentName = "direct"
            profileParam = "copy"
        case "qsvNormalize":
            intentName = "normalize"
            profileParam = "normalize"
        case "dataSaver":
            intentName = "compatible"
        default:
            intentName = "quality"
        }

        // Probe capabilities and obtain playbackDecisionToken from planner
        var playbackDecisionToken: String?
        do {
            let capabilities = Xg2gContract.PlaybackCapabilities(
                audioCodecs: ["aac", "ac3", "mp3"],
                capabilitiesVersion: 3,
                clientIdentity: Xg2gContract.PlaybackClientIdentity(
                    platform: .ios,
                    surface: .nativeApp,
                    appVersion: DeviceCapabilities.appVersion
                ),
                container: ["mp4", "ts", "fmp4"],
                videoCodecs: DeviceCapabilities.supportedCodecsList,
                allowTranscode: allowTranscode,
                deviceContext: DeviceCapabilities.deviceContext,
                hlsEngines: ["native"],
                networkContext: Xg2gContract.PlaybackNetworkContext(
                    kind: NetworkMonitor.shared.currentType.rawValue,
                    metered: NetworkMonitor.shared.isExpensive
                ),
                preferredHlsEngine: "native",
                runtimeProbeUsed: true,
                runtimeProbeVersion: 3,
                supportsHls: true,
                videoCodecSignals: DeviceCapabilities.codecSignals
            )
            let infoRequest = Xg2gContract.LiveStreamInfoRequest(
                capabilities: capabilities,
                serviceRef: serviceRef
            )
            let infoResponse: Xg2gContract.LiveStreamInfoResponse = try await api.send(
                APIRequest(
                    method: .post,
                    path: "live/stream-info",
                    body: try Self.encode(infoRequest),
                    contentType: "application/json"
                )
            )
            playbackDecisionToken = infoResponse.playbackDecisionToken
        } catch {
            // Planner probe failed or was skipped
        }

        // No client family here. The server derives it from the identity the
        // capabilities carry, and ignores anything a client puts in params.
        var params: [String: String] = [
            "intent": intentName,
            "preferred_engine": "native",
            "codecs": DeviceCapabilities.supportedCodecsHeader
        ]
        if let profile = profileParam {
            params["profile"] = profile
        }
        if let token = playbackDecisionToken {
            params["playback_decision_token"] = token
            params["playback_mode"] = "native_hls"
        }

        let intent: Xg2gContract.IntentAcceptedResponse = try await api.send(
            APIRequest(
                method: .post,
                path: "intents",
                body: try Self.encode(
                    Xg2gContract.IntentRequest(
                        params: params,
                        playbackDecisionToken: playbackDecisionToken,
                        serviceRef: serviceRef,
                        type: .streamStart
                    )
                ),
                contentType: "application/json"
            )
        )

        let sessionID = intent.sessionId.trimmingCharacters(in: .whitespaces)
        guard !sessionID.isEmpty else { throw Failure.noSessionCreated }

        let ticket = try await mintTicket(for: sessionID)

        guard let playlistURL = playlistURL(for: sessionID) else {
            throw Failure.unusableStreamURL
        }

        // Prime the cookie storage before checking readiness
        if let cookie = ticket.httpCookie(for: playlistURL) {
            HTTPCookieStorage.shared.setCookie(cookie)
        }
        if let rootCookie = ticket.rootCookie(for: playlistURL) {
            HTTPCookieStorage.shared.setCookie(rootCookie)
        }

        // Wait for transcoder/receiver to produce the initial playlist (avoids AVPlayer 404 trap)
        await waitForPlaylistReady(url: playlistURL, ticket: ticket)

        return LiveStream(sessionID: sessionID, playlistURL: playlistURL, ticket: ticket)
    }

    private func waitForPlaylistReady(url: URL, ticket: PlaybackTicket) async {
        guard api is HTTPAPIClient else { return }
        let cookie = ticket.httpCookie(for: url)

        for _ in 0..<30 {
            if let text = await MediaFetcher.playlistText(url: url, cookie: cookie) {
                // If the playlist has at least 1 segment or is a valid master playlist, hand off to AVPlayer immediately
                let hasSegment = text.contains("#EXTINF:")
                let isMaster = text.contains("#EXT-X-STREAM-INF")
                if hasSegment || isMaster {
                    return
                }
            }
            try? await Task.sleep(for: .milliseconds(100))
        }
    }

    /// Ends the session. Best-effort by design: the server also reclaims a
    /// session whose heartbeat stops, so a failure here costs a lease timeout
    /// rather than a stuck tuner.
    func stopLive(sessionID: String) async {
        _ = try? await api.send(
            APIRequest<EmptyResponse>(
                method: .post,
                path: "intents",
                body: try? Self.encode(Xg2gContract.IntentRequest(sessionId: sessionID, type: .streamStop)),
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
        let response: Xg2gContract.PlaybackTicketResponse
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
