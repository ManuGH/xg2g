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
    case guide = "Programm"
    case recordings = "Aufnahmen"
    case timers = "Timer"
    case settings = "Einstellungen"

    var id: String { rawValue }

    var systemImage: String {
        switch self {
        case .liveTV: return "tv"
        case .guide: return "calendar.badge.clock"
        case .recordings: return "play.rectangle.on.rectangle"
        case .timers: return "clock"
        case .settings: return "gearshape"
        }
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
    var selectedTab: Tab = .liveTV

    // MARK: - Live TV State
    private(set) var channels: [Channel] = [] { didSet { contentRevision &+= 1 } }
    private(set) var bouquets: [ChannelBouquet] = []
    var selectedBouquet: ChannelBouquet?
    var searchQuery: String = ""
    private(set) var schedule: [String: NowNext] = [:] { didSet { contentRevision &+= 1 } }
    private(set) var fullEpg: [String: [NowNext.Entry]] = [:] { didSet { contentRevision &+= 1 } }
    private(set) var isLoadingChannels = false
    private(set) var lastDataRefreshTime: Date?

    /// Bumped whenever the channel list, Now/Next schedule or full EPG is replaced.
    ///
    /// Views that derive an expensive projection from this data key their
    /// recomputation on this counter instead of diffing the collections
    /// themselves — a refresh that returns the same number of channels still
    /// has to invalidate them.
    private(set) var contentRevision: Int = 0

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
    private var api: (any APIClient)?

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

    /// The receiver URL for a service, or `nil` while no usable receiver
    /// address has been entered. Direct playback cannot run until it has.
    func directStreamURL(for channel: Channel) -> URL? {
        let base = receiverStreamBaseURL.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !base.isEmpty else { return nil }
        let normalised = base.hasSuffix("/") ? String(base.dropLast()) : base
        return URL(string: "\(normalised)/\(channel.serviceRef)")
    }

    /// Media URLs for the configured deployment, or `nil` while there is none.
    ///
    /// Nothing here composes a URL any more: the transport owns what an API
    /// path looks like, and an unconfigured app has no stream to offer rather
    /// than a hard-coded one on somebody else's network.
    var media: MediaEndpoints? {
        address.map(MediaEndpoints.init(address:))
    }

    /// The v3 Ingest Live Stream URL for a service reference.
    func liveStreamURL(for serviceRef: String) -> URL? {
        media?.liveStream(serviceRef: serviceRef)
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

    /// A preparation client, when a backend address has been configured.
    ///
    /// `nil` without one: preparation runs against xg2g, so the direct receiver route
    /// has nothing to prepare and the player falls back to starting a channel outright.
    func makeZapPreparationClient() -> ZapPreparationClient? {
        guard let api else { return nil }
        return ZapPreparationClient(api: api, clientID: Self.zapClientID)
    }

    /// The legacy burst smoother URL for a service reference.
    func legacySmoothStreamURL(for serviceRef: String) -> URL? {
        media?.smoothStream(serviceRef: serviceRef)
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
    private(set) var recordingProgress: [String: Double] = {
        let dict = UserDefaults.standard.dictionary(forKey: "xg2g.recordingProgress") as? [String: Double] ?? [:]
        return dict
    }()

    func updateRecordingProgress(id: String, currentTime: Double, totalDuration: Double, title: String? = nil, channelName: String? = nil) {
        guard totalDuration > 0 else { return }
        let fraction = min(1.0, max(0.0, currentTime / totalDuration))
        let finished = fraction > 0.95
        if finished {
            // Finished
            recordingProgress.removeValue(forKey: id)
        } else if fraction > 0.02 {
            recordingProgress[id] = currentTime
        }
        UserDefaults.standard.set(recordingProgress, forKey: "xg2g.recordingProgress")

        // Sync to xg2g server profile in background (Cross-Device Sync)
        if let repo = recordingsRepository {
            Task {
                _ = try? await session?.validSession()
                try? await repo.saveResume(
                    id: id,
                    position: currentTime,
                    total: totalDuration,
                    finished: finished,
                    title: title ?? "",
                    channel: channelName ?? ""
                )
            }
        }
    }

    func resumePosition(for id: String) -> Double? {
        recordingProgress[id]
    }

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
    var selectedGenre: EpgGenre = .all
    var epgViewMode: EpgViewMode = .list
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

    private static func primeTimeTarget(for date: Date = .now) -> Date {
        let calendar = Calendar.current
        var components = calendar.dateComponents([.year, .month, .day], from: date)
        components.hour = 20
        components.minute = 15
        components.second = 0
        return calendar.date(from: components) ?? date
    }

    private static func lateNightTarget(for date: Date = .now) -> Date {
        let calendar = Calendar.current
        var components = calendar.dateComponents([.year, .month, .day], from: date)
        components.hour = 22
        components.minute = 0
        components.second = 0
        return calendar.date(from: components) ?? date
    }

    /// Resolves the programme running on a channel for the active time filter
    func show(for channel: Channel, at filter: TimeFilter) -> NowNext.Entry? {
        let scheduleItem = schedule[channel.serviceRef]
        let allShows = fullEpg[channel.serviceRef] ?? []

        switch filter {
        case .now:
            let currentTime = Date.now
            if let now = scheduleItem?.now, now.start <= currentTime && now.end > currentTime {
                return now
            }
            if let current = allShows.first(where: { $0.start <= currentTime && $0.end > currentTime }) {
                return current
            }
            return scheduleItem?.now
        case .next:
            let currentTime = Date.now
            if let next = scheduleItem?.next, next.start >= currentTime {
                return next
            }
            if let current = show(for: channel, at: .now),
               let upcoming = allShows.first(where: { $0.start >= current.end }) {
                return upcoming
            }
            return scheduleItem?.next
        case .primeTimeTonight:
            let target = Self.primeTimeTarget()
            return allShows.first { $0.start <= target && $0.end > target }
                ?? allShows.first { $0.start >= target }
                ?? (allShows.isEmpty ? scheduleItem?.now : nil)
        case .lateNightTonight:
            let target = Self.lateNightTarget()
            return allShows.first { $0.start <= target && $0.end > target }
                ?? allShows.first { $0.start >= target }
                ?? (allShows.isEmpty ? scheduleItem?.now : nil)
        case .day(let dayDate):
            let target = Self.primeTimeTarget(for: dayDate)
            let calendar = Calendar.current
            return allShows.first { $0.start <= target && $0.end > target }
                ?? allShows.first { calendar.isDate($0.start, inSameDayAs: dayDate) }
                ?? allShows.first { $0.start >= target }
                ?? (allShows.isEmpty ? scheduleItem?.now : nil)
        }
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
        let genre: EpgGenre
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
                    matches = EpgGenreClassifier.channelMatches(genre: activeGenre, channelName: channel.name)
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
    }

    // MARK: - Launch

    /// Must run before anything reads a credential: on a fresh install this is
    /// what purges Keychain material left by a previous installation.
    func start() async {
        TelemetryServer.shared.start()
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
                schedule = updated
            }
        }

        // 2. Refresh full EPG schedule
        if let epgUpdated = try? await channelRepository.epgSchedule(bouquet: selectedBouquet?.name) {
            fullEpg = epgUpdated
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
            if bouquet == nil, let all = bouquetChannelsCache["all"], channels.count != all.count {
                channels = all
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
            schedule = newSchedule
            fullEpg = newEpg
            lastDataRefreshTime = Date()
        } catch {
            handle(error)
        }
    }

    /// Refresh the live Now/Next EPG schedule for specific channels or all channels.
    func refreshSchedule(for serviceRefs: [String] = []) async {
        let targets = serviceRefs.isEmpty ? channels.map(\.serviceRef) : serviceRefs
        guard !targets.isEmpty else { return }
        if let updated = try? await channelRepository?.nowNext(for: targets) {
            // One merge rather than a per-key loop: each individual assignment
            // would be its own observation notification.
            schedule.merge(updated) { _, new in new }
            lastDataRefreshTime = Date()
        }
    }

    /// Returns the full chronological schedule for a given channel.
    func channelSchedule(for channel: Channel) -> [NowNext.Entry] {
        if let list = fullEpg[channel.serviceRef], !list.isEmpty {
            return list.sorted { $0.start < $1.start }
        }
        var list: [NowNext.Entry] = []
        if let nn = schedule[channel.serviceRef] {
            if let now = nn.now { list.append(now) }
            if let next = nn.next { list.append(next) }
        }
        return list
    }

    /// Finds all other airings / reruns of a given show across all channels in the full EPG buffer.
    func findReruns(for entry: NowNext.Entry, excludingChannelID: String? = nil) -> [RerunItem] {
        let targetNorm = normalizeShowTitle(entry.title)
        guard targetNorm.count >= 3 else { return [] }

        var results: [RerunItem] = []

        for channel in channels {
            let shows = fullEpg[channel.serviceRef] ?? []
            for show in shows {
                // Skip the exact same event instance if on the same channel
                if show.id == entry.id && channel.id == excludingChannelID {
                    continue
                }
                // Skip past events that already ended
                if show.end < .now {
                    continue
                }

                let showNorm = normalizeShowTitle(show.title)
                if showNorm == targetNorm ||
                   (targetNorm.count > 4 && (showNorm.contains(targetNorm) || targetNorm.contains(showNorm))) {
                    results.append(RerunItem(channel: channel, entry: show))
                }
            }
        }

        // Deduplicate and sort chronologically
        var seen = Set<String>()
        var unique: [RerunItem] = []
        for item in results.sorted(by: { $0.entry.start < $1.entry.start }) {
            let key = "\(item.channel.id)_\(Int(item.entry.start.timeIntervalSince1970))"
            if !seen.contains(key) {
                seen.insert(key)
                unique.append(item)
            }
        }
        return unique
    }

    private static let parenRegex = try? NSRegularExpression(pattern: #"\(.*?\)|\[.*?\]"#, options: [])
    private static let seasonEpisodeRegex = try? NSRegularExpression(pattern: #"\b(staffel|folge|episode|s\d+|e\d+|hd|live|wh\.|wiederholung)\b"#, options: [.caseInsensitive])

    private func normalizeShowTitle(_ title: String) -> String {
        var s = title.lowercased().trimmingCharacters(in: .whitespacesAndNewlines)
        if let r = Self.parenRegex {
            s = r.stringByReplacingMatches(in: s, range: NSRange(s.startIndex..., in: s), withTemplate: "")
        }
        if let r = Self.seasonEpisodeRegex {
            s = r.stringByReplacingMatches(in: s, range: NSRange(s.startIndex..., in: s), withTemplate: "")
        }
        return s.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    func loadRecordings() async {
        guard let recordingsRepository, !isLoadingRecordings else { return }
        isLoadingRecordings = true
        defer { isLoadingRecordings = false }

        do {
            _ = try? await session?.validSession()
            let list = try await recordingsRepository.recordings()
            recordings = list

            // Seed local cache from server profile resume states
            for rec in list {
                if let sPos = rec.serverResumePos, sPos > 0 {
                    recordingProgress[rec.id] = sPos
                }
            }
            UserDefaults.standard.set(recordingProgress, forKey: "xg2g.recordingProgress")
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

    func addCustomTimer(
        channel: Channel,
        name: String,
        description: String?,
        start: Date,
        end: Date
    ) async -> Bool {
        guard let timersRepository else { return false }
        do {
            try await timersRepository.createTimer(
                serviceRef: channel.serviceRef,
                name: name,
                description: description,
                begin: start,
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

    func recordingPlaybackUrl(for recordingId: String) async throws -> String? {
        guard let recordingsRepository else { return nil }
        return try await recordingsRepository.playbackUrl(for: recordingId)
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

    func heartbeat(sessionID: String) async throws {
        try await playback?.heartbeat(sessionID: sessionID)
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
        UIDevice.current.userInterfaceIdiom == .pad ? .iPad : .iPhone
    }

    private static var deviceName: String {
        let name = UIDevice.current.name.trimmingCharacters(in: .whitespaces)
        return name.isEmpty ? "iPhone" : name
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
