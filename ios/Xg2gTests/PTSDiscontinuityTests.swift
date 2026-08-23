// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import CoreMedia
import Foundation
import Testing

@testable import Xg2g

@Suite struct PTSContinuityMonitorTests {

    private func time(_ seconds: Double) -> CMTime {
        CMTime(seconds: seconds, preferredTimescale: 90_000)
    }

    @Test func firstTimestampEstablishesTheTimeline() {
        var monitor = PTSContinuityMonitor()
        // Nothing to be discontinuous with yet, however large the value.
        #expect(monitor.jump(for: time(70_000)) == nil)
    }

    @Test func ordinaryCadenceIsNeverAJump() {
        var monitor = PTSContinuityMonitor()
        var pts = 1_000.0
        _ = monitor.jump(for: time(pts))

        // 32 ms AC-3 frames across ten seconds.
        for _ in 0..<312 {
            pts += 0.032
            #expect(monitor.jump(for: time(pts)) == nil)
        }
    }

    @Test func aGapShorterThanTheThresholdIsNotAJump() {
        var monitor = PTSContinuityMonitor()
        _ = monitor.jump(for: time(100.0))
        // Well beyond frame spacing but still inside one timeline — a dropped
        // run of packets, not a new clock.
        #expect(monitor.jump(for: time(100.4)) == nil)
    }

    @Test func forwardJumpIsReported() {
        var monitor = PTSContinuityMonitor()
        _ = monitor.jump(for: time(100.0))
        let delta = monitor.jump(for: time(105.0))
        #expect(delta != nil)
        #expect(abs((delta ?? 0) - 5.0) < 0.001)
    }

    @Test func backwardJumpIsReported() {
        var monitor = PTSContinuityMonitor()
        _ = monitor.jump(for: time(100.0))
        let delta = monitor.jump(for: time(99.5))
        #expect(delta != nil)
        #expect((delta ?? 0) < 0)
    }

    /// After a jump the new timestamp becomes the reference, so the stream that
    /// follows it reads as continuous rather than as a second jump.
    @Test func theNewTimelineBecomesTheReference() {
        var monitor = PTSContinuityMonitor()
        _ = monitor.jump(for: time(100.0))
        #expect(monitor.jump(for: time(400.0)) != nil)
        #expect(monitor.jump(for: time(400.032)) == nil)
        #expect(monitor.jump(for: time(400.064)) == nil)
    }

    /// An access unit can legitimately carry no timestamp. Letting that clear
    /// the reference would make the next real one look like a jump.
    @Test func invalidTimestampsAreIgnoredAndDoNotClearTheReference() {
        var monitor = PTSContinuityMonitor()
        _ = monitor.jump(for: time(100.0))
        #expect(monitor.jump(for: .invalid) == nil)
        #expect(monitor.jump(for: time(100.032)) == nil)
    }

    @Test func resetForgetsTheTimeline() {
        var monitor = PTSContinuityMonitor()
        _ = monitor.jump(for: time(100.0))
        monitor.reset()
        #expect(monitor.jump(for: time(9_000.0)) == nil)
    }
}

/// Video timestamps have to survive the 2^33 boundary the same way audio's do.
/// They did not, and the two tracks ending up 95443 seconds apart is a stall
/// that no amount of buffering recovers from.
@Suite struct VideoPESTimestampWrapTests {

    /// Builds a video PES packet carrying the given 33-bit timestamps.
    private func pesPacket(pts: UInt64, dts: UInt64? = nil, payload: Data) -> Data {
        func stamp(_ value: UInt64, marker: UInt8) -> [UInt8] {
            [
                UInt8(marker | UInt8(((value >> 30) & 0x07) << 1)),
                UInt8((value >> 22) & 0xFF),
                UInt8(0x01 | (((value >> 15) & 0x7F) << 1)),
                UInt8((value >> 7) & 0xFF),
                UInt8(0x01 | ((value & 0x7F) << 1))
            ]
        }

        var pes = Data()
        pes.append(contentsOf: [0x00, 0x00, 0x01, 0xE0])
        pes.append(contentsOf: [0x00, 0x00])
        if let dts = dts {
            pes.append(contentsOf: [0x80, 0xC0, 0x0A])          // PTS + DTS, header len 10
            pes.append(contentsOf: stamp(pts, marker: 0x31))
            pes.append(contentsOf: stamp(dts, marker: 0x11))
        } else {
            pes.append(contentsOf: [0x80, 0x80, 0x05])          // PTS only, header len 5
            pes.append(contentsOf: stamp(pts, marker: 0x21))
        }
        pes.append(payload)
        return pes
    }

