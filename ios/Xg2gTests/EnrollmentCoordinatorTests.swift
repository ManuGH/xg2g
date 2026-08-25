// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

// MARK: - Doubles

/// An `APIClient` that answers from a per-path script and records what it was
/// asked.
///
/// It decodes canned JSON with the app's own `JSONDecoder.xg2g` rather than
/// handing back pre-built Swift values, so these tests still fail if a
/// `CodingKeys` mapping stops matching the wire.
final class ScriptedAPI: APIClient, @unchecked Sendable {

    struct Call: Sendable {
        let method: HTTPMethod
        let path: String
        let body: Data?
    }

    private let lock = NSLock()
    private var scripted: [String: [Result<Data, APIError>]] = [:]
    private var recorded: [Call] = []

    var calls: [Call] {
        lock.lock(); defer { lock.unlock() }
        return recorded
    }

    func stub(_ path: String, json: String) {
        lock.lock(); defer { lock.unlock() }
        scripted[path, default: []].append(.success(Data(json.utf8)))
    }

    func stub(_ path: String, failure: APIError) {
        lock.lock(); defer { lock.unlock() }
        scripted[path, default: []].append(.failure(failure))
    }

    /// Non-async so the lock is never held across a suspension point.
    private func recordAndTake(_ method: HTTPMethod, _ path: String, _ body: Data?) -> Result<Data, APIError>? {
        lock.lock(); defer { lock.unlock() }
        recorded.append(Call(method: method, path: path, body: body))
        guard var queue = scripted[path], !queue.isEmpty else { return nil }
        let next = queue.removeFirst()
        scripted[path] = queue
        return next
    }

    func send<Response>(_ request: APIRequest<Response>) async throws -> Response where Response: Decodable & Sendable {
        let next = recordAndTake(request.method, request.path, request.body)

        guard let next else {
            throw APIError.invalidEndpoint(path: request.path)
        }
        switch next {
        case .failure(let error):
            throw error
        case .success(let data):
            return try JSONDecoder.xg2g.decode(Response.self, from: data)
        }
    }
}

actor RecordingCredentialStore: CredentialStore {

    private(set) var commits: [EnrolledCredentials] = []
    private(set) var forgotten: [ServerIdentity] = []
    private(set) var clearedSessions: [ServerIdentity] = []
    private var current: EnrolledCredentials?
    private var failCommitWith: CredentialStoreError?

    init(seeded: EnrolledCredentials? = nil, failCommitWith: CredentialStoreError? = nil) {
        self.current = seeded
        self.failCommitWith = failCommitWith
    }

    func prepareForLaunch() async throws {}
    func deviceGrant(for identity: ServerIdentity) async throws -> DeviceGrant? { current?.grant }
    func accessSession(for identity: ServerIdentity) async throws -> AccessSession? { current?.session }

    func commit(_ credentials: EnrolledCredentials, for identity: ServerIdentity) async throws {
        if let failCommitWith { throw failCommitWith }
        commits.append(credentials)
        current = credentials
    }

    // These are not no-ops on purpose. A double that accepts a lifecycle call
    // and changes nothing reports whatever the test hoped for: the revocation
    // tests asked whether the grant was gone and got "still there" from the
    // double rather than from the code under test.
    func endSession(for identity: ServerIdentity) async throws {
        guard let existing = current else { return }
        current = EnrolledCredentials(
            grant: existing.grant,
            session: AccessSession(sessionID: "", token: "", expiresAt: .distantPast, policyVersion: nil)
        )
        clearedSessions.append(identity)
    }

    func forgetServer(_ identity: ServerIdentity) async throws {
        current = nil
        forgotten.append(identity)
    }

    func migrate(from old: ServerIdentity, to new: ServerIdentity) async throws {}
    func purgeEverything() async throws { current = nil }
}

/// A key store that cannot provision. Stands in for a device whose policy
/// demands hardware backing on hardware that has none.
struct UnprovisionableKeyStore: DeviceKeyStore {
    func attestation() async throws -> DeviceKeyAttestation? { nil }
    func provisionKey() async throws -> DeviceKeyAttestation { throw DeviceKeyError.secureEnclaveUnavailable }
    func sign(_ data: Data) async throws -> Data { throw DeviceKeyError.keyNotProvisioned }
    func destroyKey() async throws {}
}

