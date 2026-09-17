// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

/// Records what was destroyed, so the ordering rule can be asserted rather than
/// assumed.
actor DestructionLog: DeviceKeyStore {

    private(set) var destroyed = false
    private let inner: SecureEnclaveDeviceKeyStore

    init(inner: SecureEnclaveDeviceKeyStore) { self.inner = inner }

    func attestation() async throws -> DeviceKeyAttestation? { try await inner.attestation() }
    func provisionKey() async throws -> DeviceKeyAttestation { try await inner.provisionKey() }
    func sign(_ data: Data) async throws -> Data { try await inner.sign(data) }

    func destroyKey() async throws {
        destroyed = true
        try await inner.destroyKey()
    }
}

struct RevokeCoordinatorTests {

    private let now = Date(timeIntervalSince1970: 1_800_000_000)

    private func identity() throws -> ServerIdentity {
        .address(try ServerAddressParser.parseTrusted("https://tv.example/"))
    }

    private func enrolled() -> EnrolledCredentials {
        EnrolledCredentials(
            grant: DeviceGrant(id: "dev_abc", secret: "rt_1", expiresAt: nil),
            session: AccessSession(
                sessionID: "dev_abc",
                token: "at_1",
                expiresAt: now.addingTimeInterval(600),
                policyVersion: nil
            )
        )
    }

    private func keys() -> DestructionLog {
        DestructionLog(
            inner: SecureEnclaveDeviceKeyStore(
                policy: .allowSoftware(reason: .development),
                tag: "io.github.manugh.xg2g.tests.revoke.\(UUID().uuidString)",
                secureEnclaveProbe: { false }
            )
        )
    }

    private func makeCoordinator(
        api: ScriptedAPI,
        store: RecordingCredentialStore,
        keyStore: DestructionLog
    ) throws -> RevokeCoordinator {
        let identity = try identity()
        return RevokeCoordinator(
            identity: identity,
            api: api,
            credentials: store,
            keyStore: keyStore,
            session: SessionCoordinator(identity: identity, api: api, credentials: store, now: { self.now })
        )
    }

    private func problem(status: Int) -> APIError {
        .problem(
            ProblemDetails(
                type: "auth/unauthorized", title: "Unauthorized", status: status,
                requestId: "req_1", code: "UNAUTHORIZED", detail: nil, instance: nil
            )
        )
    }

    // MARK: - The happy path

    @Test func aConfirmedRevocationClearsEverything() async throws {
        let api = ScriptedAPI()
        api.stub("auth/device/revoke", json: "{}")
        let store = RecordingCredentialStore(seeded: enrolled())
        let keyStore = keys()
        let coordinator = try makeCoordinator(api: api, store: store, keyStore: keyStore)

        #expect(try await coordinator.revokeThisDevice() == .revoked)

        #expect(try await store.deviceGrant(for: identity()) == nil)
        #expect(try await store.accessSession(for: identity()) == nil)
        #expect(await store.forgotten.count == 1)
        #expect(await keyStore.destroyed)
    }

    /// The server no longer recognising this device is the end state the caller
    /// asked for, so it is a success — otherwise a device could be permanently
    /// unable to clean up after an admin removed it.
    @Test func aServerThatAlreadyForgotUsCountsAsSuccess() async throws {
        let api = ScriptedAPI()
        api.stub("auth/device/revoke", failure: problem(status: 401))
        let store = RecordingCredentialStore(seeded: enrolled())
        let keyStore = keys()
        let coordinator = try makeCoordinator(api: api, store: store, keyStore: keyStore)

        #expect(try await coordinator.revokeThisDevice() == .alreadyRevoked)
        #expect(try await store.deviceGrant(for: identity()) == nil)
        #expect(await keyStore.destroyed)
    }

