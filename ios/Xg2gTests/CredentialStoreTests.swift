// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

// MARK: - Doubles

final class InMemoryKeychainBackend: KeychainBackend, @unchecked Sendable {
    private let lock = NSLock()
    private var storage: [String: Data] = [:]

    private func key(_ service: String, _ account: String) -> String {
        // U+0000 cannot occur in either component, so this cannot alias.
        "\(service)\u{0}\(account)"
    }

    func set(_ data: Data, service: String, account: String) throws {
        lock.lock(); defer { lock.unlock() }
        storage[key(service, account)] = data
    }

    func data(service: String, account: String) throws -> Data? {
        lock.lock(); defer { lock.unlock() }
        return storage[key(service, account)]
    }

    func remove(service: String, account: String) throws {
        lock.lock(); defer { lock.unlock() }
        storage.removeValue(forKey: key(service, account))
    }

    func removeAll(service: String) throws {
        lock.lock(); defer { lock.unlock() }
        storage = storage.filter { !$0.key.hasPrefix("\(service)\u{0}") }
    }

    var count: Int {
        lock.lock(); defer { lock.unlock() }
        return storage.count
    }

    func seedRaw(_ data: Data, service: String, account: String) {
        lock.lock(); defer { lock.unlock() }
        storage[key(service, account)] = data
    }
}

final class FakeInstallationMarker: InstallationMarkerStore, @unchecked Sendable {
    private let lock = NSLock()
    private var marked: Bool

    init(marked: Bool) { self.marked = marked }

    func isMarked() -> Bool {
        lock.lock(); defer { lock.unlock() }
        return marked
    }

    func mark() {
        lock.lock(); defer { lock.unlock() }
        marked = true
    }
}

// MARK: - Tests

struct CredentialStoreTests {

    private func identity(_ raw: String) throws -> ServerIdentity {
        .address(try ServerAddressParser.parseTrusted(raw))
    }

    private func grant(id: String = "dgr-1") -> DeviceGrant {
        DeviceGrant(id: id, secret: "secret", expiresAt: Date(timeIntervalSince1970: 2_000_000_000))
    }

    private func session(token: String = "token") -> AccessSession {
        AccessSession(
            sessionID: "sess-1",
            token: token,
            expiresAt: Date(timeIntervalSince1970: 2_000_000_000),
            policyVersion: "v1"
        )
    }

    private func makeStore(
        backend: InMemoryKeychainBackend = InMemoryKeychainBackend(),
        marked: Bool = true
    ) -> (KeychainCredentialStore, InMemoryKeychainBackend) {
        (KeychainCredentialStore(backend: backend, marker: FakeInstallationMarker(marked: marked)), backend)
    }

    // MARK: - Reinstall purge

    /// The Keychain outlives app deletion; `UserDefaults` does not. A missing
    /// marker therefore means a fresh installation looking at a previous
    /// installation's credentials.
    @Test func freshInstallPurgesSurvivingKeychainMaterial() async throws {
        let backend = InMemoryKeychainBackend()
        backend.seedRaw(Data("stale".utf8), service: CredentialKeyEncoding.service, account: "v1.addr.old.device_grant")
        let (store, _) = makeStore(backend: backend, marked: false)

        try await store.prepareForLaunch()

        #expect(backend.count == 0)
    }

    @Test func reopeningAnExistingInstallKeepsCredentials() async throws {
        let backend = InMemoryKeychainBackend()
        backend.seedRaw(Data("kept".utf8), service: CredentialKeyEncoding.service, account: "v1.addr.old.device_grant")
        let (store, _) = makeStore(backend: backend, marked: true)

        try await store.prepareForLaunch()

        #expect(backend.count == 1)
    }

    @Test func purgeCoversOnlyOurOwnService() async throws {
        let backend = InMemoryKeychainBackend()
        backend.seedRaw(Data("ours".utf8), service: CredentialKeyEncoding.service, account: "a")
        backend.seedRaw(Data("theirs".utf8), service: "com.other.app", account: "a")
        let (store, _) = makeStore(backend: backend, marked: false)

        try await store.prepareForLaunch()

        #expect(try backend.data(service: "com.other.app", account: "a") != nil)
    }

    // MARK: - Preparation is mandatory

