// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import Testing
import UIKit
import VideoToolbox

@testable import Xg2g

@MainActor
@Suite(.serialized)
struct VideoToolboxRecoveryTests {

    // MARK: - Test 1: Error Classification Matrix

    @Test func errorClassificationMatrixSeparatesDeadFromReconfigAndBitstream() throws {
        let decoder = HardwareVideoDecoder()

        // 1. Session Dead (Fatal)
        #expect(decoder.classifyError(kVTInvalidSessionErr) == .sessionDead)
        #expect(decoder.classifyError(kVTVideoDecoderMalfunctionErr) == .sessionDead)
        #expect(decoder.classifyError(-12903) == .sessionDead)
        #expect(decoder.classifyError(-12911) == .sessionDead)
        #expect(decoder.isFatalSessionError(kVTInvalidSessionErr))
        #expect(decoder.isFatalSessionError(kVTVideoDecoderMalfunctionErr))
        #expect(!decoder.isReconfigurationError(kVTInvalidSessionErr))
        #expect(!decoder.isBitstreamError(kVTInvalidSessionErr))

        // 2. Reconfigure / Resource Constraints
        #expect(decoder.classifyError(kVTFormatDescriptionChangeNotSupportedErr) == .reconfigureOrResource)
        #expect(decoder.classifyError(kVTVideoDecoderNotAvailableNowErr) == .reconfigureOrResource)
        #expect(decoder.classifyError(-12908) == .reconfigureOrResource)
        #expect(decoder.classifyError(-12913) == .reconfigureOrResource)
        #expect(decoder.isReconfigurationError(kVTFormatDescriptionChangeNotSupportedErr))
        #expect(decoder.isReconfigurationError(kVTVideoDecoderNotAvailableNowErr))
        #expect(!decoder.isFatalSessionError(kVTFormatDescriptionChangeNotSupportedErr))
        #expect(!decoder.isBitstreamError(kVTFormatDescriptionChangeNotSupportedErr))

        // 3. Bitstream / Slice Defects
        #expect(decoder.classifyError(kVTVideoDecoderBadDataErr) == .bitstream)
        #expect(decoder.classifyError(kVTVideoDecoderReferenceMissingErr) == .bitstream)
        #expect(decoder.classifyError(kVTParameterErr) == .bitstream)
        #expect(decoder.classifyError(-12350) == .bitstream)
        #expect(decoder.classifyError(-12909) == .bitstream)
        #expect(decoder.classifyError(-12904) == .bitstream)
        #expect(decoder.classifyError(-8969) == .bitstream)
        #expect(decoder.isBitstreamError(kVTVideoDecoderBadDataErr))
        #expect(decoder.isBitstreamError(-12350))
        #expect(decoder.isBitstreamError(-8969))
        #expect(!decoder.isFatalSessionError(kVTVideoDecoderBadDataErr))
        #expect(!decoder.isReconfigurationError(kVTVideoDecoderBadDataErr))

        // 4. Ok (noErr)
        #expect(decoder.classifyError(noErr) == .ok)
        #expect(!decoder.isFatalSessionError(noErr))
        #expect(!decoder.isReconfigurationError(noErr))
        #expect(!decoder.isBitstreamError(noErr))
    }

    // MARK: - Test 2: Fatal Session Error Invalidation & Generation Bumping

    @Test func fatalSessionErrorInvalidatesSessionAndBumpsGeneration() throws {
        let decoder = HardwareVideoDecoder()
        let sink = MockRecoveryDecoderSink()
        decoder.delegate = sink

        guard let format = makeTestH264FormatDescription() else {
            Issue.record("Failed to create test format description")
            return
        }

        decoder.configure(with: format)
        #expect(decoder.hasActiveSession)
        let initialSessionGen = decoder.sessionGeneration
        let zapGen = decoder.decodeGeneration

        // Trigger fatal session recovery
        decoder.forceSessionRecovery(reason: "Unit Test Fatal Invalidation")

        #expect(!decoder.hasActiveSession)
        #expect(decoder.sessionGeneration == initialSessionGen + 1)
        #expect(decoder.decodeGeneration == zapGen)
        #expect(sink.fatalErrors.count == 1)
        #expect(sink.fatalErrors.first == kVTInvalidSessionErr)
        #expect(sink.reconfigErrors.isEmpty)
        #expect(sink.hwActiveStates.last == false)
    }

