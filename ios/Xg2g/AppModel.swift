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

/// Top-level sections in the Broadcast Console.
enum Tab: String, CaseIterable, Identifiable, Sendable {
    case liveTV = "Live TV"
    case recordings = "Aufnahmen"
    case timers = "Timer"
    case settings = "Einstellungen"

    var id: String { rawValue }

    var systemImage: String {
        switch self {
        case .liveTV: return "tv"
        case .recordings: return "play.rectangle.on.rectangle"
        case .timers: return "clock"
        case .settings: return "gearshape"
        }
    }
}

/// Composes the app: one server, one identity, one set of coordinators.
@Observable
@MainActor
final class AppModel {

    private(set) var state: AppState = .needsServer
    var selectedTab: Tab = .liveTV

    // MARK: - Live TV State
    private(set) var channels: [Channel] = []
    private(set) var bouquets: [Bouquet] = []
    var selectedBouquet: Bouquet?
    var searchQuery: String = ""
    private(set) var schedule: [String: NowNext] = [:]
    private(set) var isLoadingChannels = false

    // MARK: - Recordings & Timers State
    private(set) var recordings: [Recording] = []
    private(set) var isLoadingRecordings = false
    private(set) var timers: [DVRTimer] = []
    private(set) var isLoadingTimers = false

    private(set) var lastError: String?

    /// The stream currently handed to the player, if any.
    private(set) var liveStream: LiveStream?

    private let addressStore: ServerAddressStore
    private let credentials: CredentialStore
    private let keyStore: DeviceKeyStore

    private var address: ServerAddress?
    private var identity: ServerIdentity?
    private var channelRepository: ChannelRepository?
    private var recordingsRepository: RecordingsRepository?
    private var timersRepository: TimersRepository?
    private var playback: PlaybackCoordinator?
    private var session: SessionCoordinator?
    private var enrollment: EnrollmentCoordinator?
    private var revokeCoordinator: RevokeCoordinator?

    var serverURLString: String {
        address?.rootURL.absoluteString ?? "–"
    }

    var currentDeviceName: String {
        Self.deviceName
    }

    var currentDeviceType: String {
        Self.deviceType.rawValue
    }

    enum StreamingQualityPreference: String, CaseIterable, Identifiable, Sendable {
        case auto = "auto"
        case passthrough = "passthrough"
        case dataSaver = "dataSaver"

        var id: String { rawValue }

        var displayName: String {
            switch self {
            case .auto: return "Automatisch (WLAN: 1:1 Direct / 5G: ABR)"
            case .passthrough: return "Immer Original (Verlustfrei 1:1)"
            case .dataSaver: return "Datensparmodus (HEVC/AV1)"
            }
        }
    }

    var qualityPreference: StreamingQualityPreference {
        get {
            let raw = UserDefaults.standard.string(forKey: "xg2g.quality_preference") ?? "auto"
            return StreamingQualityPreference(rawValue: raw) ?? .auto
        }
        set {
            UserDefaults.standard.set(newValue.rawValue, forKey: "xg2g.quality_preference")
        }
    }

    var playerGesturesEnabled: Bool {
        get {
            UserDefaults.standard.bool(forKey: "xg2g.player_gestures_enabled")
        }
        set {
            UserDefaults.standard.set(newValue, forKey: "xg2g.player_gestures_enabled")
        }
    }

    static let favoritesBouquetID = "xg2g_local_favorites"

    private(set) var favoriteChannelIDs: Set<String> = {
        let stored = UserDefaults.standard.stringArray(forKey: "xg2g.favorites") ?? []
        return Set(stored)
    }()

    func toggleFavorite(_ channel: Channel) {
        if favoriteChannelIDs.contains(channel.id) {
            favoriteChannelIDs.remove(channel.id)
        } else {
            favoriteChannelIDs.insert(channel.id)
        }
        UserDefaults.standard.set(Array(favoriteChannelIDs), forKey: "xg2g.favorites")
    }

    func isFavorite(_ channel: Channel) -> Bool {
        favoriteChannelIDs.contains(channel.id)
    }

    enum TimeFilter: String, CaseIterable, Identifiable, Sendable {
        case now = "Jetzt"
        case primeTime = "Heute 20:15"

        var id: String { rawValue }
    }

    var selectedTimeFilter: TimeFilter = .now
    var playingChannel: Channel?

    /// Channels filtered by selected bouquet, favorites, and search query.
    var filteredChannels: [Channel] {
        var result = channels

        if selectedBouquet?.id == Self.favoritesBouquetID {
            result = result.filter { favoriteChannelIDs.contains($0.id) }
        }

        let query = searchQuery.trimmingCharacters(in: .whitespaces).lowercased()
        if !query.isEmpty {
            result = result.filter { channel in
                channel.name.lowercased().contains(query) ||
                (channel.number?.contains(query) ?? false) ||
                (schedule[channel.serviceRef]?.now?.title.lowercased().contains(query) ?? false)
            }
        }
        return result
    }

    /// Next channel in the active list (wraps around).
    func channelAfter(_ channel: Channel) -> Channel? {
        let list = filteredChannels
        guard !list.isEmpty else { return nil }
        guard let idx = list.firstIndex(where: { $0.id == channel.id }) else { return list.first }
        let nextIdx = (idx + 1) % list.count
        return list[nextIdx]
    }

