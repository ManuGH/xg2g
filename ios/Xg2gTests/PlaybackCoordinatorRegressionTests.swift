// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import Foundation
import Testing

@testable import Xg2g

/// Thread-safe completion barrier that coordinates asynchronous test execution deterministically
/// without arbitrary sleeps.
final class ControlledBarrier: @unchecked Sendable {
    private let lock = NSLock()
    private let condition = NSCondition()
    private var isReleased = false
    private var hasArrived = false
    private var arrivalContinuation: CheckedContinuation<Void, Never>?

    func waitForArrival() async {
        await withCheckedContinuation { continuation in
            lock.lock()
            if hasArrived {
                lock.unlock()
                continuation.resume()
            } else {
                arrivalContinuation = continuation
                lock.unlock()
            }
        }
    }

    func markArrived() {
        lock.withLock {
            hasArrived = true
            arrivalContinuation?.resume()
            arrivalContinuation = nil
        }
    }

    func release() {
        condition.lock()
        isReleased = true
        condition.broadcast()
        condition.unlock()
    }

    func blockUntilReleased() {
        condition.lock()
        while !isReleased {
            condition.wait()
        }
        condition.unlock()
    }
}

@MainActor
@Suite("Playback Coordinator Regression & Error Invariants", .serialized)
struct PlaybackCoordinatorRegressionTests {

    private let srefA = "1:0:19:132F:3EF:1:C00000:0:0:0:" // ORF 1 HD
    private let srefB = "1:0:19:1330:3EF:1:C00000:0:0:0:" // ORF 2 HD
    private let srefC = "1:0:19:1331:3EF:1:C00000:0:0:0:" // ATV HD

