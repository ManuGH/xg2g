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
/// Thread safety is enforced via an internal lock protecting synchronizer replacement,
/// timebase queries, and active detach token lifetimes.
public final class PlaybackClock: @unchecked Sendable {

    private let lock = NSLock()
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
        let sync = _synchronizer
        lock.unlock()
        return sync.timebase
    }

    /// Current rate of the master clock timebase.
    public var rate: Float {
        lock.lock()
        let sync = _synchronizer
        lock.unlock()
        return Float(CMTimebaseGetRate(sync.timebase))
    }

    /// Current presentation timestamp of the master clock.
    public var currentTime: CMTime {
        lock.lock()
        let sync = _synchronizer
        lock.unlock()
        return CMTimebaseGetTime(sync.timebase)
    }

    /// True if the master clock is actively running (rate > 0).
    public var isClockRunning: Bool {
        rate > 0
    }

    public init(synchronizer: AVSampleBufferRenderSynchronizer = AVSampleBufferRenderSynchronizer()) {
        self._synchronizer = synchronizer
    }

    /// Sets the playback clock rate starting at a specific reference PTS.
    public func setRate(_ rate: Float, time: CMTime) {
        lock.lock()
        let sync = _synchronizer
        lock.unlock()
        sync.setRate(rate, time: time)
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
    /// synchronizer. In-flight detachments keep the old synchronizer alive via RetainedDetachToken.
    @discardableResult
    public func reset() -> AVSampleBufferRenderSynchronizer {
        lock.lock()
        let old = _synchronizer
        _synchronizer = AVSampleBufferRenderSynchronizer()
        lock.unlock()
        return old
    }

    /// Attaches an audio or video queued sample buffer renderer to this clock.
    public func attachRenderer(_ renderer: AVQueuedSampleBufferRendering) {
        lock.lock()
        let sync = _synchronizer
        lock.unlock()
        sync.addRenderer(renderer)
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
        lock.unlock()

        syncToDetach.removeRenderer(renderer, at: time) { [weak self] _ in
            if let self {
                self.lock.lock()
                self.activeDetachTokens.removeAll(where: { $0 === token })
                self.lock.unlock()
            }
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
        lock.lock()
        let sync = _synchronizer
        lock.unlock()
        return sync.addBoundaryTimeObserver(forTimes: times, queue: queue, using: block)
    }

    /// Removes a previously registered boundary time observer.
    public func removeTimeObserver(_ observer: Any) {
        lock.lock()
        let sync = _synchronizer
        lock.unlock()
        sync.removeTimeObserver(observer)
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
