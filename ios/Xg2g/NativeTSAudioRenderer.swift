// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import CoreMedia
import Foundation
import OSLog

private let logger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "audio-renderer")

public protocol NativeTSAudioRendererDelegate: AnyObject, Sendable {
    func audioRendererDidEncounterError(_ renderer: NativeTSAudioRenderer, rendererToken: Int64, error: Error)
    func audioRendererDidChangeStatus(_ renderer: NativeTSAudioRenderer, rendererToken: Int64, status: AVQueuedSampleBufferRenderingStatus)
}

/// Plays compressed audio (`CMSampleBuffer`s) using `AVSampleBufferAudioRenderer`
/// synchronized via `AVSampleBufferRenderSynchronizer`.
///
/// Principles:
/// - Master clock is owned by PlaybackClock; audio renderer attaches as a sink.
/// - Supports immediate `flush()` on discontinuity, channel zap, or reset.
public final class NativeTSAudioRenderer: @unchecked Sendable {

    public private(set) var clock: PlaybackClock
    private let bufferLock = NSLock()
    private var _audioRenderer: AVSampleBufferAudioRenderer
    public var audioRenderer: AVSampleBufferAudioRenderer {
        bufferLock.lock()
        defer { bufferLock.unlock() }
        return _audioRenderer
    }

    private var _isAttachedToClock: Bool = true
    public var isAttachedToClock: Bool {
        bufferLock.lock()
        defer { bufferLock.unlock() }
        return _isAttachedToClock
    }

    private var _rendererToken: Int64 = 1
    public var activeRendererToken: Int64 {
        bufferLock.lock()
        defer { bufferLock.unlock() }
        return _rendererToken
    }

    public func activeRendererTokenIfCurrent(_ renderer: AVSampleBufferAudioRenderer) -> Int64? {
        bufferLock.lock()
        defer { bufferLock.unlock() }
        guard _isAttachedToClock && renderer === _audioRenderer else { return nil }
        return _rendererToken
    }

    #if DEBUG
    public var onBeforeDelegateErrorDelivery: ((Int64, Error) -> Void)?
    public var onBeforeDelegateStatusDelivery: ((Int64, AVQueuedSampleBufferRenderingStatus) -> Void)?

    public func simulateFailureForTesting(error: Error) {
        guard let token = activeRendererTokenIfCurrent(_audioRenderer) else { return }
        notifyErrorEncountered(rendererToken: token, error: error)
    }

    public func simulateStatusChangeForTesting(status: AVQueuedSampleBufferRenderingStatus) {
        guard let token = activeRendererTokenIfCurrent(_audioRenderer) else { return }
        notifyStatusChanged(rendererToken: token, status: status)
    }
    #endif

    private func notifyErrorEncountered(rendererToken: Int64, error: Error) {
        #if DEBUG
        onBeforeDelegateErrorDelivery?(rendererToken, error)
        #endif
        delegate?.audioRendererDidEncounterError(self, rendererToken: rendererToken, error: error)
    }

    private func notifyStatusChanged(rendererToken: Int64, status: AVQueuedSampleBufferRenderingStatus) {
        #if DEBUG
        onBeforeDelegateStatusDelivery?(rendererToken, status)
        #endif
        delegate?.audioRendererDidChangeStatus(self, rendererToken: rendererToken, status: status)
    }

    public var synchronizer: AVSampleBufferRenderSynchronizer {
        clock.synchronizer
    }

    private let renderQueue = DispatchQueue(label: "io.github.manugh.xg2g.audio.renderer", qos: .userInteractive)
    private var isAudioSessionActive = false

    private var pendingBuffers: [CMSampleBuffer] = []
    private var enqueuedCount = 0
    /// The span of audio already handed to the system renderer.
    private var enqueuedStartPTS: CMTime?
    private var enqueuedEndPTS: CMTime?
    private var lastDiagnosticLogTime: CFTimeInterval = 0

    /// True while a backpressure `requestMediaDataWhenReady` is armed. Guarded by
    /// `bufferLock` together with `pendingBuffers`, because arming and tearing
    /// down have to agree with the queue's emptiness to stay lossless.
    private var isRequestingData = false

