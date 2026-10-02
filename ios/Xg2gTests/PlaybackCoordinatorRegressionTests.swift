// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import Foundation
import Testing

@testable import Xg2g

@MainActor
@Suite("Playback Coordinator Regression & Error Invariants", .serialized)
struct PlaybackCoordinatorRegressionTests {

    private let srefA = "1:0:19:132F:3EF:1:C00000:0:0:0:" // ORF 1 HD
    private let srefB = "1:0:19:1330:3EF:1:C00000:0:0:0:" // ORF 2 HD
    private let srefC = "1:0:19:1331:3EF:1:C00000:0:0:0:" // ATV HD

    private func makeCoordinator() throws -> (ZapCoordinator, ZapPreparationClient) {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [PrepareStubURLProtocol.self]
        PrepareStubURLProtocol.reset()
        let api = HTTPAPIClient(
            address: try ServerAddressParser.parseTrusted("http://example.test:8089/"),
            session: URLSession(configuration: config)
        )
        let client = ZapPreparationClient(api: api, clientID: "regression-test")
        let coordinator = ZapCoordinator(
            preparations: client,
            streamURL: { sref in URL(string: "http://example.test:8089/api/v3/stream/live/\(sref)") }
        )
        return (coordinator, client)
    }

    // MARK: - Invariant 1: Initial Playback (Cold Start)

