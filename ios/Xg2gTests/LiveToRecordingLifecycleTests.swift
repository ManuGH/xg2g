// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

@MainActor
@Suite(.serialized)
struct LiveToRecordingLifecycleTests {

    private let channelA = Channel(
        id: "orf1",
        name: "ORF 1 HD",
        number: "1",
        serviceRef: "1:0:19:132F:3EF:1:C00000:0:0:0:",
        logoURL: nil
    )

    private let channelB = Channel(
        id: "orf2",
        name: "ORF 2 HD",
        number: "2",
        serviceRef: "1:0:19:1330:3EF:1:C00000:0:0:0:",
        logoURL: nil
    )

    private let testRecording = Recording(
        id: "rec-f1-2026",
        title: "Formula 1: Austrian GP",
        description: "Live Rennen",
        beginDate: Date(),
        durationSeconds: 7200,
        serviceRef: "1:0:19:132F:3EF:1:C00000:0:0:0:",
        filename: "f1.ts",
        status: "completed",
        serverResumePos: 0
    )

    private let testOffline = OfflineRecording(
        id: "offline-f1-2026",
        recordingId: "rec-f1-2026",
        title: "Formula 1: Austrian GP (Offline)",
        channelName: "ORF 1 HD",
        durationSeconds: 7200,
        fileSize: 4_500_000_000,
        downloadDate: Date(),
        localRelativePath: "Recordings/f1.mp4",
        quality: .original
    )

    private func makeManager() -> PlaybackManager {
        let streamURL = URL(string: "http://127.0.0.1:8089/api/v3/stream/live/dummy")!
        return PlaybackManager(streamURL: { _ in streamURL })
    }

    // MARK: - Invariant 1: Canonical State Integrity & Derived Properties

    @Test("Initial state is idle and derived properties are clean")
    func initialStateIsIdle() {
        let manager = makeManager()
        #expect(manager.state == .idle)
        #expect(manager.currentChannel == nil)
        #expect(manager.presentationMode == .hidden)
        #expect(manager.activeRecordingItem == nil)
        #expect(manager.activeOfflineRecording == nil)
        #expect(manager.isStreaming == false)
        #expect(manager.isPlaying == false)
    }

    @Test("Live TV mode transitions between fullscreen and miniplayer within single state")
    func liveTvPresentationTransitions() async {
        let manager = makeManager()

        // 1. Play Fullscreen
        await manager.play(channel: channelA, mode: .fullscreen)
        #expect(manager.state == .live(channelA, mode: .fullscreen))
        #expect(manager.currentChannel == channelA)
        #expect(manager.presentationMode == .fullscreen)
        #expect(manager.isStreaming == true)

        // 2. Minimize to MiniPlayer
        manager.minimize()
        #expect(manager.state == .live(channelA, mode: .miniplayer))
        #expect(manager.currentChannel == channelA)
        #expect(manager.presentationMode == .miniplayer)

        // 3. Expand back to Fullscreen
        manager.expand()
        #expect(manager.state == .live(channelA, mode: .fullscreen))
        #expect(manager.presentationMode == .fullscreen)

        // 4. Stop
        await manager.stop()
        #expect(manager.state == .idle)
        #expect(manager.currentChannel == nil)
        #expect(manager.presentationMode == .hidden)
    }

    // MARK: - Invariant 2: Live -> Recording Handover