    @Test func timestampsStayContinuousAcrossThe33BitWrap() throws {
        let assembler = PESPacketAssembler()
        let sink = WrapSink()
        assembler.delegate = sink

        let payload = Data([0x00, 0x00, 0x00, 0x01, 0x09, 0x10])
        let beforeWrap: UInt64 = (1 << 33) - 9_000       // 100 ms short of the boundary
        let afterWrap: UInt64 = 1_000                    // just past it

        assembler.feed(payload: pesPacket(pts: beforeWrap, payload: payload), unitStart: true)
        assembler.feed(payload: pesPacket(pts: afterWrap, payload: payload), unitStart: true)
        assembler.feed(payload: Data([0x00, 0x00, 0x01, 0xE0]), unitStart: true)

        #expect(sink.timestamps.count == 2)
        guard sink.timestamps.count == 2 else { return }

        let first = try #require(sink.timestamps[0])
        let second = try #require(sink.timestamps[1])

        // The wrap is 10000 ticks wide here. Raw, the second reads 95443 seconds
        // *behind* the first; unwrapped it is an ordinary 111 ms ahead.
        let delta = second.seconds - first.seconds
        #expect(delta > 0)
        #expect(abs(delta - (10_000.0 / 90_000.0)) < 0.001)
    }

    /// DTS is derived from the unwrapped PTS, so their spacing has to survive
    /// the boundary too — a second, independent wrap detector would disagree
    /// about where the epoch changed.
    @Test func decodeTimestampKeepsItsLeadAcrossTheWrap() throws {
        let assembler = PESPacketAssembler()
        let sink = WrapSink()
        assembler.delegate = sink

        let payload = Data([0x00, 0x00, 0x00, 0x01, 0x09, 0x10])
        let lead: UInt64 = 3_600                          // 40 ms, one 1080i50 picture

        assembler.feed(
            payload: pesPacket(pts: (1 << 33) - 9_000, dts: (1 << 33) - 9_000 - lead, payload: payload),
            unitStart: true
        )
        // PTS has wrapped; DTS has not yet.
        assembler.feed(
            payload: pesPacket(pts: 1_000, dts: ((1 << 33) &- (lead &- 1_000)) & 0x1_FFFF_FFFF, payload: payload),
            unitStart: true
        )
        assembler.feed(payload: Data([0x00, 0x00, 0x01, 0xE0]), unitStart: true)

        #expect(sink.payloads.count == 2)
        guard sink.payloads.count == 2 else { return }

        for entry in sink.payloads {
            let pts = try #require(entry.pts)
            let dts = try #require(entry.dts)
            #expect(abs((pts.seconds - dts.seconds) - (Double(lead) / 90_000.0)) < 0.001)
        }
    }

    @Test func resetForgetsTheEpoch() throws {
        let assembler = PESPacketAssembler()
        let sink = WrapSink()
        assembler.delegate = sink
        let payload = Data([0x00, 0x00, 0x00, 0x01, 0x09, 0x10])

        assembler.feed(payload: pesPacket(pts: (1 << 33) - 9_000, payload: payload), unitStart: true)
        assembler.feed(payload: pesPacket(pts: 1_000, payload: payload), unitStart: true)
        assembler.feed(payload: Data([0x00, 0x00, 0x01, 0xE0]), unitStart: true)
        assembler.reset()
        sink.payloads.removeAll()

        // A zap starts over: the next stream's timestamps are its own, not a
        // continuation of the epoch the previous one had accumulated.
        assembler.feed(payload: pesPacket(pts: 90_000, payload: payload), unitStart: true)
        assembler.feed(payload: Data([0x00, 0x00, 0x01, 0xE0]), unitStart: true)

        #expect(sink.payloads.count == 1)
        #expect(sink.payloads.first?.pts?.seconds == 1.0)
    }
}

private final class WrapSink: PESPacketAssemblerDelegate, @unchecked Sendable {
    var payloads: [PESVideoData] = []
    var timestamps: [CMTime?] { payloads.map { $0.pts } }

    func pesAssembler(_ assembler: PESPacketAssembler, didEmitVideoPayload payload: PESVideoData) {
        payloads.append(payload)
    }

    func pesAssembler(_ assembler: PESPacketAssembler, didEncounterPESError reason: String) {}
}

/// Covers the wiring rather than the decision: that a jump on the master
/// timeline actually reaches the pipeline's re-anchor and is counted.
///
/// The detector being right is worth nothing if nothing calls it, which is a
/// failure this codebase has had before.
@MainActor
@Suite(.serialized)
struct AudioTimelineJumpWiringTests {