    @Test func readingBeforePreparationThrows() async throws {
        let (store, _) = makeStore()

        await #expect(throws: CredentialStoreError.notPrepared) {
            _ = try await store.deviceGrant(for: try identity("https://tv.example/"))
        }
    }

    @Test func writingBeforePreparationThrows() async throws {
        let (store, _) = makeStore()

        await #expect(throws: CredentialStoreError.notPrepared) {
            try await store.store(grant(), for: try identity("https://tv.example/"))
        }
    }

    // MARK: - Round trips

    @Test func grantAndSessionRoundTrip() async throws {
        let (store, _) = makeStore()
        try await store.prepareForLaunch()
        let server = try identity("https://tv.example/")

        try await store.store(grant(), for: server)
        try await store.store(session(), for: server)

        #expect(try await store.deviceGrant(for: server) == grant())
        #expect(try await store.accessSession(for: server) == session())
    }

    @Test func unknownIdentityReadsAsNil() async throws {
        let (store, _) = makeStore()
        try await store.prepareForLaunch()

        #expect(try await store.deviceGrant(for: try identity("https://unknown.example/")) == nil)
    }

    @Test func identitiesAreIsolatedFromEachOther() async throws {
        let (store, _) = makeStore()
        try await store.prepareForLaunch()
        let home = try identity("https://example.com/home/")
        let lab = try identity("https://example.com/lab/")

        try await store.store(grant(id: "home-grant"), for: home)
        try await store.store(grant(id: "lab-grant"), for: lab)

        #expect(try await store.deviceGrant(for: home)?.id == "home-grant")
        #expect(try await store.deviceGrant(for: lab)?.id == "lab-grant")
    }

    // MARK: - The three lifecycle operations

    /// Logout drops the session and keeps the pairing.
    @Test func endSessionKeepsTheDeviceGrant() async throws {
        let (store, _) = makeStore()
        try await store.prepareForLaunch()
        let server = try identity("https://tv.example/")
        try await store.store(grant(), for: server)
        try await store.store(session(), for: server)

        try await store.endSession(for: server)

        #expect(try await store.accessSession(for: server) == nil)
        #expect(try await store.deviceGrant(for: server) == grant())
    }

    /// Forget server drops everything for that identity — and nothing else.
    @Test func forgetServerClearsOnlyThatIdentity() async throws {
        let (store, _) = makeStore()
        try await store.prepareForLaunch()
        let gone = try identity("https://gone.example/")
        let kept = try identity("https://kept.example/")
        try await store.store(grant(), for: gone)
        try await store.store(session(), for: gone)
        try await store.store(grant(id: "kept"), for: kept)

        try await store.forgetServer(gone)

        #expect(try await store.deviceGrant(for: gone) == nil)
        #expect(try await store.accessSession(for: gone) == nil)
        #expect(try await store.deviceGrant(for: kept)?.id == "kept")
    }

    @Test func purgeEverythingClearsAllIdentities() async throws {
        let (store, backend) = makeStore()
        try await store.prepareForLaunch()
        try await store.store(grant(), for: try identity("https://a.example/"))
        try await store.store(grant(), for: try identity("https://b.example/"))

        try await store.purgeEverything()

        #expect(backend.count == 0)
    }

    // MARK: - Identity migration

    @Test func migrationMovesCredentialsToTheStrongerIdentity() async throws {
        let (store, _) = makeStore()
        try await store.prepareForLaunch()
        let addressBound = try identity("https://tv.example/")
        let instanceBound = ServerIdentity.instance(try InstanceID("instance-aaaa"))

        try await store.store(grant(), for: addressBound)
        try await store.store(session(), for: addressBound)

        try await store.migrate(from: addressBound, to: instanceBound)

        #expect(try await store.deviceGrant(for: instanceBound) == grant())
        #expect(try await store.accessSession(for: instanceBound) == session())
        #expect(try await store.deviceGrant(for: addressBound) == nil)
        #expect(try await store.accessSession(for: addressBound) == nil)
    }

    @Test func migrationToTheSameIdentityIsANoOp() async throws {
        let (store, _) = makeStore()
        try await store.prepareForLaunch()
        let server = try identity("https://tv.example/")
        try await store.store(grant(), for: server)

        try await store.migrate(from: server, to: server)

        #expect(try await store.deviceGrant(for: server) == grant())
    }

    @Test func migrationOfAnEmptyIdentityDoesNothing() async throws {
        let (store, backend) = makeStore()
        try await store.prepareForLaunch()

        try await store.migrate(
            from: try identity("https://tv.example/"),
            to: .instance(try InstanceID("instance-aaaa"))
        )

        #expect(backend.count == 0)
    }

    // MARK: - Corrupted storage

    @Test func malformedStoredValueIsReportedNotIgnored() async throws {
        let backend = InMemoryKeychainBackend()
        let (store, _) = makeStore(backend: backend)
        try await store.prepareForLaunch()
        let server = try identity("https://tv.example/")

        backend.seedRaw(
            Data("not json".utf8),
            service: CredentialKeyEncoding.service,
            account: CredentialKeyEncoding.account(for: server, kind: .deviceGrant)
        )

        await #expect(throws: CredentialStoreError.malformedStoredValue(.deviceGrant)) {
            _ = try await store.deviceGrant(for: server)
        }
    }
}

struct AccessSessionTests {

    private func session(expiresIn seconds: TimeInterval, token: String = "t") -> AccessSession {
        AccessSession(
            sessionID: "s",
            token: token,
            expiresAt: Date(timeIntervalSince1970: 1_000_000).addingTimeInterval(seconds),
            policyVersion: nil
        )
    }

    private let now = Date(timeIntervalSince1970: 1_000_000)

    @Test func aComfortablyValidSessionIsUsable() {
        #expect(session(expiresIn: 600).isUsable(at: now))
    }

    @Test func anExpiredSessionIsNotUsable() {
        #expect(!session(expiresIn: -1).isUsable(at: now))
    }

    /// A token that dies mid-flight is worse than one refreshed slightly early.
    @Test func aSessionInsideTheSkewWindowIsNotUsable() {
        #expect(!session(expiresIn: AccessSession.expirySkew - 1).isUsable(at: now))
        #expect(session(expiresIn: AccessSession.expirySkew + 1).isUsable(at: now))
    }

    @Test func anEmptyTokenIsNeverUsable() {
        #expect(!session(expiresIn: 600, token: "").isUsable(at: now))
    }
}