    /// How far the audio handed to the renderer runs ahead of the playback clock.
    ///
    /// This is the number that decides whether the ear hears a dropout, and until
    /// now nothing measured it — "massive Aussetzer" had no counterpart in any
    /// log. In steady playback the lead sits at roughly the pre-roll cushion; when
    /// it reaches zero the renderer has nothing left to play and the sound breaks
    /// up. Counted, not inferred.
    private var underrunCount = 0
    private var minLeadMs: Double = .greatestFiniteMagnitude
    private var lastLeadMs: Double = 0

    /// Lead below this counts as an underrun — close enough to empty that any
    /// further jitter is audible.
    private static let underrunThresholdMs: Double = 50.0

    public struct AudioFlowStats: Sendable {
        /// How many buffers have been handed to the system renderer.
        ///
        /// The queue length says what is waiting; this says what already went through,
        /// which is what proves a prepared session's audio really reached the renderer
        /// that would play it rather than stopping somewhere earlier.
        public let enqueued: Int
        public let underruns: Int
        public let minLeadMs: Double
        public let currentLeadMs: Double
        public let pendingBuffers: Int
    }

    /// Current flow figures, and resets the windowed minimum for the next report.
    public func consumeFlowStats() -> AudioFlowStats {
        bufferLock.lock()
        defer { bufferLock.unlock() }
        let stats = AudioFlowStats(
            enqueued: enqueuedCount,
            underruns: underrunCount,
            minLeadMs: minLeadMs == .greatestFiniteMagnitude ? 0 : minLeadMs,
            currentLeadMs: lastLeadMs,
            pendingBuffers: pendingBuffers.count
        )
        minLeadMs = .greatestFiniteMagnitude
        return stats
    }

    public weak var delegate: NativeTSAudioRendererDelegate?

    public var timebase: CMTimebase {
        return clock.timebase
    }

    public var status: AVQueuedSampleBufferRenderingStatus {
        bufferLock.lock()
        defer { bufferLock.unlock() }
        return _audioRenderer.status
    }

    private var statusObserver: NSKeyValueObservation?

    public init(clock: PlaybackClock = PlaybackClock()) {
        self.clock = clock
        let renderer = AVSampleBufferAudioRenderer()
        self._audioRenderer = renderer
        clock.attachRenderer(renderer)
        // Silent until granted audibility. A session is built to be prepared, and
        // preparing one must never be heard.
        renderer.isMuted = true
        renderer.volume = 0.0
        setupStatusObserver()
    }

    /// Binds this audio renderer to a session's master playback clock.
    public func bind(to newClock: PlaybackClock) {
        guard self.clock !== newClock else { return }
        detachFromClock()

        self.clock = newClock
        let freshRenderer = AVSampleBufferAudioRenderer()
        freshRenderer.isMuted = !isAudible
        freshRenderer.volume = isAudible ? 1.0 : 0.0

        bufferLock.lock()
        self._audioRenderer = freshRenderer
        self._isAttachedToClock = true
        self._rendererToken += 1
        self.clock.attachRenderer(freshRenderer)
        setupStatusObserver()
        enqueuedCount = 0
        lastDiagnosticLogTime = 0
        underrunCount = 0
        minLeadMs = .greatestFiniteMagnitude
        lastLeadMs = 0
        bufferLock.unlock()
    }

    private func setupStatusObserver() {
        statusObserver?.invalidate()
        let renderer = _audioRenderer
        statusObserver = renderer.observe(\.status, options: [.new]) { [weak self] observedRenderer, _ in
            guard let self = self else { return }
            guard let token = self.activeRendererTokenIfCurrent(observedRenderer) else { return }
            self.notifyStatusChanged(rendererToken: token, status: observedRenderer.status)
        }
    }