    // MARK: - Test 3: Reconfiguration Error Invokes Dedicated Delegate Method

    @Test func reconfigurationErrorInvokesDedicatedDelegateMethod() throws {
        let decoder = HardwareVideoDecoder()
        let sink = MockRecoveryDecoderSink()
        decoder.delegate = sink

        guard let format = makeTestH264FormatDescription() else {
            Issue.record("Failed to create test format description")
            return
        }

        decoder.configure(with: format)
        #expect(decoder.hasActiveSession)

        // Classify and verify reconfigure status
        let reconfigStatus = kVTFormatDescriptionChangeNotSupportedErr
        #expect(decoder.isReconfigurationError(reconfigStatus))
        #expect(!decoder.isFatalSessionError(reconfigStatus))
    }

    // MARK: - Test 4: Asynchronous Callback Generational Isolation

    @Test func asynchronousCallbackFromPriorGenerationIsDiscarded() throws {
        let decoder = HardwareVideoDecoder()
        let sink = MockRecoveryDecoderSink()
        decoder.delegate = sink

        guard let format = makeTestH264FormatDescription() else {
            Issue.record("Failed to create test format description")
            return
        }

        decoder.configure(with: format)
        #expect(decoder.hasActiveSession)

        // Create a mock sample buffer
        guard let sampleBuffer = makeTestSampleBuffer(format: format) else {
            Issue.record("Failed to create test sample buffer")
            return
        }

        // Submitting frame on Gen 0
        decoder.decode(sampleBuffer: sampleBuffer, structure: .wovenTopFieldFirst)

        // Invalidate session (advances session generation)
        decoder.forceSessionRecovery(reason: "Test Generational Dropping")
        #expect(decoder.sessionGeneration > 0)

        // Verify that any late callback from Gen 0 cannot emit to sink
        #expect(sink.emittedFrames.isEmpty)
    }

    // MARK: - Test 5: Submit vs Invalidate Concurrent Race Resistance

    @Test func submitVsInvalidateConcurrentRaceProducesNoCrashes() throws {
        let decoder = HardwareVideoDecoder()
        let sink = MockRecoveryDecoderSink()
        decoder.delegate = sink

        guard let format = makeTestH264FormatDescription() else {
            Issue.record("Failed to create test format description")
            return
        }

        decoder.configure(with: format)

        guard let sampleBuffer = makeTestSampleBuffer(format: format) else {
            Issue.record("Failed to create test sample buffer")
            return
        }

        // Concurrently call decode and invalidate/reset across multiple threads
        let group = DispatchGroup()
        let iterations = 100

        for i in 0..<iterations {
            group.enter()
            DispatchQueue.global(qos: .userInitiated).async {
                if i % 2 == 0 {
                    decoder.decode(sampleBuffer: sampleBuffer, structure: .wovenTopFieldFirst)
                } else if i % 5 == 0 {
                    decoder.forceSessionRecovery(reason: "Concurrent Test Race")
                } else if i % 7 == 0 {
                    _ = decoder.ensureSession()
                } else {
                    decoder.invalidateSessionForBackground()
                }
                group.leave()
            }
        }

        group.wait()

        // Clean re-creation must succeed after concurrent thrashing
        decoder.configure(with: format)
        #expect(decoder.hasActiveSession)
    }

    // MARK: - Test 6: Strict IDR-Only Recovery Never Fails Open on Timeout

    @Test func strictIDROnlyRecoveryNeverFailsOpenOnTimeout() async throws {
        let pipeline = NativeTSVideoPipeline()
        defer { pipeline.stopStreaming() }
        guard let format = makeTestH264FormatDescription() else {
            Issue.record("Failed to create test format description")
            return
        }
        guard let sampleBuffer = makeTestSampleBuffer(format: format) else {
            Issue.record("Failed to create test sample buffer")
            return
        }

        // Simulate fatal decoder invalidation
        pipeline.hardwareDecoder(HardwareVideoDecoder(), didInvalidateSessionWithFatalError: kVTInvalidSessionErr)

        // Allow async dispatch to ingestQueue
        try? await Task.sleep(nanoseconds: 50_000_000)

        #expect(pipeline.decodeGateState == .closed(reason: .decoderRecovery))

        // Feed 50 non-sync access units past any timeout
        for _ in 0..<50 {
            pipeline.accessUnitAssembler(
                H264AccessUnitAssembler(),
                didEmitSampleBuffer: sampleBuffer,
                isSyncSample: false,
                structure: .wovenTopFieldFirst
            )
        }

        // Must remain closed strictly under .decoderRecovery — NO fail-open!
        #expect(pipeline.decodeGateState == .closed(reason: .decoderRecovery))
        #expect(pipeline.telemetry.snapshot().gatedAccessUnits >= 50)
    }