    @Test("Starting a recording cleanly terminates live playback and claims exclusive ownership")
    func liveToRecordingHandover() async throws {
        let manager = makeManager()

        // 1. Start Live TV
        await manager.play(channel: channelA, mode: .fullscreen)
        try? await Task.sleep(for: .milliseconds(30))
        #expect(manager.coordinator.presentedServiceRef != nil)

        // 2. Start Recording
        await manager.play(recording: testRecording, startPosition: 120.0)

        // 3. Assert exact canonical state
        let expectedItem = PlayingRecordingItem(id: testRecording.id, recording: testRecording, initialPosition: 120.0)
        #expect(manager.state == .recording(expectedItem))
        #expect(manager.activeRecordingItem == expectedItem)
        #expect(manager.currentChannel == nil, "Live channel must be cleared")
        #expect(manager.presentationMode == .hidden, "Live presentationMode must be hidden")
        #expect(manager.isStreaming == false, "isStreaming must be false for VOD recordings")
        #expect(manager.isPlaying == true, "isPlaying is true while watching a recording")

        // 4. Teardown
        await manager.stop()
        #expect(manager.state == .idle)
        #expect(manager.activeRecordingItem == nil)
    }

    // MARK: - Invariant 3: Recording -> Live Handover & Cleanup Hook

    @Test("Switching from recording to live executes explicit recording cleanup hook")
    func recordingToLiveHandoverWithCleanupHook() async throws {
        let manager = makeManager()

        // 1. Start Recording
        await manager.play(recording: testRecording, startPosition: 0)
        #expect(manager.activeRecordingItem != nil)

        // 2. Recording Player registers its cleanup hook (e.g. AVPlayer teardown)
        var cleanupExecutionCount = 0
        manager.registerRecordingCleanup {
            cleanupExecutionCount += 1
        }

        // 3. User switches to Live TV (ORF 2)
        await manager.play(channel: channelB, mode: .fullscreen)

        // 4. Assert cleanup executed BEFORE live playback committed
        #expect(cleanupExecutionCount == 1, "Recording cleanup hook must be executed deterministically")
        #expect(manager.state == .live(channelB, mode: .fullscreen))
        #expect(manager.currentChannel == channelB)
        #expect(manager.activeRecordingItem == nil, "Recording item must be nil")

        // 5. Teardown
        await manager.stop()
        #expect(manager.state == .idle)
    }

    // MARK: - Invariant 4: Offline Playback Handover

    @Test("Offline recording playback integrates into the single state machine")
    func offlinePlaybackHandover() async {
        let manager = makeManager()

        // 1. Play Live
        await manager.play(channel: channelA, mode: .fullscreen)
        #expect(manager.currentChannel == channelA)

        // 2. Switch to Offline
        await manager.play(offline: testOffline)
        #expect(manager.state == .offline(testOffline))
        #expect(manager.activeOfflineRecording == testOffline)
        #expect(manager.currentChannel == nil)
        #expect(manager.activeRecordingItem == nil)

        // 3. Switch back to Live
        await manager.play(channel: channelB, mode: .fullscreen)
        #expect(manager.state == .live(channelB, mode: .fullscreen))
        #expect(manager.activeOfflineRecording == nil)

        await manager.stop()
        #expect(manager.state == .idle)
    }

    // MARK: - Invariant 5: Rapid Zigzag Transitions (No Phantom State)

    @Test("Rapid zigzag transitions between Live and Recording leave exactly one valid active state")
    func rapidZigzagTransitions() async throws {
        let manager = makeManager()

        for _ in 1...10 {
            await manager.play(channel: channelA, mode: .fullscreen)
            #expect(manager.currentChannel == channelA)
            #expect(manager.activeRecordingItem == nil)

            await manager.play(recording: testRecording, startPosition: 50.0)
            #expect(manager.activeRecordingItem?.id == testRecording.id)
            #expect(manager.currentChannel == nil)

            await manager.play(channel: channelB, mode: .fullscreen)
            #expect(manager.currentChannel == channelB)
            #expect(manager.activeRecordingItem == nil)
        }

        await manager.stop()
        #expect(manager.state == .idle)
    }

    // MARK: - Invariant 6: Fully Awaited Handover Teardown & Concurrency

