// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import CoreMedia
import Foundation
import Metal
import Testing

@testable import Xg2g

/// End-to-end cover for the render half of the native pipeline.
///
/// The other pipeline tests never attach a `MetalVideoView`, so nothing they do
/// reaches the shader, the pixel-buffer pool or the display layer — a render
/// path that fails outright still leaves every one of them green. These drive a
/// live stream through decode, deinterlace and presentation on a real Metal
/// device, which is the only place an uncompilable shader or a plane the
/// texture cache will not vend as a render target shows up.
///
/// Live rather than fixture-driven because the embedded capture cannot serve:
/// it carries no parameter sets, so no format description is possible and the
/// assembler emits nothing. See `PULS24FixtureContentTests`.
///
/// Serialized because the suite competes for the main actor with the other live
/// tests, and presentation is driven from there.
@MainActor
@Suite(.serialized)
struct NativeRenderPathTests {

    /// The broadcast source to render, supplied by whoever runs the test.
    ///
    /// Opt-in for the same reason as `LiveIngest`: a hard-coded receiver made
    /// this suite silently network-dependent. `nil` skips.
    private static let streamURL: URL? = {
        let raw = ProcessInfo.processInfo.environment["XG2G_LIVE_RECEIVER_STREAM_URL"] ?? ""
        let trimmed = raw.trimmingCharacters(in: .whitespaces)
        guard !trimmed.isEmpty, !trimmed.contains("$(") else { return nil }
        return URL(string: trimmed)
    }()

    private struct Harness {
        let pipeline: NativeTSVideoPipeline
        let view: MetalVideoView
        let presenter: SystemVideoPresenter
        /// False when no bytes ever arrived, which means the broadcast source is
        /// not on this network rather than that the pipeline is broken.
        let sourceReachable: Bool
    }

    /// Runs the live stream until `settled` holds or the deadline passes.
    ///
    /// A state to poll for rather than a duration to guess at: VideoToolbox
    /// decodes off-thread and presentation hops back to the main actor, so a
    /// fixed sleep races whatever else the test run is doing.
    private func run(
        until settled: @MainActor (NativeTSVideoPipeline, SystemVideoPresenter) -> Bool
    ) async -> Harness? {
        guard MTLCreateSystemDefaultDevice() != nil, let streamURL = Self.streamURL else { return nil }

        let pipeline = NativeTSVideoPipeline()
        let presenter = SystemVideoPresenter()
        let view = MetalVideoView(frame: CGRect(x: 0, y: 0, width: 640, height: 360))

        view.telemetry = pipeline.telemetry
        view.systemPresenter = presenter

        // The session reaches the surface only through the context, and only once the
        // context has handed it over. Binding here is what a commit does; this harness
        // stands in for a screen with a single channel that is committed immediately.
        let context = giveOwnSurface(to: pipeline, presenter: presenter, renderView: view)
        defer { _ = context }

        pipeline.startStreaming(url: streamURL)

        // Up to 25 s. Generous on purpose: the wait for parameter sets on this
        // source is the dominant startup cost and swings hard — 1.4 s on one
        // measured tune, 7.0 s on the next — and the decode gate can add up to
        // its own timeout on top. An 8 s budget made this test fail on stream
        // conditions rather than on code.
        for _ in 0..<1250 {
            if settled(pipeline, presenter) { break }
            try? await Task.sleep(nanoseconds: 20_000_000)
        }

        let reachable = pipeline.telemetry.snapshot().videoPID != 0
        if !reachable {
            print("[RenderPath] ⏭️ Broadcast source unreachable — skipping (this is not a pass)")
        }
        return Harness(
            pipeline: pipeline,
            view: view,
            presenter: presenter,
            sourceReachable: reachable
        )
    }

    @Test func nv12RenderPathDeliversFieldsToTheDisplayLayer() async throws {
        guard let harness = await run(until: { _, presenter in presenter.enqueuedCount > 0 })
        else { return }
        defer { harness.pipeline.stopStreaming() }
        guard harness.sourceReachable else { return }

        let telemetry = harness.pipeline.telemetry.snapshot()
        print("[RenderPath] emitted=\(telemetry.sampleBuffersEmittedCount) decoded=\(telemetry.sampleBuffersDecodedCount) enqueued=\(harness.presenter.enqueuedCount) gated=\(telemetry.gatedAccessUnits) dropped=\(telemetry.droppedFrames)")

        #expect(telemetry.sampleBuffersEmittedCount > 0)
        #expect(telemetry.sampleBuffersDecodedCount > 0)

        // The point of the test: decoded pictures become deinterlaced NV12
        // surfaces AVFoundation accepts. A shader that failed to compile, a
        // plane the cache will not render into, or a pool that cannot allocate
        // all land here as zero.
        #expect(harness.presenter.enqueuedCount > 0)
    }

