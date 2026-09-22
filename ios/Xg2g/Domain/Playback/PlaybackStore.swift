// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Observation

/// UI-facing store managing player state and channel tuning actions.
///
/// Bound to `@MainActor` for seamless observation by SwiftUI player overlays, MiniPlayer bars,
/// and lockscreen transport observers.
@MainActor
@Observable
final class PlaybackStore {
    private(set) var currentChannel: Channel?
    private(set) var isPlaying: Bool = false
    private(set) var errorMessage: String?

    private weak var controller: (any PlaybackControlling)?

    init(controller: (any PlaybackControlling)? = nil) {
        self.controller = controller
        if let controller {
            self.currentChannel = controller.currentChannel
            self.isPlaying = controller.isPlaying
        }
    }

    /// Attaches an active playback controller.
    func attach(controller: any PlaybackControlling) {
        self.controller = controller
        self.currentChannel = controller.currentChannel
        self.isPlaying = controller.isPlaying
    }

    /// Tunes to a specific channel.
    func play(channel: Channel) {
        currentChannel = channel
        isPlaying = true
        errorMessage = nil
        controller?.play(channel: channel)
    }

    /// Stops playback and dismisses active playback state.
    func stop() {
        currentChannel = nil
        isPlaying = false
        controller?.stop()
    }

    /// Toggles play / pause state.
    func togglePlayPause() {
        isPlaying.toggle()
        controller?.togglePlayPause()
    }

    /// Updates internal state from external controller notifications.
    func updateState(channel: Channel?, isPlaying: Bool, errorMessage: String? = nil) {
        self.currentChannel = channel
        self.isPlaying = isPlaying
        self.errorMessage = errorMessage
    }
}
