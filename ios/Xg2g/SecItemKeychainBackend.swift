// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Security

/// The real Keychain, behind the narrow surface `KeychainCredentialStore` needs.
///
/// ## Accessibility
///
/// `kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly`, chosen deliberately:
///
/// - *AfterFirstUnlock* rather than *WhenUnlocked*, because heartbeat and token
///   refresh have to keep working while the screen is locked once background
///   audio playback exists. A `WhenUnlocked` item would fail exactly when live
///   TV is still playing in the user's pocket.
/// - *ThisDeviceOnly*, because a device-bound grant that rides an iCloud
///   Keychain sync or an encrypted backup to a second device is no longer
///   device-bound. That is the entire point of pairing a device.
///
/// Items are written non-synchronizable for the same reason. Reads and deletes
/// use `kSecAttrSynchronizableAny` so that anything a previous build might have
/// written as synchronizable is still found — and, more importantly, still
/// removable.
struct SecItemKeychainBackend: KeychainBackend {

    func set(_ data: Data, service: String, account: String) throws {
        // Delete first rather than SecItemUpdate: an existing item may carry
        // different accessibility attributes, and update would keep them.
        try remove(service: service, account: account)

        let attributes: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecValueData as String: data,
            kSecAttrAccessible as String: kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
            kSecAttrSynchronizable as String: false,
        ]

        let status = SecItemAdd(attributes as CFDictionary, nil)
        guard status == errSecSuccess else {
            throw CredentialStoreError.keychain(status)
        }
    }

    func data(service: String, account: String) throws -> Data? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecAttrSynchronizable as String: kSecAttrSynchronizableAny,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]

        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)

        switch status {
        case errSecSuccess:
            return result as? Data
        case errSecItemNotFound:
            return nil
        default:
            throw CredentialStoreError.keychain(status)
        }
    }

    func remove(service: String, account: String) throws {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecAttrSynchronizable as String: kSecAttrSynchronizableAny,
        ]

        let status = SecItemDelete(query as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw CredentialStoreError.keychain(status)
        }
    }

    func removeAll(service: String) throws {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrSynchronizable as String: kSecAttrSynchronizableAny,
        ]

        let status = SecItemDelete(query as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw CredentialStoreError.keychain(status)
        }
    }
}
