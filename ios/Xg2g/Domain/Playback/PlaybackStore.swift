// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Observation
import Combine

/// UI-facing store projecting canonical playback state and dispatching channel actions.
///
/// In Apple B, `PlaybackStore` is a pure read-model projection over `PlaybackManager` (via
/// `PlaybackControlling`). It holds NO independent optimistic state authority:
/// - Commands (`play(channel:)`, `stop()`, `togglePlayPause()`) are forwarded to the controller.
/// - State mutations (`currentChannel`, `isPlaying`) occur strictly via one-way projection updates.
@MainActor
@Observable
final class PlaybackStore {
    private(set) var currentTarget: PlaybackTarget?
    private(set) var currentChannel: Channel?
    private(set) var currentRecording: Recording?
    private(set) var isPlaying: Bool = false
    /// Transient local UI command error message (e.g. tuning network/validation error).
    ///
    /// NOTE: Represents a local UI presentation state, NOT canonical media or playback lifecycle truth.
    /// Canonical playback state authority is held exclusively by `PlaybackManager.state`.
    private(set) var errorMessage: String?

    @ObservationIgnored
    private var controller: (any PlaybackControlling)?

    @ObservationIgnored
    private var projectionCancellable: AnyCancellable?

    init(controller: (any PlaybackControlling)? = nil) {
        if let controller {
            attach(controller: controller)
        }
    }

    /// Attaches an active playback controller and subscribes to its canonical state projection.
    func attach(controller: any PlaybackControlling) {
        self.controller = controller
        // Cancel existing subscription before attaching new controller
        self.projectionCancellable = nil

        // Take initial snapshot and subscribe to one-way canonical updates
        self.projectionCancellable = controller.observeTargetState { [weak self] target, isPlaying in
            self?.applySnapshot(target: target, isPlaying: isPlaying)
        }
    }

    /// Tunes to a specific channel by forwarding command to canonical authority.
    ///
    /// NOTE: Does NOT mutate state optimistically. State updates occur strictly when
    /// the canonical `PlaybackManager` commits the state transition.
    func play(channel: Channel) {
        errorMessage = nil
        controller?.play(channel: channel)
    }

    /// Starts VOD playback for a recording by forwarding command to canonical authority.
    ///
    /// NOTE: Does NOT mutate state optimistically. State updates occur strictly when
    /// the canonical `PlaybackManager` commits the state transition.
    func play(recording: Recording, startPosition: Double? = nil) {
        errorMessage = nil
        controller?.play(recording: recording, startPosition: startPosition)
    }

    /// Seeks to a specific timestamp in seconds (VOD / DVR recordings).
    func seek(to seconds: Double) {
        controller?.seek(to: seconds)
    }

    /// Stops playback and releases presentation resources by forwarding command.
    func stop() {
        controller?.stop()
    }

    /// Toggles play / pause transport state by forwarding command.
    func togglePlayPause() {
        controller?.togglePlayPause()
    }

    /// Sets a transient local UI command error message.
    func setCommandError(_ message: String?) {
        self.errorMessage = message
    }

    /// Clears any transient local UI command error message.
    func clearError() {
        self.errorMessage = nil
    }

    // MARK: - Private Projection Sink

    private func applySnapshot(target: PlaybackTarget?, isPlaying: Bool) {
        self.currentTarget = target
        switch target {
        case .live(let channel):
            self.currentChannel = channel
            self.currentRecording = nil
        case .recording(let rec, _):
            self.currentChannel = nil
            self.currentRecording = rec
        case .offline, .none:
            self.currentChannel = nil
            self.currentRecording = nil
        }
        self.isPlaying = isPlaying
    }
}
