// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Combine

/// Minimal contract for controlling video playback and projecting canonical state.
///
/// In Apple A/B, this is deliberately an `AnyObject` protocol rather than an actor to prevent
/// impedance mismatches with AVFoundation and UIKit/AppKit presentation lifecycles.
@MainActor
protocol PlaybackControlling: AnyObject {
    /// Currently active broadcast channel, if any.
    var currentChannel: Channel? { get }

    /// Whether playback is currently active and presenting frames.
    var isPlaying: Bool { get }

    /// Tunes and starts playback for the specified channel.
    func play(channel: Channel)

    /// Stops playback and releases presentation resources.
    func stop()

    /// Toggles play / pause transport state.
    func togglePlayPause()

    /// Subscribes to canonical state projection updates.
    ///
    /// The handler is invoked immediately with the initial snapshot, and subsequently
    /// whenever the canonical `PlaybackManager` state transitions.
    func observeState(_ handler: @escaping @MainActor (_ channel: Channel?, _ isPlaying: Bool) -> Void) -> AnyCancellable
}