// MARK: - Tests

struct EnrollmentCoordinatorTests {

    private let startedAt = Date(timeIntervalSince1970: 1_800_000_000)

    private func keyStore() -> SecureEnclaveDeviceKeyStore {
        SecureEnclaveDeviceKeyStore(
            policy: .allowSoftware(reason: .development),
            tag: "io.github.manugh.xg2g.tests.enroll.\(UUID().uuidString)",
            secureEnclaveProbe: { false }
        )
    }

    private func identity() throws -> ServerIdentity {
        .address(try ServerAddressParser.parseTrusted("https://tv.example/"))
    }

    private func makeCoordinator(
        api: ScriptedAPI,
        credentials: RecordingCredentialStore = RecordingCredentialStore(),
        keys: DeviceKeyStore? = nil
    ) throws -> EnrollmentCoordinator {
        EnrollmentCoordinator(
            identity: try identity(),
            api: api,
            keyStore: keys ?? keyStore(),
            credentials: credentials,
            now: { self.startedAt }
        )
    }

    private let startJSON = """
        {"pairingId":"pr_1","pairingSecret":"ps_secret","userCode":"ABCD-1234",
         "qrPayload":"xg2g://pair?pairing_id=pr_1&user_code=ABCD-1234",
         "expiresAt":"2027-01-15T12:00:00Z"}
        """

    private func exchangeJSON(
        pairingID: String = "pr_1",
        deviceID: String = "dev_abc",
        tokenType: String = "DPoP",
        accessToken: String = "at_1",
        expiresIn: Int = 900,
        refreshToken: String = "rt_1"
    ) -> String {
        """
        {"pairingId":"\(pairingID)","deviceId":"\(deviceID)","tokenType":"\(tokenType)",
         "accessToken":"\(accessToken)","expiresIn":\(expiresIn),"refreshToken":"\(refreshToken)",
         "scope":"stream epg","policyVersion":"v3","endpoints":[]}
        """
    }

    // MARK: - Start

    @Test func startPairingReturnsTheInvitationTheUserMustActOn() async throws {
        let api = ScriptedAPI()
        api.stub("pairing/start", json: startJSON)
        let coordinator = try makeCoordinator(api: api)

        let invitation = try await coordinator.startPairing(deviceName: "Manuel's iPhone", deviceType: .iPhone)

        #expect(invitation.pairingID == "pr_1")
        #expect(invitation.userCode == "ABCD-1234")
        #expect(invitation.qrPayload.hasPrefix("xg2g://pair"))
    }

