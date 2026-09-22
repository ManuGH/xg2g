// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

// MARK: - Mock Repositories & Controllers

private final class MockGuideRepository: GuideRepository, @unchecked Sendable {
    var stubbedChannels: [Channel] = []
    var stubbedNowNext: [String: NowNext] = [:]
    var stubbedSchedule: [GuideChannelSchedule] = []

    func channels() async throws -> [Channel] {
        stubbedChannels
    }

    func nowNext() async throws -> [String: NowNext] {
        stubbedNowNext
    }

    func schedule(for channels: [Channel], in window: GuideWindow) async throws -> [GuideChannelSchedule] {
        stubbedSchedule
    }
}

private final class MockDVRRepository: DVRRepository, @unchecked Sendable {
    var stubbedRecordings: [Recording] = []
    var stubbedTimers: [DVRTimer] = []

    func recordings() async throws -> [Recording] {
        stubbedRecordings
    }

    func timers() async throws -> [DVRTimer] {
        stubbedTimers
    }

    func createTimer(serviceRef: String, name: String, description: String?, begin: Date, end: Date) async throws {
        let newTimer = DVRTimer(
            id: UUID().uuidString,
            name: name,
            description: description,
            serviceRef: serviceRef,
            serviceName: nil,
            beginDate: begin,
            endDate: end,
            state: "waiting"
        )
        stubbedTimers.append(newTimer)
    }

    func deleteRecording(id: String) async throws {
        stubbedRecordings.removeAll { $0.id == id }
    }
}

private final class MockPlaybackController: PlaybackControlling {
    var currentChannel: Channel?
    var isPlaying: Bool = false

    func play(channel: Channel) {
        currentChannel = channel
        isPlaying = true
    }

    func stop() {
        currentChannel = nil
        isPlaying = false
    }

    func togglePlayPause() {
        isPlaying.toggle()
    }
}

private final class LifetimePlaybackController: PlaybackControlling {
    var currentChannel: Channel?
    var isPlaying: Bool = false
    var onPlay: ((Channel) -> Void)?

    func play(channel: Channel) {
        currentChannel = channel
        isPlaying = true
        onPlay?(channel)
    }

    func stop() {
        currentChannel = nil
        isPlaying = false
    }

    func togglePlayPause() {
        isPlaying.toggle()
    }
}

@MainActor
private final class MockDeviceSession: DeviceSession {
    var isConnected: Bool = false
    var activeServerAddress: ServerAddress?

    func connect(address: ServerAddress) async throws {
        isConnected = true
        activeServerAddress = address
    }

    func disconnect() async {
        isConnected = false
        activeServerAddress = nil
    }
}

// MARK: - Store Isolation Tests

struct StoreIsolationTests {

    @Test("GuideStore loads channels and updates UI state on MainActor")
    @MainActor
    func testGuideStore() async {
        let repo = MockGuideRepository()
        let testChannel = Channel(id: "1", name: "Das Erste HD", number: "1", serviceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:", logoURL: nil)
        repo.stubbedChannels = [testChannel]

        let store = GuideStore(repository: repo)
        #expect(store.channels.isEmpty)

        await store.loadChannels()
        #expect(store.channels.count == 1)
        #expect(store.channels.first?.name == "Das Erste HD")
        #expect(store.errorMessage == nil)

        store.selectChannel(testChannel)
        #expect(store.selectedChannel == testChannel)
    }

    @Test("DVRStore loads recordings, creates timers, and deletes recordings")
    @MainActor
    func testDVRStore() async {
        let repo = MockDVRRepository()
        let testRec = Recording(
            id: "rec-1",
            title: "Tatort",
            description: "Krimi",
            beginDate: Date(),
            durationSeconds: 5400,
            serviceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:",
            filename: "tatort.ts",
            status: "completed",
            serverResumePos: nil
        )
        repo.stubbedRecordings = [testRec]

        let store = DVRStore(repository: repo)
        await store.loadRecordings()
        #expect(store.recordings.count == 1)
        #expect(store.recordings.first?.title == "Tatort")

        await store.createTimer(
            serviceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:",
            name: "Tagesschau",
            description: "Nachrichten",
            begin: Date(),
            end: Date().addingTimeInterval(900)
        )
        #expect(store.timers.count == 1)
        #expect(store.timers.first?.name == "Tagesschau")

        await store.deleteRecording(id: "rec-1")
        #expect(store.recordings.isEmpty)
    }

    @Test("PlaybackStore coordinates tuning and transport with controller")
    @MainActor
    func testPlaybackStore() {
        let controller = MockPlaybackController()
        let store = PlaybackStore(controller: controller)
        let channel = Channel(id: "1", name: "ZDF HD", number: "2", serviceRef: "1:0:19:2B66:3F3:1:C00000:0:0:0:", logoURL: nil)

        #expect(store.isPlaying == false)
        #expect(store.currentChannel == nil)

        store.play(channel: channel)
        #expect(store.isPlaying == true)
        #expect(store.currentChannel?.name == "ZDF HD")
        #expect(controller.isPlaying == true)
        #expect(controller.currentChannel?.name == "ZDF HD")

        store.togglePlayPause()
        #expect(store.isPlaying == false)

        store.stop()
        #expect(store.isPlaying == false)
        #expect(store.currentChannel == nil)
        #expect(controller.isPlaying == false)
    }

    @Test("PlaybackStore strongly owns its controller across creation scope exit")
    @MainActor
    func testPlaybackStoreStronglyOwnsController() {
        var forwardedChannel: Channel?
        let store: PlaybackStore = {
            let controller = LifetimePlaybackController()
            controller.onPlay = { forwardedChannel = $0 }
            return PlaybackStore(controller: controller)
        }()

        let channel = Channel(id: "1", name: "Das Erste HD", number: "1", serviceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:", logoURL: nil)
        store.play(channel: channel)
        #expect(forwardedChannel == channel)
    }

    @Test("DeviceStore coordinates backend session and address state")
    @MainActor
    func testDeviceStore() async {
        let session = MockDeviceSession()
        let store = DeviceStore(session: session)

        #expect(store.isConnected == false)

        let address = try! ServerAddressParser.parseTrusted("http://192.0.2.1:8089/")
        await store.connect(to: address)
        #expect(store.isConnected == true)
        #expect(store.serverAddress?.origin.host == "192.0.2.1")
        #expect(store.serverAddress?.origin.port == 8089)

        await store.disconnect()
        #expect(store.isConnected == false)
        #expect(store.serverAddress == nil)
    }
}
