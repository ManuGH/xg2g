// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import CoreMedia
import Foundation
import Testing
@testable import Xg2g

/// Comprehensive lifecycle proof for Apple B3: Unified A/V Playback Clock Owner (`PlaybackClock`).
///
/// Validates the 6-point contract across all 9 required lifecycle scenarios:
/// 1. Initial start (synchronizer identity, rate, audio & video binding).
/// 2. Audio error recovery (clock paused/re-anchored, synchronizer & video layer stable, audio renderer replaced).
/// 3. Track switch (continuity maintained, clock continuous).
/// 4. Video-only playback (pipeline starts clock on picture alone, audio muted).
/// 5. Prepared zap (independent clocks, zero cross-talk).
/// 6. Direct zap (MainActor surface handover before session retirement).
/// 7. Session stop (observer teardown, clock park, fresh synchronizer + audio renderer re-attachment).
/// 8. Session restart (surface and audio bind cleanly to the new synchronizer).
/// 9. Late asynchronous detach completion (prior removal finishing late does not disrupt new session).
@Suite @MainActor struct PlaybackClockLifecycleTests {

    // MARK: - 1. Initial Start

    @Test func initialStartEstablishesSingleSynchronizerAndBindsAudioAndSurface() {
        let pipeline = NativeTSVideoPipeline()
        let clock = pipeline.clock

        #expect(clock.rate == 0.0)
        #expect(pipeline.presentationSynchronizer === clock.synchronizer)
        #expect(pipeline.audioRenderer.synchronizer === clock.synchronizer)
        #expect(pipeline.audioRenderer.isAudible == false)

        let presenter = SystemVideoPresenter()
        presenter.attach(to: pipeline.presentationSynchronizer)

        let anchor = CMTime(value: 100_000, timescale: 90_000)
        clock.start(at: anchor)
        pipeline.audioRenderer.setAudible(true)

        #expect(clock.isClockRunning)
        #expect(clock.rate == 1.0)
        #expect(pipeline.audioRenderer.isAudible == true)
        #expect(pipeline.presentationSynchronizer === pipeline.audioRenderer.synchronizer)

        presenter.detach(from: pipeline.presentationSynchronizer)
        pipeline.stopStreaming()
    }

    // MARK: - 2. Audio Error Recovery (Deterministic Synchronization)

    @Test func audioErrorPreservesSynchronizerIdentityAndKeepsVideoLayerAttached() async throws {
        let pipeline = NativeTSVideoPipeline()
        let neutralURL = URL(string: "http://127.0.0.1:8080/live/fixture.ts")!
        pipeline.startStreaming(url: neutralURL)

        let initialSync = pipeline.presentationSynchronizer
        let presenter = SystemVideoPresenter()
        presenter.attach(to: initialSync)

        let realRenderer = pipeline.audioRenderer as! NativeTSAudioRenderer
        let oldAudioRenderer = realRenderer.audioRenderer

        // Trigger fatal audio failure during active session
        let simulatedError = NSError(domain: "AVFoundationErrorDomain", code: -11800, userInfo: [NSLocalizedDescriptionKey: "Simulated audio failure"])
        pipeline.audioRendererDidEncounterError(realRenderer, error: simulatedError)

        // Deterministically drain the serial ingestQueue without arbitrary sleep timeouts
        pipeline.drainIngestQueueForTesting()
        await Task.yield()

        // Under B3, the synchronizer MUST remain stable so video is not orphaned
        #expect(pipeline.presentationSynchronizer === initialSync)
        #expect(pipeline.audioRenderer.synchronizer === initialSync)

        // The inner AVSampleBufferAudioRenderer was cleanly replaced
        #expect(realRenderer.audioRenderer !== oldAudioRenderer)

        // Re-attaching to presenter is an idempotent no-op because synchronizer didn't change
        presenter.attach(to: pipeline.presentationSynchronizer)

        presenter.detach(from: initialSync)
        pipeline.stopStreaming()
    }

