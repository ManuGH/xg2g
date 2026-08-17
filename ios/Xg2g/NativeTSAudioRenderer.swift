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
            try session.setCategory(.playback, mode: .moviePlayback, policy: .longFormAudio)
            try session.setPreferredIOBufferDuration(0.02) // 20 ms low latency
            try session.setActive(true)
            isAudioSessionActive = true
            logger.notice("[AudioRenderer] ✅ AVAudioSession activated (.playback / .moviePlayback)")
        } catch {
            logger.error("[AudioRenderer] ❌ Failed to activate AVAudioSession: \(error.localizedDescription)")
        }
    }

    /// Enqueues a parsed `CMSampleBuffer` (AC-3, E-AC-3, or AAC) for playback.
    public func enqueue(sampleBuffer: CMSampleBuffer) {
        bufferLock.lock()
        pendingBuffers.append(sampleBuffer)
        bufferLock.unlock()

        drainPendingBuffers()
    }

    private func drainPendingBuffers() {
        renderQueue.async { [weak self] in
            guard let self = self else { return }

            self.bufferLock.lock()
            while !self.pendingBuffers.isEmpty {
                if !self.audioRenderer.isReadyForMoreMediaData {
                    // Renderer buffer full, wait for next drain callback
                    self.startRequestingMediaDataIfNeeded()
                    self.bufferLock.unlock()
                    return
                }

                let buffer = self.pendingBuffers.removeFirst()
                self.bufferLock.unlock()

                self.audioRenderer.enqueue(buffer)

                if self.audioRenderer.status == .failed {
                    if let error = self.audioRenderer.error {
                        logger.error("[AudioRenderer] ❌ Render error: \(error.localizedDescription)")
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
            guard let self = self else { return }
            self.drainPendingBuffers()

            self.bufferLock.lock()
            let hasPending = !self.pendingBuffers.isEmpty
            self.bufferLock.unlock()

            if !hasPending {
                self.audioRenderer.stopRequestingMediaData()
                self.isRequestingData = false
            }
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
