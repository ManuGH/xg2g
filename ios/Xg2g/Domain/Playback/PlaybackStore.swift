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
    private(set) var currentChannel: Channel?
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
        self.projectionCancellable = controller.observeState { [weak self] channel, isPlaying in
            self?.applySnapshot(channel: channel, isPlaying: isPlaying)
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

    private func applySnapshot(channel: Channel?, isPlaying: Bool) {
        self.currentChannel = channel
        self.isPlaying = isPlaying
    }
}