    /// A minimal but real LPCM buffer. The pipeline only reads its timestamp,
    /// but the renderer it is handed to expects something well-formed.
    private func audioBuffer(at seconds: Double, duration: Double = 0.032) -> CMSampleBuffer? {
        var asbd = AudioStreamBasicDescription(
            mSampleRate: 48_000,
            mFormatID: kAudioFormatLinearPCM,
            mFormatFlags: kAudioFormatFlagIsSignedInteger | kAudioFormatFlagIsPacked,
            mBytesPerPacket: 4,
            mFramesPerPacket: 1,
            mBytesPerFrame: 4,
            mChannelsPerFrame: 2,
            mBitsPerChannel: 16,
            mReserved: 0
        )

        var format: CMAudioFormatDescription?
        guard CMAudioFormatDescriptionCreate(
            allocator: kCFAllocatorDefault,
            asbd: &asbd,
            layoutSize: 0, layout: nil,
            magicCookieSize: 0, magicCookie: nil,
            extensions: nil,
            formatDescriptionOut: &format
        ) == noErr, let format = format else { return nil }

        let frames = Int(duration * 48_000)
        let byteCount = frames * 4
        var block: CMBlockBuffer?
        guard CMBlockBufferCreateWithMemoryBlock(
            allocator: kCFAllocatorDefault,
            memoryBlock: nil,
            blockLength: byteCount,
            blockAllocator: kCFAllocatorDefault,
            customBlockSource: nil,
            offsetToData: 0,
            dataLength: byteCount,
            flags: kCMBlockBufferAssureMemoryNowFlag,
            blockBufferOut: &block
        ) == kCMBlockBufferNoErr, let block = block else { return nil }
        CMBlockBufferFillDataBytes(with: 0, blockBuffer: block, offsetIntoDestination: 0, dataLength: byteCount)

        var timing = CMSampleTimingInfo(
            duration: CMTime(value: 1, timescale: 48_000),
            presentationTimeStamp: CMTime(seconds: seconds, preferredTimescale: 90_000),
            decodeTimeStamp: .invalid
        )
        var buffer: CMSampleBuffer?
        guard CMSampleBufferCreateReady(
            allocator: kCFAllocatorDefault,
            dataBuffer: block,
            formatDescription: format,
            sampleCount: frames,
            sampleTimingEntryCount: 1,
            sampleTimingArray: &timing,
            sampleSizeEntryCount: 0,
            sampleSizeArray: nil,
            sampleBufferOut: &buffer
        ) == noErr else { return nil }
        return buffer
    }

    private func feed(_ pipeline: NativeTSVideoPipeline, at seconds: Double) {
        guard let buffer = audioBuffer(at: seconds) else { return }
        pipeline.audioSampleBufferAssembler(
            AudioSampleBufferAssembler(),
            didEmitSampleBuffer: buffer,
            codec: .ac3,
            duration: CMTime(seconds: 0.032, preferredTimescale: 90_000)
        )
    }

    /// Pre-rolls past `audioPreRollSeconds` so the master clock is running,
    /// which is the state in which a jump used to be ignored entirely.
    @MainActor
    private func startedPipeline() -> NativeTSVideoPipeline? {
        let pipeline = NativeTSVideoPipeline()
        // The clock only starts for the session that owns the visible surface, so a
        // harness that expects a running clock has to say who owns it.
        let surface = giveOwnSurface(to: pipeline)
        defer { _ = surface }
        var pts = 1_000.0
        for _ in 0..<40 {                     // 40 x 32 ms = 1.28 s, past the 500 ms pre-roll
            feed(pipeline, at: pts)
            pts += 0.032
        }
        guard pipeline.telemetry.snapshot().isAudioMasterClockActive else { return nil }
        return pipeline
    }

    @Test func aJumpOnTheRunningClockIsCountedAndReAnchored() throws {
        let pipeline = try #require(startedPipeline())
        defer { pipeline.stopStreaming() }

        #expect(pipeline.telemetry.snapshot().ptsDiscontinuities == 0)

        // The receiver retuned underneath us: same stream, new timeline.
        feed(pipeline, at: 5_000.0)

        let after = pipeline.telemetry.snapshot()
        #expect(after.ptsDiscontinuities == 1)
        // Re-anchoring means the clock is down until the new timeline has a
        // cushion, not that it keeps running where it was.
        #expect(after.isAudioMasterClockActive == false)
    }

    @Test func theClockRestartsOnTheNewTimeline() throws {
        let pipeline = try #require(startedPipeline())
        defer { pipeline.stopStreaming() }

        feed(pipeline, at: 5_000.0)
        #expect(pipeline.telemetry.snapshot().isAudioMasterClockActive == false)

        var pts = 5_000.032
        for _ in 0..<40 {
            feed(pipeline, at: pts)
            pts += 0.032
        }

        let after = pipeline.telemetry.snapshot()
        #expect(after.isAudioMasterClockActive == true)
        // One jump, not one per buffer that followed it.
        #expect(after.ptsDiscontinuities == 1)
    }

    @Test func ordinaryCadenceNeverTriggersAReAnchor() throws {
        let pipeline = try #require(startedPipeline())
        defer { pipeline.stopStreaming() }

        var pts = 1_000.0 + 40 * 0.032
        for _ in 0..<200 {
            feed(pipeline, at: pts)
            pts += 0.032
        }

        let after = pipeline.telemetry.snapshot()
        #expect(after.ptsDiscontinuities == 0)
        #expect(after.isAudioMasterClockActive == true)
    }
}
