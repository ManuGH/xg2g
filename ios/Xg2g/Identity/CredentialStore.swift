// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Typed access to stored credentials.
///
/// ## Invariant
///
/// **No caller ever sees a storage key or a namespace string.** Every operation
/// is expressed in domain terms — "the grant for this identity", "this session
/// was refreshed". Without that rule `CredentialKeyEncoding` protects nothing:
/// the storage representation simply leaks out through whoever composes a key
/// by hand, and from then on the format is load-bearing everywhere.
///
/// ## What this deliberately does not offer
///
/// There is no `revoke`. Revoking a device is remote-first — revoke server-side,
/// wait for success or a defined terminal failure, and only then destroy local
/// material. A local-first `revoke` here would be an attractive nuisance: the
/// key needed to authenticate the revocation would already be gone, leaving a
/// grant that is valid on the server and no longer revocable from this device.
/// That sequencing belongs to the auth coordinator, which owns the network call.
///
/// Nothing here touches the DPoP device key either; that is `DeviceKeyStore`'s
/// property and has its own lifecycle.
protocol CredentialStore: Sendable {

    /// Must run before any read. On a fresh install it purges Keychain material
    /// that survived a previous installation.
    func prepareForLaunch() async throws

    func deviceGrant(for identity: ServerIdentity) async throws -> DeviceGrant?
    func accessSession(for identity: ServerIdentity) async throws -> AccessSession?

    /// Commits a complete credential set from an exchange or a refresh.
    ///
    /// There is deliberately no way to store a grant or a session on its own.
    /// Both arrive from one server response, so a call that wrote only one of
    /// them could only ever produce a state the server never issued.
    func commit(_ credentials: EnrolledCredentials, for identity: ServerIdentity) async throws

    /// Log out: drop the session, keep the pairing.
    func endSession(for identity: ServerIdentity) async throws

    /// Forget server: drop every credential for this identity. Keeps the device
    /// key, which is not server-specific.
    func forgetServer(_ identity: ServerIdentity) async throws

    /// Re-key credentials after an identity upgrade (`.address` → `.instance`).
    func migrate(from old: ServerIdentity, to new: ServerIdentity) async throws

    /// Drop everything this app owns.
    func purgeEverything() async throws
}

enum CredentialStoreError: Error, Equatable, Sendable {
    /// A read was attempted before ``CredentialStore/prepareForLaunch()``.
    case notPrepared
    case keychain(OSStatus)
    case malformedStoredValue(CredentialKind)
    case encodingFailed(CredentialKind)
}

// MARK: - Keychain backend

/// The raw key/value surface `KeychainCredentialStore` needs.
///
/// Extracted so the store's lifecycle rules can be tested without a Keychain,
/// and so the real `SecItem` attributes are testable on their own.
protocol KeychainBackend: Sendable {
    func set(_ data: Data, service: String, account: String) throws
    func data(service: String, account: String) throws -> Data?
    func remove(service: String, account: String) throws
    func removeAll(service: String) throws
}

/// Records whether this *installation* has run before.
///
/// Backed by `UserDefaults`, which is erased when the app is deleted — unlike
/// the Keychain, which is not. That asymmetry is the whole detection mechanism.
protocol InstallationMarkerStore: Sendable {
    func isMarked() -> Bool
    func mark()
}

struct UserDefaultsInstallationMarker: InstallationMarkerStore {
    private let key = "io.github.manugh.xg2g.installationMarker"

    // `UserDefaults` is documented as thread-safe but is not annotated
    // `Sendable`, so the guarantee has to be asserted here rather than derived.
    nonisolated(unsafe) private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    func isMarked() -> Bool { defaults.bool(forKey: key) }
    func mark() { defaults.set(true, forKey: key) }
}

// MARK: - Store

