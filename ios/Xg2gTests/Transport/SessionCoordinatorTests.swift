// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

struct SessionCoordinatorTests {

    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func identity() throws -> ServerIdentity {
        .address(try ServerAddressParser.parseTrusted("https://tv.example/"))
    }

    private func credentials(refreshToken: String = "rt_1", sessionExpiresIn: TimeInterval) -> EnrolledCredentials {
        EnrolledCredentials(
            grant: DeviceGrant(id: "dev_abc", secret: refreshToken, expiresAt: nil),
            session: AccessSession(
                sessionID: "dev_abc",
                token: "at_1",
                expiresAt: now.addingTimeInterval(sessionExpiresIn),
                policyVersion: "v3"
            )
        )
    }

    private func makeCoordinator(
        api: ScriptedAPI,
        store: RecordingCredentialStore
    ) throws -> SessionCoordinator {
        SessionCoordinator(identity: try identity(), api: api, credentials: store, now: { self.now })
    }

    private func refreshJSON(
        accessToken: String = "at_2",
        refreshToken: String = "rt_2",
        tokenType: String = "DPoP",
        expiresIn: Int = 900
    ) -> String {
        """
        {"token_type":"\(tokenType)","access_token":"\(accessToken)","refresh_token":"\(refreshToken)",
         "expires_in":\(expiresIn),"device_id":"dev_abc","scope":"stream epg"}
        """
    }

    private func problem(status: Int, code: String?) -> APIError {
        .problem(
            ProblemDetails(
                type: "auth/invalid_grant",
                title: "Invalid Grant",
                status: status,
                requestId: "req_1",
                code: code,
                detail: nil,
                instance: nil
            )
        )
    }

    // MARK: - Refreshing only when needed

    @Test func aStillValidSessionIsReturnedWithoutAnyNetworkCall() async throws {
        let api = ScriptedAPI()
        let store = RecordingCredentialStore(seeded: credentials(sessionExpiresIn: 600))
        let coordinator = try makeCoordinator(api: api, store: store)

        #expect(try await coordinator.validSession().token == "at_1")
        #expect(api.calls.isEmpty)
    }

    /// The skew window exists so a token cannot die in flight; a session inside
    /// it is refreshed rather than used.
    @Test func aSessionInsideTheSkewWindowIsRefreshed() async throws {
        let api = ScriptedAPI()
        api.stub("auth/device/refresh", json: refreshJSON())
        let store = RecordingCredentialStore(seeded: credentials(sessionExpiresIn: AccessSession.expirySkew - 1))
        let coordinator = try makeCoordinator(api: api, store: store)

        #expect(try await coordinator.validSession().token == "at_2")
    }

