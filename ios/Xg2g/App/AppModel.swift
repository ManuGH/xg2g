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
    case home = "Home"
    case liveTV = "TV & Guide"
    case guide = "Programm"
    case recordings = "Aufnahmen"
    case timers = "Timer"
    case search = "Suche"
    case settings = "Einstellungen"

    var id: String { rawValue }

    var systemImage: String {
        switch self {
        case .home: return "sparkles.tv"
        case .liveTV: return "tv"
        case .guide: return "calendar.badge.clock"
        case .recordings: return "play.rectangle.on.rectangle"
        case .timers: return "clock"
        case .search: return "magnifyingglass"
        case .settings: return "gearshape"
        }
    }

    var shortcutCharacter: Character? {
        switch self {
        case .home: return "1"
        case .liveTV: return "2"
        case .guide: return "2"
        case .recordings: return "3"
        case .timers: return "4"
        case .search: return "5"
        case .settings: return ","
        }
    }

    /// The tabs a platform lists in its top-level navigation.
    ///
    /// Consolidates Live TV and EPG into a unified "TV & Guide" screen.
    static var navigationCases: [Tab] {
#if os(tvOS)
        [.home, .liveTV, .recordings, .search, .settings]
#else
        [.home, .liveTV, .recordings, .settings]
#endif
    }
}

/// An upcoming rerun/repeat airing of a show on any channel.
struct RerunItem: Identifiable, Sendable {
    var id: String { "\(channel.id)_\(entry.id)_\(entry.start.timeIntervalSince1970)" }
    let channel: Channel
    let entry: NowNext.Entry

    var formattedRelativeTime: String {
        let calendar = Calendar.current
        if calendar.isDateInToday(entry.start) {
            return "Heute, \(entry.formattedStartTime) Uhr"
        } else if calendar.isDateInTomorrow(entry.start) {
            return "Morgen, \(entry.formattedStartTime) Uhr"
        } else {
            let f = DateFormatter()
            f.locale = Locale(identifier: "de_DE")
            f.dateFormat = "E, d. MMM • HH:mm"
            return "\(f.string(from: entry.start)) Uhr"
        }
    }
}

/// Composes the app: one server, one identity, one set of coordinators.
@Observable
@MainActor
final class AppModel {

    private(set) var state: AppState = .needsServer
    var selectedTab: Tab = .home

    // MARK: - Live TV State
    var channels: [Channel] = [] { didSet { contentRevision &+= 1 } }
    private(set) var bouquets: [ChannelBouquet] = []
    var selectedBouquet: ChannelBouquet?
    var searchQuery: String = ""
    var schedule: [String: NowNext] = [:] { didSet { contentRevision &+= 1 } }
    var fullEpg: [String: [NowNext.Entry]] = [:] { didSet { contentRevision &+= 1 } }
    private(set) var isLoadingChannels = false
    var lastDataRefreshTime: Date?

    /// Bumped whenever the channel list, Now/Next schedule or full EPG is replaced.
    ///
    /// Views that derive an expensive projection from this data key their
    /// recomputation on this counter instead of diffing the collections
    /// themselves — a refresh that returns the same number of channels still
    /// has to invalidate them.
    private(set) var contentRevision: Int = 0

    // MARK: - Recordings & Timers State
    var recordings: [Recording] = []
    var isLoadingRecordings = false
    var timers: [DVRTimer] = []
    var isLoadingTimers = false

    var lastError: String?

    /// The stream currently handed to the player, if any.
    var liveStream: LiveStream?

    private let addressStore: ServerAddressStore
    private let credentials: CredentialStore
    private let keyStore: DeviceKeyStore

    private var address: ServerAddress?
    private var identity: ServerIdentity?
    var channelRepository: ChannelRepository?
    var recordingsRepository: RecordingsRepository?
    var timersRepository: TimersRepository?
    var playback: PlaybackCoordinator?
    var session: SessionCoordinator?
    private var enrollment: EnrollmentCoordinator?
    private var revokeCoordinator: RevokeCoordinator?
    var api: (any APIClient)?

    var serverURLString: String {
        address?.rootURL.absoluteString ?? "–"
    }

    /// The configured deployment, for the views that hand it to the transport.
    ///
    /// Exposed as a `ServerAddress` rather than as a string: a view that
    /// received text had to decide what a missing scheme meant, and three of
    /// them decided it separately.
    var serverAddress: ServerAddress? { address }

