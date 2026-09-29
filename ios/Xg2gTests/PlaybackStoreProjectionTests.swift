// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
import Combine
import AVFoundation
@testable import Xg2g

private final class SpyAVPlayer: AVPlayer, @unchecked Sendable {
    nonisolated(unsafe) var seekCallCount = 0
    nonisolated(unsafe) var lastSeekTargetTime: CMTime?

    override func seek(to time: CMTime, toleranceBefore: CMTime, toleranceAfter: CMTime) {
        seekCallCount += 1
        lastSeekTargetTime = time
    }
}

private final class SpyAudioSessionController: AudioSessionControlling, @unchecked Sendable {
    private let lock = NSLock()
    private(set) var activeLeaseToken: UUID?
    private(set) var configureForPlaybackCallCount = 0
    private(set) var deactivateCallCount = 0
    private(set) var activatedTokens: [UUID] = []
    private(set) var deactivatedTokens: [UUID] = []

    func configureForPlayback() {
        lock.lock()
        defer { lock.unlock() }
        configureForPlaybackCallCount += 1
    }

    func deactivate() {
        lock.lock()
        defer { lock.unlock() }
        deactivateCallCount += 1
    }

    @discardableResult
    func activate(for token: UUID) -> Bool {
        lock.lock()
        defer { lock.unlock() }
        activeLeaseToken = token
        activatedTokens.append(token)
        configureForPlaybackCallCount += 1
        return true
    }

