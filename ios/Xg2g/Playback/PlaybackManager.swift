// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI
import Combine
import AVFoundation
import Observation

public enum PlaybackPresentationMode: String, Sendable, Equatable {
    case hidden
    case miniplayer
    case fullscreen
}

public enum PiPState: String, Sendable, Equatable {
    case inactive
    case starting
    case active
}

public enum BackgroundPlaybackState: String, Sendable, Equatable {
    case foreground
    case backgroundAudio
}

struct PlayingRecordingItem: Identifiable, Equatable, Sendable {
    let id: String
    let recording: Recording
    let initialPosition: Double

    static func == (lhs: PlayingRecordingItem, rhs: PlayingRecordingItem) -> Bool {
        lhs.id == rhs.id && lhs.initialPosition == rhs.initialPosition
    }
}

enum PlaybackState: Equatable {
    case idle
    case live(Channel, mode: PlaybackPresentationMode)
    case recording(PlayingRecordingItem, mode: PlaybackPresentationMode)
    case offline(OfflineRecording)
}

/// Central controller for all active screen playback sessions and presentation modes.
///
/// Decoupled from `AppModel` and individual screen lifecycles:
/// Acts as the single source of truth and state machine for active visible playback.
///
/// Note: DVR Timers and background RecordingJobs are intentionally completely independent
/// of this controller and run independently on the backend.
@Observable @MainActor
final class PlaybackManager {

    var state: PlaybackState = .idle
    private(set) var pipState: PiPState = .inactive
    private(set) var backgroundState: BackgroundPlaybackState = .foreground
    private(set) var recordingPlayer: AVPlayer?

    func setRecordingPlayer(_ player: AVPlayer?) {
        if let current = self.recordingPlayer, current !== player {
            current.pause()
        }
        self.recordingPlayer = player
    }

    let coordinator: ZapCoordinator
    @ObservationIgnored private let streamURLProvider: @MainActor (String) -> URL?
    @ObservationIgnored private var recordingCleanupHook: (@MainActor () -> Void)?
    @ObservationIgnored private var activeTransitionID: UUID = UUID()

    init(preparations: ZapPreparationClient? = nil,
         preparationsProvider: (@MainActor () -> ZapPreparationClient?)? = nil,
         streamURL: @escaping @MainActor (String) -> URL?) {
        self.streamURLProvider = streamURL
        self.coordinator = ZapCoordinator(
            preparations: preparations,
            preparationsProvider: preparationsProvider,
            streamURL: streamURL
        )
    }

    // MARK: - Derived Canonical Properties (No Duplicate Published States)

    var currentChannel: Channel? {
        if case .live(let channel, _) = state { return channel }
        return nil
    }

    var presentationMode: PlaybackPresentationMode {
        switch state {
        case .live(_, let mode): return mode
        case .recording(_, let mode): return mode
        case .idle, .offline: return .hidden
        }
    }

    var activeRecordingItem: PlayingRecordingItem? {
        if case .recording(let item, _) = state { return item }
        return nil
    }

    var activeOfflineRecording: OfflineRecording? {
        if case .offline(let rec) = state { return rec }
        return nil
    }

    var isStreaming: Bool {
        if case .live = state { return true }
        return false
    }

    var isPlaying: Bool {
        switch state {
        case .idle: return false
        case .live(_, let mode): return mode != .hidden && coordinator.playing != nil
        case .recording(_, let mode): return mode != .hidden && (recordingPlayer?.timeControlStatus == .playing || recordingPlayer == nil)
        case .offline: return true
        }
    }

    func togglePlayPause() {
        if let recPlayer = recordingPlayer {
            if recPlayer.timeControlStatus == .playing {
                recPlayer.pause()
            } else {
                recPlayer.play()
            }
        } else if case .live(let ch, let mode) = state {
            if coordinator.playing != nil {
                Task { await coordinator.stop() }
            } else {
                Task { await play(channel: ch, mode: mode) }
            }
        }
    }

    var displayedPlan: SessionRuntimePlan? {
        coordinator.playing?.runtimePlan
    }

    var displayedServiceRef: String? {
        coordinator.displayedServiceRef ?? coordinator.presentedServiceRef ?? currentChannel?.serviceRef
    }

    // MARK: - Lifecycle Hooks

    /// Registers a deterministic cleanup callback for the active recording player (e.g. AVPlayer teardown).
    func registerRecordingCleanup(_ hook: @escaping @MainActor () -> Void) {
        self.recordingCleanupHook = hook
    }

    func unregisterRecordingCleanup() {
        self.recordingCleanupHook = nil
    }