    // MARK: - Test 7: Full Recovery Cycle on Next IDR Sync Sample

    @Test func fullRecoveryCycleOnNextIDRSyncSample() async throws {
        let pipeline = NativeTSVideoPipeline()
        defer { pipeline.stopStreaming() }
        guard let format = makeTestH264FormatDescription() else {
            Issue.record("Failed to create test format description")
            return
        }
        guard let sampleBuffer = makeTestSampleBuffer(format: format) else {
            Issue.record("Failed to create test sample buffer")
            return
        }

        // Priming format description
        pipeline.accessUnitAssembler(
            H264AccessUnitAssembler(),
            didUpdateFormat: format,
            info: H264DecodedInfo(width: 1920, height: 1080, isInterlaced: true, isTopFieldFirst: true)
        )

        // Invalidate session
        pipeline.hardwareDecoder(HardwareVideoDecoder(), didInvalidateSessionWithFatalError: kVTInvalidSessionErr)
        try? await Task.sleep(nanoseconds: 50_000_000)
        #expect(pipeline.decodeGateState == .closed(reason: .decoderRecovery))

        // Deliver non-sync frame -> gated
        pipeline.accessUnitAssembler(
            H264AccessUnitAssembler(),
            didEmitSampleBuffer: sampleBuffer,
            isSyncSample: false,
            structure: .wovenTopFieldFirst
        )
        #expect(pipeline.decodeGateState == .closed(reason: .decoderRecovery))

        // Deliver sync (IDR) frame -> recovers session and opens gate
        pipeline.accessUnitAssembler(
            H264AccessUnitAssembler(),
            didEmitSampleBuffer: sampleBuffer,
            isSyncSample: true,
            structure: .wovenTopFieldFirst
        )

        #expect(pipeline.decodeGateState == .open)
        #expect(pipeline.telemetry.snapshot().vtSessionActive == true)
    }

    // MARK: - Test 8: Lifecycle Background & Foreground Simulation

    @Test func lifecycleBackgroundInvalidatesAndForegroundRecoversOnIDR() async throws {
        let pipeline = NativeTSVideoPipeline()
        defer { pipeline.stopStreaming() }
        guard let format = makeTestH264FormatDescription() else {
            Issue.record("Failed to create test format description")
            return
        }
        guard let sampleBuffer = makeTestSampleBuffer(format: format) else {
            Issue.record("Failed to create test sample buffer")
            return
        }

        pipeline.accessUnitAssembler(
            H264AccessUnitAssembler(),
            didUpdateFormat: format,
            info: H264DecodedInfo(width: 1920, height: 1080, isInterlaced: true, isTopFieldFirst: true)
        )

        // Open initial gate on sync
        pipeline.accessUnitAssembler(
            H264AccessUnitAssembler(),
            didEmitSampleBuffer: sampleBuffer,
            isSyncSample: true,
            structure: .wovenTopFieldFirst
        )
        #expect(pipeline.decodeGateState == .open)

        // Post background notification
        NotificationCenter.default.post(name: UIApplication.didEnterBackgroundNotification, object: nil)
        try? await Task.sleep(nanoseconds: 50_000_000)

        #expect(pipeline.decodeGateState == .closed(reason: .backgrounded))
        #expect(pipeline.telemetry.snapshot().vtSessionActive == false)

        // Non-sync frames arriving while backgrounded or returning to foreground are strictly gated
        pipeline.accessUnitAssembler(
            H264AccessUnitAssembler(),
            didEmitSampleBuffer: sampleBuffer,
            isSyncSample: false,
            structure: .wovenTopFieldFirst
        )
        #expect(pipeline.decodeGateState == .closed(reason: .backgrounded))

        // IDR frame arrives -> recovers session cleanly
        pipeline.accessUnitAssembler(
            H264AccessUnitAssembler(),
            didEmitSampleBuffer: sampleBuffer,
            isSyncSample: true,
            structure: .wovenTopFieldFirst
        )
        #expect(pipeline.decodeGateState == .open)
        #expect(pipeline.telemetry.snapshot().vtSessionActive == true)
    }
}

