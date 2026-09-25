// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import CoreMedia
import Foundation
import OSLog

private let logger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "playback-clock")

/// Master A/V playback clock authority for a playback session.
///
/// Encapsulates an `AVSampleBufferRenderSynchronizer` and governs master timebase rate,
/// anchor synchronization, renderer attachment/detachment, and boundary time observation.
/// Decouples clock ownership from audio rendering so that video and audio are timed
/// against a single authoritative clock owner.
public final class PlaybackClock: @unchecked Sendable {

    /// The underlying system render synchronizer timing both audio and video renderers.
    public private(set) var synchronizer: AVSampleBufferRenderSynchronizer

    /// The master timebase established by the synchronizer.
    public var timebase: CMTimebase {
        synchronizer.timebase
    }

    /// Current rate of the master clock timebase.
    public var rate: Float {
        Float(CMTimebaseGetRate(timebase))
    }

    /// Current presentation timestamp of the master clock.
    public var currentTime: CMTime {
        CMTimebaseGetTime(timebase)
    }

    /// True if the master clock is actively running (rate > 0).
    public var isClockRunning: Bool {
        rate > 0
    }

    public init(synchronizer: AVSampleBufferRenderSynchronizer = AVSampleBufferRenderSynchronizer()) {
        self.synchronizer = synchronizer
    }

    /// Sets the playback clock rate starting at a specific reference PTS.
    public func setRate(_ rate: Float, time: CMTime) {
        synchronizer.setRate(rate, time: time)
    }

    /// Shorthand to start playback at rate 1.0 from a given anchor timestamp.
    public func start(at anchor: CMTime) {
        setRate(1.0, time: anchor)
    }

    /// Shorthand to park the clock at rate 0.0 with an invalid timestamp.
    public func stop() {
        setRate(0.0, time: .invalid)
    }

    /// Rebuilds the synchronizer for full session teardown, returning the retired
    /// synchronizer so the caller can keep it alive until asynchronous detachments finish.
    @discardableResult
    public func reset() -> AVSampleBufferRenderSynchronizer {
        let old = synchronizer
        synchronizer = AVSampleBufferRenderSynchronizer()
        return old
    }

    /// Attaches an audio or video queued sample buffer renderer to this clock.
    public func attachRenderer(_ renderer: AVQueuedSampleBufferRendering) {
        synchronizer.addRenderer(renderer)
    }

    /// Asynchronously detaches a renderer from this clock, keeping it alive until AVFoundation finishes.
    public func detachRenderer(
        _ renderer: AVQueuedSampleBufferRendering,
        at time: CMTime = .invalid,
        completion: (@Sendable () -> Void)? = nil
    ) {
        let token = RetainedToken(renderer)
        synchronizer.removeRenderer(renderer, at: time) { _ in
            _ = token
            completion?()
        }
    }

    /// Registers a boundary time observer on this clock.
    public func addBoundaryTimeObserver(
        forTimes times: [NSValue],
        queue: DispatchQueue?,
        using block: @Sendable @escaping () -> Void
    ) -> Any {
        synchronizer.addBoundaryTimeObserver(forTimes: times, queue: queue, using: block)
    }

    /// Removes a previously registered boundary time observer.
    public func removeTimeObserver(_ observer: Any) {
        synchronizer.removeTimeObserver(observer)
    }
}

private final class RetainedToken: @unchecked Sendable {
    let value: Any
    init(_ value: Any) {
        self.value = value
    }
}
