// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
import Combine
@testable import Xg2g

@MainActor
private final class RejectingPlaybackController: PlaybackControlling {
    var currentTarget: PlaybackTarget? = nil
    var currentChannel: Channel? = nil
    var currentRecording: Recording? = nil
    var isPlaying: Bool = false

    func play(channel: Channel) {
        // Intentionally no-op to simulate a rejected, permission-denied, or delayed command
    }

    var lastSeekPosition: Double? = nil

    func play(recording: Recording, startPosition: Double?) {
        // Intentionally no-op
    }

    func seek(to position: Double) {
        lastSeekPosition = position
    }
    func stop() {}
    func togglePlayPause() {}

    func observeState(_ handler: @escaping @MainActor (Channel?, Bool) -> Void) -> AnyCancellable {
        handler(currentChannel, isPlaying)
        return AnyCancellable {}
    }

    func observeTargetState(_ handler: @escaping @MainActor (PlaybackTarget?, Bool) -> Void) -> AnyCancellable {
        handler(currentTarget, isPlaying)
        return AnyCancellable {}
    }
}

@MainActor
private final class DynamicMockPlaybackController: PlaybackControlling {
    var currentTarget: PlaybackTarget? = nil
    var currentChannel: Channel? = nil
    var currentRecording: Recording? = nil
    var isPlaying: Bool = false
    var lastSeekPosition: Double? = nil

    private var targetObservers: [UUID: @MainActor (PlaybackTarget?, Bool) -> Void] = [:]

    func play(channel: Channel) {
        currentTarget = .live(channel)
        isPlaying = true
        notify()
    }

    func play(recording: Recording, startPosition: Double?) {
        currentTarget = .recording(recording, startPosition: startPosition)
        isPlaying = true
        notify()
    }

    func seek(to position: Double) {
        lastSeekPosition = position
    }

    func stop() {
        currentTarget = nil
        isPlaying = false
        notify()
    }

    func togglePlayPause() {
        isPlaying.toggle()
        notify()
    }

    func observeState(_ handler: @escaping @MainActor (Channel?, Bool) -> Void) -> AnyCancellable {
        observeTargetState { target, isPlaying in
            let channel: Channel?
            if case .live(let ch) = target { channel = ch } else { channel = nil }
            handler(channel, isPlaying)
        }
    }

    func observeTargetState(_ handler: @escaping @MainActor (PlaybackTarget?, Bool) -> Void) -> AnyCancellable {
        let id = UUID()
        targetObservers[id] = handler
        handler(currentTarget, isPlaying)
        return AnyCancellable { [weak self] in
            self?.targetObservers.removeValue(forKey: id)
        }
    }

    func pushUpdate(target: PlaybackTarget?, isPlaying: Bool) {
        self.currentTarget = target
        self.isPlaying = isPlaying
        notify()
    }

    private func notify() {
        for observer in targetObservers.values {
            observer(currentTarget, isPlaying)
        }
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
        #expect(store.currentTarget == nil)
        #expect(store.currentChannel == nil)
        #expect(store.currentRecording == nil)
        #expect(store.isPlaying == false)
        #expect(store.errorMessage == nil)
    }

    @Test("idle → live projection updates PlaybackStore accurately")
    func idleToLiveProjection() async {
        let (manager, store) = makeHarness()

        await manager.play(channel: channelA, mode: .fullscreen)

        #expect(store.currentTarget == .live(channelA))
        #expect(store.currentChannel == channelA)
        #expect(store.currentRecording == nil)
        #expect(manager.currentChannel == channelA)
    }

    @Test("live → recording projection updates PlaybackStore accurately")
    func liveToRecordingProjection() async {
        let (manager, store) = makeHarness()

        await manager.play(channel: channelA, mode: .fullscreen)
        #expect(store.currentChannel == channelA)

        await manager.play(recording: testRecording, startPosition: 0)
        #expect(store.currentTarget == .recording(testRecording, startPosition: 0))
        #expect(store.currentChannel == nil, "Live channel must clear when playing recording")
        #expect(store.currentRecording == testRecording, "currentRecording must reflect active recording")
        #expect(store.isPlaying == true, "isPlaying must reflect active recording playback")
    }

