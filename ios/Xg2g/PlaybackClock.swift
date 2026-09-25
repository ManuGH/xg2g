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
///
/// Execution Contract & Serialization Guarantee:
/// All mutative clock operations (`setRate`, `start`, `stop`, `reset`, `attachRenderer`, `detachRenderer`),
/// observer operations (`addBoundaryTimeObserver`, `removeTimeObserver`), and state queries (`rate`,
/// `timebase`, `currentTime`, `synchronizer`) are strictly serialized under an internal lock.
/// This guarantees:
/// 1. `setRate` and `attachRenderer` never operate on a synchronizer that is concurrently being retired by `reset()`.
/// 2. `reset()` atomically replaces the internal synchronizer; any in-flight or subsequent clock operation
///    either completes on the old synchronizer prior to reset or executes cleanly against the new synchronizer.
/// 3. Detached renderers and retired synchronizers are strongly retained in `activeDetachTokens` until AVFoundation
///    asynchronously signals completion via `removeRenderer` completion handlers.
public final class PlaybackClock: @unchecked Sendable {

    private let lock = NSRecursiveLock()
    private var _synchronizer: AVSampleBufferRenderSynchronizer
    private var activeDetachTokens: [RetainedDetachToken] = []

    /// The underlying system render synchronizer timing both audio and video renderers.
    public var synchronizer: AVSampleBufferRenderSynchronizer {
        lock.lock()
        defer { lock.unlock() }
        return _synchronizer
    }

    /// The master timebase established by the synchronizer.
    public var timebase: CMTimebase {
        lock.lock()
        defer { lock.unlock() }
        return _synchronizer.timebase
    }

    /// Current rate of the master clock timebase.
    public var rate: Float {
        lock.lock()
        defer { lock.unlock() }
        return Float(CMTimebaseGetRate(_synchronizer.timebase))
    }

    /// Current presentation timestamp of the master clock.
    public var currentTime: CMTime {
        lock.lock()
        defer { lock.unlock() }
        return CMTimebaseGetTime(_synchronizer.timebase)
    }

    /// True if the master clock is actively running (rate > 0).
    public var isClockRunning: Bool {
        rate > 0
    }

    public init(synchronizer: AVSampleBufferRenderSynchronizer = AVSampleBufferRenderSynchronizer()) {
        self._synchronizer = synchronizer
    }

    /// Sets the playback clock rate starting at a specific reference PTS.
    /// Serialized against synchronizer replacement (`reset()`).
    public func setRate(_ rate: Float, time: CMTime) {
        lock.lock()
        defer { lock.unlock() }
        _synchronizer.setRate(rate, time: time)
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
    /// synchronizer. Atomic and serialized with all other clock operations.
    /// In-flight detachments keep the old synchronizer alive via RetainedDetachToken.
    @discardableResult
    public func reset() -> AVSampleBufferRenderSynchronizer {
        lock.lock()
        defer { lock.unlock() }
        let old = _synchronizer
        _synchronizer = AVSampleBufferRenderSynchronizer()
        return old
    }

    /// Attaches an audio or video queued sample buffer renderer to this clock.
    /// Serialized against synchronizer replacement (`reset()`).
    public func attachRenderer(_ renderer: AVQueuedSampleBufferRendering) {
        lock.lock()
        defer { lock.unlock() }
        _synchronizer.addRenderer(renderer)
    }

    /// Asynchronously detaches a renderer from this clock (or an explicit synchronizer),
    /// guaranteeing that BOTH the synchronizer and the renderer remain strongly retained in memory
    /// until AVFoundation completes the removal operation and fires the completion callback.
    public func detachRenderer(
        _ renderer: AVQueuedSampleBufferRendering,
        from explicitSynchronizer: AVSampleBufferRenderSynchronizer? = nil,
        at time: CMTime = .invalid,
        completion: (@Sendable () -> Void)? = nil
    ) {
        lock.lock()
        let syncToDetach = explicitSynchronizer ?? _synchronizer
        let token = RetainedDetachToken(synchronizer: syncToDetach, renderer: renderer)
        activeDetachTokens.append(token)
        syncToDetach.removeRenderer(renderer, at: time) { [weak self] _ in
            if let self {
                self.lock.lock()
                self.activeDetachTokens.removeAll(where: { $0 === token })
                self.lock.unlock()
            }
            _ = token
            completion?()
        }
        lock.unlock()
    }

    /// Registers a boundary time observer on this clock.
    public func addBoundaryTimeObserver(
        forTimes times: [NSValue],
        queue: DispatchQueue?,
        using block: @Sendable @escaping () -> Void
    ) -> Any {
        lock.lock()
        defer { lock.unlock() }
        return _synchronizer.addBoundaryTimeObserver(forTimes: times, queue: queue, using: block)
    }

    /// Removes a previously registered boundary time observer.
    public func removeTimeObserver(_ observer: Any) {
        lock.lock()
        defer { lock.unlock() }
        _synchronizer.removeTimeObserver(observer)
    }
}

/// Strongly holds both the synchronizer and the renderer for the duration of an asynchronous detach.
private final class RetainedDetachToken: @unchecked Sendable {
    let synchronizer: AVSampleBufferRenderSynchronizer
    let renderer: AVQueuedSampleBufferRendering

    init(synchronizer: AVSampleBufferRenderSynchronizer, renderer: AVQueuedSampleBufferRendering) {
        self.synchronizer = synchronizer
        self.renderer = renderer
    }
}
