// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

extension AppModel {

    // MARK: - Playback Stream URLs & Zap Preparation

    /// The receiver URL for a service, or `nil` while no usable receiver
    /// address has been entered. Direct playback cannot run until it has.
    func directStreamURL(for channel: Channel) -> URL? {
        directStreamURL(for: channel.serviceRef)
    }

    func directStreamURL(for serviceRef: String) -> URL? {
        let base = receiverStreamBaseURL.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !base.isEmpty else { return nil }
        let normalised = base.hasSuffix("/") ? String(base.dropLast()) : base
        return URL(string: "\(normalised)/\(serviceRef)")
    }

    /// The v3 Ingest Live Stream URL for a service reference, with direct receiver as primary.
    func liveStreamURL(for serviceRef: String) -> URL? {
        if let direct = directStreamURL(for: serviceRef) {
            return direct
        }
        if let serverURL = media?.liveStream(serviceRef: serviceRef) {
            return serverURL
        }
        #if DEBUG
        return MediaEndpoints.debugFallbackLiveStream(serviceRef: serviceRef)
        #else
        return nil
        #endif
    }

    /// The backend server stream URL used as a fallback when direct receiver streaming
    /// cannot be reached (e.g. outside the home Wi-Fi) or when a channel requires audio transcoding.
    func fallbackServerStreamURL(for serviceRef: String) -> URL? {
        if let serverURL = media?.liveStream(serviceRef: serviceRef) {
            return serverURL
        }
        #if DEBUG
        return MediaEndpoints.debugFallbackLiveStream(serviceRef: serviceRef)
        #else
        return nil
        #endif
    }

    /// The legacy burst smoother URL for a service reference.
    func legacySmoothStreamURL(for serviceRef: String) -> URL? {
        media?.smoothStream(serviceRef: serviceRef)
    }

    /// A preparation client, when a backend address has been configured.
    ///
    /// `nil` without one: preparation runs against xg2g, so the direct receiver route
    /// has nothing to prepare and the player falls back to starting a channel outright.
    func makeZapPreparationClient() -> ZapPreparationClient? {
        guard let api else { return nil }
        return ZapPreparationClient(api: api, clientID: Self.zapClientID)
    }

    // MARK: - Playback Session Lifecycle

    func play(_ channel: Channel) async {
        guard let playback else { return }
        await stopPlayback()

        do {
            liveStream = try await playback.startLive(
                serviceRef: channel.serviceRef,
                qualityPreference: qualityPreference.rawValue
            )
            lastError = nil
        } catch {
            handle(error)
        }
    }

    func stopPlayback() async {
        guard let stream = liveStream else { return }
        liveStream = nil
        await playback?.stopLive(sessionID: stream.sessionID)
    }

    /// Starts or connects to an HLS Timeshift stream for the specified channel.
    func startTimeshift(for channel: Channel) async throws -> LiveStream? {
        guard let playback else { return nil }
        if let existing = liveStream, playingChannel?.id == channel.id {
            return existing
        }
        await stopPlayback()
        let stream = try await playback.startLive(
            serviceRef: channel.serviceRef,
            qualityPreference: qualityPreference.rawValue
        )
        self.liveStream = stream
        self.playingChannel = channel
        return stream
    }

    /// Releases any active HLS timeshift session on the backend.
    func stopTimeshift() async {
        await stopPlayback()
    }

    func heartbeat(sessionID: String) async throws {
        try await playback?.heartbeat(sessionID: sessionID)
    }
}