    var currentDeviceName: String {
        Self.deviceName
    }

    var currentDeviceType: String {
        Self.deviceType.rawValue
    }

    var appVersion: String {
        Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "2.0.0"
    }

    var buildNumber: String {
        Bundle.main.infoDictionary?["CFBundleVersion"] as? String ?? "1"
    }

    @MainActor
    func clearCaches() async {
        filteredChannelsCache = nil
        bouquetChannelsCache.removeAll()
        URLCache.shared.removeAllCachedResponses()
        await loadInitialData()
    }

    enum StreamingQualityPreference: String, CaseIterable, Identifiable, Sendable {
        case auto = "auto"
        case passthrough = "passthrough"
        case qsvNormalize = "qsvNormalize"
        case dataSaver = "dataSaver"

        var id: String { rawValue }

        /// User-friendly label for standard settings UI
        var displayName: String {
            switch self {
            case .auto: return "Automatisch"
            case .passthrough: return "Originalqualität"
            case .qsvNormalize: return "Kompatibilität"
            case .dataSaver: return "Datensparen"
            }
        }

        /// Subtitle / explanation
        var summary: String {
            switch self {
            case .auto: return "xg2g ermittelt die beste Balance aus Qualität und Latenz (Empfohlen)"
            case .passthrough: return "1:1 Bitstream ohne Video-Transkodierung"
            case .qsvNormalize: return "Standardisiertes HLS mit maximaler Gerätekompatibilität"
            case .dataSaver: return "Bandbreitenoptimiertes Streaming (HEVC/AV1) für unterwegs"
            }
        }

        /// Technical pipeline name for developer / expert diagnostic view
        var technicalDetails: String {
            switch self {
            case .auto: return "Copy fMP4 (Original Video + AAC)"
            case .passthrough: return "Copy MPEG-TS (1:1 Bitstream Passthrough)"
            case .qsvNormalize: return "QSV Normalisierung (Closed-GOP 50fps fMP4)"
            case .dataSaver: return "Transcode (HEVC / AV1 Datensparmodus)"
            }
        }
    }

    var qualityPreference: StreamingQualityPreference = {
        let raw = UserDefaults.standard.string(forKey: "xg2g.quality_preference") ?? "auto"
        return StreamingQualityPreference(rawValue: raw) ?? .auto
    }() {
        didSet {
            UserDefaults.standard.set(qualityPreference.rawValue, forKey: "xg2g.quality_preference")
        }
    }

    /// Which pipeline plays live television.
    ///
    /// The user can select a high-level intent:
    /// - `.auto`: xg2g dynamically picks the best path based on network, client capabilities, and server resources.
    /// - `.native`: Low-latency native player with direct bitstream delivery.
    /// - `.hls`: Feature-rich HLS server playback with Timeshift, restart, and remote access.
    enum PlaybackEngine: String, CaseIterable, Identifiable, Sendable {
        /// Automatically selected by xg2g planner based on network, capabilities and policy.
        case auto = "auto"
        /// Native hardware decode (VideoToolbox/Metal), minimum latency.
        case native = "native"
        /// HLS stream through xg2g server with Timeshift/DVR and adaptive bitrate.
        case hls = "hls"

        var id: String { rawValue }

        var displayName: String {
            switch self {
            case .auto: return "Automatisch"
            case .native: return "Native Live-TV"
            case .hls: return "Server-Streaming (HLS)"
            }
        }

        /// One line for the settings row.
        var summary: String {
            switch self {
            case .auto: return "xg2g wählt dynamisch den besten Weg für dein Gerät und Netzwerk (Empfohlen)"
            case .native: return "xg2g liefert den Sender möglichst unverändert an den nativen Player"
            case .hls: return "Ermöglicht Timeshift/Pause, externe Nutzung und adaptive Bitrate"
            }
        }

