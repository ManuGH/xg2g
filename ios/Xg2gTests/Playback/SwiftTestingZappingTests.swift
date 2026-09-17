// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Testing
import Foundation
@testable import Xg2g

@Suite("Swift 6.4 Modern Zapping & Playback Tests")
struct SwiftTestingZappingTests {

    @Test("PlaybackManager starts in idle state")
    func testInitialIdleState() async throws {
        let manager = await PlaybackManager(streamURL: { _ in nil })
        let state = await manager.state
        let mode = await manager.presentationMode
        let isStreaming = await manager.isStreaming

        #expect(state == .idle)
        #expect(mode == .hidden)
        #expect(!isStreaming)
    }

    @Test("PlaybackManager transitions to live playback presentation")
    func testLivePlaybackTransition() async throws {
        let manager = await PlaybackManager(streamURL: { ref in
            URL(string: "http://10.10.55.64:8001/\(ref)")
        })
        let testChannel = Channel(
            id: "test_zdf",
            name: "ZDF HD",
            number: "2",
            serviceRef: "1:0:19:2B66:3F3:1:C00000:0:0:0:",
            logoURL: nil
        )

        await manager.play(channel: testChannel, mode: .fullscreen)

        let state = await manager.state
        let mode = await manager.presentationMode
        let currentChannel = await manager.currentChannel
        let isStreaming = await manager.isStreaming

        #expect(mode == .fullscreen)
        #expect(isStreaming)
        #expect(currentChannel?.name == "ZDF HD")
        #expect(currentChannel?.serviceRef == "1:0:19:2B66:3F3:1:C00000:0:0:0:")

        if case .live(let ch, let m) = state {
            #expect(ch.id == "test_zdf")
            #expect(m == .fullscreen)
        } else {
            Issue.record("Expected .live playback state")
        }
    }

    @Test("Minimize transitions mode to miniplayer without stopping stream")
    func testMinimizeTransition() async throws {
        let manager = await PlaybackManager(streamURL: { _ in nil })
        let testChannel = Channel(
            id: "test_ard",
            name: "Das Erste HD",
            number: "1",
            serviceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:",
            logoURL: nil
        )

        await manager.play(channel: testChannel, mode: .fullscreen)
        await manager.minimize()

        let mode = await manager.presentationMode
        let isStreaming = await manager.isStreaming

        #expect(mode == .miniplayer)
        #expect(isStreaming)
    }

    @Test("Stop cleanly resets playback state to idle")
    func testStopTransition() async throws {
        let manager = await PlaybackManager(streamURL: { _ in nil })
        let testChannel = Channel(
            id: "test_ard",
            name: "Das Erste HD",
            number: "1",
            serviceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:",
            logoURL: nil
        )

        await manager.play(channel: testChannel, mode: .fullscreen)
        await manager.stop()

        let state = await manager.state
        let mode = await manager.presentationMode
        let isStreaming = await manager.isStreaming

        #expect(state == .idle)
        #expect(mode == .hidden)
        #expect(!isStreaming)
    }

    @Test("ZapCoordinator phases are equatable and distinct")
    func testZapCoordinatorPhases() {
        let idle = ZapCoordinator.Phase.idle
        let warming1 = ZapCoordinator.Phase.warming(serviceRef: "ref1")
        let warming2 = ZapCoordinator.Phase.warming(serviceRef: "ref2")
        let buffering = ZapCoordinator.Phase.buffering(serviceRef: "ref1")
        let failed = ZapCoordinator.Phase.failed(serviceRef: "ref1", reason: "timeout")

        #expect(idle != warming1)
        #expect(warming1 != warming2)
        #expect(warming1 != buffering)
        #expect(buffering != failed)
        #expect(warming1 == ZapCoordinator.Phase.warming(serviceRef: "ref1"))
    }
}