    /// Previous channel in the active list (wraps around).
    func channelBefore(_ channel: Channel) -> Channel? {
        let list = filteredChannels
        guard !list.isEmpty else { return nil }
        guard let idx = list.firstIndex(where: { $0.id == channel.id }) else { return list.last }
        let prevIdx = (idx - 1 + list.count) % list.count
        return list[prevIdx]
    }

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
        await loadInitialData()
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

        let sessionCoord = SessionCoordinator(identity: identity, api: refreshClient, credentials: credentials)
        self.session = sessionCoord

        channelRepository = ChannelRepository(api: authorized)
        recordingsRepository = RecordingsRepository(api: authorized)
        timersRepository = TimersRepository(api: authorized)
        playback = PlaybackCoordinator(address: address, api: authorized)
        enrollment = EnrollmentCoordinator(
            identity: identity,
            api: HTTPAPIClient(address: address),
            keyStore: keyStore,
            credentials: credentials
        )
        revokeCoordinator = RevokeCoordinator(
            identity: identity,
            api: authorized,
            credentials: credentials,
            keyStore: keyStore,
            session: sessionCoord
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
            await loadInitialData()
        } catch {
            lastError = "Pairing could not be completed."
        }
    }

    // MARK: - Data Loading

    func loadInitialData() async {
        await loadBouquets()
        await loadChannels()
        await loadRecordings()
        await loadTimers()
    }

    func loadBouquets() async {
        guard let channelRepository else { return }
        do {
            bouquets = try await channelRepository.bouquets()
        } catch {
            // Bouquets are optional metadata; failure does not block the channel list
        }
    }

    func selectBouquet(_ bouquet: Bouquet?) async {
        selectedBouquet = bouquet
        await loadChannels(bouquet: bouquet?.name)
    }

    func loadChannels(bouquet: String? = nil) async {
        guard let channelRepository, !isLoadingChannels else { return }
        isLoadingChannels = true
        defer { isLoadingChannels = false }

        do {
            _ = try? await session?.validSession()

            let loaded = try await channelRepository.channels(bouquet: bouquet)
            channels = loaded
            lastError = nil
            schedule = (try? await channelRepository.nowNext(for: loaded.map(\.serviceRef))) ?? [:]
        } catch {
            handle(error)
        }
    }

    func loadRecordings() async {
        guard let recordingsRepository, !isLoadingRecordings else { return }
        isLoadingRecordings = true
        defer { isLoadingRecordings = false }

        do {
            _ = try? await session?.validSession()
            recordings = try await recordingsRepository.recordings()
            lastError = nil
        } catch {
            handle(error)
        }
    }

    func loadTimers() async {
        guard let timersRepository, !isLoadingTimers else { return }
        isLoadingTimers = true
        defer { isLoadingTimers = false }

        do {
            _ = try? await session?.validSession()
            timers = try await timersRepository.timers()
            lastError = nil
        } catch {
            handle(error)
        }
    }

    // MARK: - DVR & Timer Management

    func scheduleProgramTimer(
        channel: Channel,
        entry: NowNext.Entry,
        leadMinutes: Int = 3,
        trailMinutes: Int = 7
    ) async -> Bool {
        guard let timersRepository else { return false }
        let begin = entry.start.addingTimeInterval(TimeInterval(-leadMinutes * 60))
        let end = entry.end.addingTimeInterval(TimeInterval(trailMinutes * 60))
        do {
            try await timersRepository.createTimer(
                serviceRef: channel.serviceRef,
                name: entry.title,
                description: entry.description,
                begin: begin,
                end: end
            )
            await loadTimers()
            return true
        } catch {
            handle(error)
            return false
        }
    }

    func recordLiveNow(channel: Channel, durationMinutes: Int = 120) async -> Bool {
        guard let timersRepository else { return false }
        let entry = schedule[channel.serviceRef]?.now
        let title = entry?.title ?? "\(channel.name) Sofortaufnahme"
        let begin = Date()
        let end = entry?.end ?? begin.addingTimeInterval(TimeInterval(durationMinutes * 60))
        do {
            try await timersRepository.createTimer(
                serviceRef: channel.serviceRef,
                name: title,
                description: entry?.description,
                begin: begin,
                end: end.addingTimeInterval(300)
            )
            await loadTimers()
            return true
        } catch {
            handle(error)
            return false
        }
    }

    func deleteTimer(_ timer: DVRTimer) async {
        guard let timersRepository else { return }
        do {
            try await timersRepository.deleteTimer(id: timer.id)
            timers.removeAll { $0.id == timer.id }
        } catch {
            handle(error)
        }
    }

    func deleteRecording(_ recording: Recording) async {
        guard let recordingsRepository else { return }
        do {
            try await recordingsRepository.deleteRecording(id: recording.id)
            recordings.removeAll { $0.id == recording.id }
        } catch {
            handle(error)
        }
    }

    // MARK: - Playback

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

    // MARK: - Revoke / Sign Out

    func disconnectServer() async {
        guard let revokeCoordinator else { return }
        do {
            try await revokeCoordinator.revokeThisDevice(destroyingDeviceKey: true)
        } catch {
            // Even if remote revoke failed, clear local state on explicit sign out
            if let identity {
                try? await credentials.forgetServer(identity)
            }
            try? await keyStore.destroyKey()
        }
        addressStore.clear()
        state = .needsServer
        channels = []
        bouquets = []
        recordings = []
        timers = []
        lastError = nil
    }

    // MARK: - Errors

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

    private static var deviceName: String {
        let name = UIDevice.current.name.trimmingCharacters(in: .whitespaces)
        return name.isEmpty ? "iPhone" : name
    }
}