    @Test("Live to recording handover awaits live coordinator stop before committing recording state")
    func liveToRecordingAwaitsLiveTeardown() async throws {
        let manager = makeManager()

        // 1. Start Live TV
        await manager.play(channel: channelA, mode: .fullscreen)
        #expect(manager.state == .live(channelA, mode: .fullscreen))

        // 2. Play recording using the awaited transition
        await manager.play(recording: testRecording, startPosition: 42.0)

        // 3. Verify Live is completely torn down
        #expect(manager.coordinator.playing == nil)
        #expect(manager.coordinator.presentedServiceRef == nil)
        #expect(manager.state == .recording(PlayingRecordingItem(id: testRecording.id, recording: testRecording, initialPosition: 42.0)))

        await manager.stop()
        #expect(manager.state == .idle)
    }

    @Test("Competing rapid async transitions resolve deterministically to the final winner")
    func competingAsyncTransitionsResolveDeterministically() async throws {
        let manager = makeManager()

        // Trigger rapid concurrent transitions
        await withTaskGroup(of: Void.self) { group in
            group.addTask { await manager.play(channel: self.channelA) }
            group.addTask { await manager.play(recording: self.testRecording, startPosition: 10) }
            group.addTask { await manager.play(channel: self.channelB) }
            group.addTask { await manager.play(offline: self.testOffline) }
        }

        // Must be in exactly one valid state (not a broken or partial hybrid)
        switch manager.state {
        case .idle:
            #expect(manager.currentChannel == nil)
            #expect(manager.activeRecordingItem == nil)
            #expect(manager.activeOfflineRecording == nil)
        case .live(let ch, _):
            #expect(ch == self.channelA || ch == self.channelB)
            #expect(manager.activeRecordingItem == nil)
            #expect(manager.activeOfflineRecording == nil)
        case .recording(let rec):
            #expect(rec.id == self.testRecording.id)
            #expect(manager.currentChannel == nil)
            #expect(manager.activeOfflineRecording == nil)
        case .offline(let off):
            #expect(off.id == self.testOffline.id)
            #expect(manager.currentChannel == nil)
            #expect(manager.activeRecordingItem == nil)
        }

        await manager.stop()
        #expect(manager.state == .idle)
    }

    @Test("Interrupted Live to Recording transition yields to subsequent Live request without committing recording")
    func interruptedLiveToRecordingTransitionYieldsToNewLiveRequest() async throws {
        let manager = makeManager()

        // 1. Start Live A
        await manager.play(channel: channelA, mode: .fullscreen)
        #expect(manager.currentChannel == channelA)

        // 2. Launch Live -> Recording and immediately supersede with Live B
        async let recordingTask: Void = manager.play(recording: testRecording, startPosition: 10.0)
        async let liveBTask: Void = manager.play(channel: channelB, mode: .fullscreen)

        _ = await (recordingTask, liveBTask)

        // 3. Assert Live B won exclusively and recording state was never committed
        #expect(manager.state == .live(channelB, mode: .fullscreen))
        #expect(manager.currentChannel == channelB)
        #expect(manager.activeRecordingItem == nil)

        await manager.stop()
        #expect(manager.state == .idle)
    }

    @Test("Interrupted Live to Offline transition yields to subsequent Recording request without committing offline")
    func interruptedLiveToOfflineTransitionYieldsToRecordingRequest() async throws {
        let manager = makeManager()

        // 1. Start Live A
        await manager.play(channel: channelA, mode: .fullscreen)
        #expect(manager.currentChannel == channelA)

        // 2. Launch Live -> Offline and immediately supersede with Recording
        async let offlineTask: Void = manager.play(offline: testOffline)
        async let recordingTask: Void = manager.play(recording: testRecording, startPosition: 55.0)

        _ = await (offlineTask, recordingTask)

        // 3. Assert Recording won exclusively and offline was discarded
        #expect(manager.state == .recording(PlayingRecordingItem(id: testRecording.id, recording: testRecording, initialPosition: 55.0)))
        #expect(manager.activeRecordingItem != nil)
        #expect(manager.activeOfflineRecording == nil)
        #expect(manager.currentChannel == nil)

        await manager.stop()
        #expect(manager.state == .idle)
    }
}
