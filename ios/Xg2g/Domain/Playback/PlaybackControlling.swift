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
    /// Currently active canonical playback target (Live, Recording, or Offline), if any.
    var currentTarget: PlaybackTarget? { get }

    /// Currently active broadcast channel, if any.
    var currentChannel: Channel? { get }

    /// Currently active recording, if any.
    var currentRecording: Recording? { get }

    /// Whether a playback target is actively engaged in presentation mode.
    ///
    /// - Note: For live playback, this reflects an active tuner stream session from `ZapCoordinator`.
    ///   For VOD recordings and offline items in C1, this indicates that the target is actively
    ///   selected and in a visible presentation mode; it does NOT assert that AVPlayer has buffered,
    ///   rendered frames, or reached a non-zero playback rate. Rate-based and frame-level presentation
    ///   authority is deferred to subsequent player-session convergence slices.
    var isPlaying: Bool { get }

    /// Tunes and starts playback for the specified channel.
    func play(channel: Channel)

    /// Starts VOD playback for a recording at an optional start offset in seconds.
    func play(recording: Recording, startPosition: Double?)

    /// Seeks to a specific timestamp in seconds (VOD / DVR recordings).
    func seek(to position: Double)

    /// Stops playback and releases presentation resources.
    func stop()

    /// Toggles play / pause transport state.
    ///
    /// - Note: In C1, this exposes the legacy live TV toggle behavior (stopping an active stream or
    ///   re-tuning the current channel). It does NOT yet provide genuine VOD pause/resume behavior
    ///   for recordings. True VOD pause/resume requires a dedicated paused session state without
    ///   tearing down player resources, which is deferred to subsequent player-session convergence.
    func togglePlayPause()

    /// Subscribes to canonical state projection updates (channel-focused).
    ///
    /// The handler is invoked immediately with the initial snapshot, and subsequently
    /// whenever the canonical `PlaybackManager` state transitions.
    func observeState(_ handler: @escaping @MainActor (_ channel: Channel?, _ isPlaying: Bool) -> Void) -> AnyCancellable

    /// Subscribes to canonical target projection updates (polymorphic target).
    ///
    /// The handler is invoked immediately with the initial snapshot, and subsequently
    /// whenever the canonical `PlaybackManager` state transitions.
    func observeTargetState(_ handler: @escaping @MainActor (_ target: PlaybackTarget?, _ isPlaying: Bool) -> Void) -> AnyCancellable
}

extension PlaybackControlling {
    var currentChannel: Channel? {
        if case .live(let channel) = currentTarget { return channel }
        return nil
    }

    var currentRecording: Recording? {
        if case .recording(let rec, _) = currentTarget { return rec }
        return nil
    }

    func play(recording: Recording) {
        play(recording: recording, startPosition: nil)
    }

    func observeState(_ handler: @escaping @MainActor (_ channel: Channel?, _ isPlaying: Bool) -> Void) -> AnyCancellable {
        observeTargetState { target, isPlaying in
            let channel: Channel?
            if case .live(let ch) = target {
                channel = ch
            } else {
                channel = nil
            }
            handler(channel, isPlaying)
        }
    }
}
