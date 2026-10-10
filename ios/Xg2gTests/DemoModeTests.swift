// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

@Suite("App Store Review Demo Mode & Privacy Readiness Tests")
struct DemoModeTests {

    @Test("DemoAPIClient provides channels, EPG schedule, recordings, and mutable timers via repositories")
    func demoAPIClientProvidesCompleteCatalogAndMutations() async throws {
        let client = DemoAPIClient()
        let channelRepo = ChannelRepository(api: client)
        let recordingsRepo = RecordingsRepository(api: client)
        let timersRepo = TimersRepository(api: client)

        let bouquets = try await channelRepo.bouquets()
        #expect(bouquets.count == 3)

        let channels = try await channelRepo.channels()
        #expect(channels.count == 6)
        #expect(channels.contains(where: { $0.name == "xg2g Showcase HD" }))
        #expect(channels.contains(where: { $0.name == "Apple Developer Stream" }))

        let serviceRefs = channels.map(\.serviceRef)
        let nowNext = try await channelRepo.nowNext(for: serviceRefs)
        #expect(nowNext.count == 6)
        for sref in serviceRefs {
            #expect(nowNext[sref]?.now != nil)
            #expect(nowNext[sref]?.next != nil)
        }

        let epg = try await channelRepo.epgSchedule(bouquet: nil)
        #expect(epg.count == 6)
        #expect(epg.values.allSatisfy { !$0.isEmpty })

        let initialRecordings = try await recordingsRepo.recordings()
        #expect(initialRecordings.count >= 1)

        let initialTimers = try await timersRepo.timers()
        let initialCount = initialTimers.count
        let uniqueTitle = "Review Test Timer \(UUID().uuidString.prefix(6))"
        try await timersRepo.createTimer(
            serviceRef: channels[0].serviceRef,
            name: uniqueTitle,
            description: "Created during unit test",
            begin: Date(timeIntervalSince1970: 1_760_003_600),
            end: Date(timeIntervalSince1970: 1_760_007_200)
        )
        let afterCreate = try await timersRepo.timers()
        #expect(afterCreate.count == initialCount + 1)
        if let created = afterCreate.first(where: { $0.name == uniqueTitle }) {
            try await timersRepo.deleteTimer(id: created.id)
            let afterDelete = try await timersRepo.timers()
            #expect(afterDelete.count == initialCount)
        }
    }

    @Test("Demo recordings resolve through RecordingPlayback containment to Apple's official HLS stream")
    func demoRecordingsResolveWithinOriginContainment() async throws {
        let client = DemoAPIClient()
        let recordingsRepo = RecordingsRepository(api: client)
        let recordings = try await recordingsRepo.recordings()
        #expect(!recordings.isEmpty)

        for recording in recordings {
            let negotiatedURL = try await recordingsRepo.playbackUrl(for: recording.id)
            let resolved = RecordingPlayback.resolve(
                address: DemoServer.demoServerAddress,
                recordingID: recording.id,
                negotiatedPath: negotiatedURL,
                sessionCookie: nil
            )
            #expect(resolved?.url == DemoServer.appleBipBopHLSURL)
        }
    }

    @Test("AppModel startDemoMode transitions to ready without hardware and disconnectServer exits cleanly")
    @MainActor
    func appModelStartAndExitDemoMode() async {
        let suiteName = "io.github.manugh.xg2g.tests.demomode.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suiteName)!
        defer { defaults.removePersistentDomain(forName: suiteName) }

        let model = AppModel(addressStore: ServerAddressStore(defaults: defaults))
        await model.startDemoMode()

        #expect(model.isDemoMode == true)
        #expect(model.state == .ready)
        #expect(model.serverURLString == DemoServer.displayAddress)
        #expect(model.channels.count == 6)
        #expect(!model.recordings.isEmpty)
        #expect(!model.timers.isEmpty)

        if let firstChannel = model.channels.first {
            #expect(model.liveStreamURL(for: firstChannel.serviceRef) == DemoServer.appleBipBopHLSURL)
        }

        await model.disconnectServer()
        #expect(model.isDemoMode == false)
        #expect(model.state == .needsServer)
        #expect(model.channels.isEmpty)
    }

    @Test("Bundled PrivacyInfo.xcprivacy is valid and declares CA92.1 UserDefaults and zero tracking")
    func privacyManifestIsBundledAndValid() throws {
        let bundle = Bundle(for: AppModel.self)
        let url = try #require(bundle.url(forResource: "PrivacyInfo", withExtension: "xcprivacy"))
        let data = try Data(contentsOf: url)
        let plist = try #require(
            PropertyListSerialization.propertyList(from: data, options: [], format: nil) as? [String: Any]
        )

        #expect(plist["NSPrivacyTracking"] as? Bool == false)
        let collected = plist["NSPrivacyCollectedDataTypes"] as? [[String: Any]]
        #expect(collected?.isEmpty == true)

        let apiTypes = try #require(plist["NSPrivacyAccessedAPITypes"] as? [[String: Any]])
        let userDefaultsEntry = apiTypes.first {
            ($0["NSPrivacyAccessedAPIType"] as? String) == "NSPrivacyAccessedAPICategoryUserDefaults"
        }
        #expect(userDefaultsEntry != nil)
        let reasons = userDefaultsEntry?["NSPrivacyAccessedAPITypeReasons"] as? [String]
        #expect(reasons?.contains("CA92.1") == true)
    }
}