    @Test("recording → offline projection updates PlaybackStore accurately")
    func recordingToOfflineProjection() async {
        let (manager, store) = makeHarness()

        await manager.play(recording: testRecording, startPosition: 0)
        #expect(store.currentChannel == nil)
        #expect(store.currentRecording == testRecording)
        #expect(store.isPlaying == true)

        await manager.play(offline: testOffline)
        #expect(store.currentTarget == .offline(testOffline))
        #expect(store.currentChannel == nil)
        #expect(store.currentRecording == nil)
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

    @Test("delayed or rejected recording command does NOT mutate PlaybackStore optimistically")
    func delayedOrRejectedRecordingCommandDoesNotMutateOptimistically() {
        let rejectingController = RejectingPlaybackController()
        let store = PlaybackStore(controller: rejectingController)

        #expect(store.currentTarget == nil)
        #expect(store.currentRecording == nil)
        #expect(store.isPlaying == false)

        store.play(recording: testRecording, startPosition: 0)

        #expect(store.currentTarget == nil, "Store must not mutate optimistically on uncommitted VOD commands")
        #expect(store.currentRecording == nil)
        #expect(store.isPlaying == false)
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

    @Test("PlaybackStore forwards seek(to:) command to attached controller")
    func seekForwardingToController() {
        let controller = RejectingPlaybackController()
        let store = PlaybackStore(controller: controller)
        #expect(controller.lastSeekPosition == nil)

        store.seek(to: 42.5)
        #expect(controller.lastSeekPosition == 42.5)
    }

    @Test("LegacyBridgePlaybackController seek(to:) is a safe no-op when inactive or deallocated")
    func legacyBridgeSeekSafeNoOpWhenInactiveOrDeallocated() {
        var manager: PlaybackManager? = PlaybackManager(streamURL: { _ in nil })
        let bridge = LegacyBridgePlaybackController(playbackManager: manager!)
        // With no player active, seek is a safe no-op
        bridge.seek(to: 120.0)

        // Non-finite and negative positions are safely rejected
        bridge.seek(to: -5.0)
        bridge.seek(to: .nan)
        bridge.seek(to: .infinity)

        // When playbackManager is deallocated, seek is a safe no-op
        manager = nil
        bridge.seek(to: 120.0)
    }

    @Test("PlaybackManager observeTargetState cancellation immediately removes observer so subsequent transitions are ignored")
    func targetSubscriptionCancellationRemovesObserverImmediately() async {
        let manager = PlaybackManager(streamURL: { _ in nil })
        var targetUpdates: [PlaybackTarget?] = []

        var subscription: AnyCancellable? = manager.observeTargetState { target, _ in
            targetUpdates.append(target)
        }
        #expect(targetUpdates.count == 1, "Initial snapshot delivered")
        #expect(targetUpdates == [nil])

        // Cancel subscription
        subscription?.cancel()
        subscription = nil

        // Trigger transition
        await manager.play(channel: channelA, mode: .fullscreen)

        #expect(targetUpdates.count == 1, "Cancelled target observer must not be called after cancellation")
    }

    @Test("PlaybackStore reattachment cancels previous controller subscription and attaches to new controller")
    func reattachmentCancelsPreviousSubscription() {
        let controllerA = DynamicMockPlaybackController()
        let controllerB = DynamicMockPlaybackController()

        let store = PlaybackStore(controller: controllerA)
        #expect(store.currentTarget == nil)

        // Reattach to controller B
        controllerB.pushUpdate(target: .live(channelA), isPlaying: true)
        store.attach(controller: controllerB)
        #expect(store.currentTarget == .live(channelA))
        #expect(store.isPlaying == true)

        // Updates from old controllerA must be ignored
        controllerA.pushUpdate(target: .recording(testRecording, startPosition: 10), isPlaying: false)
        #expect(store.currentTarget == .live(channelA), "Store must ignore updates from detached controller A")
        #expect(store.isPlaying == true)

        // Updates from new controllerB are projected
        controllerB.pushUpdate(target: nil, isPlaying: false)
        #expect(store.currentTarget == nil)
        #expect(store.isPlaying == false)
    }
}
