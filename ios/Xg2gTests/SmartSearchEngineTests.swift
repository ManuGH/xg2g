// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

@Suite("SmartSearchEngineTests")
struct SmartSearchEngineTests {

    private func makeChannel(id: String, name: String, sRef: String) -> Channel {
        Channel(
            id: id,
            name: name,
            number: "1",
            serviceRef: sRef,
            logoURL: nil
        )
    }

    private func makeEntry(title: String, start: Date, duration: TimeInterval) -> NowNext.Entry {
        NowNext.Entry(
            title: title,
            description: "Description for \(title)",
            start: start,
            end: start.addingTimeInterval(duration)
        )
    }

    @Test("Empty or whitespace queries return empty results")
    func emptyQueryReturnsEmpty() {
        let now = Date(timeIntervalSince1970: 1700000000)
        let ch = makeChannel(id: "1", name: "Das Erste HD", sRef: "1:0:19:283D:3FB:1:C00000:0:0:0:")

        let result1 = SmartSearchEngine.search(query: "", channels: [ch], schedule: [:], fullEpg: [:], now: now)
        #expect(result1.isEmpty)

        let result2 = SmartSearchEngine.search(query: "   \n\t ", channels: [ch], schedule: [:], fullEpg: [:], now: now)
        #expect(result2.isEmpty)
    }

    @Test("Matches channel name directly")
    func matchesChannelName() {
        let now = Date(timeIntervalSince1970: 1700000000)
        let ch1 = makeChannel(id: "1", name: "Das Erste HD", sRef: "sref1")
        let ch2 = makeChannel(id: "2", name: "ZDF HD", sRef: "sref2")

        let result = SmartSearchEngine.search(query: "ZDF", channels: [ch1, ch2], schedule: [:], fullEpg: [:], now: now)
        #expect(result.channels.count == 1)
        #expect(result.channels.first?.name == "ZDF HD")
    }

    @Test("Categorizes live show vs upcoming show accurately based on clock time")
    func categorizesLiveVsUpcoming() {
        let now = Date(timeIntervalSince1970: 1700000000) // T0
        let ch = makeChannel(id: "1", name: "Das Erste HD", sRef: "sref1")

        // 1. Live show: started 15 min ago, ends in 45 min
        let liveEntry = makeEntry(title: "Tatort: Köln", start: now.addingTimeInterval(-900), duration: 3600)
        // 2. Upcoming show: starts in 2 hours
        let upcomingEntry = makeEntry(title: "Tatort: München", start: now.addingTimeInterval(7200), duration: 5400)
        // 3. Past show: ended 30 min ago
        let pastEntry = makeEntry(title: "Tatort: Hamburg", start: now.addingTimeInterval(-5400), duration: 3600)

        let epg = [ch.serviceRef: [pastEntry, liveEntry, upcomingEntry]]

        let result = SmartSearchEngine.search(
            query: "Tatort",
            channels: [ch],
            schedule: [:],
            fullEpg: epg,
            now: now
        )

        // Channels should not match "Tatort"
        #expect(result.channels.isEmpty)

        // Live shows must have exactly the on-air one
        #expect(result.liveShows.count == 1)
        #expect(result.liveShows.first?.entry.title == "Tatort: Köln")
        #expect(result.liveShows.first?.channel.name == "Das Erste HD")
        #expect((result.liveShows.first?.progress ?? 0) > 0.2) // ~25% progress

        // Upcoming shows must have only the future one
        #expect(result.upcomingShows.count == 1)
        #expect(result.upcomingShows.first?.entry.title == "Tatort: München")
    }

    @Test("Fallbacks to NowNext live schedule when fullEpg is empty")
    func fallbackToScheduleNowNext() {
        let now = Date(timeIntervalSince1970: 1700000000)
        let ch = makeChannel(id: "1", name: "RTL HD", sRef: "sref_rtl")

        let currentShow = makeEntry(title: "Gute Zeiten, Schlechte Zeiten", start: now.addingTimeInterval(-600), duration: 1800)
        let nextShow = makeEntry(title: "RTL Aktuell", start: now.addingTimeInterval(1200), duration: 1200)

        let schedule = [ch.serviceRef: NowNext(serviceRef: ch.serviceRef, now: currentShow, next: nextShow)]

        let result = SmartSearchEngine.search(
            query: "Zeiten",
            channels: [ch],
            schedule: schedule,
            fullEpg: [:], // Empty multi-day buffer
            now: now
        )

        #expect(result.liveShows.count == 1)
        #expect(result.liveShows.first?.entry.title == "Gute Zeiten, Schlechte Zeiten")
    }
}
