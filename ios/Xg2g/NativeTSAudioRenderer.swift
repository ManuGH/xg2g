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

    public let audioRenderer: AVSampleBufferAudioRenderer
    public let synchronizer: AVSampleBufferRenderSynchronizer

    private let renderQueue = DispatchQueue(label: "io.github.manugh.xg2g.audio.renderer", qos: .userInteractive)
    private var isAudioSessionActive = false
    private var isRequestingData = false

    private var pendingBuffers: [CMSampleBuffer] = []
    private let bufferLock = NSLock()

    public weak var delegate: NativeTSAudioRendererDelegate?

    public var timebase: CMTimebase {
        return synchronizer.timebase
    }

    public var status: AVQueuedSampleBufferRenderingStatus {
        return audioRenderer.status
    }

    public var isReadyForMoreMediaData: Bool {
        return audioRenderer.isReadyForMoreMediaData
    }

    public init() {
        self.audioRenderer = AVSampleBufferAudioRenderer()
        self.synchronizer = AVSampleBufferRenderSynchronizer()
        self.synchronizer.addRenderer(audioRenderer)
    }

    /// Configures and activates the system AVAudioSession for low-latency broadcast playback.
    public func activateAudioSession() {
        guard !isAudioSessionActive else { return }
        do {
            let session = AVAudioSession.sharedInstance()
            try session.setCategory(.playback, mode: .moviePlayback, options: [.defaultToSpeaker, .allowBluetooth, .allowAirPlay])
            try session.setPreferredIOBufferDuration(0.02) // 20 ms low latency
            try session.setActive(true, options: [])
            isAudioSessionActive = true
            audioRenderer.isMuted = false
            audioRenderer.volume = 1.0
            logger.notice("[AudioRenderer] ✅ AVAudioSession activated (.playback / .moviePlayback / defaultToSpeaker)")
            print("[AudioRenderer] ✅ AVAudioSession activated (.playback / .moviePlayback / defaultToSpeaker)")
        } catch {
            logger.error("[AudioRenderer] ❌ Failed to activate AVAudioSession: \(error.localizedDescription)")
            print("[AudioRenderer] ❌ Failed to activate AVAudioSession: \(error.localizedDescription)")
        }
    }

    /// Enqueues a parsed `CMSampleBuffer` (AC-3, E-AC-3, or AAC) for playback.
    public func enqueue(sampleBuffer: CMSampleBuffer) {
        startRequestingMediaDataIfNeeded()

        bufferLock.lock()
        pendingBuffers.append(sampleBuffer)
        bufferLock.unlock()

        drainPendingBuffers()
    }

    private var enqueuedCount = 0
    private var lastDiagnosticLogTime: CFTimeInterval = 0

    private func drainPendingBuffers() {
        renderQueue.async { [weak self] in
            guard let self = self else { return }

            self.bufferLock.lock()
            while !self.pendingBuffers.isEmpty {
                if !self.audioRenderer.isReadyForMoreMediaData {
                    self.bufferLock.unlock()
                    return
                }

                let buffer = self.pendingBuffers.removeFirst()
                self.bufferLock.unlock()

                self.audioRenderer.enqueue(buffer)
                self.enqueuedCount += 1

                let now = CACurrentMediaTime()
                if now - self.lastDiagnosticLogTime >= 2.0 {
                    self.lastDiagnosticLogTime = now
                    let pts = CMSampleBufferGetPresentationTimeStamp(buffer)
                    let statusStr: String
                    switch self.audioRenderer.status {
                    case .unknown: statusStr = "unknown"
                    case .rendering: statusStr = "rendering"
                    case .failed: statusStr = "failed (\(self.audioRenderer.error?.localizedDescription ?? "unknown"))"
                    @unknown default: statusStr = "other"
                    }
                    let diag = "[AudioRenderer] 📊 Enqueued: \(self.enqueuedCount) | Status: \(statusStr) | PTS: \(String(format: "%.3f", pts.seconds))s | Rate: \(CMTimebaseGetRate(self.synchronizer.timebase)) | Time: \(String(format: "%.3f", CMTimebaseGetTime(self.synchronizer.timebase).seconds))s | Ready: \(self.audioRenderer.isReadyForMoreMediaData)"
                    print(diag)
                    logger.notice("\(diag, privacy: .public)")
                }

                if self.audioRenderer.status == .failed {
                    if let error = self.audioRenderer.error {
                        let errStr = "[AudioRenderer] ❌ Render error: \(error.localizedDescription)"
                        print(errStr)
                        logger.error("\(errStr, privacy: .public)")
                        self.delegate?.audioRendererDidEncounterError(self, error: error)
                    }
                }

                self.bufferLock.lock()
            }
            self.bufferLock.unlock()
        }
    }

    private func startRequestingMediaDataIfNeeded() {
        guard !isRequestingData else { return }
        isRequestingData = true

        audioRenderer.requestMediaDataWhenReady(on: renderQueue) { [weak self] in
            self?.drainPendingBuffers()
        }
    }

    /// Immediately flushes all queued and in-flight audio sample buffers.
    public func flush() {
        bufferLock.lock()
        pendingBuffers.removeAll(keepingCapacity: true)
        bufferLock.unlock()

        if isRequestingData {
            audioRenderer.stopRequestingMediaData()
            isRequestingData = false
        }

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
    }
}
