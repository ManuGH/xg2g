// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

/// The pairing screen polls `AppModel.pollPairing()` until it is told to stop.
/// These tests pin down when it is told to stop — and, more importantly, that
/// it *is* told to stop once the server has given up on the pairing.
///
/// Background: the server expires a pairing after its TTL and then answers
/// `expired` to every later poll. A loop that only recognises `approved` never
/// ends (seen 2026-09-20: 16 hours behind "Warte auf Bestätigung…").
@MainActor
struct PairingPollTests {

    private let api = ScriptedAPI()

    private func makeModel() -> AppModel {
        let suite = "io.github.manugh.xg2g.tests.pairingpoll.\(UUID().uuidString)"
        let keyStore = SecureEnclaveDeviceKeyStore(
            policy: .allowSoftware(reason: .development),
            tag: "io.github.manugh.xg2g.tests.pairingpoll.\(UUID().uuidString)",
            secureEnclaveProbe: { false }
        )
        return AppModel(
            addressStore: ServerAddressStore(defaults: UserDefaults(suiteName: suite)!),
            credentials: RecordingCredentialStore(),
            keyStore: keyStore,
            makeAPIClient: { [api] _, _ in api }
        )
    }

    private func startJSON(pairingID: String = "pr_1") -> String {
        """
        {"pairingId":"\(pairingID)","pairingSecret":"ps_secret","userCode":"ABCD-1234",
         "qrPayload":"xg2g://pair?pairing_id=\(pairingID)&user_code=ABCD-1234",
         "expiresAt":"2027-01-15T12:00:00Z"}
        """
    }

    private func statusJSON(_ status: Xg2gContract.PairingStatus, pairingID: String = "pr_1") -> String {
        """
        {"pairingId":"\(pairingID)","status":"\(status.rawValue)","userCode":"ABCD-1234",
         "deviceName":"iPhone","deviceType":"ios_phone","expiresAt":"2027-01-15T12:00:00Z"}
        """
    }

    /// A model that has a server and an issued pairing: exactly the state the
    /// screen is in while it polls.
    private func modelWaitingOnPairing() async throws -> AppModel {
        let model = makeModel()
        api.stub("pairing/start", json: startJSON())
        await model.useServer("https://tv.example")
        let invitation = try #require(await model.beginPairing())
        #expect(invitation.pairingID == "pr_1")
        #expect(model.lastError == nil)
        return model
    }

    // MARK: - The bug

    @Test func anExpiredPollEndsTheWaitAndSaysWhy() async throws {
        let model = try await modelWaitingOnPairing()
        api.stub("pairing/pr_1/status", json: statusJSON(.expired))

        #expect(await model.pollPairing() == .ended)

        let error = try #require(model.lastError)
        #expect(error.contains("abgelaufen"), "the user has to learn that the code, not the server, is the problem")
        #expect(error.contains("neuen Code"), "the text has to point at the remedy")
        // Ended is not the same as unpaired-from-scratch: the server stays.
        #expect(model.state == .needsPairing)
    }

    @Test(arguments: [Xg2gContract.PairingStatus.consumed, .revoked])
    func aConsumedOrRevokedPollEndsTheWaitToo(status: Xg2gContract.PairingStatus) async throws {
        let model = try await modelWaitingOnPairing()
        api.stub("pairing/pr_1/status", json: statusJSON(status))

        #expect(await model.pollPairing() == .ended)
        #expect(model.lastError != nil)
        #expect(model.state == .needsPairing)
    }

    /// The three endings must not collapse into one message: "expired" asks
    /// for patience with the code, "revoked" means a human said no.
    @Test func eachEndingHasItsOwnExplanation() async throws {
        var messages: Set<String> = []
        for status in [Xg2gContract.PairingStatus.expired, .consumed, .revoked] {
            let model = try await modelWaitingOnPairing()
            api.stub("pairing/pr_1/status", json: statusJSON(status))
            _ = await model.pollPairing()
            messages.insert(try #require(model.lastError))
        }
        #expect(messages.count == 3)
    }

    // MARK: - What must keep working

    @Test func aPendingPollKeepsWaitingWithoutAnError() async throws {
        let model = try await modelWaitingOnPairing()
        api.stub("pairing/pr_1/status", json: statusJSON(.pending))

        #expect(await model.pollPairing() == .keepWaiting)
        #expect(model.lastError == nil)
    }

    /// A poll that fails in transport says nothing about the pairing. Treating
    /// it as an ending would turn every Wi-Fi blip into "request a new code".
    @Test func aFailedPollIsNotAnEnding() async throws {
        let model = try await modelWaitingOnPairing()
        api.stub("pairing/pr_1/status", failure: .transport(.offline))

        #expect(await model.pollPairing() == .keepWaiting)
        #expect(model.lastError == nil)
    }

    // MARK: - The remedy

    /// "Neuen Code anfordern" is `beginPairing()` again. It has to clear the
    /// ending's message and move the poll to the new pairing — otherwise the
    /// screen would show the fresh code under the stale "abgelaufen" banner.
    @Test func requestingANewCodeAfterAnEndingStartsOver() async throws {
        let model = try await modelWaitingOnPairing()
        api.stub("pairing/pr_1/status", json: statusJSON(.expired))
        #expect(await model.pollPairing() == .ended)
        #expect(model.lastError != nil)

        api.stub("pairing/start", json: startJSON(pairingID: "pr_2"))
        let renewed = try #require(await model.beginPairing())

        #expect(renewed.pairingID == "pr_2")
        #expect(model.lastError == nil)

        api.stub("pairing/pr_2/status", json: statusJSON(.pending, pairingID: "pr_2"))
        #expect(await model.pollPairing() == .keepWaiting)
        #expect(api.calls.last?.path == "pairing/pr_2/status", "the poll must follow the new pairing, not the dead one")
    }
}