    /// The gate has to let a real stream through. One that never opens is
    /// indistinguishable, from every other counter, from a stream that never
    /// arrived.
    @Test func decodeGateOpensOnLiveStream() async throws {
        guard let harness = await run(until: { pipeline, _ in
            pipeline.telemetry.snapshot().sampleBuffersDecodedCount > 0
        }) else { return }
        defer { harness.pipeline.stopStreaming() }
        guard harness.sourceReachable else { return }

        let opened = harness.pipeline.telemetry.snapshot()
        #expect(opened.sampleBuffersEmittedCount > 0)

        // Open means open: once a sync sample has been seen, nothing more is
        // discarded. Comparing the two counters directly does not say this —
        // the gate does its discarding before the first emit, so the gated
        // count legitimately exceeds the emitted count for a while afterwards.
        for _ in 0..<50 {
            if harness.pipeline.telemetry.snapshot().sampleBuffersEmittedCount > opened.sampleBuffersEmittedCount { break }
            try? await Task.sleep(nanoseconds: 20_000_000)
        }
        let later = harness.pipeline.telemetry.snapshot()
        print("[RenderPath] gate: gated=\(later.gatedAccessUnits) emitted \(opened.sampleBuffersEmittedCount) -> \(later.sampleBuffersEmittedCount)")

        #expect(later.sampleBuffersEmittedCount > opened.sampleBuffersEmittedCount)
        #expect(later.gatedAccessUnits == opened.gatedAccessUnits)
    }

    /// Switching presentation model mid-stream must not wedge the pipeline. The
    /// drawable path became unreachable precisely because nothing exercised it.
    @Test func switchingPresentationPathKeepsDecodeRunning() async throws {
        guard let harness = await run(until: { _, presenter in presenter.enqueuedCount > 0 })
        else { return }
        defer { harness.pipeline.stopStreaming() }
        guard harness.sourceReachable else { return }

        let before = harness.pipeline.telemetry.snapshot().sampleBuffersDecodedCount
        #expect(before > 0)

        harness.view.presentationPath = .metalDrawable
        #expect(harness.presenter.displayLayer.isHidden)

        for _ in 0..<150 {
            if harness.pipeline.telemetry.snapshot().sampleBuffersDecodedCount > before { break }
            try? await Task.sleep(nanoseconds: 20_000_000)
        }

        // Decode keeps running across the switch. The drawable path itself is
        // driven by the display link, which a headless test cannot rely on
        // firing, so the assertion stops where the test can still be honest.
        #expect(harness.pipeline.telemetry.snapshot().sampleBuffersDecodedCount > before)

        harness.view.presentationPath = .systemLayer
        #expect(!harness.presenter.displayLayer.isHidden)
    }
}

/// What the embedded capture actually contains.
///
/// Recorded as a test because it is load-bearing and counter-intuitive: the
/// capture is 25 pictures cut from the middle of a GOP, with no SPS, no PPS and
/// no IDR. It can exercise demux and the AC-3 path, and it cannot exercise video
/// decode at all — the assembler can build no format description from it, so it
/// emits nothing however much video is present. Anyone reaching for it to test
/// rendering will otherwise conclude the pipeline is broken.
///
/// It is also the clearest illustration of what the decode gate is for: submit
/// these 25 inter-coded pictures to a decoder with primed parameter sets and
/// every one of them references a frame that was never received.
@Suite struct PULS24FixtureContentTests {

    @Test func fixtureIsMidGOPAndCarriesNoParameterSets() throws {
        let parser = TSPacketParser()
        let probe = FixtureProbe()
        parser.delegate = probe
        parser.feed(data: PULS24CaptureFixture.data)

        #expect(parser.videoPID == 1279)
        #expect(parser.audioPIDs == [1283])
        #expect(probe.videoPayloadCount > 0)

        // Inter-coded slices and access unit delimiters, nothing to decode from.
        #expect((probe.nalTypes[1] ?? 0) > 0)      // non-IDR slices
        #expect((probe.nalTypes[5] ?? 0) == 0)     // no IDR
        #expect((probe.nalTypes[7] ?? 0) == 0)     // no SPS
        #expect((probe.nalTypes[8] ?? 0) == 0)     // no PPS
    }
}

/// Counts Annex-B NAL types on the video PID.
private final class FixtureProbe: TSPacketParserDelegate, @unchecked Sendable {
    var videoPayloadCount = 0
    var nalTypes: [UInt8: Int] = [:]
    private var videoPID: UInt16?
    private var buffer = Data()

    func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16) {
        videoPID = pid
    }

    func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool) {
        guard pid == videoPID else { return }
        videoPayloadCount += 1
        buffer.append(data)
        scanNALs()
    }

    func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8) {}

    /// Keeps a three-byte tail so a start code split across two payloads is not
    /// counted twice and not missed.
    private func scanNALs() {
        let bytes = [UInt8](buffer)
        guard bytes.count >= 4 else { return }
        var i = 0
        while i <= bytes.count - 4 {
            if bytes[i] == 0, bytes[i + 1] == 0, bytes[i + 2] == 1 {
                nalTypes[bytes[i + 3] & 0x1F, default: 0] += 1
                i += 4
            } else {
                i += 1
            }
        }
        buffer = Data(bytes.suffix(3))
    }
}