        /// What the viewer gains and gives up, in their terms.
        var tradeoff: (gains: [String], costs: [String]) {
            switch self {
            case .auto:
                return (
                    gains: [
                        "Der xg2g Planner wählt automatisch die optimale Pipeline",
                        "Verlustfreies Streaming und niedrigste Latenz im Heimnetz",
                        "Nahtloser Wechsel zu adaptivem Streaming unterwegs"
                    ],
                    costs: [
                        "Timeshift/Pause steht nur zur Verfügung, wenn HLS aktiv ist"
                    ]
                )
            case .native:
                return (
                    gains: [
                        "Bild und Ton möglichst unverändert mit minimaler Latenz",
                        "Deutlich schnelleres Umschalten (Hardware-Decoding)",
                        "Minimale Serverlast (keine Video-Transkodierung)",
                        "Näher am Live-Signal"
                    ],
                    costs: [
                        "Kein Pausieren oder Zurückspulen (Timeshift)",
                        "Nur im selben Netzwerk wie der Ingest verfügbar",
                        "Benötigt durchgehend die volle Bitrate des Senders"
                    ]
                )
            case .hls:
                return (
                    gains: [
                        "Live pausieren, zurückspulen und von Beginn ansehen (Timeshift)",
                        "Funktioniert auch zuverlässig außerhalb des Heimnetzes",
                        "Adaptive Qualität bei schwankender Bandbreite",
                        "AirPlay und System-Bildschirmübertragung"
                    ],
                    costs: [
                        "Höhere Latenz als bei Native Live-TV",
                        "Umschaltzeiten hängen von GOP-Segmenten ab"
                    ]
                )
            }
        }
    }

    var playbackEngine: PlaybackEngine = {
        let raw = UserDefaults.standard.string(forKey: "xg2g.playback_engine") ?? PlaybackEngine.auto.rawValue
        return PlaybackEngine(rawValue: raw) ?? .auto
    }() {
        didSet {
            UserDefaults.standard.set(playbackEngine.rawValue, forKey: "xg2g.playback_engine")
        }
    }

    @ObservationIgnored private var _playbackManager: PlaybackManager?

    var playbackManager: PlaybackManager {
        if let existing = _playbackManager {
            return existing
        }
        let pm = PlaybackManager(
            preparationsProvider: { [weak self] in
                self?.makeZapPreparationClient()
            },
            streamURL: { [weak self] serviceRef in
                self?.liveStreamURL(for: serviceRef)
            }
        )
        _playbackManager = pm
        return pm
    }

    /// Human-readable active playback plan description for settings & diagnostics.
    ///
    /// NOTE: Currently provides the baseline intent-derived description.
    /// In the target architecture, this will be populated directly from live session telemetry
    /// and the central planner decision token (actual video/audio codecs, transcode status, network path).
    var activePlaybackPlanDescription: String {
        switch playbackEngine {
        case .native:
            return "Native TS · Direkt (Minimale Serverlast)"
        case .auto:
            switch qualityPreference {
            case .auto:
                return "Auto Plan · Dynamische Pipeline & Profil"
            case .passthrough:
                return "Auto Plan · Direkt"
            case .qsvNormalize:
                return "Auto Plan · QSV Normalisierung (50fps)"
            case .dataSaver:
                return "Auto Plan · Transcode Datensparmodus (HEVC)"
            }
        case .hls:
            switch qualityPreference {
            case .auto:
                return "HLS fMP4 · Auto Video Copy / Remux"
            case .passthrough:
                return "HLS TS · Direkt"
            case .qsvNormalize:
                return "HLS fMP4 · QSV Normalisierung (50fps)"
            case .dataSaver:
                return "HLS fMP4 · Transcode Datensparmodus (HEVC)"
            }
        }
    }

    /// Whether the player aspect ratio toggle exposes advanced/legacy formats (16:9, 4:3, Cinemascope)
    /// in addition to Standard and Bildschirm füllen.
    var enableAdvancedAspectRatios: Bool = {
        UserDefaults.standard.bool(forKey: "xg2g.enable_advanced_aspect_ratios")
    }() {
        didSet {
            UserDefaults.standard.set(enableAdvancedAspectRatios, forKey: "xg2g.enable_advanced_aspect_ratios")
        }
    }

    /// Where the receiver hands out its unmodified streams, e.g. `http://receiver.local:8001`.
    ///
    /// Kept separate from the xg2g server address on purpose: direct playback
    /// bypasses the server and talks to the receiver, and nothing tells the app
    /// where that is — the intent response carries a session, not a receiver.
    var receiverStreamBaseURL: String = {
        UserDefaults.standard.string(forKey: "xg2g.receiver_stream_base_url") ?? ""
    }() {
        didSet {
            UserDefaults.standard.set(receiverStreamBaseURL, forKey: "xg2g.receiver_stream_base_url")
        }
    }

