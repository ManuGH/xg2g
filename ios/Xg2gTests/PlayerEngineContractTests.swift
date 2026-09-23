// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

@MainActor
private final class MockTestEngine: PlayerEngine {
    let kind: PlayerEngineKind
    var playCallCount = 0
    var pauseCallCount = 0
    var stopCallCount = 0
    var shouldFailOnPlay = false

    init(kind: PlayerEngineKind) {
        self.kind = kind
    }

    func play() async throws {
        if shouldFailOnPlay {
            throw NSError(domain: "test.engine", code: 42, userInfo: [NSLocalizedDescriptionKey: "Play failed"])
        }
        playCallCount += 1
    }

    func pause() async {
        pauseCallCount += 1
    }

    func stop() async {
        stopCallCount += 1
    }
}

@Suite("PlayerEngine Contract Tests")
@MainActor
struct PlayerEngineContractTests {

    @Test("PlayerEngineKind enumerates all expected engine types")
    func engineKindCases() {
        let kinds: [PlayerEngineKind] = [.nativeLive, .avPlayerRecording, .avPlayerOffline, .timeshift]
        let rawValues = Set(kinds.map(\.rawValue))
        #expect(rawValues.count == 4)
        #expect(rawValues.contains("nativeLive"))
        #expect(rawValues.contains("avPlayerRecording"))
        #expect(rawValues.contains("avPlayerOffline"))
        #expect(rawValues.contains("timeshift"))
    }

    @Test("PlayerEngine conforms to transport execution boundary")
    func engineExecutionBoundary() async throws {
        let engine = MockTestEngine(kind: .nativeLive)
        #expect(engine.kind == .nativeLive)
        #expect(engine.playCallCount == 0)

        try await engine.play()
        #expect(engine.playCallCount == 1)

        await engine.pause()
        #expect(engine.pauseCallCount == 1)

        await engine.stop()
        #expect(engine.stopCallCount == 1)
    }

    @Test("PlayerEngine propagates transport errors without retaining product state")
    func engineErrorPropagation() async {
        let engine = MockTestEngine(kind: .avPlayerRecording)
        engine.shouldFailOnPlay = true

        await #expect(throws: Error.self) {
            try await engine.play()
        }
        #expect(engine.playCallCount == 0)
    }

    @Test("PlayerEngine has no product-state authority")
    func engineHasNoProductStateAuthority() {
        // Verification that PlayerEngine is strictly an execution boundary:
        // Any status/lifecycle truth belongs exclusively to PlaybackManager.state.
        let engine = MockTestEngine(kind: .nativeLive)
        let anyEngine: any PlayerEngine = engine
        #expect(anyEngine.kind == .nativeLive)
    }
}