    /// A device that cannot produce a policy-conforming key must find out
    /// before a human is sent off to approve a pairing that can never complete.
    @Test func anUnprovisionableKeyFailsBeforeAnyNetworkCall() async throws {
        let api = ScriptedAPI()
        api.stub("pairing/start", json: startJSON)
        let coordinator = try makeCoordinator(api: api, keys: UnprovisionableKeyStore())

        await #expect(throws: DeviceKeyError.secureEnclaveUnavailable) {
            try await coordinator.startPairing(deviceName: "iPhone", deviceType: .iPhone)
        }
        #expect(api.calls.isEmpty, "the pairing must not be created if this device cannot complete it")
    }

    @Test func completingWithoutStartingIsRefused() async throws {
        let coordinator = try makeCoordinator(api: ScriptedAPI())

        await #expect(throws: EnrollmentCoordinator.Failure.noPairingInProgress) {
            _ = try await coordinator.completeEnrollment()
        }
    }

    // MARK: - The happy path

    @Test func successCommitsExactlyOneCredentialSet() async throws {
        let api = ScriptedAPI()
        let store = RecordingCredentialStore()
        api.stub("pairing/start", json: startJSON)
        api.stub("pairing/pr_1/exchange", json: exchangeJSON())
        let coordinator = try makeCoordinator(api: api, credentials: store)

        try await coordinator.startPairing(deviceName: "iPhone", deviceType: .iPhone)
        let enrolled = try await coordinator.completeEnrollment()

        let commits = await store.commits
        #expect(commits.count == 1)
        #expect(enrolled.grant.id == "dev_abc")
        // The durable half of the pair is the refresh token, not the access token.
        #expect(enrolled.grant.secret == "rt_1")
        #expect(enrolled.session.token == "at_1")
        #expect(enrolled.session.expiresAt == startedAt.addingTimeInterval(900))
    }

    /// The JWK the server binds the credentials to has to be the key that will
    /// actually sign later. If these ever diverge, every DPoP-authorized
    /// request fails with a 401 that points nowhere near the cause.
    @Test func theEnrolledJWKIsTheKeyThatSigns() async throws {
        let api = ScriptedAPI()
        let keys = keyStore()
        api.stub("pairing/start", json: startJSON)
        api.stub("pairing/pr_1/exchange", json: exchangeJSON())
        let coordinator = try makeCoordinator(api: api, keys: keys)

        try await coordinator.startPairing(deviceName: "iPhone", deviceType: .iPhone)
        _ = try await coordinator.completeEnrollment()

        let exchange = try #require(api.calls.first { $0.path == "pairing/pr_1/exchange" })
        let rawBody = try #require(exchange.body)
        let body = try #require(JSONSerialization.jsonObject(with: rawBody) as? [String: Any])
        let jwk = try #require(body["deviceJwk"] as? [String: Any])
        let stored = try await keys.attestation()
        let attestation = try #require(stored)

        #expect(jwk["x"] as? String == attestation.publicKey.x)
        #expect(jwk["y"] as? String == attestation.publicKey.y)
        #expect(jwk["kty"] as? String == "EC")
        #expect(jwk["crv"] as? String == "P-256")
        #expect(body["pairingSecret"] as? String == "ps_secret")
        // The private half must never be serialized, whatever else changes.
        #expect(jwk["d"] == nil)
    }

    /// The secret is spent. Keeping it would only enable a replay of our own.
    @Test func aSecondCompletionCannotReplayTheExchange() async throws {
        let api = ScriptedAPI()
        api.stub("pairing/start", json: startJSON)
        api.stub("pairing/pr_1/exchange", json: exchangeJSON())
        let coordinator = try makeCoordinator(api: api)

        try await coordinator.startPairing(deviceName: "iPhone", deviceType: .iPhone)
        _ = try await coordinator.completeEnrollment()

        await #expect(throws: EnrollmentCoordinator.Failure.noPairingInProgress) {
            _ = try await coordinator.completeEnrollment()
        }
    }

    // MARK: - No half-written credential state

    @Test func aFailedExchangeCommitsNothing() async throws {
        let api = ScriptedAPI()
        let store = RecordingCredentialStore()
        api.stub("pairing/start", json: startJSON)
        api.stub("pairing/pr_1/exchange", failure: .http(status: 409, contentType: nil, bodyPreview: ""))
        let coordinator = try makeCoordinator(api: api, credentials: store)

        try await coordinator.startPairing(deviceName: "iPhone", deviceType: .iPhone)
        await #expect(throws: (any Error).self) { _ = try await coordinator.completeEnrollment() }

        #expect(await store.commits.isEmpty)
    }

    /// A transport failure says nothing about whether the pairing is still
    /// good, so the pairing survives and a retry is possible.
    @Test func aRetryableFailureKeepsThePairing() async throws {
        let api = ScriptedAPI()
        api.stub("pairing/start", json: startJSON)
        api.stub("pairing/pr_1/exchange", failure: .transport(.other(code: -1005)))
        api.stub("pairing/pr_1/exchange", json: exchangeJSON())
        let coordinator = try makeCoordinator(api: api)

        try await coordinator.startPairing(deviceName: "iPhone", deviceType: .iPhone)
        await #expect(throws: (any Error).self) { _ = try await coordinator.completeEnrollment() }

        let enrolled = try await coordinator.completeEnrollment()
        #expect(enrolled.session.token == "at_1")
    }

    @Test func cancellingDropsThePairingWithoutTouchingCredentials() async throws {
        let api = ScriptedAPI()
        let store = RecordingCredentialStore()
        api.stub("pairing/start", json: startJSON)
        let coordinator = try makeCoordinator(api: api, credentials: store)

        try await coordinator.startPairing(deviceName: "iPhone", deviceType: .iPhone)
        await coordinator.cancelPairing()

        await #expect(throws: EnrollmentCoordinator.Failure.noPairingInProgress) {
            _ = try await coordinator.completeEnrollment()
        }
        #expect(await store.commits.isEmpty)
    }

    // MARK: - The response is validated in full

    @Test func aResponseForAnotherPairingIsRefused() async throws {
        try await expectDefect(.pairingMismatch, exchange: exchangeJSON(pairingID: "pr_someone_else"))
    }

    /// A `Bearer` token is not sender-constrained. Accepting one would silently
    /// discard the device binding this entire design exists to provide.
    @Test func aBearerTokenIsRefusedRatherThanUsed() async throws {
        try await expectDefect(.notSenderConstrained, exchange: exchangeJSON(tokenType: "Bearer"))
    }

    @Test func anEmptyDeviceIDIsRefused() async throws {
        try await expectDefect(.emptyDeviceID, exchange: exchangeJSON(deviceID: ""))
    }

    @Test func anEmptyAccessTokenIsRefused() async throws {
        try await expectDefect(.emptyAccessToken, exchange: exchangeJSON(accessToken: ""))
    }

    /// Without a refresh token the session is unrenewable, so it is not a
    /// credential set at all.
    @Test func anEmptyRefreshTokenIsRefused() async throws {
        try await expectDefect(.emptyRefreshToken, exchange: exchangeJSON(refreshToken: ""))
    }

    @Test func aNonPositiveLifetimeIsRefused() async throws {
        try await expectDefect(.nonPositiveLifetime, exchange: exchangeJSON(expiresIn: 0))
    }

    /// `DPoP` is compared case-insensitively, as RFC 9449 spells the scheme.
    @Test func tokenTypeCasingIsNotTreatedAsADefect() async throws {
        let api = ScriptedAPI()
        api.stub("pairing/start", json: startJSON)
        api.stub("pairing/pr_1/exchange", json: exchangeJSON(tokenType: "dpop"))
        let coordinator = try makeCoordinator(api: api)

        try await coordinator.startPairing(deviceName: "iPhone", deviceType: .iPhone)
        #expect(try await coordinator.completeEnrollment().session.token == "at_1")
    }

    private func expectDefect(
        _ defect: EnrollmentCoordinator.Failure.Defect,
        exchange: String
    ) async throws {
        let api = ScriptedAPI()
        let store = RecordingCredentialStore()
        api.stub("pairing/start", json: startJSON)
        api.stub("pairing/pr_1/exchange", json: exchange)
        let coordinator = try makeCoordinator(api: api, credentials: store)

        try await coordinator.startPairing(deviceName: "iPhone", deviceType: .iPhone)
        await #expect(throws: EnrollmentCoordinator.Failure.malformedExchange(defect)) {
            _ = try await coordinator.completeEnrollment()
        }
        #expect(await store.commits.isEmpty, "a malformed exchange must not leave credentials behind")
    }

    // MARK: - Status

    @Test func everyPairingStatusDecodes() async throws {
        for status in Xg2gContract.PairingStatus.allCases {
            let api = ScriptedAPI()
            api.stub("pairing/start", json: startJSON)
            api.stub(
                "pairing/pr_1/status",
                json: """
                    {"pairingId":"pr_1","status":"\(status.rawValue)","userCode":"ABCD-1234",
                     "deviceName":"iPhone","deviceType":"ios_phone","expiresAt":"2027-01-15T12:00:00Z"}
                    """
            )
            let coordinator = try makeCoordinator(api: api)

            try await coordinator.startPairing(deviceName: "iPhone", deviceType: .iPhone)
            #expect(try await coordinator.pairingStatus() == status)
        }
    }

    @Test func statusWithoutAPairingIsRefused() async throws {
        let coordinator = try makeCoordinator(api: ScriptedAPI())

        await #expect(throws: EnrollmentCoordinator.Failure.noPairingInProgress) {
            _ = try await coordinator.pairingStatus()
        }
    }
}