    @Test func refreshingWithoutAGrantIsNotEnrolled() async throws {
        let coordinator = try makeCoordinator(api: ScriptedAPI(), store: RecordingCredentialStore())

        await #expect(throws: SessionCoordinator.Failure.notEnrolled) {
            _ = try await coordinator.refresh()
        }
    }

    // MARK: - Rotation

    @Test func refreshRotatesBothHalvesInOneCommit() async throws {
        let api = ScriptedAPI()
        api.stub("auth/device/refresh", json: refreshJSON())
        let store = RecordingCredentialStore(seeded: credentials(sessionExpiresIn: -1))
        let coordinator = try makeCoordinator(api: api, store: store)

        let rotated = try await coordinator.refresh()

        let commits = await store.commits
        #expect(commits.count == 1)
        #expect(rotated.grant.secret == "rt_2")
        #expect(rotated.session.token == "at_2")
        #expect(rotated.session.expiresAt == now.addingTimeInterval(900))
        // The device identity survives rotation; only the secrets move.
        #expect(rotated.grant.id == "dev_abc")
    }

    /// The superseded refresh token must never be presented again — the server
    /// reads that as replay and revokes the entire family.
    @Test func theSupersededRefreshTokenIsNeverSentAgain() async throws {
        let api = ScriptedAPI()
        api.stub("auth/device/refresh", json: refreshJSON(accessToken: "at_2", refreshToken: "rt_2"))
        api.stub("auth/device/refresh", json: refreshJSON(accessToken: "at_3", refreshToken: "rt_3"))
        let store = RecordingCredentialStore(seeded: credentials(refreshToken: "rt_1", sessionExpiresIn: -1))
        let coordinator = try makeCoordinator(api: api, store: store)

        try await coordinator.refresh()
        try await coordinator.refresh()

        let sent = api.calls
            .filter { $0.path == "auth/device/refresh" }
            .compactMap { $0.body.flatMap { try? JSONSerialization.jsonObject(with: $0) as? [String: Any] } }
            .compactMap { $0["refresh_token"] as? String }

        #expect(sent == ["rt_1", "rt_2"], "each refresh must present the token the previous one returned")
    }

    /// Two callers noticing an expired token at the same moment must not each
    /// spend the same refresh token: the second would be a replay, and the
    /// server revokes the whole family for that.
    @Test func concurrentRefreshesCollapseIntoOneRotation() async throws {
        let api = ScriptedAPI()
        api.stub("auth/device/refresh", json: refreshJSON())
        let store = RecordingCredentialStore(seeded: credentials(sessionExpiresIn: -1))
        let coordinator = try makeCoordinator(api: api, store: store)

        let tokens = await withTaskGroup(of: String?.self, returning: [String].self) { group in
            for _ in 0..<8 {
                group.addTask { try? await coordinator.validSession().token }
            }
            var collected: [String] = []
            for await token in group { if let token { collected.append(token) } }
            return collected
        }

        #expect(api.calls.filter { $0.path == "auth/device/refresh" }.count == 1)
        #expect(tokens.count == 8)
        #expect(tokens.allSatisfy { $0 == "at_2" })
        #expect(await store.commits.count == 1)
    }

    // MARK: - Terminal states

    /// A rejected grant means the family is gone server-side. Retrying is not
    /// merely useless — a loop against a revoked family looks like an attack.
    @Test func aRejectedGrantIsTerminalAndStopsTouchingTheNetwork() async throws {
        let api = ScriptedAPI()
        api.stub("auth/device/refresh", failure: problem(status: 401, code: "UNAUTHORIZED"))
        let store = RecordingCredentialStore(seeded: credentials(sessionExpiresIn: -1))
        let coordinator = try makeCoordinator(api: api, store: store)

        await #expect(throws: SessionCoordinator.Failure.reauthenticationRequired(.refreshRejected)) {
            _ = try await coordinator.refresh()
        }
        #expect(await coordinator.requiresReauthentication)

        let callsAfterFirstFailure = api.calls.count
        await #expect(throws: SessionCoordinator.Failure.reauthenticationRequired(.refreshRejected)) {
            _ = try await coordinator.validSession()
        }
        #expect(api.calls.count == callsAfterFirstFailure, "a terminal state must be answered locally")
        #expect(await store.commits.isEmpty)
    }

    /// 409 `DEVICE_REAUTH_REQUIRED` on an ordinary request means no token this
    /// device can produce will ever be accepted. It must route to re-pairing,
    /// never into a refresh.
    @Test func deviceReauthRequiredIsTerminalAndNeverRefreshes() async throws {
        let api = ScriptedAPI()
        api.stub("auth/device/refresh", json: refreshJSON())
        let store = RecordingCredentialStore(seeded: credentials(sessionExpiresIn: -1))
        let coordinator = try makeCoordinator(api: api, store: store)

        await coordinator.noteRequestFailure(problem(status: 409, code: "DEVICE_REAUTH_REQUIRED"))

        #expect(await coordinator.requiresReauthentication)
        await #expect(throws: SessionCoordinator.Failure.reauthenticationRequired(.deviceRepairRequired)) {
            _ = try await coordinator.validSession()
        }
        #expect(api.calls.isEmpty, "a device that must re-pair must not attempt a refresh")
    }

    @Test func anOrdinaryServerErrorIsNotTerminal() async throws {
        let api = ScriptedAPI()
        let store = RecordingCredentialStore(seeded: credentials(sessionExpiresIn: 600))
        let coordinator = try makeCoordinator(api: api, store: store)

        await coordinator.noteRequestFailure(problem(status: 503, code: "SERVICE_UNAVAILABLE"))
        await coordinator.noteRequestFailure(.transport(.offline))

        #expect(await coordinator.requiresReauthentication == false)
        #expect(try await coordinator.validSession().token == "at_1")
    }

    /// A transport failure says nothing about the grant's validity, so it must
    /// not burn the credential set.
    @Test func aTransportFailureDuringRefreshIsNotTerminal() async throws {
        let api = ScriptedAPI()
        api.stub("auth/device/refresh", failure: .transport(.timedOut))
        api.stub("auth/device/refresh", json: refreshJSON())
        let store = RecordingCredentialStore(seeded: credentials(sessionExpiresIn: -1))
        let coordinator = try makeCoordinator(api: api, store: store)

        await #expect(throws: APIError.transport(.timedOut)) { _ = try await coordinator.refresh() }
        #expect(await coordinator.requiresReauthentication == false)

        #expect(try await coordinator.refresh().session.token == "at_2")
    }

    @Test func reenrollmentIsTheOnlyWayOutOfATerminalState() async throws {
        let api = ScriptedAPI()
        api.stub("auth/device/refresh", json: refreshJSON())
        let store = RecordingCredentialStore(seeded: credentials(sessionExpiresIn: -1))
        let coordinator = try makeCoordinator(api: api, store: store)

        await coordinator.noteRequestFailure(problem(status: 409, code: "DEVICE_REAUTH_REQUIRED"))
        await coordinator.resetAfterReenrollment()

        #expect(await coordinator.requiresReauthentication == false)
        #expect(try await coordinator.validSession().token == "at_2")
    }

    // MARK: - The refresh response is validated

    @Test func aBearerRefreshIsRefusedRatherThanStored() async throws {
        try await expectRefreshDefect(.notSenderConstrained, json: refreshJSON(tokenType: "Bearer"))
    }

    @Test func anEmptyAccessTokenIsRefused() async throws {
        try await expectRefreshDefect(.emptyAccessToken, json: refreshJSON(accessToken: ""))
    }

    /// Without a new refresh token the device could only ever present the old
    /// one again, which is exactly the replay the server punishes.
    @Test func anEmptyRefreshTokenIsRefused() async throws {
        try await expectRefreshDefect(.emptyRefreshToken, json: refreshJSON(refreshToken: ""))
    }

    @Test func aNonPositiveLifetimeIsRefused() async throws {
        try await expectRefreshDefect(.nonPositiveLifetime, json: refreshJSON(expiresIn: 0))
    }

    private func expectRefreshDefect(
        _ defect: SessionCoordinator.Failure.Defect,
        json: String
    ) async throws {
        let api = ScriptedAPI()
        api.stub("auth/device/refresh", json: json)
        let store = RecordingCredentialStore(seeded: credentials(sessionExpiresIn: -1))
        let coordinator = try makeCoordinator(api: api, store: store)

        await #expect(throws: SessionCoordinator.Failure.malformedRefresh(defect)) {
            _ = try await coordinator.refresh()
        }
        #expect(await store.commits.isEmpty, "a malformed refresh must leave the old credentials in place")
    }
}