    /// Whether this renderer contributes sound.
    ///
    /// Muted until the session it belongs to owns the visible surface, and muted again
    /// when it loses it. A parked clock is the primary guarantee that a session being
    /// prepared beside a playing one is silent; this is the second, and it does not
    /// depend on anyone remembering not to start a rate.
    ///
    /// Configuring the process-wide AVAudioSession is deliberately not done here. That
    /// is player-lifetime policy, and a playback session that owned it would reconfigure
    /// the whole process every time a channel was prepared.
    public func setAudible(_ audible: Bool) {
        bufferLock.lock()
        isAudible = audible
        let renderer = _audioRenderer
        bufferLock.unlock()

        renderer.isMuted = !audible
        renderer.volume = audible ? 1.0 : 0.0
    }

    /// Whether this renderer has been granted audibility.
    ///
    /// Remembered rather than read back from the renderer, because `reset` replaces the
    /// renderer with a fresh one - and a fresh `AVSampleBufferAudioRenderer` is audible
    /// by default. A prepared session that hit a recovery reset would have unmuted
    /// itself without anyone asking.
    public private(set) var isAudible = false

    public struct PruneResult: Sendable {
        public let prunedCount: Int
        public let firstKeptPTS: CMTime?
        public let lastPrunedPTS: CMTime?
        public let remainingLeadMs: Double
    }

    /// Explicitly prunes audio buffers in pendingBuffers that end strictly before `anchor`.
    /// Buffers that overlap `anchor` (i.e. pts + duration > anchor) are KEPT intact.
    /// Whether audio is available from a given instant onwards.
    ///
    /// Asked before a prepared session is committed. "Enough bytes buffered" is not
    /// the question: what matters is that a buffer actually covers the anchor the
    /// clock will start on, and that there is some audio beyond it. A session with a
    /// picture and no audio at that instant would be committed and then hold the
    /// picture still while audio caught up, which is the frozen start this rebuild
    /// exists to remove.
    public func hasBuffersCovering(_ anchor: CMTime) -> Bool {
        guard anchor.isValid else { return false }
        bufferLock.lock()
        defer { bufferLock.unlock() }

        var covers = false
        var endsAfter = false

        // Audio already handed to the renderer counts: it is still audio this session
        // holds at that instant, and it is what will play from the anchor.
        if let start = enqueuedStartPTS, let end = enqueuedEndPTS, start.isValid, end.isValid {
            if CMTimeCompare(start, anchor) <= 0, CMTimeCompare(end, anchor) > 0 { covers = true }
            if CMTimeCompare(end, anchor) > 0 { endsAfter = true }
        }

        for buffer in pendingBuffers {
            let pts = CMSampleBufferGetPresentationTimeStamp(buffer)
            guard pts.isValid else { continue }
            let duration = CMSampleBufferGetDuration(buffer)
            let end = duration.isValid ? CMTimeAdd(pts, duration) : pts
            // Covers the anchor: starts at or before it and ends after it.
            if CMTimeCompare(pts, anchor) <= 0, CMTimeCompare(end, anchor) > 0 {
                covers = true
            }
            if CMTimeCompare(end, anchor) > 0 {
                endsAfter = true
            }
        }
        return covers && endsAfter
    }

    public func pruneBuffersBefore(time anchor: CMTime) -> PruneResult {
        bufferLock.lock()
        defer { bufferLock.unlock() }

        var pruned = 0
        var lastPruned: CMTime? = nil

        while let first = pendingBuffers.first {
            let pts = CMSampleBufferGetPresentationTimeStamp(first)
            let duration = CMSampleBufferGetDuration(first)
            let endTime = (pts.isValid && duration.isValid) ? CMTimeAdd(pts, duration) : pts
            if endTime.isValid && CMTimeCompare(endTime, anchor) <= 0 {
                // Strictly before anchor -> prune
                lastPruned = pts
                pendingBuffers.removeFirst()
                pruned += 1
            } else {
                // Overlaps anchor or is ahead -> keep
                break
            }
        }

        let firstKept = pendingBuffers.first.map { CMSampleBufferGetPresentationTimeStamp($0) }
        let lastBuffer = pendingBuffers.last.map { CMSampleBufferGetPresentationTimeStamp($0) }
        let remainingLeadMs: Double
        if let last = lastBuffer, last.isValid, anchor.isValid {
            remainingLeadMs = max(0, (last.seconds - anchor.seconds) * 1000.0)
        } else {
            remainingLeadMs = 0
        }

        return PruneResult(
            prunedCount: pruned,
            firstKeptPTS: firstKept,
            lastPrunedPTS: lastPruned,
            remainingLeadMs: remainingLeadMs
        )
    }