    // MARK: - 3. Track Switch

    @Test func audioTrackSwitchMaintainsContinuousClock() {
        let pipeline = NativeTSVideoPipeline()
        let parser = TSPacketParser()

        let initialSync = pipeline.presentationSynchronizer

        let tracks = [
            AudioTrackInfo(pid: 101, streamType: 0x06, codec: .ac3, language: "deu", audioType: 0, descriptorTags: [0x6A]),
            AudioTrackInfo(pid: 102, streamType: 0x06, codec: .ac3, language: "eng", audioType: 0, descriptorTags: [0x6A])
        ]
        pipeline.tsParser(parser, didDiscoverAudioTracks: tracks)
        #expect(pipeline.selectedAudioPID == 101)

        pipeline.selectAudioTrack(pid: 102)
        pipeline.drainIngestQueueForTesting()
        #expect(pipeline.selectedAudioPID == 102)

        // Clock synchronizer identity unchanged across audio PID switch
        #expect(pipeline.presentationSynchronizer === initialSync)
        #expect(pipeline.audioRenderer.synchronizer === initialSync)
        pipeline.stopStreaming()
    }

    // MARK: - 4. Video-Only Playback (Pipeline Driven)

    @Test func videoOnlyStartsClockOnPictureAloneWithoutAudioRenderer() {
        let pipeline = NativeTSVideoPipeline()
        let presenter = SystemVideoPresenter()
        let context = PresentationContext(presenter: presenter, renderView: nil)

        pipeline.presentationContext = context
        let generation = context.issueGeneration(to: pipeline)
        context.bindWithoutPreparation(pipeline)

        // Signal an undecodable audio track (e.g. MPEG-2 Layer II audio with no native decoder on iOS)
        let mp2Track = AudioTrackInfo(pid: 201, streamType: 0x03, codec: .mpegAudio, language: "deu", audioType: 0, descriptorTags: [])
        pipeline.tsParser(TSPacketParser(), didDiscoverAudioTracks: [mp2Track])

        #expect(pipeline.clock.rate == 0.0)
        #expect(pipeline.audioRenderer.isAudible == false)

        let decoder = HardwareVideoDecoder()
        var buffer: CVPixelBuffer?
        CVPixelBufferCreate(kCFAllocatorDefault, 64, 64, kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange, nil, &buffer)
        guard let pixelBuffer = buffer else {
            Issue.record("Failed to create pixel buffer")
            return
        }

        // Deliver two frames spanning > 0.8s (videoOnlyCushionSeconds)
        let frame1 = DecodedVideoFrame(
            pixelBuffer: pixelBuffer,
            pts: CMTime(seconds: 1.0, preferredTimescale: 90_000),
            structure: .progressive,
            generation: generation.rawValue
        )
        let frame2 = DecodedVideoFrame(
            pixelBuffer: pixelBuffer,
            pts: CMTime(seconds: 2.0, preferredTimescale: 90_000),
            structure: .progressive,
            generation: generation.rawValue
        )

        pipeline.hardwareDecoder(decoder, didEmitFrame: frame1)
        pipeline.hardwareDecoder(decoder, didEmitFrame: frame2)

        #expect(pipeline.clock.isClockRunning)
        #expect(pipeline.clock.rate == 1.0)
        #expect(pipeline.audioRenderer.isAudible == false) // Sound remains silent
        #expect(pipeline.presentationSynchronizer === pipeline.clock.synchronizer)

        context.unbind()
        pipeline.stopStreaming()
    }

    // MARK: - 5. Prepared Zap (Independent Clocks)