    // MARK: - Handover & Transitions

    func play(channel: Channel, mode: PlaybackPresentationMode = .fullscreen) async {
        let transactionID = UUID()
        self.activeTransitionID = transactionID

        // 1. Teardown active recording if transitioning away from recording
        if case .recording = state {
            recordingCleanupHook?()
            recordingCleanupHook = nil
        }

        // 2. Commit canonical Live state
        self.state = .live(channel, mode: mode)

        // 3. Drive Live Zap Transaction
        let serviceRef = channel.serviceRef
        if coordinator.canPrepare {
            await coordinator.zap(to: serviceRef)
        } else if let url = streamURLProvider(serviceRef) {
            await coordinator.play(unprepared: url, requestedAt: CACurrentMediaTime())
        }
    }

    func play(channel: Channel, mode: PlaybackPresentationMode = .fullscreen) {
        Task { @MainActor in
            await play(channel: channel, mode: mode)
        }
    }

    func zap(to channel: Channel) async {
        let transactionID = UUID()
        self.activeTransitionID = transactionID

        if case .recording = state {
            recordingCleanupHook?()
            recordingCleanupHook = nil
            setRecordingPlayer(nil)
        }

        let targetMode = (presentationMode == .hidden) ? .fullscreen : presentationMode
        self.state = .live(channel, mode: targetMode)
        let serviceRef = channel.serviceRef
        if coordinator.canPrepare {
            await coordinator.zap(to: serviceRef)
        } else if let url = streamURLProvider(serviceRef) {
            await coordinator.play(unprepared: url, requestedAt: CACurrentMediaTime())
        }
    }

    func zap(to channel: Channel) {
        Task { @MainActor in
            await zap(to: channel)
        }
    }

    func updateLiveChannel(_ channel: Channel) {
        if case .live(_, let mode) = state {
            self.state = .live(channel, mode: mode)
        }
    }

    func play(recording: Recording, startPosition: Double, mode: PlaybackPresentationMode = .fullscreen) async {
        let transactionID = UUID()
        self.activeTransitionID = transactionID

        // 1. Teardown active Live TV session before switching ownership
        if case .live = state {
            await coordinator.stop()
        }

        // 2. Teardown existing recording cleanup if switching between recordings
        if case .recording = state {
            recordingCleanupHook?()
            recordingCleanupHook = nil
        }

        // 3. Guard against race if a new transition began while awaiting stop()
        guard self.activeTransitionID == transactionID else { return }

        // 4. Set canonical Recording state
        let item = PlayingRecordingItem(id: recording.id, recording: recording, initialPosition: startPosition)
        self.state = .recording(item, mode: mode)
    }

    func play(recording: Recording, startPosition: Double, mode: PlaybackPresentationMode = .fullscreen) {
        Task { @MainActor in
            await play(recording: recording, startPosition: startPosition, mode: mode)
        }
    }

    func play(offline: OfflineRecording) async {
        let transactionID = UUID()
        self.activeTransitionID = transactionID

        // 1. Teardown active Live TV session
        if case .live = state {
            await coordinator.stop()
        }

        // 2. Teardown existing recording if any
        if case .recording = state {
            recordingCleanupHook?()
            recordingCleanupHook = nil
        }

        // 3. Guard against race if a new transition began while awaiting stop()
        guard self.activeTransitionID == transactionID else { return }

        // 4. Set canonical Offline state
        self.state = .offline(offline)
    }

    func play(offline: OfflineRecording) {
        Task { @MainActor in
            await play(offline: offline)
        }
    }

    func minimize() {
        switch state {
        case .live(let channel, .fullscreen):
            self.state = .live(channel, mode: .miniplayer)
        case .recording(let item, .fullscreen):
            self.state = .recording(item, mode: .miniplayer)
        default:
            break
        }
    }

    func expand() {
        switch state {
        case .live(let channel, .miniplayer):
            self.state = .live(channel, mode: .fullscreen)
        case .recording(let item, .miniplayer):
            self.state = .recording(item, mode: .fullscreen)
        default:
            break
        }
    }

    func stop() async {
        let transactionID = UUID()
        self.activeTransitionID = transactionID

        if case .live = state {
            await coordinator.stop()
        } else if case .recording = state {
            recordingCleanupHook?()
            recordingCleanupHook = nil
            setRecordingPlayer(nil)
        }

        guard self.activeTransitionID == transactionID else { return }
        self.state = .idle
    }

    func stop() {
        Task { @MainActor in
            await stop()
        }
    }
}
