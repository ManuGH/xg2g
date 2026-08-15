// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Observation
import UIKit

/// Remembers which server this app is pointed at.
///
/// `UserDefaults`, not the Keychain: an address is configuration, not a secret,
/// and putting it in the Keychain would make it survive a reinstall — which is
/// precisely what `CredentialStore.prepareForLaunch` exists to prevent for the
/// credentials that go with it.
struct ServerAddressStore: Sendable {

    private let key = "io.github.manugh.xg2g.serverAddress"
    nonisolated(unsafe) private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    func load() -> ServerAddress? {
        guard let raw = defaults.string(forKey: key) else { return nil }
        // Stored by us, so parsed strictly: a value that no longer parses is a
        // bug or tampering, not something to repair silently.
        return try? ServerAddressParser.parseTrusted(raw)
    }

    func save(_ address: ServerAddress) {
        defaults.set(address.rootURL.absoluteString, forKey: key)
    }

    func clear() {
        defaults.removeObject(forKey: key)
    }
}

/// What the app is currently able to do.
///
/// One enum rather than a set of booleans: `isConfigured && !isEnrolled` and
/// friends are exactly the combinations that produce a screen nobody designed.
enum AppState: Equatable, Sendable {
    /// No server yet — the setup screen.
    case needsServer
    /// Server known, this device not paired with it.
    case needsPairing
    /// Ready. The auth layer is invisible from here on.
    case ready
    /// The server retired this device's credentials. Terminal until re-paired;
    /// never a retry.
    case needsRePairing
}

/// Composes the app: one server, one identity, one set of coordinators.
///
/// Views read state from here and call intents on it. They never see a
/// credential, a device key, or a DPoP proof — the whole point of the auth work
/// is that nothing above this line knows it happened.
@Observable
@MainActor
final class AppModel {

    private(set) var state: AppState = .needsServer
    private(set) var channels: [Channel] = []
    private(set) var schedule: [String: NowNext] = [:]
    private(set) var isLoadingChannels = false
    private(set) var lastError: String?

    /// The stream currently handed to the player, if any.
    private(set) var liveStream: LiveStream?

    private let addressStore: ServerAddressStore
    private let credentials: CredentialStore
    private let keyStore: DeviceKeyStore

    private var address: ServerAddress?
    private var identity: ServerIdentity?
    private var channelRepository: ChannelRepository?
    private var playback: PlaybackCoordinator?
    private var session: SessionCoordinator?
    private var enrollment: EnrollmentCoordinator?

    init(
        addressStore: ServerAddressStore = ServerAddressStore(),
        credentials: CredentialStore = KeychainCredentialStore(backend: SecItemKeychainBackend()),
        keyStore: DeviceKeyStore = SecureEnclaveDeviceKeyStore()
    ) {
        self.addressStore = addressStore
        self.credentials = credentials
        self.keyStore = keyStore
    }

    // MARK: - Launch

    /// Must run before anything reads a credential: on a fresh install this is
    /// what purges Keychain material left by a previous installation.
    func start() async {
        do {
            try await credentials.prepareForLaunch()
        } catch {
            lastError = "Stored credentials could not be opened."
            return
        }

        guard let stored = addressStore.load() else {
            state = .needsServer
            return
        }
        configure(with: stored)

        guard let identity,
              (try? await credentials.deviceGrant(for: identity)) != nil
        else {
            state = .needsPairing
            return
        }

        state = .ready
        await loadChannels()
    }

    // MARK: - Setup

    /// Accepts what a human typed. This is the one place lenient parsing is
    /// allowed; everything downstream deals in a parsed address.
    func useServer(_ typed: String) async {
        guard let parsed = try? ServerAddressParser.parseUserEntered(typed) else {
            lastError = "That does not look like a server address."
            return
        }
        lastError = nil
        addressStore.save(parsed)
        configure(with: parsed)
        state = .needsPairing
    }

    private func configure(with address: ServerAddress) {
        let identity = ServerIdentity.address(address)
        self.address = address
        self.identity = identity

        let authorized = HTTPAPIClient(
            address: address,
            authorizer: DPoPRequestAuthorizer(identity: identity, credentials: credentials, keyStore: keyStore)
        )
        let refreshClient = HTTPAPIClient(
            address: address,
            authorizer: DeviceProofAuthorizer(keyStore: keyStore)
        )

        channelRepository = ChannelRepository(api: authorized)
        playback = PlaybackCoordinator(address: address, api: authorized)
        session = SessionCoordinator(identity: identity, api: refreshClient, credentials: credentials)
        enrollment = EnrollmentCoordinator(
            identity: identity,
            api: HTTPAPIClient(address: address),
            keyStore: keyStore,
            credentials: credentials
        )
    }

    // MARK: - Pairing

    func beginPairing() async -> EnrollmentCoordinator.Invitation? {
        guard let enrollment else { return nil }
        do {
            return try await enrollment.startPairing(deviceName: Self.deviceName, deviceType: Self.deviceType)
        } catch {
            lastError = "The server would not start a pairing."
            return nil
        }
    }

    func pairingStatus() async -> PairingStatus? {
        try? await enrollment?.pairingStatus()
    }

    func completePairing() async {
        guard let enrollment else { return }
        do {
            _ = try await enrollment.completeEnrollment()
            await session?.resetAfterReenrollment()
            state = .ready
            lastError = nil
            await loadChannels()
        } catch {
            lastError = "Pairing could not be completed."
        }
    }

    // MARK: - Channels

    func loadChannels() async {
        guard let channelRepository, !isLoadingChannels else { return }
        isLoadingChannels = true
        defer { isLoadingChannels = false }

        do {
            // A refresh before the list, so an expired token is renewed once
            // rather than surfacing as a failed screen.
            _ = try? await session?.validSession()

            let loaded = try await channelRepository.channels()
            channels = loaded
            lastError = nil
            schedule = (try? await channelRepository.nowNext(for: loaded.map(\.serviceRef))) ?? [:]
        } catch {
            handle(error)
        }
    }

    // MARK: - Playback

    func play(_ channel: Channel) async {
        guard let playback else { return }
        await stopPlayback()

        do {
            liveStream = try await playback.startLive(serviceRef: channel.serviceRef)
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

    // MARK: - Errors

    /// Turns a failure into either a message or a state change.
    ///
    /// `DEVICE_REAUTH_REQUIRED` is the one that changes state: no token this
    /// device can produce will be accepted, so the only way forward is pairing
    /// again — never a retry, and never a refresh loop.
    private func handle(_ error: any Error) {
        if let apiError = error as? APIError {
            Task { await session?.noteRequestFailure(apiError) }
            if case .problem(let problem) = apiError,
               problem.code == SessionCoordinator.deviceReauthRequiredCode {
                state = .needsRePairing
                lastError = "This device has to be paired again."
                return
            }
        }
        if case SessionCoordinator.Failure.reauthenticationRequired = error {
            state = .needsRePairing
            lastError = "This device has to be paired again."
            return
        }
        lastError = "The server could not be reached."
    }

    // MARK: - Device description

    private static var deviceType: DeviceType {
        UIDevice.current.userInterfaceIdiom == .pad ? .iPad : .iPhone
    }

    /// The name the household list will show. `UIDevice.name` is the value the
    /// user chose for their device, which is exactly what a "which device is
    /// this?" list should say.
    private static var deviceName: String {
        let name = UIDevice.current.name.trimmingCharacters(in: .whitespaces)
        return name.isEmpty ? "iPhone" : name
    }
}