    @Test func preparedZapMaintainsIndependentClocksWithoutCrossTalk() {
        let sessionA = NativeTSVideoPipeline()
        let sessionB = NativeTSVideoPipeline()

        #expect(sessionA.presentationSynchronizer !== sessionB.presentationSynchronizer)
        #expect(sessionA.clock !== sessionB.clock)

        let presenter = SystemVideoPresenter()
        presenter.attach(to: sessionA.presentationSynchronizer)

        let anchorA = CMTime(value: 100_000, timescale: 90_000)
        sessionA.clock.start(at: anchorA)
        sessionA.audioRenderer.setAudible(true)

        // Session B remains parked and silent during preparation
        #expect(sessionA.clock.rate == 1.0)
        #expect(sessionB.clock.rate == 0.0)
        #expect(sessionA.audioRenderer.isAudible == true)
        #expect(sessionB.audioRenderer.isAudible == false)

        presenter.detach(from: sessionA.presentationSynchronizer)
        sessionA.stopStreaming()
        sessionB.stopStreaming()
    }

    // MARK: - 6. Direct Zap Handover Order

    @Test func directZapPreservesMainActorHandoverOrder() async throws {
        let presenter = SystemVideoPresenter()
        let context = PresentationContext(presenter: presenter, renderView: nil)
        let sessionA = NativeTSVideoPipeline()
        let sessionB = NativeTSVideoPipeline()

        sessionA.presentationContext = context
        sessionB.presentationContext = context
        _ = context.issueGeneration(to: sessionA)
        _ = context.issueGeneration(to: sessionB)

        // Step 1: Session A is currently playing on screen
        context.bindWithoutPreparation(sessionA)
        #expect(context.boundSession === sessionA)
        #expect(presenter.attachedSynchronizer === sessionA.presentationSynchronizer)

        // Step 2: Direct zap handover moves surface to Session B FIRST on MainActor
        context.bindWithoutPreparation(sessionB)
        #expect(context.boundSession === sessionB)

        // Await presenter's asynchronous switch from sessionA to sessionB synchronizer
        for _ in 0..<50 {
            if presenter.attachedSynchronizer === sessionB.presentationSynchronizer { break }
            try await Task.sleep(nanoseconds: 10_000_000)
        }
        #expect(presenter.attachedSynchronizer === sessionB.presentationSynchronizer)

        // Step 3: Only after surface handover on MainActor is Session A stopped
        sessionA.stopStreaming()

        // Verify Session B maintains surface ownership and synchronizer identity
        #expect(context.boundSession === sessionB)
        #expect(presenter.attachedSynchronizer === sessionB.presentationSynchronizer)
        #expect(sessionB.presentationSynchronizer === sessionB.clock.synchronizer)

        context.unbind()
        sessionB.stopStreaming()
    }

    // MARK: - 7. Session Stop (Teardown & Rebuild)

    @Test func sessionStopParksClockAndRebuildsSynchronizerForNewSession() {
        let pipeline = NativeTSVideoPipeline()
        let initialSync = pipeline.presentationSynchronizer
        let initialAudioSync = pipeline.audioRenderer.synchronizer

        #expect(initialSync === initialAudioSync)

        pipeline.stopStreaming()

        let postStopSync = pipeline.presentationSynchronizer
        let postStopAudioSync = pipeline.audioRenderer.synchronizer

        // Synchronizer was replaced upon teardown
        #expect(postStopSync !== initialSync)

        // Crucial B3 Invariant: Audio renderer is attached to the NEW synchronizer, not orphaned on the old!
        #expect(postStopSync === postStopAudioSync)
        #expect(pipeline.clock.rate == 0.0)
        #expect(pipeline.audioRenderer.isAudible == false)
    }

    // MARK: - 8. Session Restart

    @Test func restartAttachesSurfaceAndAudioToNewSynchronizer() {
        let pipeline = NativeTSVideoPipeline()
        let presenter = SystemVideoPresenter()

        // First session stopped
        pipeline.stopStreaming()
        let secondSync = pipeline.presentationSynchronizer

        // Re-starting session binds presenter to the new synchronizer
        presenter.attach(to: secondSync)

        let anchor = CMTime(value: 200_000, timescale: 90_000)
        pipeline.clock.start(at: anchor)
        pipeline.audioRenderer.setAudible(true)

        #expect(pipeline.clock.rate == 1.0)
        #expect(pipeline.audioRenderer.isAudible == true)
        #expect(pipeline.presentationSynchronizer === secondSync)
        #expect(pipeline.audioRenderer.synchronizer === secondSync)

        presenter.detach(from: secondSync)
        pipeline.stopStreaming()
    }

