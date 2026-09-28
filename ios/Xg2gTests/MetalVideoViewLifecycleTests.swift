// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import CoreVideo
import Foundation
import Metal
import Testing

@testable import Xg2g

/// A real Metal wait keeps the old field on the GPU while the main actor changes ownership.
/// These cases exercise the system-layer completion callback, not just generation predicates.
@MainActor
@Suite(.serialized)
struct MetalVideoViewLifecycleTests {
    private final class WeakHold {
        weak var object: AnyObject?
        init(_ object: AnyObject) { self.object = object }
    }

    private final class Probe: @unchecked Sendable {
        private let lock = NSLock()
        private var submitted = 0
        private var completions: [MTLCommandBufferStatus] = []
        private var decisions: [(generation: Int, admitted: Bool)] = []
        private var holds: [WeakHold] = []

        func recordSubmission() { lock.withLock { submitted += 1 } }
        func recordCompletion(_ status: MTLCommandBufferStatus) {
            lock.withLock { completions.append(status) }
        }
        func recordDecision(generation: Int, admitted: Bool) {
            lock.withLock { decisions.append((generation, admitted)) }
        }
        func recordHold(_ object: AnyObject) {
            lock.withLock { holds.append(WeakHold(object)) }
        }
        var submissionCount: Int { lock.withLock { submitted } }
        var completionStatuses: [MTLCommandBufferStatus] { lock.withLock { completions } }
        var admissions: [(generation: Int, admitted: Bool)] { lock.withLock { decisions } }
        var weakHoldCount: Int { lock.withLock { holds.count } }
        var allHoldsAlive: Bool { lock.withLock { holds.allSatisfy { $0.object != nil } } }
    }

    private func eventually(_ condition: @MainActor () -> Bool) async -> Bool {
        for _ in 0..<200 {
            if condition() { return true }
            try? await Task.sleep(nanoseconds: 5_000_000)
        }
        return condition()
    }

    private func makeFrame(generation: Int) throws -> DecodedVideoFrame {
        let attributes: [CFString: Any] = [
            kCVPixelBufferMetalCompatibilityKey: kCFBooleanTrue as Any,
            kCVPixelBufferIOSurfacePropertiesKey: [:] as CFDictionary
        ]
        var output: CVPixelBuffer?
        let result = CVPixelBufferCreate(
            kCFAllocatorDefault, 64, 64,
            kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange,
            attributes as CFDictionary, &output
        )
        #expect(result == kCVReturnSuccess)
        let buffer = try #require(output)
        CVPixelBufferLockBaseAddress(buffer, [])
        defer { CVPixelBufferUnlockBaseAddress(buffer, []) }
        for plane in 0..<CVPixelBufferGetPlaneCount(buffer) {
            let base = try #require(CVPixelBufferGetBaseAddressOfPlane(buffer, plane))
            let count = CVPixelBufferGetBytesPerRowOfPlane(buffer, plane)
                * CVPixelBufferGetHeightOfPlane(buffer, plane)
            memset(base, 0x80, count)
        }
        return DecodedVideoFrame(
            pixelBuffer: buffer,
            pts: CMTime(seconds: 1, preferredTimescale: 90_000),
            structure: .wovenTopFieldFirst,
            generation: generation
        )
    }

    private func makeHarness() throws -> (MetalVideoView, SystemVideoPresenter, any MTLSharedEvent, Probe) {
        let device = try #require(MTLCreateSystemDefaultDevice())
        let event = try #require(device.makeSharedEvent())
        let probe = Probe()
        let view = MetalVideoView(frame: CGRect(x: 0, y: 0, width: 64, height: 64))
        let presenter = SystemVideoPresenter()
        presenter.flush(generation: 1)
        view.systemPresenter = presenter
        view.resetForChannelZap(generation: 1)
        view.sourceIsInterlaced = true
        view.beforeSystemPassEncodeForTesting = { commandBuffer in
            commandBuffer.encodeWaitForEvent(event, value: 1)
            probe.recordSubmission()
            commandBuffer.addCompletedHandler { completed in
                probe.recordCompletion(completed.status)
            }
        }
        view.didCreateSurfaceHoldForTesting = { object in
            probe.recordHold(object)
        }
        view.didHandleSystemPassCompletionForTesting = { generation, admitted in
            probe.recordDecision(generation: generation, admitted: admitted)
        }
        return (view, presenter, event, probe)
    }

    @Test("A zap retires GPU fields before the next generation is presented")
    func zapWhileGPUReadsPreviousChannel() async throws {
        let (view, presenter, gate, probe) = try makeHarness()
        defer {
            gate.signaledValue = 1
            view.beforeSystemPassEncodeForTesting = nil
            view.didCreateSurfaceHoldForTesting = nil
            view.didHandleSystemPassCompletionForTesting = nil
        }
        view.enqueueFrame(try makeFrame(generation: 1))
        #expect(await eventually { probe.submissionCount == 2 })
        #expect(probe.admissions.isEmpty)
        #expect(probe.weakHoldCount == 2 && probe.allHoldsAlive)
        #expect(presenter.enqueuedCount == 0)

        presenter.flush(generation: 2)
        view.resetForChannelZap(generation: 2)
        #expect(probe.allHoldsAlive)
        view.beforeSystemPassEncodeForTesting = nil
        gate.signaledValue = 1
        #expect(await eventually {
            probe.admissions.count == 2 && probe.completionStatuses.count == 2
        })
        #expect(probe.completionStatuses.allSatisfy { $0 == .completed })
        #expect(probe.admissions.allSatisfy { $0.generation == 1 && !$0.admitted })
        #expect(presenter.enqueuedCount == 0)

        view.enqueueFrame(try makeFrame(generation: 2))
        #expect(await eventually { probe.admissions.count == 4 })
        #expect(probe.admissions.suffix(2).allSatisfy { $0.generation == 2 && $0.admitted })
    }

    @Test("A presentation-path change retires a pending system-layer field",
          arguments: [false, true])
    func pathSwitchWhileGPUReadsField(roundTrip: Bool) async throws {
        let (view, presenter, gate, probe) = try makeHarness()
        defer {
            gate.signaledValue = 1
            view.beforeSystemPassEncodeForTesting = nil
            view.didCreateSurfaceHoldForTesting = nil
            view.didHandleSystemPassCompletionForTesting = nil
        }
        view.enqueueFrame(try makeFrame(generation: 1))
        #expect(await eventually { probe.submissionCount == 2 })
        #expect(probe.admissions.isEmpty)
        #expect(probe.weakHoldCount == 2 && probe.allHoldsAlive)

        view.presentationPath = .metalDrawable
        if roundTrip { view.presentationPath = .systemLayer }
        #expect(view.currentGeneration == 1)
        #expect(probe.allHoldsAlive)
        gate.signaledValue = 1

        #expect(await eventually {
            probe.admissions.count == 2 && probe.completionStatuses.count == 2
        })
        #expect(probe.completionStatuses.count == 2)
        #expect(probe.completionStatuses.allSatisfy { $0 == .completed })
        #expect(probe.admissions.allSatisfy { $0.generation == 1 && !$0.admitted })
        #expect(presenter.enqueuedCount == 0)
    }
}
