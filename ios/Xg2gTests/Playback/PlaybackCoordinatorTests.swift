// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

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