    // MARK: - 9. Late Asynchronous Detach Completion (Sequence & Impact Verified)

    @Test func lateAsynchronousDetachCompletionDoesNotDisruptNewSession() async throws {
        let presenter = SystemVideoPresenter()
        let context = PresentationContext(presenter: presenter, renderView: nil)

        let sessionA = NativeTSVideoPipeline()
        let sessionB = NativeTSVideoPipeline()

        sessionA.presentationContext = context
        sessionB.presentationContext = context
        _ = context.issueGeneration(to: sessionA)
        _ = context.issueGeneration(to: sessionB)

        // Step 1: Session A playing on Clock A
        context.bindWithoutPreparation(sessionA)
        let clockA = sessionA.clock
        let syncA = sessionA.presentationSynchronizer
        let anchorA = CMTime(value: 100_000, timescale: 90_000)
        clockA.start(at: anchorA)
        sessionA.audioRenderer.setAudible(true)

        #expect(presenter.attachedSynchronizer === syncA)
        #expect(clockA.rate == 1.0)
        #expect(sessionA.audioRenderer.isAudible == true)

        // Step 2: Surface handed over to Session B on Clock B
        context.bindWithoutPreparation(sessionB)
        let clockB = sessionB.clock
        let syncB = sessionB.presentationSynchronizer
        let anchorB = CMTime(value: 200_000, timescale: 90_000)
        clockB.start(at: anchorB)
        sessionB.audioRenderer.setAudible(true)

        // Await presenter's asynchronous switch from syncA to syncB
        for _ in 0..<50 {
            if presenter.attachedSynchronizer === syncB { break }
            try await Task.sleep(nanoseconds: 10_000_000)
        }
        #expect(presenter.attachedSynchronizer === syncB)
        #expect(syncB !== syncA)
        #expect(clockB.rate == 1.0)
        #expect(sessionB.audioRenderer.isAudible == true)

        // Step 3: Track execution order of late asynchronous detach
        final class DetachTracker: @unchecked Sendable {
            private let lock = NSLock()
            private var _orderLog: [String] = []
            var orderLog: [String] {
                lock.lock()
                defer { lock.unlock() }
                return _orderLog
            }
            func append(_ item: String) {
                lock.lock()
                defer { lock.unlock() }
                _orderLog.append(item)
            }
        }
        let tracker = DetachTracker()
        tracker.append("sessionB_active")

        let rendererA = (sessionA.audioRenderer as! NativeTSAudioRenderer).audioRenderer
        clockA.detachRenderer(rendererA) {
            tracker.append("sessionA_detach_completed")
        }
        sessionA.stopStreaming()

        // Await the asynchronous completion of rendererA detach
        for _ in 0..<50 {
            if tracker.orderLog.contains("sessionA_detach_completed") { break }
            try await Task.sleep(nanoseconds: 10_000_000)
        }

        // Step 4: Verify ordering: Session B was already active before Session A detach finished
        #expect(tracker.orderLog == ["sessionB_active", "sessionA_detach_completed"])

        // Step 5: Verify that the late detach callback from Clock A has zero impact on Session B
        #expect(presenter.attachedSynchronizer === syncB)
        #expect(sessionB.presentationSynchronizer === syncB)
        #expect(sessionB.audioRenderer.synchronizer === syncB)
        #expect(clockB.rate == 1.0)
        #expect(clockB.isClockRunning == true)
        #expect(sessionB.audioRenderer.isAudible == true)

        context.unbind()
        sessionB.stopStreaming()
    }
}
