// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Observation
import Testing
@testable import Xg2g

/// `completePairing()` is awaited by the pairing screen's `.task`, and SwiftUI
/// cancels that task the moment `state` becomes `.ready` and the screen is
/// swapped out. Whatever the initial load still has in flight at that point
/// dies with it. These tests drive the whole flow through `AppModel` against a
/// scripted API and cancel the caller exactly when SwiftUI would.
@MainActor
struct PairingCompletionTests {

    /// Seen on tvOS 2026-09-20: bouquets were through, `/services`,
    /// `/recordings` and `/timers` were cancelled (-999) right after the flip,
    /// `channels` stayed empty and the home hub never left its spinner.
    @Test func channelsAreLoadedBeforeTheReadyTransitionCancelsTheCaller() async throws {
        let api = scriptedPairing()
        api.stub("services", json: """
            [{"id":"orf1","name":"ORF1 HD","number":"1","serviceRef":"1:0:19:132F:3EF:1:C00000:0:0:0:"},
             {"id":"orf2","name":"ORF2 HD","number":"2","serviceRef":"1:0:19:1330:3EF:1:C00000:0:0:0:"}]
            """)
        let model = try await pairedModel(api: api)

        // The caller is created first, but cannot run before this function
        // suspends: both are on the main actor. The tracking is therefore in
        // place before `completePairing()` takes its first step.
        let caller = Task { await model.completePairing() }
        withObservationTracking {
            _ = model.state
        } onChange: {
            caller.cancel()
        }
        await caller.value

        #expect(model.state == .ready)
        #expect(model.channels.map(\.name) == ["ORF1 HD", "ORF2 HD"])
        #expect(api.calls.map(\.path).contains("timers"), "the whole initial load ran, not just the part before the flip")
    }

    /// The load moving the state itself outranks the promotion to `.ready`: a
    /// server that refuses the fresh credentials sends the device back to
    /// pairing, not into the ready screen with an error banner.
    @Test func aRefusedInitialLoadIsNotPaperedOverByReady() async throws {
        let api = scriptedPairing()
        api.stub("services", failure: .http(status: 401, contentType: nil, bodyPreview: ""))
        let model = try await pairedModel(api: api)

        await model.completePairing()

        #expect(model.state == .needsRePairing)
        #expect(model.channels.isEmpty)
    }

    // MARK: - Helpers

    /// Everything the flow asks for, except `services`, which each test scripts.
    private func scriptedPairing() -> ScriptedAPI {
        let api = ScriptedAPI()
        api.stub("pairing/start", json: """
            {"pairingId":"pr_1","pairingSecret":"ps_secret","userCode":"ABCD-1234",
             "qrPayload":"xg2g://pair?pairing_id=pr_1&user_code=ABCD-1234",
             "expiresAt":"2027-01-15T12:00:00Z"}
            """)
        api.stub("pairing/pr_1/exchange", json: """
            {"pairingId":"pr_1","deviceId":"dev_abc","tokenType":"DPoP",
             "accessToken":"at_1","expiresIn":900,"refreshToken":"rt_1",
             "scope":"stream epg","policyVersion":"v3","endpoints":[]}
            """)
        api.stub("services/bouquets", json: "[]")
        api.stub("services/now-next", json: #"{"items":[]}"#)
        api.stub("epg", json: "[]")
        api.stub("recordings", json: "[]")
        api.stub("timers", json: "[]")
        return api
    }

    /// A model that has a server and an invitation, one poll away from
    /// `completePairing()`.
    private func pairedModel(api: ScriptedAPI) async throws -> AppModel {
        let suite = "io.github.manugh.xg2g.tests.pairing.\(UUID().uuidString)"
        let defaults = try #require(UserDefaults(suiteName: suite))
        defaults.removePersistentDomain(forName: suite)

        let model = AppModel(
            addressStore: ServerAddressStore(defaults: defaults),
            credentials: RecordingCredentialStore(),
            keyStore: SecureEnclaveDeviceKeyStore(
                policy: .allowSoftware(reason: .development),
                tag: "io.github.manugh.xg2g.tests.pairing.\(UUID().uuidString)",
                secureEnclaveProbe: { false }
            ),
            makeAPIClient: { _, _ in api }
        )
        await model.useServer("https://tv.example")
        _ = try #require(await model.beginPairing())
        #expect(model.state == .needsPairing)
        return model
    }
}