    /// Enqueues a parsed `CMSampleBuffer` (AC-3, E-AC-3, or AAC) for playback.
    ///
    /// The source is a live broadcast: data arrives on the stream's schedule, so
    /// arrival is what drives the renderer. See `drainPendingBuffers` for why
    /// `requestMediaDataWhenReady` is not used as the driver.
    public func enqueue(sampleBuffer: CMSampleBuffer) {
        bufferLock.lock()
        pendingBuffers.append(sampleBuffer)
        bufferLock.unlock()

        renderQueue.async { [weak self] in
            self?.drainPendingBuffers()
        }
    }

    /// Hands pending buffers to the renderer until it is full or nothing is left.
    ///
    /// A push model, with `requestMediaDataWhenReady` demoted to a backpressure
    /// signal: it is armed only while the renderer has actually reported itself
    /// full, and torn down the moment the queue empties.
    ///
    /// It cannot be the driver here. Its block is re-invoked immediately whenever
    /// it returns while `isReadyForMoreMediaData` is still true, and a live stream
    /// carrying ~190 ms of pre-roll leaves the renderer hungry nearly all the
    /// time — so driving from it pinned a `.userInteractive` core at 100 % and
    /// heated the device. Disarming on every dry moment stopped the spin but
    /// replaced it with a stop/arm cycle per 32 ms AC-3 frame, which stuttered.
    /// Arming only under real backpressure has neither failure mode: while the
    /// request is live there is by construction data waiting for it.
    ///
    /// Runs on the serial `renderQueue`, from `enqueue` and from the backpressure
    /// block, so only one drain is ever in flight.
    private func drainPendingBuffers() {
        while true {
            bufferLock.lock()

            guard !pendingBuffers.isEmpty else {
                // Never leave the request armed with an empty queue — that is the
                // exact state the spin loop lived in.
                if isRequestingData {
                    _audioRenderer.stopRequestingMediaData()
                    isRequestingData = false
                }
                bufferLock.unlock()
                return
            }

            guard _audioRenderer.isReadyForMoreMediaData else {
                // Full. Ask to be told when it drains, and stop pushing until then.
                if !isRequestingData {
                    isRequestingData = true
                    _audioRenderer.requestMediaDataWhenReady(on: renderQueue) { [weak self] in
                        self?.drainPendingBuffers()
                    }
                }
                bufferLock.unlock()
                return
            }

            let renderer = _audioRenderer
            let buffer = pendingBuffers.removeFirst()
            enqueuedCount += 1
            // Remembered because readiness is asked about the timeline, not about the
            // queue. A buffer handed to the renderer has left `pendingBuffers` but is
            // still audio the session holds at that instant, and a readiness check that
            // only looked at what was still waiting reported a gap that did not exist.
            let handedPTS = CMSampleBufferGetPresentationTimeStamp(buffer)
            let handedDuration = CMSampleBufferGetDuration(buffer)
            if handedPTS.isValid {
                if enqueuedStartPTS == nil { enqueuedStartPTS = handedPTS }
                enqueuedEndPTS = handedDuration.isValid ? CMTimeAdd(handedPTS, handedDuration) : handedPTS
            }
            let currentCount = enqueuedCount

            // Only meaningful once the clock actually runs; before that the
            // timebase sits at zero and every lead would read as astronomical.
            var leadMs: Double = 0
            if clock.rate > 0 {
                let pts = CMSampleBufferGetPresentationTimeStamp(buffer)
                let clockTime = clock.currentTime
                if pts.isValid && clockTime.isValid {
                    leadMs = (pts.seconds - clockTime.seconds) * 1000.0
                    lastLeadMs = leadMs
                    minLeadMs = min(minLeadMs, leadMs)
                    if leadMs < Self.underrunThresholdMs {
                        underrunCount += 1
                    }
                }
            }

            let now = CACurrentMediaTime()
            let shouldLog = (now - lastDiagnosticLogTime >= 2.0)
            if shouldLog {
                lastDiagnosticLogTime = now
            }
            bufferLock.unlock()

            renderer.enqueue(buffer)

            if shouldLog {
                let pts = CMSampleBufferGetPresentationTimeStamp(buffer)
                let dur = CMSampleBufferGetDuration(buffer)
                let statusStr: String
                switch renderer.status {
                case .unknown: statusStr = "unknown"
                case .rendering: statusStr = "rendering"
                case .failed: statusStr = "failed (\(renderer.error?.localizedDescription ?? "unknown"))"
                @unknown default: statusStr = "other"
                }
                let session = AVAudioSession.sharedInstance()
                let diag = "[AudioRenderer] 📊 Enqueued: \(currentCount) | Status: \(statusStr) | PTS: \(String(format: "%.3f", pts.seconds))s | Dur: \(String(format: "%.1f", dur.seconds * 1000))ms | Rate: \(self.clock.rate) | Time: \(String(format: "%.3f", self.clock.currentTime.seconds))s | Lead: \(String(format: "%.0f", leadMs))ms | Ready: \(renderer.isReadyForMoreMediaData) | Route: \(session.outputNumberOfChannels)/\(session.maximumOutputNumberOfChannels)ch"
                print(diag)
                logger.notice("\(diag, privacy: .public)")
                TelemetryServer.shared.log(diag)
            }

            if renderer.status == .failed, let error = renderer.error {
                guard let token = self.activeRendererTokenIfCurrent(renderer) else { return }
                let errStr = "[AudioRenderer] ❌ Render error: \(error.localizedDescription)"
                print(errStr)
                logger.error("\(errStr, privacy: .public)")
                self.notifyErrorEncountered(rendererToken: token, error: error)
                return
            }
        }
    }