    /// Media URLs for the configured deployment, or `nil` while there is none.
    ///
    /// Nothing here composes a URL any more: the transport owns what an API
    /// path looks like, and an unconfigured app has no stream to offer rather
    /// than a hard-coded one on somebody else's network.
    var media: MediaEndpoints? {
        address.map(MediaEndpoints.init(address:))
    }

    /// A client identity for the preparation endpoints.
    ///
    /// Stable for the life of the installation and meaningless outside it: a random
    /// value made once, not the device identifier and nothing derived from the user.
    /// The backend needs it only to answer "is this the same client" - which channel
    /// change supersedes which, and who may commit one - and that question needs no
    /// idea who the person is.
    static var zapClientID: String {
        let key = "xg2g.zap.clientID"
        if let existing = UserDefaults.standard.string(forKey: key), !existing.isEmpty {
            return existing
        }
        let fresh = "sterling-" + UUID().uuidString.prefix(12).lowercased()
        UserDefaults.standard.set(fresh, forKey: key)
        return fresh
    }

    /// Whether direct playback can actually run right now.
    var isDirectPlaybackAvailable: Bool {
        !receiverStreamBaseURL.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
    }

    var playerGesturesEnabled: Bool = {
        UserDefaults.standard.object(forKey: "xg2g.player_gestures_enabled") as? Bool ?? true
    }() {
        didSet {
            UserDefaults.standard.set(playerGesturesEnabled, forKey: "xg2g.player_gestures_enabled")
        }
    }

    enum EpgPreviewHours: Int, CaseIterable, Identifiable, Sendable {
        case twoHours = 2
        case fourHours = 4
        case sixHours = 6
        case twelveHours = 12
        case twentyFourHours = 24

        var id: Int { rawValue }
        var displayName: String {
            switch self {
            case .twoHours: return "2 Stunden"
            case .fourHours: return "4 Stunden (Empfohlen)"
            case .sixHours: return "6 Stunden"
            case .twelveHours: return "12 Stunden"
            case .twentyFourHours: return "24 Stunden (1 Tag)"
            }
        }
    }

