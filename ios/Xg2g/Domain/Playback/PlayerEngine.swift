// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Identifies the playback engine implementation.
public enum PlayerEngineKind: String, Sendable, Equatable {
    case nativeLive
    case avPlayerRecording
    case avPlayerOffline
    case timeshift
}

/// Execution boundary for video playback engines.
///
/// `PlayerEngine` defines transport mechanics only (`play()`, `pause()`, `stop()`).
/// It holds NO canonical playback state authority (`PlaybackManager` remains the single
/// source of truth for `.idle`, `.live`, `.recording`, `.offline`).
@MainActor
public protocol PlayerEngine: AnyObject {
    /// The specific engine type.
    var kind: PlayerEngineKind { get }

    /// Starts or resumes playback.
    func play() async throws

    /// Pauses playback if supported by the engine.
    func pause() async

    /// Stops playback and releases engine execution resources.
    func stop() async
}