    @discardableResult
    func deactivate(for token: UUID) -> Bool {
        lock.lock()
        defer { lock.unlock() }
        guard activeLeaseToken == token else {
            return false
        }
        activeLeaseToken = nil
        deactivatedTokens.append(token)
        deactivateCallCount += 1
        return true
    }
}

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

    private let channelB = Channel(
        id: "orf2",
        name: "ORF 2 HD",
        number: "2",
        serviceRef: "1:0:19:1330:3EF:1:C00000:0:0:0:",
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

    private let testRecordingB = Recording(
        id: "rec-test-2",
        title: "Formula 1 2026 Qualifying",
        description: "Qualifying Session",
        beginDate: Date(),
        durationSeconds: 3600,
        serviceRef: "1:0:19:1330:3EF:1:C00000:0:0:0:",
        filename: "f1_quali.ts",
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

    @Test("seek(to:) only dispatches when in recording state and clears player on transitions")
    func seekGuardsAgainstInactiveSessionsAndClearsPlayerOnTransitions() async throws {
        let manager = PlaybackManager(streamURL: { _ in nil })
        let player = SpyAVPlayer()

        // 1. In .idle state: seek must NOT dispatch even if someone attempts to attach with a dummy token
        manager.setRecordingPlayer(player, for: UUID())
        manager.seek(to: 42.0)
        #expect(player.seekCallCount == 0, "Must not seek when state is idle")

        // 2. In .recording state: seek DOES dispatch to recordingPlayer
        await manager.play(recording: testRecording, startPosition: 0)
        let token1 = try #require(manager.activeRecordingSessionToken)
        manager.setRecordingPlayer(player, for: token1)
        manager.seek(to: 55.0)
        #expect(player.seekCallCount == 1, "Must seek when state is active recording")
        #expect(player.lastSeekTargetTime?.seconds == 55.0)

        // 3. Transition to .live: recordingPlayer MUST be cleared to nil
        await manager.play(channel: channelA)
        #expect(manager.recordingPlayer == nil, "Transition to live must clear recordingPlayer")
        manager.seek(to: 60.0)
        #expect(player.seekCallCount == 1, "Must not seek after transitioning to live")

        // 4. In .live state: seek must NOT dispatch even if player attach attempted with stale token
        manager.setRecordingPlayer(player, for: token1)
        manager.seek(to: 70.0)
        #expect(player.seekCallCount == 1, "Must not seek when state is live")

        // 5. Transition to .recording then to .offline: recordingPlayer MUST be cleared to nil
        await manager.play(recording: testRecording, startPosition: 10)
        let token2 = try #require(manager.activeRecordingSessionToken)
        manager.setRecordingPlayer(player, for: token2)
        await manager.play(offline: testOffline)
        #expect(manager.recordingPlayer == nil, "Transition to offline must clear recordingPlayer")
        manager.seek(to: 80.0)
        #expect(player.seekCallCount == 1, "Must not seek when state is offline")

        // 6. Transition to .recording then stop(): recordingPlayer MUST be cleared to nil
        await manager.play(recording: testRecording, startPosition: 10)
        let token3 = try #require(manager.activeRecordingSessionToken)
        manager.setRecordingPlayer(player, for: token3)
        await manager.stop()
        #expect(manager.recordingPlayer == nil, "Stop must clear recordingPlayer")
        manager.seek(to: 90.0)
        #expect(player.seekCallCount == 1, "Must not seek after stop")
    }

    @Test("observeTargetState captures rapid sequential transitions deterministically")
    func rapidSequentialTargetTransitions() async {
        let manager = PlaybackManager(streamURL: { _ in nil })
        var observedTargets: [PlaybackTarget?] = []
        var observedPlaying: [Bool] = []

        let cancellable = manager.observeTargetState { target, isPlaying in
            observedTargets.append(target)
            observedPlaying.append(isPlaying)
        }

        #expect(observedTargets.count == 1)
        #expect(observedTargets[0] == nil)
        #expect(observedPlaying[0] == false)

        // Rapid sequential transitions
        await manager.play(channel: channelA)
        await manager.play(recording: testRecording, startPosition: 15.0)
        await manager.play(channel: channelB)
        await manager.play(offline: testOffline)
        await manager.stop()

        // Consecutive updates with same target can occur when isPlaying toggles
        let distinctTargets = observedTargets.reduce(into: [PlaybackTarget?]()) { acc, next in
            if acc.isEmpty || acc.last != next {
                acc.append(next)
            }
        }

        #expect(distinctTargets == [
            nil,
            .live(channelA),
            .recording(testRecording, startPosition: 15.0),
            .live(channelB),
            .offline(testOffline),
            nil
        ])

        #expect(observedPlaying.last == false)

        cancellable.cancel()
    }

    @Test("play(recording:) invalidates prior player on recording A -> recording B transition and zap(to:)")
    func recordingPlayerInvalidationAcrossRecordingSwitchAndZap() async throws {
        let manager = PlaybackManager(streamURL: { _ in nil })
        let playerA = SpyAVPlayer()
        let playerB = SpyAVPlayer()

        // 1. Start recording A and attach playerA
        await manager.play(recording: testRecording, startPosition: 0)
        let tokenA = try #require(manager.activeRecordingSessionToken)
        manager.setRecordingPlayer(playerA, for: tokenA)
        manager.seek(to: 30.0)
        #expect(playerA.seekCallCount == 1)

        // 2. Transition from recording A to recording B WITHOUT any cleanup hook installed
        await manager.play(recording: testRecordingB, startPosition: 10.0)

        // Assert playerA was invalidated and cleared
        #expect(manager.recordingPlayer == nil, "Switching to recording B must invalidate playerA")

        // Seek issued before playerB attaches must NOT reach playerA
        manager.seek(to: 45.0)
        #expect(playerA.seekCallCount == 1, "Seek on recording B before attach must not dispatch to playerA")

        // Attach playerB: seeks now dispatch to playerB
        let tokenB = try #require(manager.activeRecordingSessionToken)
        manager.setRecordingPlayer(playerB, for: tokenB)
        manager.seek(to: 45.0)
        #expect(playerB.seekCallCount == 1)
        #expect(playerB.lastSeekTargetTime?.seconds == 45.0)
        #expect(playerA.seekCallCount == 1, "PlayerA must remain untouched")

        // 3. Test zap(to:) when invoked from active recording
        await manager.zap(to: channelA)
        #expect(manager.recordingPlayer == nil, "zap(to:) from recording must clear recordingPlayer")
        manager.seek(to: 60.0)
        #expect(playerB.seekCallCount == 1, "Seek in live state after zap must not reach playerB")
    }

    @Test("PlayingRecordingItem includes sessionToken in state equality and presentation identity across A -> A restart")
    func playingRecordingItemSessionTokenEqualityAndIdentityOnRestart() async throws {
        let manager = PlaybackManager(streamURL: { _ in nil })
        var observedStates: [PlaybackTarget?] = []

        let cancellable = manager.observeTargetState { target, _ in
            observedStates.append(target)
        }

        // 1. First run of Recording A
        await manager.play(recording: testRecording, startPosition: 0)
        let item1 = try #require(manager.activeRecordingItem)
        let token1 = item1.sessionToken

        // 2. Restart Recording A with identical position and parameters
        await manager.play(recording: testRecording, startPosition: 0)
        let item2 = try #require(manager.activeRecordingItem)
        let token2 = item2.sessionToken

        // Assert session tokens differ
        #expect(token1 != token2, "A -> A restart must allocate a distinct session token")

        // Assert state equality includes sessionToken: item1 != item2
        #expect(item1 != item2, "PlayingRecordingItem equality must include sessionToken")

        // Assert SwiftUI presentation identity (Identifiable id) differs
        #expect(item1.id != item2.id, "SwiftUI presentation identity must change on restart")

        // Assert state.didSet was not skipped and observer observed both transitions
        #expect(observedStates.count == 3, "Initial snapshot + run 1 + run 2 must produce 3 observations")
        #expect(observedStates[1] == .recording(testRecording, startPosition: 0))
        #expect(observedStates[2] == .recording(testRecording, startPosition: 0))

        cancellable.cancel()
    }

    @Test("PlaybackManager rejects cleanup-hook registration from non-active session and preserves active hook")
    func cleanupHookRegistrationRejectsNonActiveSessionAndPreservesActiveHook() async throws {
        let manager = PlaybackManager(streamURL: { _ in nil })

        // 1. Session A starts
        await manager.play(recording: testRecording, startPosition: 0)
        let tokenA = try #require(manager.activeRecordingSessionToken)

        // 2. Session B starts
        await manager.play(recording: testRecordingB, startPosition: 0)
        let tokenB = try #require(manager.activeRecordingSessionToken)
        #expect(tokenA != tokenB)

        // 3. Screen B registers its cleanup hook
        var cleanupCountB = 0
        let regB = manager.registerRecordingCleanup(for: tokenB) {
            cleanupCountB += 1
        }
        #expect(regB == true, "Active session token must successfully register cleanup hook")

        // 4. Late .onAppear from Screen A attempts to register cleanup hook with obsolete tokenA
        var cleanupCountA = 0
        let regA = manager.registerRecordingCleanup(for: tokenA) {
            cleanupCountA += 1
        }
        #expect(regA == false, "Obsolete session token must be rejected from registering cleanup hook")

        // 5. Teardown via stop() proves Screen B's hook is preserved and executed, not displaced by A
        await manager.stop()
        #expect(cleanupCountB == 1, "Screen B cleanup hook must execute on teardown")
        #expect(cleanupCountA == 0, "Rejected screen A cleanup hook must never execute")
    }

    @Test("PlaybackManager token rejection on late attach and late ready-to-play callback across sessions and A -> A restart")
    func lateResolutionAndReadyToPlayCallbackRejection() async throws {
        let manager = PlaybackManager(streamURL: { _ in nil })
        let playerA = SpyAVPlayer()
        let playerB = SpyAVPlayer()

        // 1. Session A starts: capture tokenA
        await manager.play(recording: testRecording, startPosition: 0)
        let tokenA = try #require(manager.activeRecordingSessionToken)

        // 2. User switches to Recording B: capture tokenB
        await manager.play(recording: testRecordingB, startPosition: 0)
        let tokenB = try #require(manager.activeRecordingSessionToken)
        #expect(tokenB != tokenA)

        // Screen B attaches playerB
        let attachedB = manager.setRecordingPlayer(playerB, for: tokenB)
        #expect(attachedB == true)
        #expect(manager.recordingPlayer === playerB)

        // 3. Late resolution of A attempts attach with tokenA
        let attachedA = manager.setRecordingPlayer(playerA, for: tokenA)
        #expect(attachedA == false, "setRecordingPlayer must report rejection for superseded tokenA")
        #expect(manager.recordingPlayer === playerB, "playerB must remain active")

        // 4. Late ready-to-play callback decision evaluation for tokenA:
        // (Note: Actual KVO observer dispatch in UI runtime is not directly executed here;
        // this exercises the extracted RecordingPlaybackDecision policy and verifies that a superseded
        // session token produces .pauseAndDiscard, preventing seeking or playing).
        let decisionA = RecordingPlaybackDecision.decideStatusAction(
            status: .readyToPlay,
            error: nil,
            sessionToken: tokenA,
            activeToken: manager.activeRecordingSessionToken,
            startPosition: 42.0
        )
        #expect(decisionA == .pauseAndDiscard, "Queued callback for superseded session must be discarded")
        if case .pauseAndDiscard = decisionA {
            playerA.pause()
        }
        #expect(playerA.seekCallCount == 0, "No seek should occur for rejected session")

        // 5. Late unmount of A does not clear B
        manager.unregisterRecordingCleanup(for: tokenA)
        manager.clearRecordingPlayer(for: tokenA, ownedBy: playerA)
        #expect(manager.recordingPlayer === playerB, "Late unmount from session A must not clear session B player")

        // === A -> A Restart ===
        // 6. User restarts Recording B (B -> B restart)
        await manager.play(recording: testRecordingB, startPosition: 0)
        let tokenB2 = try #require(manager.activeRecordingSessionToken)
        #expect(tokenB2 != tokenB, "Restart must produce new session token")

        let playerB2 = SpyAVPlayer()
        let attachedB2 = manager.setRecordingPlayer(playerB2, for: tokenB2)
        #expect(attachedB2 == true)
        #expect(manager.recordingPlayer === playerB2)

        // 7. Late attach from run 1 of B is rejected
        let latePlayerB1 = SpyAVPlayer()
        let attachedLateB1 = manager.setRecordingPlayer(latePlayerB1, for: tokenB)
        #expect(attachedLateB1 == false, "Late attach from run 1 must be rejected")
        #expect(manager.recordingPlayer === playerB2)

        // 8. Late unmount from run 1 of B does not clear run 2 player
        manager.unregisterRecordingCleanup(for: tokenB)
        manager.clearRecordingPlayer(for: tokenB, ownedBy: latePlayerB1)
        #expect(manager.recordingPlayer === playerB2, "Late cleanup from run 1 must not clear run 2 player")

        await manager.stop()
        #expect(manager.recordingPlayer == nil)
    }

    @Test("AudioSession lease acquisition and legitimate deactivation via PlaybackManager")
    func audioSessionLeaseAcquisitionAndDeactivation() async throws {
        let spy = SpyAudioSessionController()
        let manager = PlaybackManager(audioSession: spy, streamURL: { _ in nil })

        // 1. Start recording session
        await manager.play(recording: testRecording, startPosition: 0)
        let token = try #require(manager.activeRecordingSessionToken)

        // 2. Activate audio session under lease
        let activated = manager.activateRecordingAudioSession(for: token)
        #expect(activated == true)
        #expect(spy.configureForPlaybackCallCount == 1)
        #expect(spy.activeLeaseToken == token)

        // 3. Legitimate deactivation
        let deactivated = manager.deactivateRecordingAudioSession(for: token)
        #expect(deactivated == true)
        #expect(spy.deactivateCallCount == 1)
        #expect(spy.activeLeaseToken == nil)
    }

    @Test("Switching recording A -> B without mounting B retires prior audio lease exactly once upon switch and stop")
    func switchRecordingWithoutMountingBRetiresPriorLeaseExactlyOnce() async throws {
        let spy = SpyAudioSessionController()
        let manager = PlaybackManager(audioSession: spy, streamURL: { _ in nil })

        // 1. Session A starts and activates audio lease
        await manager.play(recording: testRecording, startPosition: 0)
        let tokenA = try #require(manager.activeRecordingSessionToken)
        let activatedA = manager.activateRecordingAudioSession(for: tokenA)
        #expect(activatedA == true)
        #expect(spy.activeLeaseToken == tokenA)
        #expect(spy.configureForPlaybackCallCount == 1)
        #expect(spy.deactivateCallCount == 0)

        // 2. User switches to Recording B WITHOUT mounting B's screen
        await manager.play(recording: testRecordingB, startPosition: 0)
        let tokenB = try #require(manager.activeRecordingSessionToken)
        #expect(tokenB != tokenA)

        // A's audio lease must have been retired during the transition
        #expect(spy.deactivateCallCount == 1, "A's audio lease must be retired when transitioning to B")
        #expect(spy.activeLeaseToken == nil, "Audio lease must be cleared when session A ends")

        // 3. User stops playback from miniplayer or stop button
        await manager.stop()

        // A's lease must have been released exactly once (no redundant deactivations or leaked lease)
        #expect(spy.deactivateCallCount == 1, "A's lease must have been released exactly once")
        #expect(spy.activeLeaseToken == nil)
    }

    @Test("Late Screen A mount and disappear with active prior lease preserves Screen B's active AudioSession lease")
    func lateScreenAMountAndDisappearWithActivePriorLeasePreservesBLease() async throws {
        let spy = SpyAudioSessionController()
        let manager = PlaybackManager(audioSession: spy, streamURL: { _ in nil })
        let playerA = SpyAVPlayer()
        let playerB = SpyAVPlayer()

        // 1. Session A starts and activates its audio lease
        await manager.play(recording: testRecording, startPosition: 0)
        let tokenA = try #require(manager.activeRecordingSessionToken)
        let activatedA = manager.activateRecordingAudioSession(for: tokenA)
        #expect(activatedA == true)
        #expect(spy.activeLeaseToken == tokenA)
        #expect(spy.configureForPlaybackCallCount == 1)
        #expect(spy.deactivateCallCount == 0)

        // Screen A registers cleanup and attaches playerA
        let registeredA1 = manager.registerRecordingCleanup(for: tokenA) {
            manager.clearRecordingPlayer(for: tokenA, ownedBy: playerA)
            manager.deactivateRecordingAudioSession(for: tokenA)
        }
        #expect(registeredA1 == true)
        manager.setRecordingPlayer(playerA, for: tokenA)
        #expect(manager.recordingPlayer === playerA)

        // 2. User switches to Recording B
        await manager.play(recording: testRecordingB, startPosition: 0)
        let tokenB = try #require(manager.activeRecordingSessionToken)
        #expect(tokenB != tokenA)

        // Session A's audio lease must have been retired during the transition
        #expect(spy.deactivateCallCount == 1, "Session A lease must be retired during transition to B")
        #expect(spy.activeLeaseToken == nil)
        #expect(manager.recordingPlayer == nil, "Player A must be invalidated")

        // 3. Screen B mounts: registers cleanup and acquires new audio session lease
        let registeredB = manager.registerRecordingCleanup(for: tokenB) {
            manager.clearRecordingPlayer(for: tokenB, ownedBy: playerB)
            manager.deactivateRecordingAudioSession(for: tokenB)
        }
        #expect(registeredB == true)
        let activatedB = manager.activateRecordingAudioSession(for: tokenB)
        #expect(activatedB == true)
        #expect(spy.activeLeaseToken == tokenB, "Audio session lease must now belong to Session B")
        #expect(spy.configureForPlaybackCallCount == 2)
        #expect(spy.deactivateCallCount == 1)
        manager.setRecordingPlayer(playerB, for: tokenB)
        #expect(manager.recordingPlayer === playerB)

        // 4. Late Mount of Screen A (.onAppear) while B is actively playing:
        // registerRecordingCleanup rejects tokenA -> screen guards and aborts setupPlayer / audio activation
        let lateRegisteredA = manager.registerRecordingCleanup(for: tokenA) {}
        #expect(lateRegisteredA == false, "Late mount of Screen A must be rejected from registering cleanup")
        let lateActivatedA = manager.activateRecordingAudioSession(for: tokenA)
        #expect(lateActivatedA == false, "Late mount of Screen A must be rejected from activating audio session")
        #expect(spy.activeLeaseToken == tokenB, "Audio session lease must remain with Session B")
        #expect(spy.configureForPlaybackCallCount == 2, "Audio session must not be reconfigured by Screen A")

        // 5. Late Disappear of Screen A (.onDisappear -> cleanup()) while B is actively playing:
        manager.unregisterRecordingCleanup(for: tokenA)
        manager.clearRecordingPlayer(for: tokenA, ownedBy: playerA)
        let lateDeactivatedA = manager.deactivateRecordingAudioSession(for: tokenA)
        #expect(lateDeactivatedA == false, "Late disappear of Screen A must be rejected from deactivating audio session")
        #expect(spy.deactivateCallCount == 1, "Audio session must NOT be deactivated by late Screen A cleanup")
        #expect(spy.activeLeaseToken == tokenB, "Audio session lease must still belong to Session B")
        #expect(manager.recordingPlayer === playerB, "Player B must remain active and unaffected")

        // 6. Legitimate Screen B teardown deactivates audio session cleanly
        await manager.stop()
        #expect(spy.deactivateCallCount == 2, "Audio session must be deactivated when Session B stops")
        #expect(spy.activeLeaseToken == nil)
    }

    @Test("Recording A -> A restart retires prior lease and allows new session to acquire and maintain lease")
    func recordingRestartRetiresPriorLeaseAndMaintainsNewLease() async throws {
        let spy = SpyAudioSessionController()
        let manager = PlaybackManager(audioSession: spy, streamURL: { _ in nil })

        // 1. Run 1 of Recording A starts and activates lease
        await manager.play(recording: testRecording, startPosition: 0)
        let tokenA1 = try #require(manager.activeRecordingSessionToken)
        let activatedA1 = manager.activateRecordingAudioSession(for: tokenA1)
        #expect(activatedA1 == true)
        #expect(spy.activeLeaseToken == tokenA1)
        #expect(spy.configureForPlaybackCallCount == 1)
        #expect(spy.deactivateCallCount == 0)

        // 2. Restart Recording A (A -> A restart)
        await manager.play(recording: testRecording, startPosition: 0)
        let tokenA2 = try #require(manager.activeRecordingSessionToken)
        #expect(tokenA2 != tokenA1)

        // Run 1 lease must be retired
        #expect(spy.deactivateCallCount == 1, "Run 1 lease retired on restart")
        #expect(spy.activeLeaseToken == nil)

        // 3. Screen for Run 2 mounts and acquires lease
        let activatedA2 = manager.activateRecordingAudioSession(for: tokenA2)
        #expect(activatedA2 == true)
        #expect(spy.activeLeaseToken == tokenA2)
        #expect(spy.configureForPlaybackCallCount == 2)
        #expect(spy.deactivateCallCount == 1)

        // 4. Late cleanup from Run 1 does not affect Run 2 lease
        let lateDeactivatedA1 = manager.deactivateRecordingAudioSession(for: tokenA1)
        #expect(lateDeactivatedA1 == false)
        #expect(spy.deactivateCallCount == 1)
        #expect(spy.activeLeaseToken == tokenA2)

        // 5. Teardown via stop() deactivates Run 2 lease cleanly
        await manager.stop()
        #expect(spy.deactivateCallCount == 2)
        #expect(spy.activeLeaseToken == nil)
    }

    @Test("RecordingPlaybackDecision evaluates ready-to-play, seek completion, and failure policies with controlled session tokens (extracted decision policy; actual KVO observer firing in UI runtime is not directly executed in headless unit tests)")
    func recordingPlaybackDecisionPolicy() {
        let tokenA = UUID()
        let tokenB = UUID()

        // 1. Matching token + readyToPlay + resume position > 5s -> seek
        let actionSeek = RecordingPlaybackDecision.decideStatusAction(
            status: .readyToPlay,
            error: nil,
            sessionToken: tokenA,
            activeToken: tokenA,
            startPosition: 42.0
        )
        #expect(actionSeek == .seek(42.0))

        // 2. Matching token + readyToPlay + start position <= 5s -> play
        let actionPlay = RecordingPlaybackDecision.decideStatusAction(
            status: .readyToPlay,
            error: nil,
            sessionToken: tokenA,
            activeToken: tokenA,
            startPosition: 3.0
        )
        #expect(actionPlay == .play)

        // 3. Matching token + readyToPlay + nil start position -> play
        let actionPlayNil = RecordingPlaybackDecision.decideStatusAction(
            status: .readyToPlay,
            error: nil,
            sessionToken: tokenA,
            activeToken: tokenA,
            startPosition: nil
        )
        #expect(actionPlayNil == .play)

        // 4. Superseded token (tokenA when active is tokenB) + readyToPlay -> pauseAndDiscard
        let actionSuperseded = RecordingPlaybackDecision.decideStatusAction(
            status: .readyToPlay,
            error: nil,
            sessionToken: tokenA,
            activeToken: tokenB,
            startPosition: 42.0
        )
        #expect(actionSuperseded == .pauseAndDiscard, "Queued readyToPlay for superseded session must be discarded")

        // 5. Matching token + failed -> handleFailure
        struct DummyError: LocalizedError {
            var errorDescription: String? { "Disk read error" }
        }
        let actionFailed = RecordingPlaybackDecision.decideStatusAction(
            status: .failed,
            error: DummyError(),
            sessionToken: tokenA,
            activeToken: tokenA,
            startPosition: nil
        )
        #expect(actionFailed == .handleFailure("Disk read error"))

        // 6. Superseded token + failed -> pauseAndDiscard
        let actionFailedSuperseded = RecordingPlaybackDecision.decideStatusAction(
            status: .failed,
            error: DummyError(),
            sessionToken: tokenA,
            activeToken: tokenB,
            startPosition: nil
        )
        #expect(actionFailedSuperseded == .pauseAndDiscard)

        // 7. Seek completion: matching token + finished -> play
        let actionSeekDone = RecordingPlaybackDecision.decideSeekCompletionAction(
            sessionToken: tokenA,
            activeToken: tokenA,
            finished: true
        )
        #expect(actionSeekDone == .play)

        // 8. Seek completion: superseded token + finished -> pauseAndDiscard
        let actionSeekDoneSuperseded = RecordingPlaybackDecision.decideSeekCompletionAction(
            sessionToken: tokenA,
            activeToken: tokenB,
            finished: true
        )
        #expect(actionSeekDoneSuperseded == .pauseAndDiscard)

        // 9. Seek completion: superseded token + unfinished -> pauseAndDiscard
        let actionSeekUnfinishedSuperseded = RecordingPlaybackDecision.decideSeekCompletionAction(
            sessionToken: tokenA,
            activeToken: tokenB,
            finished: false
        )
        #expect(actionSeekUnfinishedSuperseded == .pauseAndDiscard)
    }

    @Test("LiveTeardownDecision evaluates whether an unmounting live screen owns playback (extracted decision policy; SwiftUI view hierarchy lifecycle unmount is modeled)")
    func liveTeardownDecisionPolicy() {
        let channelA = Channel(id: "1", name: "Channel A", number: "1", serviceRef: "1:0:19:A", logoURL: nil)
        let channelB = Channel(id: "2", name: "Channel B", number: "2", serviceRef: "1:0:19:B", logoURL: nil)
        let offlineRec = OfflineRecording(
            id: "off-1",
            recordingId: "rec-1",
            title: "Offline",
            channelName: "Channel A",
            durationSeconds: 3600,
            fileSize: 1000,
            downloadDate: Date(),
            localRelativePath: "off.mp4",
            quality: .original
        )
        let recItem = PlayingRecordingItem(
            sessionToken: UUID(),
            recording: Recording(
                id: "rec-1",
                title: "Rec",
                description: "",
                beginDate: Date(),
                durationSeconds: 3600,
                serviceRef: "1:0:19:A",
                filename: "rec.ts",
                status: "completed",
                serverResumePos: 0
            ),
            initialPosition: 0
        )

        // 1. Same channel matches
        #expect(LiveTeardownDecision.shouldStopPlayback(activeState: .live(channelA, mode: .fullscreen), screenChannel: channelA) == true)
        #expect(LiveTeardownDecision.shouldStopPlayback(activeState: .live(channelA, mode: .hidden), screenChannel: channelA) == true)

        // 2. Different channel rejects
        #expect(LiveTeardownDecision.shouldStopPlayback(activeState: .live(channelB, mode: .fullscreen), screenChannel: channelA) == false)

        // 3. Offline state rejects (Live screen unmount must not kill offline playback)
        #expect(LiveTeardownDecision.shouldStopPlayback(activeState: .offline(offlineRec), screenChannel: channelA) == false)

        // 4. Recording state rejects (Live screen unmount must not kill recording playback)
        #expect(LiveTeardownDecision.shouldStopPlayback(activeState: .recording(recItem, mode: .fullscreen), screenChannel: channelA) == false)

        // 5. Idle state rejects
        #expect(LiveTeardownDecision.shouldStopPlayback(activeState: .idle, screenChannel: channelA) == false)
    }

    @Test("Offline A screen unmount after Recording B acquires audio lease preserves Recording B audio lease (extracted lease controller; SwiftUI view unmount is modeled)")
    func offlinePlayerUnmountPreservesSuccessorRecordingAudioLease() async {
        let spy = SpyAudioSessionController()
        let manager = PlaybackManager(
            audioSession: spy,
            streamURL: { URL(string: "http://example.com/\($0)") }
        )

        // 1. Offline A screen starts and acquires lease
        let tokenOfflineA = UUID()
        let acquiredA = spy.activate(for: tokenOfflineA)
        #expect(acquiredA == true)
        #expect(spy.activeLeaseToken == tokenOfflineA)
        #expect(spy.configureForPlaybackCallCount == 1)

        // 2. User transitions to Recording B
        let recordingB = Recording(
            id: "rec-b",
            title: "Recording B",
            description: "",
            beginDate: Date(),
            durationSeconds: 3600,
            serviceRef: "1:0:19:B",
            filename: "recB.ts",
            status: "completed",
            serverResumePos: 0
        )
        await manager.play(recording: recordingB, startPosition: 0)
        guard let tokenRecordingB = manager.activeRecordingSessionToken else {
            Issue.record("Recording B should have an active session token")
            return
        }
        #expect(tokenRecordingB != tokenOfflineA)

        // 3. Recording B mounts and activates its lease
        let acquiredB = manager.activateRecordingAudioSession(for: tokenRecordingB)
        #expect(acquiredB == true)
        #expect(spy.activeLeaseToken == tokenRecordingB)
        #expect(spy.configureForPlaybackCallCount == 2)
        #expect(spy.deactivateCallCount == 0)

        // 4. Offline A screen unmounts late (.onDisappear calls audioSession.deactivate(for: tokenOfflineA))
        // With ownership check, tokenOfflineA is rejected because active token is tokenRecordingB
        let releasedA = spy.deactivate(for: tokenOfflineA)
        #expect(releasedA == false, "Offline A lease release must be rejected when Recording B owns the lease")
        #expect(spy.activeLeaseToken == tokenRecordingB, "Recording B lease must remain intact")
        #expect(spy.deactivateCallCount == 0, "Underlying audio session must NOT be deactivated by stale offline unmount")

        // 5. Clean teardown of Recording B
        await manager.stop()
        #expect(spy.activeLeaseToken == nil)
        #expect(spy.deactivateCallCount == 1)
    }
}
