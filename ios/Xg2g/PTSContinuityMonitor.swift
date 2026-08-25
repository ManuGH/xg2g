// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation

/// Watches a stream's timestamps for the jumps that mean the timeline itself
/// moved, as opposed to ordinary spacing between frames.
///
/// Broadcast does this legitimately and often enough to matter: an encoder
/// restart, a splice, or — the common one here — somebody changing channel on
/// the receiver while the app is streaming from it. What arrives afterwards is
/// not late or early data on the old timeline, it is a new timeline, and
/// anything anchored to the old one has to be re-anchored rather than waited
/// out. Waiting it out is what the pipeline did: the master clock kept running
/// where it was, every arriving buffer was timestamped somewhere it could not
/// reach, and playback stopped until the channel was tuned again.
///
/// Deliberately a separate value type. The thresholds are the whole of the
/// decision, they are easy to get wrong in both directions, and inside the
/// pipeline they would only ever be exercised by a live broadcast doing
/// something rare.
public struct PTSContinuityMonitor: Sendable, Equatable {

    /// Forward gap beyond which the timeline is taken to have moved.
    ///
    /// Far above any legitimate spacing — an AC-3 frame is 32 ms, an AAC frame
    /// at 48 kHz is 21.3 ms, a 1080i50 picture 40 ms — so ordinary cadence can
    /// never reach it.
    ///
    /// A network stall does not land here either, which is the distinction that
    /// matters: buffered data keeps the timestamps it was encoded with, so a gap
    /// in *arrival* time is not a gap in *presentation* time. The socket-stall
    /// counter measures that, and it measures something else.
    public static let forwardJumpSeconds: Double = 0.5

    /// Timestamps advance within a timeline, so any real step backwards is a
    /// discontinuity. The tolerance absorbs the small reordering a multiplex can
    /// produce without reading it as a new timeline.
    public static let backwardJumpSeconds: Double = 0.1

    public private(set) var lastPTS: CMTime = .invalid

    public init() {}

    public mutating func reset() {
        lastPTS = .invalid
    }

    /// Returns how far the timeline moved, in seconds, or nil when `pts`
    /// continues the current one.
    ///
    /// The first valid timestamp establishes the timeline and is never itself a
    /// discontinuity. Invalid timestamps are ignored rather than treated as a
    /// jump: an access unit can legitimately carry none, and letting that reset
    /// the reference would make the *next* real timestamp look like a jump.
    public mutating func jump(for pts: CMTime) -> Double? {
        guard pts.isValid else { return nil }
        guard lastPTS.isValid else {
            lastPTS = pts
            return nil
        }

        let delta = pts.seconds - lastPTS.seconds
        lastPTS = pts

        if delta > Self.forwardJumpSeconds || delta < -Self.backwardJumpSeconds {
            return delta
        }
        return nil
    }
}
