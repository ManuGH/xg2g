// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
import Combine
@testable import Xg2g

@MainActor
private final class RejectingPlaybackController: PlaybackControlling {
    var currentChannel: Channel? = nil
    var isPlaying: Bool = false

    func play(channel: Channel) {
        // Intentionally no-op to simulate a rejected, permission-denied, or delayed command
    }

    func stop() {}
    func togglePlayPause() {}

    func observeState(_ handler: @escaping @MainActor (Channel?, Bool) -> Void) -> AnyCancellable {
        handler(currentChannel, isPlaying)
        return AnyCancellable {}
    }
}

@Suite("PlaybackStore Projection Tests")
@MainActor
struct PlaybackStoreProjectionTests {

    private let channelA = Channel(
        id: "orf1",
        name: "ORF 1 HD",
        number: "1",
        serviceRef: "1:0:19:132F:3EF:1:C00000:0:0:0:",
        logoURL: nil
    )

    private let testRecording = Recording(
        id: "rec-test-1",
        title: "Formula 1 2026",
        description: "Grand Prix",
        beginDate: Date(),
        durationSeconds: 7200,
        serviceRef: "1:0:19:132F:3EF:1:C00000:0:0:0:",
        filename: "f1.ts",
        status: "completed",
        serverResumePos: 0
    )

    private let testOffline = OfflineRecording(
        id: "offline-test-1",
        recordingId: "rec-test-1",
        title: "Formula 1 2026 Offline",
        channelName: "ORF 1 HD",
        durationSeconds: 7200,
        fileSize: 4_000_000_000,
        downloadDate: Date(),
        localRelativePath: "Recordings/f1.mp4",
        quality: .original
    )

    private func makeHarness() -> (PlaybackManager, PlaybackStore) {
        let manager = PlaybackManager(streamURL: { ref in
            URL(string: "http://127.0.0.1:8089/api/v3/stream/live/\(ref)")!
        })
        let controller = LegacyBridgePlaybackController(playbackManager: manager)
        let store = PlaybackStore(controller: controller)
        return (manager, store)
    }

    @Test("PlaybackStore reflects initial PlaybackManager snapshot")
    func initialSnapshotReflection() {
        let (_, store) = makeHarness()
        #expect(store.currentChannel == nil)
        #expect(store.isPlaying == false)
        #expect(store.errorMessage == nil)
    }

    @Test("idle → live projection updates PlaybackStore accurately")
    func idleToLiveProjection() async {
        let (manager, store) = makeHarness()

        await manager.play(channel: channelA, mode: .fullscreen)

        #expect(store.currentChannel == channelA)
        #expect(manager.currentChannel == channelA)
    }

    @Test("live → recording projection updates PlaybackStore accurately")
    func liveToRecordingProjection() async {
        let (manager, store) = makeHarness()

        await manager.play(channel: channelA, mode: .fullscreen)
        #expect(store.currentChannel == channelA)

        await manager.play(recording: testRecording, startPosition: 0)
        #expect(store.currentChannel == nil, "Live channel must clear when playing recording")
        #expect(store.isPlaying == true, "isPlaying must reflect active recording playback")
    }

    @Test("recording → offline projection updates PlaybackStore accurately")
    func recordingToOfflineProjection() async {
        let (manager, store) = makeHarness()

        await manager.play(recording: testRecording, startPosition: 0)
        #expect(store.currentChannel == nil)
        #expect(store.isPlaying == true)

        await manager.play(offline: testOffline)
        #expect(store.currentChannel == nil)
        #expect(store.isPlaying == true)
    }

    @Test("offline → idle projection updates PlaybackStore accurately")
    func offlineToIdleProjection() async {
        let (manager, store) = makeHarness()

        await manager.play(offline: testOffline)
        #expect(store.isPlaying == true)

        await manager.stop()
        #expect(store.currentChannel == nil)
        #expect(store.isPlaying == false)
    }

    @Test("delayed or rejected command does NOT mutate PlaybackStore optimistically")
    func delayedOrRejectedCommandDoesNotMutateOptimistically() {
        let rejectingController = RejectingPlaybackController()
        let store = PlaybackStore(controller: rejectingController)

        #expect(store.currentChannel == nil)
        #expect(store.isPlaying == false)

        // Attempt to play channel — under Apple A this mutated store optimistically:
        store.play(channel: channelA)

        // Under Apple B canonical projection, store remains unchanged because controller rejected/delayed:
        #expect(store.currentChannel == nil, "Store must not mutate optimistically on uncommitted commands")
        #expect(store.isPlaying == false, "Store isPlaying must not become true optimistically")
    }