    /// Immediately flushes all queued and in-flight audio sample buffers.
    public func flush() {
        // Under the same lock as the drain loop's arm/disarm decision, so a flush
        // racing a drain cannot leave the request armed with an empty queue — the
        // state the spin loop used to live in.
        bufferLock.lock()
        pendingBuffers.removeAll(keepingCapacity: true)
        if isRequestingData {
            _audioRenderer.stopRequestingMediaData()
            isRequestingData = false
        }
        let renderer = _audioRenderer
        bufferLock.unlock()

        renderer.flush()
    }

    /// Detaches the active audio renderer from the clock, keeping both alive until AVFoundation finishes.
    public func detachFromClock() {
        bufferLock.lock()
        _isAttachedToClock = false
        _rendererToken += 1
        pendingBuffers.removeAll(keepingCapacity: true)
        if isRequestingData {
            _audioRenderer.stopRequestingMediaData()
            isRequestingData = false
        }
        statusObserver?.invalidate()
        statusObserver = nil
        enqueuedStartPTS = nil
        enqueuedEndPTS = nil
        let oldRenderer = _audioRenderer
        let activeClock = self.clock
        bufferLock.unlock()

        oldRenderer.flush()
        activeClock.detachRenderer(oldRenderer)
    }

    /// Instantiates a fresh audio renderer and attaches it to the current clock synchronizer.
    public func attachToClock() {
        let renderer = AVSampleBufferAudioRenderer()
        renderer.isMuted = !isAudible
        renderer.volume = isAudible ? 1.0 : 0.0

        bufferLock.lock()
        self._audioRenderer = renderer
        self.clock.attachRenderer(renderer)
        self._isAttachedToClock = true
        self._rendererToken += 1
        setupStatusObserver()
        enqueuedCount = 0
        lastDiagnosticLogTime = 0
        underrunCount = 0
        minLeadMs = .greatestFiniteMagnitude
        lastLeadMs = 0
        bufferLock.unlock()
    }

    /// Replaces the audio renderer on the existing clock without altering the synchronizer,
    /// used when recovering from an audio renderer failure during active video playback.
    public func recoverAudioRenderer() {
        detachFromClock()
        attachToClock()
    }

    /// Complete reset of the renderer on the active clock.
    public func reset() {
        detachFromClock()
        attachToClock()
    }
}