    /// Re-pairing straight away keeps the key: it is this device's identity,
    /// not a credential the server issued.
    @Test func theKeyCanBeKeptForAnImmediateRePair() async throws {
        let api = ScriptedAPI()
        api.stub("auth/device/revoke", json: "{}")
        let store = RecordingCredentialStore(seeded: enrolled())
        let keyStore = keys()
        let coordinator = try makeCoordinator(api: api, store: store, keyStore: keyStore)

        try await coordinator.revokeThisDevice(destroyingDeviceKey: false)

        #expect(try await store.deviceGrant(for: identity()) == nil, "credentials always go")
        #expect(await keyStore.destroyed == false)
    }

    // MARK: - Remote-first is the whole point

    /// The key is what authenticates the revocation. Destroying it before the
    /// server confirms would leave a grant that is valid on the server and no
    /// longer revocable from here — the exact opposite of what "log out" means.
    @Test func nothingIsDestroyedWhenTheServerCannotBeReached() async throws {
        let api = ScriptedAPI()
        api.stub("auth/device/revoke", failure: .transport(.offline))
        let store = RecordingCredentialStore(seeded: enrolled())
        let keyStore = keys()
        let coordinator = try makeCoordinator(api: api, store: store, keyStore: keyStore)

        await #expect(throws: RevokeCoordinator.Failure.remoteRevocationFailed(.transport(.offline))) {
            _ = try await coordinator.revokeThisDevice()
        }

        #expect(try await store.deviceGrant(for: identity()) != nil, "the grant must survive an unanswered revocation")
        #expect(await keyStore.destroyed == false, "the key that authenticates the retry must survive")
    }

    /// A 5xx is the server failing, not the server confirming. Same rule.
    @Test func aServerErrorDestroysNothing() async throws {
        let api = ScriptedAPI()
        api.stub("auth/device/revoke", failure: problem(status: 503))
        let store = RecordingCredentialStore(seeded: enrolled())
        let keyStore = keys()
        let coordinator = try makeCoordinator(api: api, store: store, keyStore: keyStore)

        await #expect(throws: RevokeCoordinator.Failure.remoteRevocationFailed(problem(status: 503))) {
            _ = try await coordinator.revokeThisDevice()
        }
        #expect(try await store.deviceGrant(for: identity()) != nil)
        #expect(await keyStore.destroyed == false)
    }

    /// A failed attempt leaves everything intact, so the retry is a normal
    /// revocation rather than a special recovery path.
    @Test func aRetryAfterAFailureSucceedsNormally() async throws {
        let api = ScriptedAPI()
        api.stub("auth/device/revoke", failure: .transport(.timedOut))
        api.stub("auth/device/revoke", json: "{}")
        let store = RecordingCredentialStore(seeded: enrolled())
        let keyStore = keys()
        let coordinator = try makeCoordinator(api: api, store: store, keyStore: keyStore)

        await #expect(throws: (any Error).self) { _ = try await coordinator.revokeThisDevice() }
        #expect(try await coordinator.revokeThisDevice() == .revoked)
        #expect(await keyStore.destroyed)
    }

    @Test func revokingWithoutAnEnrollmentIsRefusedBeforeAnyCall() async throws {
        let api = ScriptedAPI()
        let store = RecordingCredentialStore()
        let coordinator = try makeCoordinator(api: api, store: store, keyStore: keys())

        await #expect(throws: RevokeCoordinator.Failure.notEnrolled) {
            _ = try await coordinator.revokeThisDevice()
        }
        #expect(api.calls.isEmpty)
    }

    /// The client sends no device identifier — the server derives it from the
    /// DPoP binding. A body here would be a request to be trusted about who we
    /// are.
    @Test func theRequestNamesNoDevice() async throws {
        let api = ScriptedAPI()
        api.stub("auth/device/revoke", json: "{}")
        let store = RecordingCredentialStore(seeded: enrolled())
        let coordinator = try makeCoordinator(api: api, store: store, keyStore: keys())

        try await coordinator.revokeThisDevice()

        let call = try #require(api.calls.first { $0.path == "auth/device/revoke" })
        #expect(call.method == .post)
        #expect(call.body == nil)
    }
}
