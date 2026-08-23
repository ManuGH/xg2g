// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import CoreMedia
import Foundation
import OSLog

private let logger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "audio-renderer")

public protocol NativeTSAudioRendererDelegate: AnyObject, Sendable {
    func audioRendererDidEncounterError(_ renderer: NativeTSAudioRenderer, error: Error)
    func audioRendererDidChangeStatus(_ renderer: NativeTSAudioRenderer, status: AVQueuedSampleBufferRenderingStatus)
}

/// Plays compressed audio (`CMSampleBuffer`s) using `AVSampleBufferAudioRenderer`
/// synchronized via `AVSampleBufferRenderSynchronizer`.
///
/// Principles:
/// - Audio establishes the master physical clock through `synchronizer.addRenderer(audioRenderer)`.
/// - Supports immediate `flush()` on discontinuity, channel zap, or reset.
public final class NativeTSAudioRenderer: @unchecked Sendable {

    public private(set) var audioRenderer: AVSampleBufferAudioRenderer
    public private(set) var synchronizer: AVSampleBufferRenderSynchronizer

    private let renderQueue = DispatchQueue(label: "io.github.manugh.xg2g.audio.renderer", qos: .userInteractive)
    private var isAudioSessionActive = false

    private var pendingBuffers: [CMSampleBuffer] = []
    private var enqueuedCount = 0
    private var lastDiagnosticLogTime: CFTimeInterval = 0
    private let bufferLock = NSLock()

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
        return synchronizer.timebase
    }

    public var status: AVQueuedSampleBufferRenderingStatus {
        return audioRenderer.status
    }

    private var statusObserver: NSKeyValueObservation?

    public init() {
        self.audioRenderer = AVSampleBufferAudioRenderer()
        self.synchronizer = AVSampleBufferRenderSynchronizer()
        self.synchronizer.addRenderer(audioRenderer)
        // Silent until granted audibility. A session is built to be prepared, and
        // preparing one must never be heard.
        audioRenderer.isMuted = true
        audioRenderer.volume = 0.0
        setupStatusObserver()
    }

    private func setupStatusObserver() {
        statusObserver = audioRenderer.observe(\.status, options: [.new]) { [weak self] renderer, _ in
            guard let self = self else { return }
            self.delegate?.audioRendererDidChangeStatus(self, status: renderer.status)
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
        audioRenderer.isMuted = !audible
        audioRenderer.volume = audible ? 1.0 : 0.0
    }

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
                    audioRenderer.stopRequestingMediaData()
                    isRequestingData = false
                }
                bufferLock.unlock()
                return
            }

            guard audioRenderer.isReadyForMoreMediaData else {
                // Full. Ask to be told when it drains, and stop pushing until then.
                if !isRequestingData {
                    isRequestingData = true
                    audioRenderer.requestMediaDataWhenReady(on: renderQueue) { [weak self] in
                        self?.drainPendingBuffers()
                    }
                }
                bufferLock.unlock()
                return
            }

            let renderer = audioRenderer
            let buffer = pendingBuffers.removeFirst()
            enqueuedCount += 1
            let currentCount = enqueuedCount

            // Only meaningful once the clock actually runs; before that the
            // timebase sits at zero and every lead would read as astronomical.
            var leadMs: Double = 0
            if CMTimebaseGetRate(synchronizer.timebase) > 0 {
                let pts = CMSampleBufferGetPresentationTimeStamp(buffer)
                let clock = CMTimebaseGetTime(synchronizer.timebase)
                if pts.isValid && clock.isValid {
                    leadMs = (pts.seconds - clock.seconds) * 1000.0
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
                let statusStr: String
                switch renderer.status {
                case .unknown: statusStr = "unknown"
                case .rendering: statusStr = "rendering"
                case .failed: statusStr = "failed (\(renderer.error?.localizedDescription ?? "unknown"))"
                @unknown default: statusStr = "other"
                }
                let diag = "[AudioRenderer] 📊 Enqueued: \(currentCount) | Status: \(statusStr) | PTS: \(String(format: "%.3f", pts.seconds))s | Rate: \(CMTimebaseGetRate(self.synchronizer.timebase)) | Time: \(String(format: "%.3f", CMTimebaseGetTime(self.synchronizer.timebase).seconds))s | Lead: \(String(format: "%.0f", leadMs))ms | Ready: \(renderer.isReadyForMoreMediaData)"
                print(diag)
                logger.notice("\(diag, privacy: .public)")
            }

            if renderer.status == .failed, let error = renderer.error {
                let errStr = "[AudioRenderer] ❌ Render error: \(error.localizedDescription)"
                print(errStr)
                logger.error("\(errStr, privacy: .public)")
                delegate?.audioRendererDidEncounterError(self, error: error)
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
            audioRenderer.stopRequestingMediaData()
            isRequestingData = false
        }
        bufferLock.unlock()

        audioRenderer.flush()
    }

    /// Starts or resumes the synchronizer playback clock at a specific reference time.
    public func setRate(_ rate: Float, time: CMTime) {
        synchronizer.setRate(rate, time: time)
    }

    /// Stops the master playback clock.
    public func stopClock() {
        synchronizer.setRate(0.0, time: .invalid)
    }

    /// Complete reset of the renderer and synchronizer state.
    public func reset() {
        flush()
        stopClock()

        synchronizer.removeRenderer(audioRenderer, at: .invalid) { _ in }

        let renderer = AVSampleBufferAudioRenderer()
        let sync = AVSampleBufferRenderSynchronizer()
        sync.addRenderer(renderer)

        if isAudioSessionActive {
            renderer.isMuted = false
            renderer.volume = 1.0
        }

        bufferLock.lock()
        // `flush()` above emptied the queue and tore down any armed request, so a
        // drain block still in flight for the old renderer finds nothing to do and
        // returns without touching the replacement.
        audioRenderer = renderer
        synchronizer = sync
        setupStatusObserver()
        enqueuedCount = 0
        lastDiagnosticLogTime = 0
        underrunCount = 0
        minLeadMs = .greatestFiniteMagnitude
        lastLeadMs = 0
        bufferLock.unlock()
    }
}
