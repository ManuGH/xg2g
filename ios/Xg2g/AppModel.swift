// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Observation
import os
import UIKit

private let appLogger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "app")

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
    case home = "Für dich"
    case liveTV = "Live TV"
    case guide = "Programm"
    case recordings = "Aufnahmen"
    case timers = "Timer"
    case settings = "Einstellungen"

    var id: String { rawValue }

    var title: LocalizedStringResource {
        switch self {
        case .home:
            return LocalizedStringResource("For You", comment: "Home tab title")
        case .liveTV:
            return LocalizedStringResource("Live TV", comment: "Live TV tab title")
        case .guide:
            return LocalizedStringResource("Guide", comment: "Guide tab title")
        case .recordings:
            return LocalizedStringResource("Recordings", comment: "Recordings tab title")
        case .timers:
            return LocalizedStringResource("Timers", comment: "Timers tab title")
        case .settings:
            return LocalizedStringResource("Settings", comment: "Settings tab title")
        }
    }

    var systemImage: String {
        switch self {
        case .home: return "sparkles.tv"
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

    var formattedRelativeTimeResource: LocalizedStringResource {
        let calendar = Calendar.current
        let timeString = entry.formattedStartTime
        if calendar.isDateInToday(entry.start) {
            return LocalizedStringResource("Today, \(timeString)")
        } else if calendar.isDateInTomorrow(entry.start) {
            return LocalizedStringResource("Tomorrow, \(timeString)")
        } else {
            let dateString = entry.start.formatted(.dateTime.weekday(.abbreviated).day().month(.abbreviated))
            return LocalizedStringResource("\(dateString) • \(timeString)")
        }
    }

    var formattedRelativeTime: String {
        String(localized: formattedRelativeTimeResource)
    }
}

/// Composes the app: one server, one identity, one set of coordinators.
@Observable
@MainActor
final class AppModel {

    private(set) var state: AppState = .needsServer
    var selectedTab: Tab = .home

    // MARK: - Live TV State
    private(set) var channels: [Channel] = [] { didSet { contentRevision &+= 1 } }
    private(set) var bouquets: [ChannelBouquet] = []
    var selectedBouquet: ChannelBouquet?
    var searchQuery: String = ""
    private(set) var schedule: [String: NowNext] = [:] { didSet { contentRevision &+= 1 } }
    private(set) var fullEpg: [String: [NowNext.Entry]] = [:] { didSet { contentRevision &+= 1 } }
    private(set) var isLoadingChannels = false
    private(set) var lastDataRefreshTime: Date?

    func setChannelsForTesting(
        _ channels: [Channel],
        schedule: [String: NowNext] = [:],
        fullEpg: [String: [NowNext.Entry]] = [:]
    ) {
        self.channels = channels
        self.schedule = schedule
        self.fullEpg = fullEpg
    }

    func setRecordingsAndTimersForTesting(
        recordings: [Recording] = [],
        timers: [DVRTimer] = []
    ) {
        self.recordings = recordings
        self.timers = timers
    }

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

    private(set) var currentError: UserFacingError?
    private(set) var lastError: String?

    func clearError() {
        setError(nil)
    }

    private func setError(_ error: UserFacingError?) {
        currentError = error
        lastError = error?.localizedMessage
        if let error {
            appLogger.error("User-facing error presented: [\(error.diagnosticSummary, privacy: .public)]")
            TelemetryServer.shared.log("[ERROR] \(error.diagnosticSummary)")
        }
    }

    /// The stream currently handed to the player, if any.
    private(set) var liveStream: LiveStream?

    private let addressStore: ServerAddressStore
    private let credentials: CredentialStore
    private let keyStore: DeviceKeyStore
    private let makeAPIClient: (ServerAddress, any RequestAuthorizer) -> any APIClient

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

    var isDemoMode: Bool {
        DemoServer.isDemoAddress(address)
    }

    var serverURLString: String {
        if isDemoMode {
            return DemoServer.displayAddress
        }
        return address?.rootURL.absoluteString ?? "–"
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
            case .auto: return String(localized: "Auto")
            case .passthrough: return String(localized: "Original Quality")
            case .qsvNormalize: return String(localized: "Compatibility")
            case .dataSaver: return String(localized: "Data Saver")
            }
        }

        var localizedTitle: LocalizedStringResource {
            switch self {
            case .auto: return LocalizedStringResource("Auto")
            case .passthrough: return LocalizedStringResource("Original Quality")
            case .qsvNormalize: return LocalizedStringResource("Compatibility")
            case .dataSaver: return LocalizedStringResource("Data Saver")
            }
        }

        /// Subtitle / explanation
        var summary: String {
            switch self {
            case .auto: return String(localized: "xg2g finds the best balance of quality and latency (Recommended)")
            case .passthrough: return String(localized: "1:1 bitstream without video transcoding")
            case .qsvNormalize: return String(localized: "Standardized HLS with maximum device compatibility")
            case .dataSaver: return String(localized: "Bandwidth-optimized streaming (HEVC/AV1) for on-the-go")
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
            case .auto: return String(localized: "Auto")
            case .native: return String(localized: "Native Live TV")
            case .hls: return String(localized: "Server Streaming (HLS)")
            }
        }

        var localizedTitle: LocalizedStringResource {
            switch self {
            case .auto: return LocalizedStringResource("Auto")
            case .native: return LocalizedStringResource("Native Live TV")
            case .hls: return LocalizedStringResource("Server Streaming (HLS)")
            }
        }

        /// One line for the settings row.
        var summary: String {
            switch self {
            case .auto: return String(localized: "xg2g dynamically chooses the best route for your device and network (Recommended)")
            case .native: return String(localized: "xg2g delivers the channel as directly as possible to the native player")
            case .hls: return String(localized: "Enables timeshift/pause, remote streaming, and adaptive bitrate")
            }
        }

        /// What the viewer gains and gives up, in their terms.
        var tradeoff: (gains: [LocalizedStringResource], costs: [LocalizedStringResource]) {
            switch self {
            case .auto:
                return (
                    gains: [
                        LocalizedStringResource("The xg2g Planner automatically selects the optimal pipeline"),
                        LocalizedStringResource("Lossless streaming and lowest latency on local network"),
                        LocalizedStringResource("Seamless switch to adaptive streaming when away from home")
                    ],
                    costs: [
                        LocalizedStringResource("Timeshift/pause is only available when HLS is active")
                    ]
                )
            case .native:
                return (
                    gains: [
                        LocalizedStringResource("Direct video and audio with minimal latency"),
                        LocalizedStringResource("Significantly faster channel zapping (hardware decoding)"),
                        LocalizedStringResource("Minimal server load (no video transcoding)"),
                        LocalizedStringResource("Closer to the live broadcast")
                    ],
                    costs: [
                        LocalizedStringResource("No pausing or rewinding (timeshift)"),
                        LocalizedStringResource("Only available on the same network as the ingest receiver"),
                        LocalizedStringResource("Requires the full broadcast bitrate continuously")
                    ]
                )
            case .hls:
                return (
                    gains: [
                        LocalizedStringResource("Pause live TV, rewind, and watch from the beginning (timeshift)"),
                        LocalizedStringResource("Works reliably outside the home network"),
                        LocalizedStringResource("Adaptive quality on fluctuating bandwidth"),
                        LocalizedStringResource("AirPlay and system screen sharing")
                    ],
                    costs: [
                        LocalizedStringResource("Higher latency than Native Live TV"),
                        LocalizedStringResource("Zapping times depend on GOP segment boundaries")
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
            telemetryProvider: { [weak self] in
                self?.makePlaybackTelemetryClient()
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
        if isDemoMode {
            return DemoServer.demoHLSStreamURL(forServiceRef: serviceRef)
        }
        return media?.liveStream(serviceRef: serviceRef)
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
        guard let api, !isDemoMode else { return nil }
        return ZapPreparationClient(api: api, clientID: Self.zapClientID)
    }

    /// A playback telemetry client, when a backend address has been configured.
    ///
    /// Same identity as the preparation client, so the server can put a client's
    /// telemetry next to the channel changes it made.
    func makePlaybackTelemetryClient() -> HTTPPlaybackTelemetrySink? {
        guard let api, !isDemoMode else { return nil }
        return HTTPPlaybackTelemetrySink(api: api, clientID: Self.zapClientID)
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

    nonisolated static let favoritesBouquetID = "xg2g_local_favorites"
    nonisolated static var favoritesBouquet: ChannelBouquet {
        ChannelBouquet(id: favoritesBouquetID, name: "Favorites")
    }

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

        var localizedLabel: LocalizedStringResource {
            switch self {
            case .now:
                return LocalizedStringResource("Live Now")
            case .next:
                return LocalizedStringResource("Next")
            case .primeTimeTonight:
                return LocalizedStringResource(stringLiteral: Self.formatPresetTime(hour: 20, minute: 15))
            case .lateNightTonight:
                return LocalizedStringResource(stringLiteral: Self.formatPresetTime(hour: 22, minute: 0))
            case .day(let date):
                let calendar = Calendar.current
                if calendar.isDateInToday(date) {
                    return LocalizedStringResource("Today")
                } else if calendar.isDateInTomorrow(date) {
                    return LocalizedStringResource("Tomorrow")
                } else {
                    return LocalizedStringResource(stringLiteral: date.formatted(.dateTime.weekday(.abbreviated).day().month(.abbreviated)))
                }
            }
        }

        var label: String {
            String(localized: localizedLabel)
        }

        nonisolated static func formatPresetTime(hour: Int, minute: Int) -> String {
            let calendar = Calendar.current
            var components = calendar.dateComponents([.year, .month, .day], from: Date())
            components.hour = hour
            components.minute = minute
            let date = calendar.date(from: components) ?? Date()
            return date.formatted(date: .omitted, time: .shortened)
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

    /// `makeAPIClient` builds the transport for one server. Production wires
    /// `HTTPAPIClient`; tests hand in a scripted client so the pairing flow can
    /// be driven end to end without a network.
    init(
        addressStore: ServerAddressStore = ServerAddressStore(),
        credentials: CredentialStore = KeychainCredentialStore(backend: SecItemKeychainBackend()),
        keyStore: DeviceKeyStore = SecureEnclaveDeviceKeyStore(),
        makeAPIClient: @escaping (ServerAddress, any RequestAuthorizer) -> any APIClient = {
            HTTPAPIClient(address: $0, authorizer: $1)
        }
    ) {
        self.addressStore = addressStore
        self.credentials = credentials
        self.keyStore = keyStore
        self.makeAPIClient = makeAPIClient
    }

    // MARK: - Launch

    /// Must run before anything reads a credential: on a fresh install this is
    /// what purges Keychain material left by a previous installation.
    func start() async {
        TelemetryServer.shared.start()
        do {
            try await credentials.prepareForLaunch()
        } catch {
            setError(UserFacingError(
                title: LocalizedStringResource("Authentication Required"),
                detail: LocalizedStringResource("Stored credentials could not be opened."),
                isRetryable: false,
                severity: .warning
            ))
            return
        }

        guard let stored = addressStore.load() else {
            state = .needsServer
            return
        }

        if DemoServer.isDemoAddress(stored) {
            configureDemoMode()
            state = .ready
            await loadInitialData()
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

    /// Enters built-in demonstration mode with official Apple HLS streams and synthetic EPG/DVR catalog.
    func startDemoMode() async {
        setError(nil)
        let demoAddress = DemoServer.demoServerAddress
        addressStore.save(demoAddress)
        configureDemoMode()
        await loadInitialData()
        state = .ready
    }

    private func configureDemoMode() {
        let demoAddress = DemoServer.demoServerAddress
        let identity = ServerIdentity.address(demoAddress)
        self.address = demoAddress
        self.identity = identity
        self.session = nil
        self.enrollment = nil
        self.revokeCoordinator = nil

        let demoAPI = DemoAPIClient()
        self.api = demoAPI
        self.channelRepository = ChannelRepository(api: demoAPI, baseURL: demoAddress.rootURL)
        self.recordingsRepository = RecordingsRepository(api: demoAPI)
        self.timersRepository = TimersRepository(api: demoAPI)
        self.playback = PlaybackCoordinator(address: demoAddress, api: demoAPI)
    }

    /// Accepts what a human typed. This is the one place lenient parsing is
    /// allowed; everything downstream deals in a parsed address.
    func useServer(_ typed: String) async {
        if DemoServer.isDemoInput(typed) {
            await startDemoMode()
            return
        }

        guard let parsed = try? ServerAddressParser.parseUserEntered(typed) else {
            setError(UserFacingError(
                title: LocalizedStringResource("Request Failed"),
                detail: LocalizedStringResource("That does not look like a server address."),
                isRetryable: false,
                severity: .warning
            ))
            return
        }
        setError(nil)
        addressStore.save(parsed)
        configure(with: parsed)
        state = .needsPairing
    }

    private func configure(with address: ServerAddress) {
        let identity = ServerIdentity.address(address)
        self.address = address
        self.identity = identity

        let refreshClient = makeAPIClient(address, DeviceProofAuthorizer(keyStore: keyStore))

        let sessionCoord = SessionCoordinator(identity: identity, api: refreshClient, credentials: credentials)
        self.session = sessionCoord

        let authorized = makeAPIClient(
            address,
            DPoPRequestAuthorizer(identity: identity, credentials: credentials, keyStore: keyStore, sessionCoordinator: sessionCoord)
        )
        self.api = authorized

        channelRepository = ChannelRepository(api: authorized, baseURL: address.rootURL)
        recordingsRepository = RecordingsRepository(api: authorized)
        timersRepository = TimersRepository(api: authorized)
        playback = PlaybackCoordinator(address: address, api: authorized)
        enrollment = EnrollmentCoordinator(
            identity: identity,
            api: makeAPIClient(address, UnauthenticatedRequests()),
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
            setError(nil)
            return try await enrollment.startPairing(deviceName: Self.deviceName, deviceType: Self.deviceType)
        } catch {
            handle(error)
            return nil
        }
    }

    func pairingStatus() async -> Xg2gContract.PairingStatus? {
        try? await enrollment?.pairingStatus()
    }

    /// What one status poll means for the screen that is waiting for approval.
    enum PairingPoll: Equatable, Sendable {
        /// Still pending — or the poll itself failed, which says nothing about
        /// the pairing. Ask again later.
        case keepWaiting
        /// Approved; the credential exchange has run. `state` and `lastError`
        /// carry its outcome.
        case completed
        /// The server will never approve this pairing. `lastError` says why;
        /// only a fresh code can help, so there is nothing left to wait for.
        case ended
    }

    /// One poll of the pairing the user is waiting on.
    ///
    /// The pending/approved half is the happy path. The other three statuses
    /// are the reason this exists: the server expires a pairing after its TTL
    /// and keeps answering `expired` to every later poll, so a loop that only
    /// looks for `approved` spins forever behind a spinner (seen 2026-09-20:
    /// 16 hours of "Warte auf Bestätigung…").
    func pollPairing() async -> PairingPoll {
        guard let status = await pairingStatus() else { return .keepWaiting }
        switch status {
        case .pending:
            return .keepWaiting
        case .approved:
            await completePairing()
            return .completed
        // Each ending names its remedy: the button next to the text is
        // "request a new code", and the text has to make that the obvious
        // next step rather than "choose another server".
        case .expired:
            setError(UserFacingError(
                title: LocalizedStringResource("Pairing Code Expired"),
                detail: LocalizedStringResource("Please request a new pairing code."),
                isRetryable: false,
                severity: .info,
                code: "PAIRING_EXPIRED"
            ))
            return .ended
        case .consumed:
            setError(UserFacingError(
                title: LocalizedStringResource("Code Already Used"),
                detail: LocalizedStringResource("This code has already been claimed. Please request a new code."),
                isRetryable: false,
                severity: .info,
                code: "PAIRING_CONSUMED"
            ))
            return .ended
        case .revoked:
            setError(UserFacingError(
                title: LocalizedStringResource("Pairing Denied"),
                detail: LocalizedStringResource("Pairing was denied in the admin console. Please request a new code."),
                isRetryable: false,
                severity: .warning,
                code: "PAIRING_REVOKED"
            ))
            return .ended
        }
    }

    func completePairing() async {
        guard let enrollment else { return }
        do {
            _ = try await enrollment.completeEnrollment()
            await session?.resetAfterReenrollment()
            setError(nil)

            // Load before announcing `.ready`. The pairing screen's task is
            // what awaits this call, and SwiftUI cancels that task the moment
            // the ready screen replaces it. With the flip first, the initial
            // requests died with -999 and the app came up with no channels —
            // on tvOS nothing reloads on the way to the home hub, so it stayed
            // that way. `start()` is not affected: its caller is the root view.
            let stateBeforeLoad = state
            await loadInitialData()

            // The load can move the state itself — a 401 routes to re-pairing
            // through `handle`, "Anderen Server wählen" to setup. Those win.
            guard state == stateBeforeLoad else { return }
            state = .ready
        } catch {
            handle(error)
        }
    }

    func changeServer() {
        addressStore.clear()
        address = nil
        identity = nil
        enrollment = nil
        session = nil
        setError(nil)
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
            setError(nil)

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
            setError(nil)
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
            setError(nil)
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
            setError(nil)
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

    // MARK: - Revoke / Sign Out

    func disconnectServer() async {
        if let revokeCoordinator {
            do {
                try await revokeCoordinator.revokeThisDevice(destroyingDeviceKey: true)
            } catch {
                // Even if remote revoke failed, clear local state on explicit sign out
                if let identity {
                    try? await credentials.forgetServer(identity)
                }
                try? await keyStore.destroyKey()
            }
        }
        await playbackManager.stop()
        addressStore.clear()
        address = nil
        identity = nil
        api = nil
        state = .needsServer
        channels = []
        bouquets = []
        recordings = []
        timers = []
        setError(nil)
    }

    // MARK: - Errors

    private func handle(_ error: any Error) {
        if let apiError = error as? APIError {
            Task { await session?.noteRequestFailure(apiError) }
        }
        guard let classified = ErrorClassifier.classify(error) else {
            // Cancellation suppressed: normal user transitions do not flash error states
            setError(nil)
            return
        }

        if classified.code == SessionCoordinator.deviceReauthRequiredCode ||
           classified.code == "DEVICE_REAUTH_REQUIRED" {
            state = .needsRePairing
        } else if let apiError = error as? APIError, case .http(let status, _, _) = apiError, status == 401 {
            state = .needsRePairing
        }

        setError(classified)
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