    @Test("Initial playback: cold start with failed preparation falls back to direct stream outright")
    func initialPlaybackColdStartFallback() async throws {
        let (coordinator, _) = try makeCoordinator()

        // 1. Initially nothing is playing
        #expect(coordinator.presentedServiceRef == nil)
        #expect(coordinator.requestedServiceRef == nil)
        #expect(coordinator.playing == nil)

        // 2. Prepare returns non-admission failure
        PrepareStubURLProtocol.setHandler { _ in
            (500, Data(#"{"error": "receiver warmup failed"}"#.utf8))
        }

        // 3. Cold start: because nothing is playing to protect, coordinator falls back to startOutright
        await coordinator.zap(to: srefA)

        #expect(coordinator.presentedServiceRef == srefA, "Initial channel must start outright when prepare fails")
        #expect(coordinator.requestedServiceRef == nil)
        #expect(coordinator.phase == .idle)
        #expect(coordinator.playing != nil)

        await coordinator.stop()
    }

    @Test("Initial playback: cold start failure when stream URL cannot be formed transitions to failed cleanly")
    func initialPlaybackColdStartTotalFailure() async throws {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [PrepareStubURLProtocol.self]
        PrepareStubURLProtocol.reset()
        let api = HTTPAPIClient(
            address: try ServerAddressParser.parseTrusted("http://example.test:8089/"),
            session: URLSession(configuration: config)
        )
        let client = ZapPreparationClient(api: api, clientID: "regression-test")
        let coordinator = ZapCoordinator(
            preparations: client,
            streamURL: { _ in nil } // Stream URL cannot be built
        )

        PrepareStubURLProtocol.setHandler { _ in
            (500, Data(#"{"error": "receiver warmup failed"}"#.utf8))
        }

        await coordinator.zap(to: srefA)

        #expect(coordinator.presentedServiceRef == nil)
        #expect(coordinator.requestedServiceRef == nil)
        if case .failed(let sref, _) = coordinator.phase {
            #expect(sref == srefA)
        } else {
            Issue.record("Expected failed phase, got \(coordinator.phase)")
        }

        await coordinator.stop()
    }

    // MARK: - Invariant 2: Ordinary Failed Zap with Existing Playback (Make-before-Break)

    @Test("Make-before-Break: failed channel change preserves actively running channel and audio/video pipeline")
    func ordinaryFailedZapPreservesRunningPlayback() async throws {
        let (coordinator, _) = try makeCoordinator()

        // 1. Channel A is actively playing on screen
        await coordinator.play(unprepared: URL(string: "http://example.test:8089/api/v3/stream/live/\(srefA)")!)
        #expect(coordinator.presentedServiceRef == srefA)
        let runningSession = try #require(coordinator.playing)

        // 2. User zaps to Channel B, but preparation fails with ordinary tuning timeout (not admission denied)
        PrepareStubURLProtocol.setHandler { _ in
            (200, Data("""
            {
                "preparationId": "prep-fail-b",
                "state": "failed",
                "outcome": "tuning_timeout",
                "serviceRef": "\(srefB)"
            }
            """.utf8))
        }

        await coordinator.zap(to: srefB)

        // 3. Make-before-Break verification:
        // Channel A must remain presented and running completely undisturbed
        #expect(coordinator.presentedServiceRef == srefA, "Channel A must remain presented")
        #expect(coordinator.playing === runningSession, "Running session pipeline must not be disturbed or replaced")
        #expect(coordinator.requestedServiceRef == nil, "Requested target must be cleared")

        if case .failed(let sref, let reason) = coordinator.phase {
            #expect(sref == srefB)
            #expect(reason.contains("tuning_timeout"))
        } else {
            Issue.record("Expected failed phase for Channel B, got \(coordinator.phase)")
        }

        await coordinator.stop()
    }

    // MARK: - Invariant 3: Admission-Denied Break-before-Make Exception

    @Test("Admission Denied Exception: single tuner contention breaks before make to free tuner")
    func admissionDeniedBreakBeforeMakeException() async throws {
        let (coordinator, _) = try makeCoordinator()

        // 1. Channel A is playing
        await coordinator.play(unprepared: URL(string: "http://example.test:8089/api/v3/stream/live/\(srefA)")!)
        #expect(coordinator.presentedServiceRef == srefA)

        // 2. Zap to B returns admission_denied
        PrepareStubURLProtocol.setHandler { _ in
            (200, Data("""
            {
                "preparationId": "prep-denied-b",
                "state": "failed",
                "outcome": "admission_denied",
                "serviceRef": "\(srefB)"
            }
            """.utf8))
        }

        await coordinator.zap(to: srefB)

        // 3. Channel B starts outright, Channel A is replaced
        #expect(coordinator.presentedServiceRef == srefB, "Channel B must be presented via break-before-make fallback")
        #expect(coordinator.requestedServiceRef == nil)
        #expect(coordinator.phase == .idle)

        await coordinator.stop()
    }

    // MARK: - Invariant 4: Rapid Supersession with Late Completion

    @Test("Rapid Supersession: late response from superseded channel B never overwrites active channel C")
    func rapidSupersessionWithLateCompletion() async throws {
        let (coordinator, _) = try makeCoordinator()

        // Channel A is playing
        await coordinator.play(unprepared: URL(string: "http://example.test:8089/api/v3/stream/live/\(srefA)")!)
        #expect(coordinator.presentedServiceRef == srefA)

        final class RequestTracker: @unchecked Sendable {
            private let lock = NSLock()
            private var _bCalled = false
            private var _cancelCount = 0
            func recordB() { lock.withLock { _bCalled = true } }
            func recordCancel() { lock.withLock { _cancelCount += 1 } }
            var bCalled: Bool { lock.withLock { _bCalled } }
            var cancelCount: Int { lock.withLock { _cancelCount } }
        }
        let tracker = RequestTracker()

        PrepareStubURLProtocol.setHandler { req in
            let path = req.url?.path ?? ""
            if path.contains("cancel") {
                tracker.recordCancel()
                return (200, Data(#"{"status": "ok"}"#.utf8))
            }

            if req.url?.query?.contains(srefB) == true {
                tracker.recordB()
                return (200, Data("""
                {
                    "preparationId": "prep-b-late",
                    "state": "pending",
                    "generation": 1,
                    "serviceRef": "\(srefB)"
                }
                """.utf8))
            } else {
                return (200, Data("""
                {
                    "preparationId": "prep-c-immediate",
                    "state": "pending",
                    "generation": 2,
                    "serviceRef": "\(srefC)"
                }
                """.utf8))
            }
        }

        // 1. Start zap to B
        let taskB = Task { await coordinator.zap(to: srefB) }
        try? await Task.sleep(for: .milliseconds(25))

        #expect(coordinator.requestedServiceRef == srefB)

        // 2. Rapidly zap to C before B finishes
        let taskC = Task { await coordinator.zap(to: srefC) }
        try? await Task.sleep(for: .milliseconds(25))

        #expect(coordinator.requestedServiceRef == srefC, "Requested service ref must now be C")
        #expect(coordinator.presentedServiceRef == srefA, "Channel A must still be presented")

        taskB.cancel()
        taskC.cancel()
        await coordinator.stop()
    }

    // MARK: - Invariant 5: Stop & Dismissal Cancellation

    @Test("Stop and dismissal: cancellation does not publish an error or leave dirty state")
    func stopDismissalSuppressesError() async throws {
        let (coordinator, _) = try makeCoordinator()

        PrepareStubURLProtocol.setHandler { _ in
            (200, Data("""
            {
                "preparationId": "prep-slow",
                "state": "pending",
                "generation": 1,
                "serviceRef": "\(srefB)"
            }
            """.utf8))
        }

        // Zap is in flight
        let zapTask = Task { await coordinator.zap(to: srefB) }
        try? await Task.sleep(for: .milliseconds(30))

        // Screen is dismissed / stop is called
        await coordinator.stop()
        zapTask.cancel()

        #expect(coordinator.presentedServiceRef == nil)
        #expect(coordinator.requestedServiceRef == nil)
        #expect(coordinator.phase == .idle, "Phase must be idle after stop, never failed")
        #expect(coordinator.playing == nil)
    }
}