    private func makeCoordinator(makePresentable: Bool = false) throws -> (ZapCoordinator, ZapPreparationClient) {
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
            streamURL: { sref in URL(string: "http://example.test:8089/api/v3/stream/live/\(sref)") },
            makeSession: {
                let pipeline = NativeTSVideoPipeline()
                #if DEBUG
                if makePresentable {
                    pipeline.forcePresentableForTesting = true
                }
                #endif
                return pipeline
            }
        )
        return (coordinator, client)
    }

    // MARK: - Invariant 1: Initial Playback (Cold Start)

    /// Proves that cold-start channel playback falls back to direct streaming when background preparation
    /// fails, since there is no existing playback session to protect.
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

    /// Proves that when cold-start direct streaming URL resolution fails, the coordinator transitions
    /// cleanly to the structured failed phase without crashing or corrupting state.
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
        if case .failed(let sref, let error) = coordinator.phase {
            #expect(sref == srefA)
            #expect(error.title == LocalizedStringResource("Server Error"))
        } else {
            Issue.record("Expected failed phase, got \(coordinator.phase)")
        }

        await coordinator.stop()
    }

    // MARK: - Invariant 2: Ordinary Failed Zap with Existing Playback (Make-before-Break)

    /// Proves that when a zap fails with an ordinary tuning failure, the running session instance identity
    /// and surface binding are preserved untouched, and the target channel is reported as failed with
    /// structured error codes.
    ///
    /// NOTE: This verifies session instance identity (`playing === runningSession`) and presentation lease
    /// retention; it does not claim hardware-level media clock or uninterrupted decoding continuity.
    @Test("Make-before-Break: failed channel change preserves session instance identity and display lease")
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
                "serviceRef": "\(self.srefB)"
            }
            """.utf8))
        }

        await coordinator.zap(to: srefB)

        // 3. Make-before-Break verification:
        // Channel A must remain presented and running session instance must not be disturbed or replaced
        #expect(coordinator.presentedServiceRef == srefA, "Channel A must remain presented")
        #expect(coordinator.playing === runningSession, "Running session pipeline must not be disturbed or replaced")
        #expect(coordinator.requestedServiceRef == nil, "Requested target must be cleared")

        if case .failed(let sref, let error) = coordinator.phase {
            #expect(sref == srefB)
            #expect(error.code == "TUNING_TIMEOUT")
            #expect(error.title == LocalizedStringResource("Channel Tuning Timed Out"))
        } else {
            Issue.record("Expected failed phase for Channel B, got \(coordinator.phase)")
        }

        await coordinator.stop()
    }

    // MARK: - Invariant 3: Admission-Denied Break-before-Make Exception

    /// Proves that when all receiver tuners are occupied (`admission_denied`), Make-before-Break is intentionally
    /// relaxed to Break-before-Make, tearing down the running session so the single tuner can be freed for the new channel.
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
                "serviceRef": "\(self.srefB)"
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

    /// Proves that when channel B is delayed at the backend and superseded by channel C, C is committed and
    /// presented, and B's delayed ready response is safely dropped without disturbing C or corrupting state.
    ///
    /// Uses a deterministic completion barrier (`ControlledBarrier`) instead of timing delays.
    @Test("Rapid Supersession: late response from superseded channel B never overwrites active channel C")
    func rapidSupersessionWithLateCompletion() async throws {
        let (coordinator, _) = try makeCoordinator(makePresentable: true)

        // Channel A is playing
        await coordinator.play(unprepared: URL(string: "http://example.test:8089/api/v3/stream/live/\(srefA)")!)
        #expect(coordinator.presentedServiceRef == srefA)

        final class RequestTracker: @unchecked Sendable {
            private let lock = NSLock()
            private var _cancelledPreps: [String] = []
            func recordCancel(prepID: String) { lock.withLock { _cancelledPreps.append(prepID) } }
            var cancelledPreps: [String] { lock.withLock { _cancelledPreps } }
        }
        let tracker = RequestTracker()
        let barrierB = ControlledBarrier()

        PrepareStubURLProtocol.setHandler { req in
            let path = req.url?.path ?? ""
            if req.httpMethod == "DELETE" || path.contains("cancel") {
                let prepId = path.components(separatedBy: "/").last ?? "unknown"
                tracker.recordCancel(prepID: prepId)
                return (200, Data(#"{"status": "ok"}"#.utf8))
            }

            if req.url?.query?.contains(self.srefB) == true {
                barrierB.markArrived()
                barrierB.blockUntilReleased()
                return (200, Data("""
                {
                    "preparationId": "prep-b-late",
                    "state": "ready",
                    "generation": 1,
                    "serviceRef": "\(self.srefB)"
                }
                """.utf8))
            } else {
                return (200, Data("""
                {
                    "preparationId": "prep-c-immediate",
                    "state": "ready",
                    "generation": 2,
                    "serviceRef": "\(self.srefC)"
                }
                """.utf8))
            }
        }

        // 1. Start zap to B
        let taskB = Task { await coordinator.zap(to: self.srefB) }
        await barrierB.waitForArrival()

        #expect(coordinator.requestedServiceRef == self.srefB)
        #expect(coordinator.presentedServiceRef == self.srefA)

        // 2. Rapidly zap to C while B is held
        let taskC = Task { await coordinator.zap(to: self.srefC) }
        await taskC.value

        coordinator.playing?.onFirstPictureVisible?()
        await Task.yield()

        #expect(coordinator.presentedServiceRef == self.srefC, "Channel C must be presented")
        #expect(coordinator.requestedServiceRef == nil)
        #expect(coordinator.phase == .idle)

        // 3. Deliver the delayed response for B explicitly
        barrierB.release()
        await taskB.value

        // 4. Assert that the late response from B was dropped and never overwrote C
        #expect(coordinator.presentedServiceRef == self.srefC, "Late response from B must never overwrite active channel C")
        #expect(coordinator.requestedServiceRef == nil)
        #expect(coordinator.phase == .idle)
        #expect(tracker.cancelledPreps.contains("prep-b-late"), "Superseded preparation B must be cancelled on backend")

        await coordinator.stop()
    }

    // MARK: - Invariant 5: Cancellation Cleanup After Preparation Creation

    /// Proves that cancelling an in-flight zap after its backend preparation was created invokes backend
    /// cancellation with the exact preparation ID, resets phase to .idle, clears requestedServiceRef,
    /// and publishes no errors or stale updates.
    @Test("Cancellation cleanup: cancelling in-flight zap cancels exact backend preparation and clears state")
    func cancellationCleanupAfterPreparationCreation() async throws {
        let (coordinator, _) = try makeCoordinator()

        final class CancelTracker: @unchecked Sendable {
            private let lock = NSLock()
            private var _cancelledPreps: [String] = []
            func recordCancel(prepID: String) {
                lock.withLock { _cancelledPreps.append(prepID) }
            }
            var cancelledPreps: [String] {
                lock.withLock { _cancelledPreps }
            }
        }
        let cancelTracker = CancelTracker()
        let barrierStatus = ControlledBarrier()

        PrepareStubURLProtocol.setHandler { req in
            let path = req.url?.path ?? ""
            if req.httpMethod == "DELETE" {
                let prepId = path.components(separatedBy: "/").last ?? "unknown"
                cancelTracker.recordCancel(prepID: prepId)
                return (200, Data(#"{"preparationId":"prep-target-cancel","state":"cancelled"}"#.utf8))
            }

            // Status poll endpoint /stream/prepare/prep-target-cancel
            if path.contains("prep-target-cancel") {
                barrierStatus.markArrived()
                barrierStatus.blockUntilReleased()
                return (200, Data("""
                {
                    "preparationId": "prep-target-cancel",
                    "state": "pending",
                    "generation": 1,
                    "serviceRef": "\(self.srefB)"
                }
                """.utf8))
            }

            // Start preparation endpoint
            return (200, Data("""
            {
                "preparationId": "prep-target-cancel",
                "state": "pending",
                "generation": 1,
                "serviceRef": "\(self.srefB)"
            }
            """.utf8))
        }

        let zapTask = Task { await coordinator.zap(to: self.srefB) }
        await barrierStatus.waitForArrival()

        #expect(coordinator.requestedServiceRef == self.srefB)

        // Cancel the task while in awaitSettled
        zapTask.cancel()
        barrierStatus.release()
        await zapTask.value

        // Assert exact preparation cancellation and cleared state
        #expect(cancelTracker.cancelledPreps.contains("prep-target-cancel"), "Backend must cancel the exact in-flight preparation ID")
        #expect(coordinator.requestedServiceRef == nil, "Requested service ref must be cleared")
        #expect(coordinator.phase == .idle, "Phase must reset to idle, never failed")
        #expect(coordinator.presentedServiceRef == nil, "No stale presentation updates")

        await coordinator.stop()
    }

    // MARK: - Invariant 6: Stop & Dismissal Cancellation

    /// Proves that stopping playback or dismissing the player cancels in-flight work and suppresses
    /// error presentation cleanly without leaving dirty state.
    @Test("Stop and dismissal: cancellation does not publish an error or leave dirty state")
    func stopDismissalSuppressesError() async throws {
        let (coordinator, _) = try makeCoordinator()
        let barrier = ControlledBarrier()

        PrepareStubURLProtocol.setHandler { req in
            let path = req.url?.path ?? ""
            if req.httpMethod == "DELETE" {
                return (200, Data(#"{"preparationId":"prep-slow","state":"cancelled"}"#.utf8))
            }
            if path.contains("prep-slow") {
                barrier.markArrived()
                barrier.blockUntilReleased()
            }
            return (200, Data("""
            {
                "preparationId": "prep-slow",
                "state": "pending",
                "generation": 1,
                "serviceRef": "\(self.srefB)"
            }
            """.utf8))
        }

        // Zap is in flight
        let zapTask = Task { await coordinator.zap(to: self.srefB) }
        await barrier.waitForArrival()

        // Screen is dismissed / stop is called
        await coordinator.stop()
        zapTask.cancel()
        barrier.release()
        await zapTask.value

        #expect(coordinator.presentedServiceRef == nil)
        #expect(coordinator.requestedServiceRef == nil)
        #expect(coordinator.phase == .idle, "Phase must be idle after stop, never failed")
        #expect(coordinator.playing == nil)
    }
}