actor KeychainCredentialStore: CredentialStore {

    private let backend: KeychainBackend
    private let marker: InstallationMarkerStore
    private var isPrepared = false

    private let encoder: JSONEncoder = {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        return encoder
    }()

    private let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }()

    init(backend: KeychainBackend, marker: InstallationMarkerStore = UserDefaultsInstallationMarker()) {
        self.backend = backend
        self.marker = marker
    }

    func prepareForLaunch() throws {
        if !marker.isMarked() {
            // A fresh installation that nevertheless finds Keychain entries is
            // looking at a previous installation's credentials. Purge by
            // service: at this point we do not know which identities were ever
            // stored, so enumerating them is not possible.
            try wrap { try backend.removeAll(service: CredentialKeyEncoding.service) }
            marker.mark()
        }
        isPrepared = true
    }

    func deviceGrant(for identity: ServerIdentity) throws -> DeviceGrant? {
        try load(DeviceGrant.self, kind: .deviceGrant, identity: identity)
    }

    func accessSession(for identity: ServerIdentity) throws -> AccessSession? {
        try load(AccessSession.self, kind: .accessSession, identity: identity)
    }

    func commit(_ credentials: EnrolledCredentials, for identity: ServerIdentity) throws {
        try requirePrepared()

        // Two Keychain items and no transaction spanning them, so the write
        // order *is* the guarantee. The grant goes first: a grant without a
        // session is an ordinary state the app already handles — it is what
        // every cold start after token expiry looks like, and the refresh path
        // recovers from it without the user noticing. A session without a grant
        // is a dead end: nothing can renew it and, once it expires, nothing can
        // revoke it either. Writing in this order means an interrupted commit
        // can only leave behind the recoverable half.
        try save(credentials.grant, kind: .deviceGrant, identity: identity)
        try save(credentials.session, kind: .accessSession, identity: identity)
    }

    func endSession(for identity: ServerIdentity) throws {
        try requirePrepared()
        try remove(kind: .accessSession, identity: identity)
        try remove(kind: .accessToken, identity: identity)
    }

    func forgetServer(_ identity: ServerIdentity) throws {
        try requirePrepared()
        for kind in CredentialKind.allCases {
            try remove(kind: kind, identity: identity)
        }
    }

    func migrate(from old: ServerIdentity, to new: ServerIdentity) throws {
        try requirePrepared()
        guard old != new else { return }

        // Write the new entries before removing the old ones. An interruption
        // then leaves a harmless duplicate rather than no credentials at all;
        // the stale copy is cleaned up by a later forget or reinstall purge.
        for kind in CredentialKind.allCases {
            let account = CredentialKeyEncoding.account(for: old, kind: kind)
            guard let data = try wrap({ try backend.data(service: CredentialKeyEncoding.service, account: account) }) else {
                continue
            }
            try wrap {
                try backend.set(
                    data,
                    service: CredentialKeyEncoding.service,
                    account: CredentialKeyEncoding.account(for: new, kind: kind)
                )
            }
        }

        for kind in CredentialKind.allCases {
            try remove(kind: kind, identity: old)
        }
    }

    func purgeEverything() throws {
        try wrap { try backend.removeAll(service: CredentialKeyEncoding.service) }
    }

    // MARK: - Internals

    private func requirePrepared() throws {
        guard isPrepared else { throw CredentialStoreError.notPrepared }
    }

    private func load<T: Decodable>(_ type: T.Type, kind: CredentialKind, identity: ServerIdentity) throws -> T? {
        try requirePrepared()
        let account = CredentialKeyEncoding.account(for: identity, kind: kind)
        guard let data = try wrap({ try backend.data(service: CredentialKeyEncoding.service, account: account) }) else {
            return nil
        }
        guard let value = try? decoder.decode(T.self, from: data) else {
            throw CredentialStoreError.malformedStoredValue(kind)
        }
        return value
    }

    private func save<T: Encodable>(_ value: T, kind: CredentialKind, identity: ServerIdentity) throws {
        try requirePrepared()
        guard let data = try? encoder.encode(value) else {
            throw CredentialStoreError.encodingFailed(kind)
        }
        try wrap {
            try backend.set(
                data,
                service: CredentialKeyEncoding.service,
                account: CredentialKeyEncoding.account(for: identity, kind: kind)
            )
        }
    }

    private func remove(kind: CredentialKind, identity: ServerIdentity) throws {
        try wrap {
            try backend.remove(
                service: CredentialKeyEncoding.service,
                account: CredentialKeyEncoding.account(for: identity, kind: kind)
            )
        }
    }

    @discardableResult
    private func wrap<T>(_ body: () throws -> T) throws -> T {
        do {
            return try body()
        } catch let error as CredentialStoreError {
            throw error
        } catch {
            throw CredentialStoreError.keychain(errSecInternalError)
        }
    }
}
