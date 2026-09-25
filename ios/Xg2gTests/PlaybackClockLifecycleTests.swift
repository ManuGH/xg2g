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
/// Validates the 6-point contract across key lifecycle scenarios:
/// 1. Initial start (synchronizer identity, rate, audio & video binding).
/// 2. Audio error recovery (clock paused/re-anchored, synchronizer & video layer stable, audio renderer replaced).
/// 3. Track switch (continuity maintained, clock continuous).
/// 4. Video-only playback (pipeline starts clock on picture alone, audio muted).
/// 5. Prepared zap isolation & transition (independent clocks during preparation, clean transition to Session B).
/// 6. Direct zap PresentationContext handover order (surface handed to incoming session before outgoing stops).
/// 7. Session stop (observer teardown, clock park, fresh synchronizer + audio renderer re-attachment).
/// 8. Session restart (surface and audio bind cleanly to the new synchronizer).
/// 9. Late asynchronous audio detach (prior removal finishing late does not disrupt new active session).
/// 10. Overlapping stop & audio recovery serialization (quiesces ingestQueue and cancels recovery before clock reset).
/// 11. Coordinator direct zap handover (tests full ZapCoordinator atomic handover and retirement).
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

    // MARK: - 5. Prepared Zap (Clock Isolation & Transition)

    @Test func preparedZapMaintainsIndependentClocksAndTransitionsCleanly() async throws {
        let presenter = SystemVideoPresenter()
        let context = PresentationContext(presenter: presenter, renderView: nil)

        let sessionA = NativeTSVideoPipeline()
        let sessionB = NativeTSVideoPipeline()

        sessionA.presentationContext = context
        sessionB.presentationContext = context
        _ = context.issueGeneration(to: sessionA)
        _ = context.issueGeneration(to: sessionB)

        #expect(sessionA.presentationSynchronizer !== sessionB.presentationSynchronizer)
        #expect(sessionA.clock !== sessionB.clock)

        // Session A playing on screen
        context.bindWithoutPreparation(sessionA)
        let anchorA = CMTime(value: 100_000, timescale: 90_000)
        sessionA.clock.start(at: anchorA)
        sessionA.audioRenderer.setAudible(true)

        // Phase 1: Verify clock isolation during preparation of Session B
        #expect(sessionA.clock.rate == 1.0)
        #expect(sessionB.clock.rate == 0.0)
        #expect(sessionA.audioRenderer.isAudible == true)
        #expect(sessionB.audioRenderer.isAudible == false)

        // Phase 2: Transition surface and clock to Session B
        context.bindWithoutPreparation(sessionB)
        let anchorB = CMTime(value: 200_000, timescale: 90_000)
        sessionB.clock.start(at: anchorB)
        sessionB.audioRenderer.setAudible(true)

        // Outgoing Session A is stopped
        sessionA.stopStreaming()

        for _ in 0..<50 {
            if presenter.attachedSynchronizer === sessionB.presentationSynchronizer { break }
            try await Task.sleep(nanoseconds: 10_000_000)
        }

        // Phase 3: Verify Session B owns surface and clock while Session A is stopped
        #expect(presenter.attachedSynchronizer === sessionB.presentationSynchronizer)
        #expect(sessionB.clock.rate == 1.0)
        #expect(sessionB.clock.isClockRunning == true)
        #expect(sessionB.audioRenderer.isAudible == true)
        #expect(sessionA.clock.rate == 0.0)
        #expect(sessionA.audioRenderer.isAudible == false)

        context.unbind()
        sessionB.stopStreaming()
    }

    // MARK: - 6. Direct Zap Handover Order (PresentationContext)

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

    // MARK: - 9. Late Asynchronous Audio Detach

    @Test func lateAsynchronousAudioRendererDetachDoesNotDisruptActiveSession() async throws {
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

        // Step 3: Track execution order of late asynchronous audio renderer detach from Clock A
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

        // Step 4: Verify ordering: Session B was already active before Session A audio detach finished
        #expect(tracker.orderLog == ["sessionB_active", "sessionA_detach_completed"])

        // Step 5: Verify that the late audio detach callback from Clock A has zero impact on Session B
        #expect(presenter.attachedSynchronizer === syncB)
        #expect(sessionB.presentationSynchronizer === syncB)
        #expect(sessionB.audioRenderer.synchronizer === syncB)
        #expect(clockB.rate == 1.0)
        #expect(clockB.isClockRunning == true)
        #expect(sessionB.audioRenderer.isAudible == true)

        context.unbind()
        sessionB.stopStreaming()
    }

    // MARK: - 10. Overlapping Stop and Audio Error Recovery Serialization

    @Test func overlappingStopAndAudioErrorRecoverySerializesSafelyWithoutCorruptingNewClock() async throws {
        let pipeline = NativeTSVideoPipeline()
        let neutralURL = URL(string: "http://127.0.0.1:8080/live/fixture.ts")!
        pipeline.startStreaming(url: neutralURL)

        let initialClock = pipeline.clock
        let initialSync = pipeline.presentationSynchronizer
        let realRenderer = pipeline.audioRenderer as! NativeTSAudioRenderer

        // Trigger an audio error that posts recovery onto ingestQueue
        let simulatedError = NSError(domain: "AVFoundationErrorDomain", code: -11800, userInfo: [NSLocalizedDescriptionKey: "Simulated audio failure"])
        pipeline.audioRendererDidEncounterError(realRenderer, error: simulatedError)

        // Immediately invoke stopStreaming() concurrently / before recovery drains
        pipeline.stopStreaming()

        // Drain any work remaining on ingestQueue
        pipeline.drainIngestQueueForTesting()
        await Task.yield()

        // Post-stop: new synchronizer must be active, audioRenderer must be bound to new synchronizer
        let postStopSync = pipeline.presentationSynchronizer
        #expect(postStopSync !== initialSync)
        #expect(pipeline.clock.rate == 0.0)
        #expect(pipeline.audioRenderer.synchronizer === postStopSync)
        #expect(pipeline.audioRenderer.isAudible == false)

        // Verify that restarting after this race starts cleanly with the new clock
        let restartAnchor = CMTime(value: 300_000, timescale: 90_000)
        pipeline.clock.start(at: restartAnchor)
        pipeline.audioRenderer.setAudible(true)

        #expect(pipeline.clock.rate == 1.0)
        #expect(pipeline.clock.isClockRunning == true)
        #expect(pipeline.audioRenderer.isAudible == true)
        #expect(pipeline.presentationSynchronizer === postStopSync)
        #expect(initialClock === pipeline.clock)
        #expect(CMTimebaseGetRate(initialSync.timebase) == 0.0) // Old synchronizer was parked and never touched by stale recovery

        pipeline.stopStreaming()
    }

    // MARK: - 11. Coordinator Direct Zap Handover

    @Test func coordinatorHandoverTransfersSurfaceAtomicallyBeforeRetiringSession() async throws {
        let srefA = "fixture-channel-a"
        let srefB = "fixture-channel-b"

        let coordinator = ZapCoordinator(
            streamURL: { sref in URL(string: "http://127.0.0.1:8080/live/\(sref).ts") }
        )

        // Step 1: Start Channel A outright through coordinator
        let urlA = URL(string: "http://127.0.0.1:8080/live/\(srefA).ts")!
        await coordinator.play(unprepared: urlA)

        #expect(coordinator.presentedServiceRef == "\(srefA).ts")
        guard let sessionA = coordinator.playing else {
            Issue.record("Expected sessionA to be playing")
            return
        }
        #expect(coordinator.surface.attachedSynchronizer === sessionA.presentationSynchronizer)
        #expect(sessionA.presentationSynchronizer === sessionA.clock.synchronizer)

        // Step 2: Hand over to Channel B directly through coordinator
        let urlB = URL(string: "http://127.0.0.1:8080/live/\(srefB).ts")!
        await coordinator.play(unprepared: urlB)

        #expect(coordinator.presentedServiceRef == "\(srefB).ts")
        guard let sessionB = coordinator.playing else {
            Issue.record("Expected sessionB to be playing")
            return
        }
        #expect(sessionB !== sessionA)
        #expect(sessionB.presentationSynchronizer !== sessionA.presentationSynchronizer)

        // Await surface attachment to Session B
        for _ in 0..<50 {
            if coordinator.surface.attachedSynchronizer === sessionB.presentationSynchronizer { break }
            try await Task.sleep(nanoseconds: 10_000_000)
        }
        #expect(coordinator.surface.attachedSynchronizer === sessionB.presentationSynchronizer)

        // Verify Session A was retired and stopped by the coordinator
        #expect(sessionA.clock.rate == 0.0)
        #expect(sessionA.audioRenderer.isAudible == false)

        await coordinator.stop()
        #expect(coordinator.presentedServiceRef == nil)
        #expect(coordinator.playing == nil)
    }

    // MARK: - 12. Surface Attachment While Clock Is Already Running

    @Test func attachSurfaceWhileClockIsAlreadyRunningPreservesClockRateAndDoesNotShiftTime() async throws {
        let pipeline = NativeTSVideoPipeline()
        let clock = pipeline.clock
        let anchor = CMTime(value: 100_000, timescale: 90_000)

        // Step 1: Start clock on pipeline before any surface is attached
        clock.start(at: anchor)
        pipeline.audioRenderer.setAudible(true)

        #expect(clock.isClockRunning == true)
        #expect(clock.rate == 1.0)
        let rateBeforeAttach = clock.rate

        // Allow playback to advance slightly
        try await Task.sleep(nanoseconds: 20_000_000)
        let timeBeforeAttach = clock.currentTime

        // Step 2: Attach surface (SystemVideoPresenter) to the running session
        let presenter = SystemVideoPresenter()
        presenter.attach(to: pipeline.presentationSynchronizer)

        for _ in 0..<50 {
            if presenter.attachedSynchronizer === pipeline.presentationSynchronizer { break }
            try await Task.sleep(nanoseconds: 10_000_000)
        }

        // Step 3: Assert surface attachment did NOT alter clock rate or rewrite timebase
        #expect(presenter.attachedSynchronizer === pipeline.presentationSynchronizer)
        #expect(clock.rate == rateBeforeAttach)
        #expect(clock.isClockRunning == true)

        let timeAfterAttach = clock.currentTime
        // Verify time progressed monotonically without being reset or shifted backwards
        #expect(CMTimeCompare(timeAfterAttach, timeBeforeAttach) >= 0)

        presenter.detach(from: pipeline.presentationSynchronizer)
        pipeline.stopStreaming()
    }

    // MARK: - 13. Concurrent Reset and Clock Operations Serialization

    @Test func concurrentResetAndClockOperationsSerializeSafelyUnderExecutionContract() async throws {
        let clock = PlaybackClock()

        // Concurrently run setRate, attachRenderer, stop, and reset across multiple tasks
        await withTaskGroup(of: Void.self) { group in
            // Group A: Mutate rate and attach/detach
            for i in 0..<10 {
                group.addTask {
                    let renderer = AVSampleBufferAudioRenderer()
                    let anchor = CMTime(value: Int64(i * 10_000), timescale: 90_000)
                    clock.setRate(1.0, time: anchor)
                    clock.attachRenderer(renderer)
                    clock.stop()
                    _ = clock.rate
                    _ = clock.isClockRunning
                }
            }

            // Group B: Concurrently trigger reset()
            for _ in 0..<5 {
                group.addTask {
                    let retired = clock.reset()
                    #expect(retired !== clock.synchronizer)
                }
            }
        }

        // Post-concurrency sanity: clock must remain functional and internally consistent
        let finalAnchor = CMTime(value: 500_000, timescale: 90_000)
        clock.start(at: finalAnchor)
        #expect(clock.rate == 1.0)
        #expect(clock.isClockRunning == true)

        clock.stop()
        #expect(clock.rate == 0.0)
        #expect(clock.isClockRunning == false)
    }

    // MARK: - 14. Late Audio Error Callback Rejection After Stop or From Detached Renderer

    @Test func lateAudioErrorFromStoppedSessionOrDetachedRendererIsRejectedWithoutDisruptingClock() async throws {
        let pipeline = NativeTSVideoPipeline()
        let neutralURL = URL(string: "http://127.0.0.1:8080/live/fixture.ts")!

        // Step 1: Start streaming on session
        pipeline.startStreaming(url: neutralURL)
        #expect(pipeline.isStreaming == true)

        let oldRenderer = pipeline.audioRenderer as! NativeTSAudioRenderer
        let oldSync = pipeline.presentationSynchronizer
        #expect(oldRenderer.isAttachedToClock == true)

        // Step 2: Stop streaming
        pipeline.stopStreaming()
        #expect(pipeline.isStreaming == false)
        #expect(pipeline.presentationSynchronizer !== oldSync)

        // Step 3: Simulate that the clock was restarted or prepared on the fresh synchronizer
        let restartAnchor = CMTime(value: 300_000, timescale: 90_000)
        pipeline.clock.start(at: restartAnchor)
        #expect(pipeline.clock.rate == 1.0)
        #expect(pipeline.clock.isClockRunning == true)
        #expect(pipeline.lifecycle == .stable)

        // Step 4: A late audio error callback arrives from the old renderer of the stopped session
        let simulatedError = NSError(domain: "AVFoundationErrorDomain", code: -11800, userInfo: [NSLocalizedDescriptionKey: "Late error from old renderer"])
        pipeline.audioRendererDidEncounterError(oldRenderer, error: simulatedError)
        pipeline.drainIngestQueueForTesting()
        await Task.yield()

        // Assert: The callback must be rejected; clock must NOT be stopped, no recovery initiated
        #expect(pipeline.clock.rate == 1.0)
        #expect(pipeline.clock.isClockRunning == true)
        #expect(pipeline.lifecycle == .stable)

        // Step 5: A late failure callback arrives from an explicitly detached renderer
        let detachedRenderer = NativeTSAudioRenderer(clock: PlaybackClock())
        detachedRenderer.detachFromClock()
        #expect(detachedRenderer.isAttachedToClock == false)

        pipeline.audioRendererDidChangeStatus(detachedRenderer, status: .failed)
        pipeline.drainIngestQueueForTesting()
        await Task.yield()

        // Assert: Detached renderer callback must be rejected; clock continues running
        #expect(pipeline.clock.rate == 1.0)
        #expect(pipeline.clock.isClockRunning == true)
        #expect(pipeline.lifecycle == .stable)

        // Step 6: Verify that active error recovery still works when session is actually streaming
        pipeline.startStreaming(url: neutralURL)
        #expect(pipeline.isStreaming == true)

        let activeRenderer = pipeline.audioRenderer as! NativeTSAudioRenderer
        #expect(activeRenderer.isAttachedToClock == true)

        pipeline.audioRendererDidEncounterError(activeRenderer, error: simulatedError)
        #expect(pipeline.lifecycle == .recovering)

        pipeline.stopStreaming()
    }

    // MARK: - 15. Late Audio Recovery Overlapping Stop & Restart Discarded

    @Test func lateAudioRecoveryOverlappingStopAndRestartIsDiscardedWithoutDisruptingNewSession() async throws {
        let pipeline = NativeTSVideoPipeline()
        let url1 = URL(string: "http://127.0.0.1:8080/live/fixture1.ts")!
        let url2 = URL(string: "http://127.0.0.1:8080/live/fixture2.ts")!

        // Step 1: Start Session 1 and verify initial active renderer identity
        pipeline.startStreaming(url: url1)
        #expect(pipeline.isStreaming == true)
        let gen1 = pipeline.activeSessionGeneration
        #expect(gen1 > 0)
        guard let nativeAudio = pipeline.audioRenderer as? NativeTSAudioRenderer else {
            Issue.record("Expected NativeTSAudioRenderer")
            return
        }
        let token1 = nativeAudio.activeRendererToken
        let sync1 = pipeline.presentationSynchronizer

        let anchor1 = CMTime(value: 100_000, timescale: 90_000)
        pipeline.clock.start(at: anchor1)
        #expect(pipeline.clock.rate == 1.0)
        #expect(pipeline.lifecycle == .stable)

        // Step 2: Intercept callback between origin token capture and pipeline delegate delivery.
        // Simulate rapid Stop/Restart occurring precisely inside this race window.
        var interceptedOriginToken: Int64?
        var session2Started = false
        var sync2: AVSampleBufferRenderSynchronizer?
        var token2: Int64 = 0

        nativeAudio.onBeforeDelegateErrorDelivery = { originToken, error in
            interceptedOriginToken = originToken

            // Stop Session 1 right after origin check captured originToken
            pipeline.stopStreaming()
            #expect(pipeline.isStreaming == false)
            #expect(pipeline.activeSessionGeneration == 0)

            // Start Session 2 immediately (rapid channel zap / re-tune)
            pipeline.startStreaming(url: url2)
            #expect(pipeline.isStreaming == true)
            #expect(pipeline.activeSessionGeneration > gen1)
            token2 = nativeAudio.activeRendererToken
            #expect(token2 != originToken)
            sync2 = pipeline.presentationSynchronizer
            #expect(sync2 !== sync1)

            let anchor2 = CMTime(value: 200_000, timescale: 90_000)
            pipeline.clock.start(at: anchor2)
            #expect(pipeline.clock.rate == 1.0)
            #expect(pipeline.lifecycle == .stable)
            session2Started = true
        }

        // Step 3: Trigger error using the production simulation path on NativeTSAudioRenderer.
        // This executes activeRendererTokenIfCurrent(_audioRenderer) to capture token1,
        // triggers onBeforeDelegateErrorDelivery (which performs stop & restart),
        // and then delivers audioRendererDidEncounterError(self, rendererToken: token1, error: error).
        let simulatedError = NSError(
            domain: "AVFoundationErrorDomain",
            code: -11800,
            userInfo: [NSLocalizedDescriptionKey: "Hardware error during Session 1"]
        )
        nativeAudio.simulateFailureForTesting(error: simulatedError)
        nativeAudio.onBeforeDelegateErrorDelivery = nil

        // Verify the callback window was traversed
        #expect(interceptedOriginToken == token1)
        #expect(session2Started == true)

        pipeline.drainIngestQueueForTesting()
        await Task.yield()

        // Assert: Session 2 must be completely undisturbed!
        // Clock remains running at 1.0, lifecycle is stable, synchronizer is sync2, token is token2
        #expect(pipeline.clock.rate == 1.0)
        #expect(pipeline.clock.isClockRunning == true)
        #expect(pipeline.lifecycle == .stable)
        #expect(pipeline.presentationSynchronizer === sync2)
        #expect(nativeAudio.activeRendererToken == token2)
        #expect(pipeline.activeSessionGeneration > gen1)

        // Step 4: Test status change through the same race window
        // Direct call to delegate with old token1 must also be rejected
        pipeline.audioRendererDidChangeStatus(nativeAudio, rendererToken: token1, status: .failed)
        pipeline.drainIngestQueueForTesting()
        await Task.yield()

        #expect(pipeline.clock.rate == 1.0)
        #expect(pipeline.clock.isClockRunning == true)
        #expect(pipeline.lifecycle == .stable)
        #expect(pipeline.presentationSynchronizer === sync2)

        // Step 5: Test delayed recovery block queued for old session generation and token arriving on ingestQueue
        let epoch1 = pipeline.recoveryEpoch
        pipeline.simulateDelayedAudioRecoveryBlockForTesting(
            sessionGeneration: gen1,
            rendererToken: token1,
            epoch: epoch1
        )
        pipeline.drainIngestQueueForTesting()
        await Task.yield()

        // Assert: Session 2 still completely untouched
        #expect(pipeline.clock.rate == 1.0)
        #expect(pipeline.clock.isClockRunning == true)
        #expect(pipeline.lifecycle == .stable)
        #expect(pipeline.presentationSynchronizer === sync2)
        #expect(nativeAudio.activeRendererToken == token2)

        pipeline.stopStreaming()
    }
}
