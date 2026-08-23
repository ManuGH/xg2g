// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

// Three identities, deliberately separate.
//
// They were one number once, and every time two of them coincided something worked by
// accident until a second session made them differ:
//
//   - the session's zap counter says which stream this session has started, and is
//     what the recovery paths test before they act;
//   - the recovery epoch says which attempt at rebuilding this session's decoder and
//     audio is current, and is what a late result must be checked against;
//   - the presentation generation says which stream the visible surface belongs to,
//     and is the only one the surface compares.
//
// Keeping them apart is what stops a stale answer from an abandoned attempt landing on
// a session that has moved on.

/// Where a session is in its own lifecycle, independent of who owns the screen.
public enum PlaybackLifecycle: String, Sendable {
    /// Decoding and buffering normally.
    case stable
    /// Rebuilding after a renderer failure, a flush, or a timeline that moved. The
    /// session holds no usable anchor while this is true, and must not be committed.
    case recovering
}

/// Identifies one attempt at recovery.
///
/// Recovery runs asynchronously, and a second failure can arrive before the first
/// attempt has finished. Without this, the late half of attempt one would clear the
/// state attempt two had just rebuilt - and the session would sit unanchorable with
/// nothing to show for it. Every asynchronous step carries the epoch it belongs to and
/// does nothing if the session has moved past it.
public struct RecoveryEpoch: Hashable, Sendable, CustomStringConvertible {
    public let value: Int
    public var description: String { "recovery-\(value)" }

    /// The epoch of a session that has never had to recover.
    public static let initial = RecoveryEpoch(value: 0)

    public func next() -> RecoveryEpoch { RecoveryEpoch(value: value + 1) }
}
