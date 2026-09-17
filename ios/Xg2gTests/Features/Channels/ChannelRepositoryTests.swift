// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

struct ChannelRepositoryTests {

    private func repository(_ api: ScriptedAPI) -> ChannelRepository {
        ChannelRepository(api: api)
    }

    @Test func channelsAreOrderedByNumberNotByString() async throws {
        let api = ScriptedAPI()
        api.stub("services", json: """
            [{"id":"s2","name":"Two","number":"2","serviceRef":"1:0:1:2::"},
             {"id":"s100","name":"Hundred","number":"100","serviceRef":"1:0:1:100::"},
             {"id":"s10","name":"Ten","number":"10","serviceRef":"1:0:1:10::"}]
            """)

        let channels = try await repository(api).channels()

        // Sorting the strings would give 10, 100, 2.
        #expect(channels.map(\.number) == ["2", "10", "100"])
    }

    /// A channel with no name or no service reference cannot be shown or
    /// played, so it must not reach the list as an empty row.
    @Test func unusableEntriesAreDroppedAtTheBoundary() async throws {
        let api = ScriptedAPI()
        api.stub("services", json: """
            [{"id":"ok","name":"Good","number":"1","serviceRef":"1:0:1:1::"},
             {"id":"noname","name":"  ","number":"2","serviceRef":"1:0:1:2::"},
             {"id":"noref","name":"No Ref","number":"3"},
             {"id":"emptyref","name":"Empty Ref","number":"4","serviceRef":""}]
            """)

        let channels = try await repository(api).channels()

        #expect(channels.map(\.name) == ["Good"])
    }

    /// A channel without a catalogue id still has a unique identity.
    @Test func aMissingIDFallsBackToTheServiceReference() async throws {
        let api = ScriptedAPI()
        api.stub("services", json: #"[{"name":"Anon","serviceRef":"1:0:1:9::"}]"#)

        let channel = try #require(try await repository(api).channels().first)
        #expect(channel.id == "1:0:1:9::")
    }

    /// Duplicate service references across bouquets or master list are deduplicated cleanly.
    @Test func duplicateServicesAreDeduplicatedByServiceReference() async throws {
        let api = ScriptedAPI()
        api.stub("services", json: """
            [{"id":"orf1_main","name":"ORF1 HD","number":"1","serviceRef":"1:0:19:132F:3EF:1:C00000:0:0:0:"},
             {"id":"orf1_fav","name":"ORF1 HD","number":"1","serviceRef":"1:0:19:132F:3EF:1:C00000:0:0:0:"},
             {"id":"orf2_main","name":"ORF2 HD","number":"2","serviceRef":"1:0:19:1330:3EF:1:C00000:0:0:0:"}]
            """)

        let channels = try await repository(api).channels()
        #expect(channels.count == 2)
        #expect(channels.map(\.name) == ["ORF1 HD", "ORF2 HD"])
    }

    @Test func bouquetFilterIsSentAsAQueryItem() async throws {
        let api = ScriptedAPI()
        api.stub("services", json: "[]")

        _ = try await repository(api).channels(bouquet: "Favourites")

        let call = try #require(api.calls.first)
        #expect(call.path == "services")
    }

    /// Epoch seconds, not RFC 3339 — this endpoint speaks integers while the
    /// pairing endpoints speak timestamps.
    @Test func nowNextDecodesEpochSeconds() async throws {
        let api = ScriptedAPI()
        api.stub("services/now-next", json: """
            {"items":[{"serviceRef":"1:0:1:1::",
                       "now":{"title":"Tagesschau","desc":"News","start":1800000000,"end":1800000900},
                       "next":{"title":"Tatort","start":1800000900,"end":1800006300}}]}
            """)

        let schedule = try await repository(api).nowNext(for: ["1:0:1:1::"])
        let entry = try #require(schedule["1:0:1:1::"])

        #expect(entry.now?.title == "Tagesschau")
        #expect(entry.now?.description == "News")
        #expect(entry.now?.start == Date(timeIntervalSince1970: 1_800_000_000))
        #expect(entry.next?.title == "Tatort")
        #expect(entry.next?.description == nil, "an absent description must not become an empty string")
    }

    /// The endpoint requires at least one service. Asking about nothing is not
    /// an error the caller should have to handle.
    @Test func anEmptyRequestMakesNoCall() async throws {
        let api = ScriptedAPI()

        #expect(try await repository(api).nowNext(for: []).isEmpty)
        #expect(api.calls.isEmpty)
    }

    /// A repeated service reference would crash a plain Dictionary(uniquing:)
    /// -free construction. Neither entry is more correct than the other.
    @Test func aDuplicatedServiceReferenceDoesNotTrap() async throws {
        let api = ScriptedAPI()
        api.stub("services/now-next", json: """
            {"items":[{"serviceRef":"dup","now":{"title":"First","start":1,"end":2}},
                      {"serviceRef":"dup","now":{"title":"Second","start":3,"end":4}}]}
            """)

        let schedule = try await repository(api).nowNext(for: ["dup"])
        #expect(schedule["dup"]?.now?.title == "Second")
    }
}