// MARK: - The refresh authorizer

struct DeviceProofAuthorizerTests {

    private func keyStore() -> SecureEnclaveDeviceKeyStore {
        SecureEnclaveDeviceKeyStore(
            policy: .allowSoftware(reason: .development),
            tag: "io.github.manugh.xg2g.tests.proofauth.\(UUID().uuidString)",
            secureEnclaveProbe: { false }
        )
    }

    /// The refresh route authenticates the device key alone. An `Authorization`
    /// header would at best be ignored — and at the moment refresh matters, the
    /// token it would carry is the expired one.
    @Test func itProvesTheDeviceKeyAndSendsNoAuthorizationHeader() async throws {
        let store = keyStore()
        defer { Task { try? await store.destroyKey() } }
        let authorizer = DeviceProofAuthorizer(keyStore: store)

        var request = URLRequest(url: try #require(URL(string: "https://tv.example/api/v3/auth/device/refresh")))
        request.httpMethod = "POST"

        let authorized = try await authorizer.authorized(request)

        #expect(authorized.value(forHTTPHeaderField: "Authorization") == nil)
        let proof = try #require(authorized.value(forHTTPHeaderField: "DPoP"))
        #expect(proof.split(separator: ".", omittingEmptySubsequences: false).count == 3)
    }

    /// The server validates this proof with an empty access token, so an `ath`
    /// claim has nothing to bind to.
    @Test func theProofCarriesNoATH() async throws {
        let store = keyStore()
        defer { Task { try? await store.destroyKey() } }
        let authorizer = DeviceProofAuthorizer(keyStore: store)

        var request = URLRequest(url: try #require(URL(string: "https://tv.example/api/v3/auth/device/refresh")))
        request.httpMethod = "POST"
        let signed = try await authorizer.authorized(request)
        let proof = try #require(signed.value(forHTTPHeaderField: "DPoP"))

        let parts = proof.split(separator: ".", omittingEmptySubsequences: false)
        var encoded = String(parts[1]).replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        while encoded.count % 4 != 0 { encoded += "=" }
        let payloadData = try #require(Data(base64Encoded: encoded))
        let payload = try #require(JSONSerialization.jsonObject(with: payloadData) as? [String: Any])

        #expect(payload["ath"] == nil)
        #expect(payload["htm"] as? String == "POST")
        #expect(payload["htu"] as? String == "https://tv.example/api/v3/auth/device/refresh")
    }
}

// MARK: - Single-flight under a slow server

/// Holds every call open until released, so the window in which a refresh is
/// in flight can be inspected deterministically.
final class GatedAPI: APIClient, @unchecked Sendable {

    private let gate = Gate()
    private let lock = NSLock()
    private var started = 0
    private let json: String

    init(json: String) { self.json = json }

    var startedCalls: Int {
        lock.lock(); defer { lock.unlock() }
        return started
    }

    func release() async { await gate.release() }

    /// Non-async so the lock is never held across a suspension point.
    private func noteStart() {
        lock.lock(); defer { lock.unlock() }
        started += 1
    }

    func send<Response>(_ request: APIRequest<Response>) async throws -> Response where Response: Decodable & Sendable {
        noteStart()
        await gate.wait()
        return try JSONDecoder.xg2g.decode(Response.self, from: Data(json.utf8))
    }

    actor Gate {
        private var isReleased = false
        private var waiters: [CheckedContinuation<Void, Never>] = []

        func wait() async {
            if isReleased { return }
            await withCheckedContinuation { waiters.append($0) }
        }

        func release() {
            isReleased = true
            for waiter in waiters { waiter.resume() }
            waiters = []
        }
    }
}

struct RefreshSingleFlightTests {

    /// A caller arriving while a rotation is still in flight must join it
    /// rather than spend the same refresh token a second time — the server
    /// reads the loser as replay and revokes the entire family. The earlier
    /// test covers callers that arrive together; this one holds the server open
    /// so the window is real rather than incidental.
    ///
    /// The first caller is cancelled to show that this does not depend on it
    /// staying interested. Note what that does *not* prove: `Task.value` is not
    /// cancelled by the task awaiting it, so the abandoned caller goes on
    /// waiting for the rotation to finish regardless. This test therefore does
    /// not distinguish clearing the in-flight flag in the work from clearing it
    /// in the caller — measured, not assumed.
    @Test func aCallerArrivingMidRotationJoinsIt() async throws {
        let json = """
            {"token_type":"DPoP","access_token":"at_2","refresh_token":"rt_2",
             "expires_in":900,"device_id":"dev_abc","scope":"stream"}
            """
        let api = GatedAPI(json: json)
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        let store = RecordingCredentialStore(
            seeded: EnrolledCredentials(
                grant: DeviceGrant(id: "dev_abc", secret: "rt_1", expiresAt: nil),
                session: AccessSession(sessionID: "dev_abc", token: "at_1", expiresAt: now, policyVersion: nil)
            )
        )
        let coordinator = SessionCoordinator(
            identity: .address(try ServerAddressParser.parseTrusted("https://tv.example/")),
            api: api,
            credentials: store,
            now: { now }
        )

        let abandoned = Task { try await coordinator.refresh() }
        while api.startedCalls == 0 { await Task.yield() }
        abandoned.cancel()

        // A second caller arrives while the first rotation is still in flight.
        let joined = Task { try await coordinator.refresh() }
        await Task.yield()
        await api.release()

        _ = try await joined.value
        _ = try? await abandoned.value

        #expect(api.startedCalls == 1, "the second caller must join the rotation, not start another")
        #expect(await store.commits.count == 1)
    }
}
