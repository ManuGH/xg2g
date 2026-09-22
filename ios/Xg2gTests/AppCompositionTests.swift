// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI
import Testing
@testable import Xg2g

private final class StubGuideRepo: GuideRepository, @unchecked Sendable {
    func channels() async throws -> [Channel] { [] }
    func nowNext() async throws -> [String: NowNext] { [:] }
    func schedule(for channels: [Channel], in window: GuideWindow) async throws -> [GuideChannelSchedule] { [] }
}

private final class StubDVRRepo: DVRRepository, @unchecked Sendable {
    func recordings() async throws -> [Recording] { [] }
    func timers() async throws -> [DVRTimer] { [] }
    func createTimer(serviceRef: String, name: String, description: String?, begin: Date, end: Date) async throws {}
    func deleteRecording(id: String) async throws {}
}

private final class StubPlaybackCtrl: PlaybackControlling {
    var currentChannel: Channel? = nil
    var isPlaying: Bool = false
    func play(channel: Channel) {}
    func stop() {}
    func togglePlayPause() {}
}

@MainActor
private final class StubDeviceSess: DeviceSession {
    var isConnected: Bool = true
    var activeServerAddress: ServerAddress? = try? ServerAddressParser.parseTrusted("http://192.0.2.1:8089/")
    func connect(address: ServerAddress) async throws {}
    func disconnect() async {}
}

struct AppCompositionTests {

    @Test("AppComposition initializes and provides all domain stores and platform services")
    @MainActor
    func testAppCompositionInitialization() {
        let guideStore = GuideStore(repository: StubGuideRepo())
        let dvrStore = DVRStore(repository: StubDVRRepo())
        let playbackStore = PlaybackStore(controller: StubPlaybackCtrl())
        let deviceStore = DeviceStore(session: StubDeviceSess())

        let composition = AppComposition(
            capabilities: .current,
            feedback: DefaultFeedbackService(),
            presentation: DefaultPlaybackPresentationService(),
            guideStore: guideStore,
            dvrStore: dvrStore,
            playbackStore: playbackStore,
            deviceStore: deviceStore
        )

        #expect(composition.guideStore === guideStore)
        #expect(composition.dvrStore === dvrStore)
        #expect(composition.playbackStore === playbackStore)
        #expect(composition.deviceStore === deviceStore)
        #expect(composition.capabilities == PlatformCapabilities.current)
    }

    @Test("AppComposition.makeBridged builds a functioning composition tree from AppModel")
    @MainActor
    func testAppCompositionBridged() {
        let appModel = AppModel()
        let composition = AppComposition.makeBridged(appModel: appModel)

        #expect(composition.guideStore.channels.isEmpty)
        #expect(composition.dvrStore.recordings.isEmpty)
        #expect(composition.playbackStore.isPlaying == false)
        #expect(composition.deviceStore.isConnected == false)
    }

    @Test("AppComposition maintains stable reference identities for all domain stores")
    @MainActor
    func testAppCompositionMaintainsStableStoreIdentities() {
        let appModel = AppModel()
        let composition = AppComposition.makeBridged(appModel: appModel)

        let initialGuide = composition.guideStore
        let initialDVR = composition.dvrStore
        let initialPlayback = composition.playbackStore
        let initialDevice = composition.deviceStore

        #expect(composition.guideStore === initialGuide)
        #expect(composition.dvrStore === initialDVR)
        #expect(composition.playbackStore === initialPlayback)
        #expect(composition.deviceStore === initialDevice)
    }
}