    var epgPreviewHours: EpgPreviewHours = {
        let raw = UserDefaults.standard.integer(forKey: "xg2g.epg_preview_hours")
        return EpgPreviewHours(rawValue: raw) ?? .fourHours
    }() {
        didSet {
            UserDefaults.standard.set(epgPreviewHours.rawValue, forKey: "xg2g.epg_preview_hours")
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

    var favoriteChannels: [Channel] {
        channels.filter { favoriteChannelIDs.contains($0.id) }
    }

    // MARK: - Recently Watched (Google TV "Jump Back In")
    private(set) var recentChannelIDs: [String] = {
        UserDefaults.standard.stringArray(forKey: "xg2g.recents") ?? []
    }()

    func recordChannelPlayback(_ channel: Channel) {
        var recents = recentChannelIDs.filter { $0 != channel.id }
        recents.insert(channel.id, at: 0)
        if recents.count > 8 {
            recents = Array(recents.prefix(8))
        }
        recentChannelIDs = recents
        UserDefaults.standard.set(recents, forKey: "xg2g.recents")
    }

    var recentChannels: [Channel] {
        recentChannelIDs.compactMap { id in
            channels.first { $0.id == id }
        }
    }

    // MARK: - Recording Playback Resume Tracking
    var recordingProgress: [String: Double] = {
        let dict = UserDefaults.standard.dictionary(forKey: "xg2g.recordingProgress") as? [String: Double] ?? [:]
        return dict
    }()

    func currentAccessToken() async throws -> String? {
        try await session?.validSession().token
    }

    enum TimeFilter: Hashable, Identifiable, Sendable {
        case now
        case next
        case primeTimeTonight
        case lateNightTonight
        case day(Date)

        var id: String {
            switch self {
            case .now: return "now"
            case .next: return "next"
            case .primeTimeTonight: return "prime_2015"
            case .lateNightTonight: return "late_2200"
            case .day(let date):
                let f = DateFormatter()
                f.dateFormat = "yyyyMMdd"
                return "day_\(f.string(from: date))"
            }
        }

        var label: String {
            switch self {
            case .now: return "Jetzt"
            case .next: return "Gleich"
            case .primeTimeTonight: return "20:15"
            case .lateNightTonight: return "22:00"
            case .day(let date):
                let calendar = Calendar.current
                if calendar.isDateInToday(date) {
                    return "Heute"
                } else if calendar.isDateInTomorrow(date) {
                    return "Morgen"
                } else {
                    let f = DateFormatter()
                    f.locale = Locale(identifier: "de_DE")
                    f.dateFormat = "E, d. MMM"
                    return f.string(from: date)
                }
            }
        }

        var icon: String {
            switch self {
            case .now: return "play.circle.fill"
            case .next: return "clock.arrow.circlepath"
            case .primeTimeTonight: return "star.fill"
            case .lateNightTonight: return "moon.fill"
            case .day: return "calendar"
            }
        }
    }

    var selectedTimeFilter: TimeFilter = .now
    var selectedGenre: EPGGenre = .all
    var epgViewMode: EPGViewMode = .list
    var playingChannel: Channel? {
        get { playbackManager.currentChannel }
        set {
            if let newValue {
                recordChannelPlayback(newValue)
                playbackManager.play(channel: newValue, mode: .fullscreen)
            } else {
                playbackManager.stop()
            }
        }
    }

    /// Sleek essential quick time jumps (Live, 20:15, 22:00)
    var availableTimeFilters: [TimeFilter] {
        [.now, .primeTimeTonight, .lateNightTonight]
    }

    /// Identifies one filter configuration, so a repeated read can reuse its result.
    ///
    /// `contentRevision` stands in for `channels`, `schedule` and `fullEpg`:
    /// comparing the collections themselves on every read would cost as much as
    /// the filtering it is meant to avoid. Comparing the favourites set is cheap
    /// because an unchanged `Set` hits the identical-storage fast path.
    private struct FilterKey: Equatable {
        let revision: Int
        let bouquetID: String?
        let genre: EPGGenre
        let query: String
        let timeFilter: TimeFilter
        let favorites: Set<String>
        /// Minute bucket, and only when a filter actually resolves the running
        /// show — otherwise the result does not depend on the clock at all.
        let minuteBucket: Int
    }

    /// `@ObservationIgnored`: this is a cache of observable state, not state of
    /// its own. Writing it from the `filteredChannels` getter must not itself
    /// count as a mutation — views track the inputs read to build the key.
    @ObservationIgnored private var filteredChannelsCache: (key: FilterKey, value: [Channel])?

    /// Channels filtered by selected bouquet, favorites, genre, and search query (Single-pass optimized).
    ///
    /// Memoized: a channel zap calls `channelAfter`/`channelBefore`, and each of
    /// those used to rebuild the whole list — dedup set included — from scratch.
    var filteredChannels: [Channel] {
        let resolvesCurrentShow = selectedGenre != .all
            || !searchQuery.trimmingCharacters(in: .whitespaces).isEmpty

        let key = FilterKey(
            revision: contentRevision,
            bouquetID: selectedBouquet?.id,
            genre: selectedGenre,
            query: searchQuery,
            timeFilter: selectedTimeFilter,
            favorites: favoriteChannelIDs,
            minuteBucket: resolvesCurrentShow ? Int(Date.now.timeIntervalSince1970 / 60) : 0
        )

        if let cached = filteredChannelsCache, cached.key == key {
            return cached.value
        }

        let result = computeFilteredChannels()
        filteredChannelsCache = (key, result)
        return result
    }

    private func computeFilteredChannels() -> [Channel] {
        let isFavBouquet = selectedBouquet?.id == Self.favoritesBouquetID
        let favSet = isFavBouquet ? favoriteChannelIDs : nil
        let hasGenreFilter = selectedGenre != .all
        let query = searchQuery.trimmingCharacters(in: .whitespaces).lowercased()
        let hasQuery = !query.isEmpty
        let activeTimeFilter = selectedTimeFilter
        let activeGenre = selectedGenre

        var result: [Channel] = []
        result.reserveCapacity(channels.count)
        var seen = Set<String>()

        for channel in channels {
            // 1. Deduplication
            guard seen.insert(channel.serviceRef).inserted else { continue }

            // 2. Favorites check
            if let favSet, !favSet.contains(channel.id) { continue }

            // 3. Resolve show only if needed for genre or search
            let currentShow: NowNext.Entry?
            if hasGenreFilter || hasQuery {
                currentShow = show(for: channel, at: activeTimeFilter)
            } else {
                currentShow = nil
            }

            // 4. Genre check
            if hasGenreFilter {
                let matches: Bool
                if let currentShow {
                    matches = currentShow.matches(genre: activeGenre, channelName: channel.name)
                } else {
                    matches = EPGGenreClassifier.channelMatches(genre: activeGenre, channelName: channel.name)
                }
                guard matches else { continue }
            }

            // 5. Search query check
            if hasQuery {
                let nameMatches = channel.name.lowercased().contains(query)
                let numberMatches = channel.number?.contains(query) ?? false
                let titleMatches = currentShow?.title.lowercased().contains(query) ?? false
                let descMatches = currentShow?.description?.lowercased().contains(query) ?? false
                guard nameMatches || numberMatches || titleMatches || descMatches else { continue }
            }

            result.append(channel)
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
        if CommandLine.arguments.contains("--tab-recordings") {
            self.selectedTab = .recordings
        } else if CommandLine.arguments.contains("--tab-search") {
            self.selectedTab = .search
        }
    }

    // MARK: - Launch

    /// Must run before anything reads a credential: on a fresh install this is
    /// what purges Keychain material left by a previous installation.
    func start() async {
        TelemetryServer.shared.start()
#if DEBUG
        if CommandLine.arguments.contains("--demo-mode") || CommandLine.arguments.contains("--uitesting") {
            let ard = GuidePreviewData.channel("1", "Das Erste HD")
            let zdf = GuidePreviewData.channel("2", "ZDF HD")
            let sky = GuidePreviewData.channel("201", "Sky Sport Top Event")
            let puls = GuidePreviewData.channel("14", "PULS 24 HD")
            let fm4 = GuidePreviewData.channel("99", "FM4 Radio")
            self.channels = [ard, zdf, sky, puls, fm4]
            self.schedule = [
                ard.serviceRef: NowNext(serviceRef: ard.serviceRef, now: GuidePreviewData.entry("Tagesschau", startingMinutesFromNow: -10, lasting: 15), next: GuidePreviewData.entry("Tatort", startingMinutesFromNow: 5, lasting: 90)),
                zdf.serviceRef: NowNext(serviceRef: zdf.serviceRef, now: GuidePreviewData.entry("heute journal", startingMinutesFromNow: -20, lasting: 30), next: GuidePreviewData.entry("auslandsjournal", startingMinutesFromNow: 10, lasting: 30)),
                sky.serviceRef: NowNext(serviceRef: sky.serviceRef, now: GuidePreviewData.entry("UEFA Champions League", startingMinutesFromNow: -40, lasting: 120), next: nil),
                puls.serviceRef: NowNext(serviceRef: puls.serviceRef, now: GuidePreviewData.entry("Nachrichten", startingMinutesFromNow: -5, lasting: 20), next: nil),
                fm4.serviceRef: NowNext(serviceRef: fm4.serviceRef, now: GuidePreviewData.entry("Morning Show", startingMinutesFromNow: -30, lasting: 60), next: nil)
            ]
            if self.receiverStreamBaseURL.isEmpty {
                self.receiverStreamBaseURL = MediaEndpoints.debugDemoReceiverBaseURL
            }
            self.state = .ready
            return
        }
#endif
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

        let refreshClient = HTTPAPIClient(
            address: address,
            authorizer: DeviceProofAuthorizer(keyStore: keyStore)
        )

        let sessionCoord = SessionCoordinator(identity: identity, api: refreshClient, credentials: credentials)
        self.session = sessionCoord

        let authorized = HTTPAPIClient(
            address: address,
            authorizer: DPoPRequestAuthorizer(identity: identity, credentials: credentials, keyStore: keyStore, sessionCoordinator: sessionCoord)
        )
        self.api = authorized

        channelRepository = ChannelRepository(api: authorized, baseURL: address.rootURL)
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
            lastError = nil
            return try await enrollment.startPairing(deviceName: Self.deviceName, deviceType: Self.deviceType)
        } catch let apiErr as APIError {
            switch apiErr {
            case .problem(let prob):
                lastError = prob.detail ?? prob.title
            case .http(let status, _, let body):
                lastError = "Server antwortete mit HTTP \(status): \(body)"
            case .transport(let tr):
                switch tr {
                case .offline:
                    lastError = "Keine Netzwerkverbindung oder Server nicht erreichbar."
                case .timedOut:
                    lastError = "Zeitüberschreitung beim Verbinden mit \(serverURLString)."
                case .cannotConnect:
                    lastError = "Verbindung zu \(serverURLString) fehlgeschlagen. Bitte Server-Adresse prüfen."
                case .tls:
                    lastError = "Sichere TLS/HTTPS-Verbindung fehlgeschlagen."
                default:
                    lastError = "Netzwerkfehler: \(tr)"
                }
            case .unexpectedPayload(let payload):
                lastError = "Unerwartete Serverantwort (Status \(payload.status)): \(payload.bodyPreview)"
            case .invalidEndpoint(let path):
                lastError = "Ungültiger API-Pfad: \(path)"
            }
            return nil
        } catch {
            lastError = "Fehler bei der Kopplung: \(error.localizedDescription)"
            return nil
        }
    }

    func pairingStatus() async -> Xg2gContract.PairingStatus? {
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
            lastError = "Pairing could not be completed: \(error.localizedDescription)"
        }
    }

    func changeServer() {
        addressStore.clear()
        address = nil
        identity = nil
        enrollment = nil
        session = nil
        lastError = nil
        state = .needsServer
    }

    // MARK: - Data Loading

    func loadInitialData() async {
        await loadBouquets()
        await loadChannels()
        await loadRecordings()
        await loadTimers()
        lastDataRefreshTime = Date()
    }

    /// Called when the app returns from background/suspended state.
    func handleAppBecameActive() async {
        TelemetryServer.shared.restartAfterForeground()
        guard state == .ready else { return }

        let now = Date()
        let interval = lastDataRefreshTime.map { now.timeIntervalSince($0) } ?? Double.infinity

        if channels.isEmpty {
            await loadInitialData()
            return
        }

        // If it has been more than 60 seconds (or overnight) since the last refresh, refresh live content
        if interval >= 60 {
            await refreshLiveContent()
        }
    }

    /// Fast, comprehensive background refresh of Now/Next schedule, multi-day EPG, and DVR states.
    func refreshLiveContent() async {
        guard let channelRepository, state == .ready else { return }

        _ = try? await session?.validSession()

        // 1. Refresh live Now/Next immediately
        let targets = channels.map(\.serviceRef)
        if !targets.isEmpty {
            if let updated = try? await channelRepository.nowNext(for: targets) {
                if selectedBouquet == nil {
                    schedule = updated
                } else {
                    schedule.merge(updated) { _, new in new }
                }
            }
        }

        // 2. Refresh full EPG schedule
        if let epgUpdated = try? await channelRepository.epgSchedule(bouquet: selectedBouquet?.name) {
            if selectedBouquet == nil {
                fullEpg = epgUpdated
            } else {
                fullEpg.merge(epgUpdated) { _, new in new }
            }
        }

        lastDataRefreshTime = Date()

        // 3. Refresh timers and recordings in background
        if let tr = timersRepository, let list = try? await tr.timers() {
            timers = list
        }
        if let rr = recordingsRepository, let list = try? await rr.recordings() {
            recordings = list
        }
    }

    func loadBouquets() async {
        guard let channelRepository else { return }
        do {
            bouquets = try await channelRepository.bouquets()
        } catch {
            // Bouquets are optional metadata; failure does not block the channel list
        }
    }

    @ObservationIgnored private var bouquetChannelsCache: [String: [Channel]] = [:]

    func selectBouquet(_ bouquet: ChannelBouquet?) async {
        selectedBouquet = bouquet
        guard let bouquet, bouquet.id != Self.favoritesBouquetID else {
            // "Alle Sender" (nil) or "Favoriten" filter locally in memory in 0ms
            if bouquet == nil {
                if let all = bouquetChannelsCache["all"], channels.count != all.count {
                    channels = all
                }
                if fullEpg.isEmpty {
                    await loadChannels()
                }
            }
            return
        }

        // Instant switch if this bouquet was already loaded
        if let cached = bouquetChannelsCache[bouquet.id] {
            channels = cached
            return
        }

        await loadChannels(bouquet: bouquet.name, bouquetID: bouquet.id)
    }

    func loadChannels(bouquet: String? = nil, bouquetID: String? = nil) async {
        guard let channelRepository, !isLoadingChannels else { return }
        isLoadingChannels = true
        defer { isLoadingChannels = false }

        do {
            _ = try? await session?.validSession()

            let loaded = try await channelRepository.channels(bouquet: bouquet)
            if let bouquetID {
                bouquetChannelsCache[bouquetID] = loaded
            } else if bouquet == nil {
                bouquetChannelsCache["all"] = loaded
            }
            channels = loaded
            lastError = nil

            async let nowNextTask = (try? await channelRepository.nowNext(for: loaded.map(\.serviceRef))) ?? [:]
            async let epgTask = (try? await channelRepository.epgSchedule(bouquet: bouquet)) ?? [:]

            let (newSchedule, newEpg) = await (nowNextTask, epgTask)
            if bouquet == nil {
                schedule = newSchedule
                fullEpg = newEpg
            } else {
                schedule.merge(newSchedule) { _, new in new }
                fullEpg.merge(newEpg) { _, new in new }
            }
            lastDataRefreshTime = Date()
        } catch {
            handle(error)
        }
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

    func handle(_ error: any Error) {
        if let apiError = error as? APIError {
            Task { await session?.noteRequestFailure(apiError) }
            switch apiError {
            case .problem(let problem):
                if problem.code == SessionCoordinator.deviceReauthRequiredCode {
                    state = .needsRePairing
                    lastError = "Dieses Gerät muss erneut gekoppelt werden."
                    return
                }
                lastError = problem.detail ?? problem.title
                return
            case .http(let status, _, let preview):
                if status == 401 {
                    state = .needsRePairing
                    lastError = "Authentifizierung abgelaufen. Bitte neu koppeln."
                    return
                }
                lastError = "Server-Fehler (HTTP \(status)): \(preview)"
                return
            case .transport(let transport):
                switch transport {
                case .cancelled:
                    return // Ignore cancelled SwiftUI task transitions
                case .offline:
                    lastError = "Keine Internetverbindung."
                case .timedOut:
                    lastError = "Zeitüberschreitung bei der Serververbindung."
                case .cannotConnect:
                    lastError = "Verbindung zum Server fehlgeschlagen."
                case .tls:
                    lastError = "TLS / Zertifikatsfehler bei Verbindung."
                case .other(let code):
                    lastError = "Netzwerkfehler (Code \(code))."
                }
                return
            case .invalidEndpoint(let path):
                lastError = "Endpunkt nicht verfügbar (\(path))"
                return
            case .unexpectedPayload(let payload):
                lastError = "Unerwartete Server-Antwort (Status \(payload.status))"
                return
            }
        }
        if case SessionCoordinator.Failure.reauthenticationRequired = error {
            state = .needsRePairing
            lastError = "Dieses Gerät muss erneut gekoppelt werden."
            return
        }
        lastError = error.localizedDescription
    }

    // MARK: - Device description

    private static var deviceType: DeviceType {
#if os(tvOS)
        return .appleTV
#else
        return UIDevice.current.userInterfaceIdiom == .pad ? .iPad : .iPhone
#endif
    }

    private static var deviceName: String {
        let name = UIDevice.current.name.trimmingCharacters(in: .whitespaces)
#if os(tvOS)
        return name.isEmpty ? "Apple TV" : name
#else
        return name.isEmpty ? "iPhone" : name
#endif
    }

    // MARK: - Download Auth Access

    /// Obtains a short-lived, media-scoped session cookie for background downloads.
    func mediaSessionCookie() async throws -> String {
        guard let api else {
            throw APIError.transport(.other(code: 401))
        }

        let request = APIRequest<Xg2gContract.AuthSessionResponse>(
            method: .post,
            path: "auth/session"
        )
        let response = try await api.send(request)
        let id = response.sessionId.trimmingCharacters(in: .whitespaces)
        guard !id.isEmpty else {
            throw APIError.transport(.other(code: 500))
        }
        return id
    }
}