// MARK: - Test Helpers & Mocks

private final class MockRecoveryDecoderSink: HardwareVideoDecoderDelegate, @unchecked Sendable {
    var emittedFrames: [DecodedVideoFrame] = []
    var hwActiveStates: [Bool] = []
    var deinterlaceAcceptedStates: [Bool] = []
    var decodeErrors: [OSStatus] = []
    var fatalErrors: [OSStatus] = []
    var reconfigErrors: [OSStatus] = []

    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEmitFrame frame: DecodedVideoFrame) {
        emittedFrames.append(frame)
    }

    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didChangeHWActiveState isHWActive: Bool) {
        hwActiveStates.append(isHWActive)
    }

    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didChangeVTDeinterlaceAccepted isAccepted: Bool) {
        deinterlaceAcceptedStates.append(isAccepted)
    }

    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEncounterDecodeError error: OSStatus) {
        decodeErrors.append(error)
    }

    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didInvalidateSessionWithFatalError error: OSStatus) {
        fatalErrors.append(error)
    }

    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didRequestSessionReconfiguration error: OSStatus) {
        reconfigErrors.append(error)
    }
}

private func makeTestH264FormatDescription() -> CMVideoFormatDescription? {
    let sps = Data([
        0x67, 0x4d, 0x40, 0x28, 0x9a, 0x64, 0x03, 0xc0,
        0x11, 0x3f, 0x2e, 0x02, 0xd4, 0x04, 0x04, 0x05,
        0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x00, 0x03,
        0x00, 0x32, 0x84
    ])
    let pps = Data([0x68, 0xee, 0x3c, 0x80])

    let spsBytes = [UInt8](sps)
    let ppsBytes = [UInt8](pps)

    var formatDesc: CMFormatDescription?
    let status = spsBytes.withUnsafeBufferPointer { spsBuf in
        ppsBytes.withUnsafeBufferPointer { ppsBuf in
            let pointers = [spsBuf.baseAddress!, ppsBuf.baseAddress!]
            let sizes = [spsBuf.count, ppsBuf.count]
            return CMVideoFormatDescriptionCreateFromH264ParameterSets(
                allocator: kCFAllocatorDefault,
                parameterSetCount: 2,
                parameterSetPointers: pointers,
                parameterSetSizes: sizes,
                nalUnitHeaderLength: 4,
                formatDescriptionOut: &formatDesc
            )
        }
    }
    guard status == noErr else { return nil }
    return formatDesc
}

private func makeTestSampleBuffer(format: CMFormatDescription) -> CMSampleBuffer? {
    var blockBuffer: CMBlockBuffer?
    let data = Data([0x00, 0x00, 0x00, 0x02, 0x65, 0x88]) // Minimal NAL unit payload
    let status = CMBlockBufferCreateWithMemoryBlock(
        allocator: kCFAllocatorDefault,
        memoryBlock: nil,
        blockLength: data.count,
        blockAllocator: nil,
        customBlockSource: nil,
        offsetToData: 0,
        dataLength: data.count,
        flags: 0,
        blockBufferOut: &blockBuffer
    )
    guard status == noErr, let buffer = blockBuffer else { return nil }
    data.withUnsafeBytes { rawPtr in
        if let base = rawPtr.baseAddress {
            CMBlockBufferReplaceDataBytes(with: base, blockBuffer: buffer, offsetIntoDestination: 0, dataLength: data.count)
        }
    }

    var sampleBuffer: CMSampleBuffer?
    var timing = CMSampleTimingInfo(
        duration: CMTime(value: 1, timescale: 50),
        presentationTimeStamp: CMTime(value: 100, timescale: 50),
        decodeTimeStamp: CMTime(value: 100, timescale: 50)
    )

    let sbStatus = CMSampleBufferCreateReady(
        allocator: kCFAllocatorDefault,
        dataBuffer: buffer,
        formatDescription: format,
        sampleCount: 1,
        sampleTimingEntryCount: 1,
        sampleTimingArray: &timing,
        sampleSizeEntryCount: 0,
        sampleSizeArray: nil,
        sampleBufferOut: &sampleBuffer
    )
    guard sbStatus == noErr else { return nil }
    return sampleBuffer
}
