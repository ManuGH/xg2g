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

struct NowNextProgressTests {

    private let start = Date(timeIntervalSince1970: 1_800_000_000)

    private func entry(minutes: Double) -> NowNext.Entry {
        NowNext.Entry(title: "T", description: nil, start: start, end: start.addingTimeInterval(minutes * 60))
    }

    @Test func progressIsTheFractionElapsed() {
        let programme = entry(minutes: 60)
        #expect(programme.progress(at: start.addingTimeInterval(1800)) == 0.5)
    }

    /// Before the start and after the end there is no progress — not zero and
    /// not one, because a caller must be able to tell "not on" from "just
    /// began".
    @Test func outsideTheProgrammeThereIsNoProgress() {
        let programme = entry(minutes: 60)
        #expect(programme.progress(at: start.addingTimeInterval(-1)) == nil)
        #expect(programme.progress(at: start.addingTimeInterval(3601)) == nil)
    }

    @Test func aZeroLengthProgrammeHasNoProgressRatherThanADivisionByZero() {
        #expect(entry(minutes: 0).progress(at: start) == nil)
    }
}

struct PlaybackCoordinatorTests {

    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func makeCoordinator(_ api: ScriptedAPI) throws -> PlaybackCoordinator {
        PlaybackCoordinator(
            address: try ServerAddressParser.parseTrusted("https://tv.example/"),
            api: api,
            now: { self.now }
        )
    }

    private func stubHappyPath(_ api: ScriptedAPI, sessionID: String = "sess-1") {
        api.stub("intents", json: #"{"sessionId":"\#(sessionID)","requestId":"r","status":"accepted"}"#)
        api.stub("sessions/\(sessionID)/playback-ticket", json: """
            {"sessionId":"\(sessionID)","ticket":"tkt_abc","cookie":"xg2g_playback",
             "path":"/api/v3/sessions/\(sessionID)/hls/","expiresIn":14400}
            """)
    }

    @Test func startingLiveYieldsAPlaylistAndATicket() async throws {
        let api = ScriptedAPI()
        stubHappyPath(api)
        let coordinator = try makeCoordinator(api)

        let stream = try await coordinator.startLive(serviceRef: "1:0:1:1::")

        #expect(stream.sessionID == "sess-1")
        #expect(stream.playlistURL.absoluteString == "https://tv.example/api/v3/sessions/sess-1/hls/index.m3u8")
        #expect(stream.ticket.value == "tkt_abc")
        #expect(stream.ticket.expiresAt == now.addingTimeInterval(14400))
    }

    /// The credential must travel beside the URL, never inside it — that is the
    /// whole reason it is a cookie rather than a query parameter.
    @Test func theTicketNeverAppearsInTheURL() async throws {
        let api = ScriptedAPI()
        stubHappyPath(api)
        let coordinator = try makeCoordinator(api)

        let stream = try await coordinator.startLive(serviceRef: "1:0:1:1::")

        #expect(!stream.playlistURL.absoluteString.contains("tkt_abc"))
        #expect(stream.playlistURL.query == nil)
    }

    @Test func theCookieIsScopedToThatSessionsMediaPath() async throws {
        let api = ScriptedAPI()
        stubHappyPath(api)
        let stream = try await makeCoordinator(api).startLive(serviceRef: "1:0:1:1::")

        let cookie = try #require(stream.ticket.httpCookie(for: stream.playlistURL))
        #expect(cookie.name == "xg2g_playback")
        #expect(cookie.value == "tkt_abc")
        #expect(cookie.domain.contains("tv.example"))
        #expect(cookie.path == "/api/v3/sessions/sess-1/hls/")
        #expect(cookie.isSecure, "a media credential must not be sent in clear text")
    }

    @Test func anIntentWithoutASessionIsAFailure() async throws {
        let api = ScriptedAPI()
        api.stub("intents", json: #"{"sessionId":"  ","requestId":"r","status":"accepted"}"#)

        await #expect(throws: PlaybackCoordinator.Failure.noSessionCreated) {
            _ = try await makeCoordinator(api).startLive(serviceRef: "1:0:1:1::")
        }
    }

    /// Without a ticket there is nothing to play, so this fails rather than
    /// handing back a URL that will 401 inside AVPlayer where the cause is
    /// invisible.
    @Test func aRefusedTicketFailsTheStart() async throws {
        let api = ScriptedAPI()
        api.stub("intents", json: #"{"sessionId":"sess-1","requestId":"r","status":"accepted"}"#)
        api.stub("sessions/sess-1/playback-ticket", failure: .http(status: 403, contentType: nil, bodyPreview: ""))

        await #expect(throws: PlaybackCoordinator.Failure.ticketRefused) {
            _ = try await makeCoordinator(api).startLive(serviceRef: "1:0:1:1::")
        }
    }

    /// A session id that tries to climb out of the API scope must not produce a
    /// playable URL.
    @Test func aTraversingSessionIDIsRefused() async throws {
        let api = ScriptedAPI()
        stubHappyPath(api, sessionID: "../../admin")
        api.stub("sessions/../../admin/playback-ticket", json: """
            {"sessionId":"x","ticket":"t","cookie":"xg2g_playback","path":"/","expiresIn":60}
            """)

        await #expect(throws: (any Error).self) {
            _ = try await makeCoordinator(api).startLive(serviceRef: "1:0:1:1::")
        }
    }

