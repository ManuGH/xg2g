// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Security
import Testing
@testable import Xg2g

/// Exercises the real Keychain. The fake backend proves the store's lifecycle
/// rules; only these prove that the actual `SecItem` attributes are what we
/// claim — in particular the accessibility class, which is a security property
/// and therefore has to be read back rather than asserted in a comment.
///
/// Each test uses its own service name so runs cannot interfere with each other
/// or with the app's real entries.
struct SecItemKeychainBackendTests {

    private let backend = SecItemKeychainBackend()

    private func makeService(_ label: String) -> String {
        "io.github.manugh.xg2g.tests.\(label).\(UUID().uuidString)"
    }

    @Test func storesAndReadsBack() throws {
        let service = makeService("roundtrip")
        defer { try? backend.removeAll(service: service) }

        try backend.set(Data("hello".utf8), service: service, account: "a")

        #expect(try backend.data(service: service, account: "a") == Data("hello".utf8))
    }

    @Test func missingItemReadsAsNil() throws {
        let service = makeService("missing")

        #expect(try backend.data(service: service, account: "absent") == nil)
    }

    @Test func overwritingReplacesTheValue() throws {
        let service = makeService("overwrite")
        defer { try? backend.removeAll(service: service) }

        try backend.set(Data("first".utf8), service: service, account: "a")
        try backend.set(Data("second".utf8), service: service, account: "a")

        #expect(try backend.data(service: service, account: "a") == Data("second".utf8))
    }

    @Test func removingIsIdempotent() throws {
        let service = makeService("remove")

        try backend.set(Data("x".utf8), service: service, account: "a")
        try backend.remove(service: service, account: "a")
        // A second delete must not surface errSecItemNotFound as a failure.
        try backend.remove(service: service, account: "a")

        #expect(try backend.data(service: service, account: "a") == nil)
    }

    @Test func removeAllClearsTheServiceAndNothingElse() throws {
        let mine = makeService("wipe-mine")
        let other = makeService("wipe-other")
        defer { try? backend.removeAll(service: other) }

        try backend.set(Data("1".utf8), service: mine, account: "a")
        try backend.set(Data("2".utf8), service: mine, account: "b")
        try backend.set(Data("3".utf8), service: other, account: "a")

        try backend.removeAll(service: mine)

        #expect(try backend.data(service: mine, account: "a") == nil)
        #expect(try backend.data(service: mine, account: "b") == nil)
        #expect(try backend.data(service: other, account: "a") == Data("3".utf8))
    }

    @Test func accountsAreIsolated() throws {
        let service = makeService("accounts")
        defer { try? backend.removeAll(service: service) }

        try backend.set(Data("a".utf8), service: service, account: "a")
        try backend.set(Data("b".utf8), service: service, account: "b")

        #expect(try backend.data(service: service, account: "a") == Data("a".utf8))
        #expect(try backend.data(service: service, account: "b") == Data("b".utf8))
    }

    // MARK: - The security properties, read back from the Keychain

    /// `AfterFirstUnlock` so refresh and heartbeat survive a locked screen once
    /// background audio exists; `ThisDeviceOnly` so a device-bound grant cannot
    /// ride a sync or a backup onto another device.
    @Test func itemsAreAccessibleAfterFirstUnlockAndBoundToThisDevice() throws {
        let service = makeService("accessibility")
        defer { try? backend.removeAll(service: service) }

        try backend.set(Data("x".utf8), service: service, account: "a")

        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: "a",
            kSecReturnAttributes as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)

        #expect(status == errSecSuccess)
        let attributes = try #require(result as? [String: Any])
        #expect(
            attributes[kSecAttrAccessible as String] as? String
                == (kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly as String)
        )
    }

    @Test func itemsAreNotSynchronizable() throws {
        let service = makeService("sync")
        defer { try? backend.removeAll(service: service) }

        try backend.set(Data("x".utf8), service: service, account: "a")

        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: "a",
            kSecReturnAttributes as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var result: CFTypeRef?
        _ = SecItemCopyMatching(query as CFDictionary, &result)

        let attributes = try #require(result as? [String: Any])
        let synchronizable = attributes[kSecAttrSynchronizable as String] as? Bool ?? false
        #expect(!synchronizable)
    }
}