    @Test("projection lifetime survives AppComposition factory return")
    func projectionSurvivesAppCompositionFactory() async {
        let appModel = AppModel()
        let composition = AppComposition.makeBridged(appModel: appModel)

        composition.playbackStore.play(channel: channelA)

        for _ in 0..<20 {
            if composition.playbackStore.currentChannel == channelA { break }
            try? await Task.sleep(nanoseconds: 10_000_000)
        }

        #expect(composition.playbackStore.currentChannel == channelA)
        #expect(appModel.playbackManager.currentChannel == channelA)
    }

    @Test("projection observation is released on teardown and re-attachment")
    func projectionObservationReleasedOnTeardown() {
        let (manager1, store) = makeHarness()
        let (manager2, _) = makeHarness()
        let controller2 = LegacyBridgePlaybackController(playbackManager: manager2)

        store.attach(controller: controller2)

        // manager1 updates should no longer affect store
        manager1.play(channel: channelA, mode: .fullscreen)
        #expect(store.currentChannel == nil, "Old manager state must not affect store after re-attachment")
    }

    @Test("no retain cycle between PlaybackManager, controller, and PlaybackStore")
    func noRetainCycle() {
        weak var weakManager: PlaybackManager?
        weak var weakController: LegacyBridgePlaybackController?
        weak var weakStore: PlaybackStore?

        do {
            let manager = PlaybackManager(streamURL: { _ in nil })
            let controller = LegacyBridgePlaybackController(playbackManager: manager)
            let store = PlaybackStore(controller: controller)

            weakManager = manager
            weakController = controller
            weakStore = store

            #expect(weakManager != nil)
            #expect(weakController != nil)
            #expect(weakStore != nil)
        }

        #expect(weakStore == nil, "PlaybackStore must deallocate when dropped")
        #expect(weakController == nil, "LegacyBridgePlaybackController must deallocate when dropped")
        #expect(weakManager == nil, "PlaybackManager must deallocate when dropped")
    }

    @Test("playing nil → session and session → nil directly update isPlaying without other property changes")
    func playingPublisherDirectlyDrivesStoreIsPlayingWithoutOtherPropertyChanges() async {
        let (manager, store) = makeHarness()

        // 1. Enter live state
        await manager.play(channel: channelA, mode: .fullscreen)
        #expect(store.currentChannel == channelA)
        #expect(store.isPlaying == true, "Initially isPlaying is true once direct stream is active")

        // 2. Capture baseline coordinator properties
        let initialPresented = manager.coordinator.presentedServiceRef
        let initialDisplayed = manager.coordinator.displayedServiceRef
        let initialRequested = manager.coordinator.requestedServiceRef
        let initialPhase = manager.coordinator.phase

        // 3. Mutate ONLY coordinator.playing (session -> nil)
        manager.coordinator._simulatePlayingSessionForTesting(nil)

        // Verify other properties remained strictly unchanged
        #expect(manager.coordinator.presentedServiceRef == initialPresented)
        #expect(manager.coordinator.displayedServiceRef == initialDisplayed)
        #expect(manager.coordinator.requestedServiceRef == initialRequested)
        #expect(manager.coordinator.phase == initialPhase)

        // Assert isPlaying became false strictly driven by coordinator.$playing
        #expect(store.isPlaying == false, "playing session -> nil must project isPlaying = false")

        // 4. Mutate ONLY coordinator.playing (nil -> session)
        let dummySession = NativeTSVideoPipeline()
        manager.coordinator._simulatePlayingSessionForTesting(dummySession)

        // Verify other properties remained strictly unchanged
        #expect(manager.coordinator.presentedServiceRef == initialPresented)
        #expect(manager.coordinator.displayedServiceRef == initialDisplayed)
        #expect(manager.coordinator.requestedServiceRef == initialRequested)
        #expect(manager.coordinator.phase == initialPhase)

        // Assert isPlaying became true strictly driven by coordinator.$playing
        #expect(store.isPlaying == true, "playing nil -> session must project isPlaying = true")
    }

    @Test("subscription cancellation immediately removes observer so subsequent transitions are ignored")
    func subscriptionCancellationRemovesObserverImmediately() async {
        let manager = PlaybackManager(streamURL: { _ in nil })
        var updateCount = 0

        var subscription: AnyCancellable? = manager.observeState { _, _ in
            updateCount += 1
        }
        #expect(updateCount == 1, "Initial snapshot delivered")

        // Cancel subscription
        subscription?.cancel()
        subscription = nil

        // Trigger transition
        await manager.play(channel: channelA, mode: .fullscreen)

        #expect(updateCount == 1, "Cancelled observer must not be called after cancellation")
    }

    @Test("errorMessage represents local UI command state and does not alter canonical playback")
    func errorMessageSemanticsAreLocalUIOnly() {
        let (_, store) = makeHarness()
        #expect(store.errorMessage == nil)

        store.setCommandError("Stream network unreachable")
        #expect(store.errorMessage == "Stream network unreachable")
        #expect(store.isPlaying == false)

        store.clearError()
        #expect(store.errorMessage == nil)
    }
}