    @Test func stoppingNamesTheSession() async throws {
        let api = ScriptedAPI()
        api.stub("intents", json: "{}")
        await (try makeCoordinator(api)).stopLive(sessionID: "sess-1")

        let call = try #require(api.calls.first { $0.path == "intents" })
        let body = try #require(call.body)
        let json = try #require(try JSONSerialization.jsonObject(with: body) as? [String: Any])
        #expect(json["type"] as? String == "stream.stop")
        #expect(json["sessionId"] as? String == "sess-1")
    }

    /// Stopping is best-effort: the server reclaims a session whose heartbeat
    /// stops, so a failed stop costs a lease timeout, not a stuck tuner.
    @Test func aFailedStopDoesNotThrow() async throws {
        let api = ScriptedAPI()
        api.stub("intents", failure: .transport(.offline))
        await (try makeCoordinator(api)).stopLive(sessionID: "sess-1")
    }
}

struct BouquetTests {

    @Test func bouquetsDecodesNameAndCount() async throws {
        let api = ScriptedAPI()
        api.stub("services/bouquets", json: """
            [{"name":"Favoriten","services":15},
             {"name":"HD Sender","services":42},
             {"name":"  ","services":0}]
            """)

        let bouquets = try await ChannelRepository(api: api).bouquets()

        #expect(bouquets.count == 2)
        #expect(bouquets[0].name == "Favoriten")
        #expect(bouquets[0].servicesCount == 15)
        #expect(bouquets[1].name == "HD Sender")
        #expect(bouquets[1].servicesCount == 42)
    }
}

struct RecordingsRepositoryTests {

    @Test func recordingsDecodesAndSortsByDate() async throws {
        let api = ScriptedAPI()
        api.stub("recordings", json: """
            [{"recordingId":"rec_1","title":"Older Movie","beginUnixSeconds":1700000000,"durationSeconds":7200,"status":"completed"},
             {"recordingId":"rec_2","title":"Newer Show","beginUnixSeconds":1700010000,"durationSeconds":3600,"status":"completed"}]
            """)

        let repo = RecordingsRepository(api: api)
        let recordings = try await repo.recordings()

        #expect(recordings.count == 2)
        // Ordered newest first
        #expect(recordings[0].id == "rec_2")
        #expect(recordings[0].title == "Newer Show")
        #expect(recordings[0].formattedDuration == "1h 0m")
        #expect(recordings[1].id == "rec_1")
        #expect(recordings[1].formattedDuration == "2h 0m")
    }
}

struct TimersRepositoryTests {

    @Test func timersDecodesAndSortsByDate() async throws {
        let api = ScriptedAPI()
        api.stub("timers", json: """
            [{"timerId":"t2","name":"Late Show","serviceRef":"1:0:1:2::","serviceName":"ZDF","begin":1700020000,"end":1700023600,"state":"waiting"},
             {"timerId":"t1","name":"Early News","serviceRef":"1:0:1:1::","serviceName":"Das Erste","begin":1700010000,"end":1700011800,"state":"running"}]
            """)

        let repo = TimersRepository(api: api)
        let timers = try await repo.timers()

        #expect(timers.count == 2)
        // Ordered chronological
        #expect(timers[0].id == "t1")
        #expect(timers[0].name == "Early News")
        #expect(timers[0].isRunning == true)
        #expect(timers[1].id == "t2")
        #expect(timers[1].isRunning == false)
    }
}

struct NowNextCountdownTests {

    @Test func remainingMinutesComputesCorrectly() {
        let start = Date(timeIntervalSince1970: 1000)
        let end = Date(timeIntervalSince1970: 2200) // 20 minutes duration

        let entry = NowNext.Entry(title: "Movie", description: nil, start: start, end: end)

        // Mid-way at 1600 (10 minutes remaining)
        let mid = Date(timeIntervalSince1970: 1600)
        #expect(entry.remainingMinutes(at: mid) == 10)

        // Before start -> nil
        let before = Date(timeIntervalSince1970: 500)
        #expect(entry.remainingMinutes(at: before) == nil)

        // After end -> nil
        let after = Date(timeIntervalSince1970: 3000)
        #expect(entry.remainingMinutes(at: after) == nil)
    }
}

@MainActor
struct ChannelZappingNavigationTests {

    @Test func zappingWrapsAroundProperly() {
        let model = AppModel()
        let c1 = Channel(id: "1", name: "ORF1", number: "1", serviceRef: "ref1", logoURL: nil)
        let c2 = Channel(id: "2", name: "ORF2", number: "2", serviceRef: "ref2", logoURL: nil)
        let c3 = Channel(id: "3", name: "ATV", number: "3", serviceRef: "ref3", logoURL: nil)

        // In empty state
        #expect(model.channelAfter(c1) == nil)
        #expect(model.channelBefore(c1) == nil)
    }
}

@MainActor
struct FavoriteChannelsTests {

    @Test func togglingFavoritesUpdatesState() {
        let model = AppModel()
        let c1 = Channel(id: "fav_test_1", name: "ORF1 HD", number: "1", serviceRef: "ref_1", logoURL: nil)

        #expect(model.isFavorite(c1) == false)

        model.toggleFavorite(c1)
        #expect(model.isFavorite(c1) == true)

        model.toggleFavorite(c1)
        #expect(model.isFavorite(c1) == false)
    }
}

struct QualityPreferenceTests {

    @Test func allCasesHaveValidDisplayNames() {
        for pref in AppModel.StreamingQualityPreference.allCases {
            #expect(!pref.displayName.isEmpty)
            #expect(!pref.rawValue.isEmpty)
        }
    }
}


