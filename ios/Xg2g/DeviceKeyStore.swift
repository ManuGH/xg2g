// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Security

/// The device's cryptographic identity.
///
/// ## Invariants
///
/// 1. **Provenance is part of the API.** Callers always know whether they hold
///    a Secure Enclave key or a software key.
/// 2. **A software key never silently substitutes for a hardware key.** It is
///    created only under an explicit ``DeviceKeyPolicy/allowSoftware(reason:)``,
///    which a real iOS build cannot even construct. If a key exists whose
///    provenance is weaker than the policy, every operation fails rather than
///    proceeding.
/// 3. **The private key never leaves this type.** The surface is "sign these
///    bytes"; no `SecKey`, no key data, no Security-framework object is handed
///    out. DPoP proof construction is built entirely on top of `sign(_:)`.
///
/// This is deliberately *not* a "DPoP provider". Only the non-exportable key
/// lives in the Secure Enclave; JWT assembly, `jti`, `htm`/`htu`, `ath` and any
/// nonce handling are ordinary software concerns belonging to a layer above.
protocol DeviceKeyStore: Sendable {
    /// The existing key's attestation, or `nil` if none has been provisioned.
    func attestation() async throws -> DeviceKeyAttestation?

    /// Returns the existing key, creating one if absent.
    func provisionKey() async throws -> DeviceKeyAttestation

    /// ES256 signature over `data`, as raw `R || S`, exactly 64 bytes.
    func sign(_ data: Data) async throws -> Data

    /// Destroys the device identity.
    ///
    /// Separate from credential clearing on purpose: this is not a credential
    /// the server issued but an identity this device owns, and it must survive
    /// a logout or a forgotten server. Revocation is remote-first — the server
    /// grant is revoked while this key still exists to authenticate the
    /// revocation, and only then is the key destroyed.
    func destroyKey() async throws
}

actor SecureEnclaveDeviceKeyStore: DeviceKeyStore {

    private let policy: DeviceKeyPolicy
    private let tag: Data
    private let secureEnclaveProbe: @Sendable () -> Bool

    /// - Parameter secureEnclaveProbe: overridable so the "a software key is
    ///   never accepted under a hardware policy" invariant can be exercised on
    ///   machines that *do* have a Secure Enclave — which now includes the
    ///   simulator on Apple Silicon. Without the seam that test silently
    ///   no-ops, and a security test that quietly skips is worse than none.
    ///
    ///   This cannot weaken anything: the probe only decides what kind of key
    ///   gets *created*, while `policy` remains the sole gate on what may be
    ///   *used*. Probing `false` under `.requireHardware` still fails.
    init(
        policy: DeviceKeyPolicy = .forCurrentEnvironment,
        tag: String = "io.github.manugh.xg2g.deviceKey",
        secureEnclaveProbe: @escaping @Sendable () -> Bool = { SecureEnclaveDeviceKeyStore.isSecureEnclaveAvailable() }
    ) {
        self.policy = policy
        self.tag = Data(tag.utf8)
        self.secureEnclaveProbe = secureEnclaveProbe
    }

    /// Whether this device can create Secure Enclave keys.
    ///
    /// Probed by actually attempting a key creation rather than by inferring it
    /// from the platform, because the answer differs between simulator
    /// generations and inference would be a guess.
    static func isSecureEnclaveAvailable() -> Bool {
        guard let access = SecAccessControlCreateWithFlags(
            nil,
            kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
            .privateKeyUsage,
            nil
        ) else { return false }

        let attributes: [String: Any] = [
            kSecAttrKeyType as String: kSecAttrKeyTypeECSECPrimeRandom,
            kSecAttrKeySizeInBits as String: 256,
            kSecAttrTokenID as String: kSecAttrTokenIDSecureEnclave,
            kSecPrivateKeyAttrs as String: [
                kSecAttrIsPermanent as String: false,
                kSecAttrAccessControl as String: access,
            ],
        ]

        return SecKeyCreateRandomKey(attributes as CFDictionary, nil) != nil
    }

    // MARK: - DeviceKeyStore

    func attestation() throws -> DeviceKeyAttestation? {
        guard let key = try loadKey() else { return nil }
        return try attestation(for: key)
    }

    func provisionKey() throws -> DeviceKeyAttestation {
        if let existing = try loadKey() {
            let attestation = try attestation(for: existing)
            try enforcePolicy(on: attestation.provenance)
            return attestation
        }
        return try createKey()
    }

    func sign(_ data: Data) throws -> Data {
        guard let key = try loadKey() else { throw DeviceKeyError.keyNotProvisioned }
        // The provenance is checked on every signature, not only at creation:
        // otherwise a key created under a permissive policy would keep signing
        // after the policy tightened.
        try enforcePolicy(on: try attestation(for: key).provenance)

        var error: Unmanaged<CFError>?
        guard let der = SecKeyCreateSignature(
            key,
            .ecdsaSignatureMessageX962SHA256,
            data as CFData,
            &error
        ) as Data? else {
            throw DeviceKeyError.signingFailed
        }

        return try ECDSASignature.rawFromDER(der)
    }

    func destroyKey() throws {
        let query: [String: Any] = [
            kSecClass as String: kSecClassKey,
            kSecAttrApplicationTag as String: tag,
            kSecAttrKeyType as String: kSecAttrKeyTypeECSECPrimeRandom,
        ]
        let status = SecItemDelete(query as CFDictionary)
        guard status == errSecSuccess || status == errSecItemNotFound else {
            throw DeviceKeyError.keychain(status)
        }
    }

    // MARK: - Internals

    private func enforcePolicy(on provenance: DeviceKeyProvenance) throws {
        guard provenance == .software, !policy.permitsSoftware else { return }
        throw DeviceKeyError.provenanceMismatch(found: provenance, policy: .hardwareBacked)
    }

    private func createKey() throws -> DeviceKeyAttestation {
        let useSecureEnclave = secureEnclaveProbe()

        if !useSecureEnclave, !policy.permitsSoftware {
            // No fallback. A build that expects hardware fails loudly rather
            // than quietly downgrading its own security properties.
            throw DeviceKeyError.secureEnclaveUnavailable
        }

        guard let access = SecAccessControlCreateWithFlags(
            nil,
            kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
            useSecureEnclave ? [.privateKeyUsage] : [],
            nil
        ) else {
            throw DeviceKeyError.keyGenerationFailed
        }

        var privateAttributes: [String: Any] = [
            kSecAttrIsPermanent as String: true,
            kSecAttrApplicationTag as String: tag,
            kSecAttrAccessControl as String: access,
        ]
        // Not biometry-gated: refresh and heartbeat must be able to sign while
        // the screen is locked once background audio playback exists.
        privateAttributes[kSecAttrCanSign as String] = true

        var attributes: [String: Any] = [
            kSecAttrKeyType as String: kSecAttrKeyTypeECSECPrimeRandom,
            kSecAttrKeySizeInBits as String: 256,
            kSecPrivateKeyAttrs as String: privateAttributes,
        ]
        if useSecureEnclave {
            attributes[kSecAttrTokenID as String] = kSecAttrTokenIDSecureEnclave
        }

        guard let key = SecKeyCreateRandomKey(attributes as CFDictionary, nil) else {
            throw DeviceKeyError.keyGenerationFailed
        }

        return try attestation(for: key)
    }

    private func loadKey() throws -> SecKey? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassKey,
            kSecAttrApplicationTag as String: tag,
            kSecAttrKeyType as String: kSecAttrKeyTypeECSECPrimeRandom,
            kSecReturnRef as String: true,
        ]

        var result: CFTypeRef?
        let status = SecItemCopyMatching(query as CFDictionary, &result)

        switch status {
        case errSecSuccess:
            guard let item = result else { return nil }
            // CFTypeRef -> SecKey without an unchecked bridge.
            guard CFGetTypeID(item) == SecKeyGetTypeID() else { return nil }
            return (item as! SecKey)
        case errSecItemNotFound:
            return nil
        default:
            throw DeviceKeyError.keychain(status)
        }
    }

    private func attestation(for key: SecKey) throws -> DeviceKeyAttestation {
        let attributes = SecKeyCopyAttributes(key) as? [String: Any] ?? [:]
        let isSecureEnclave = (attributes[kSecAttrTokenID as String] as? String)
            == (kSecAttrTokenIDSecureEnclave as String)

        guard let publicKey = SecKeyCopyPublicKey(key) else {
            throw DeviceKeyError.malformedPublicKey
        }
        guard let representation = SecKeyCopyExternalRepresentation(publicKey, nil) as Data? else {
            throw DeviceKeyError.malformedPublicKey
        }

        // ANSI X9.63 uncompressed point: 0x04 || X(32) || Y(32).
        guard representation.count == 65, representation.first == 0x04 else {
            throw DeviceKeyError.malformedPublicKey
        }
        let x = representation[representation.index(representation.startIndex, offsetBy: 1)..<representation.index(representation.startIndex, offsetBy: 33)]
        let y = representation[representation.index(representation.startIndex, offsetBy: 33)...]

        return DeviceKeyAttestation(
            provenance: isSecureEnclave ? .hardwareBacked : .software,
            publicKey: ECPublicKeyJWK(x: Base64URL.encode(Data(x)), y: Base64URL.encode(Data(y)))
        )
    }
}

/// DER to raw signature conversion.
///
/// `SecKeyCreateSignature` returns an ASN.1 DER `SEQUENCE { INTEGER r, INTEGER s }`,
/// which for P-256 is 68–72 bytes. JWS ES256 (RFC 7518 §3.4) requires the raw
/// concatenation `R || S`, 32 bytes each, exactly 64 total — and the server
/// enforces precisely that (`verifyES256Signature` rejects any other length).
/// Emitting the DER blob directly is the mistake the Android client currently
/// makes, and it fails silently because the server's DPoP check sits in a
/// fallback branch.
enum ECDSASignature {

    static func rawFromDER(_ der: Data) throws -> Data {
        var index = der.startIndex

        func byte() throws -> UInt8 {
            guard index < der.endIndex else { throw DeviceKeyError.malformedSignature }
            defer { index = der.index(after: index) }
            return der[index]
        }

        func length() throws -> Int {
            let first = try byte()
            guard first & 0x80 != 0 else { return Int(first) }
            let count = Int(first & 0x7F)
            guard count > 0, count <= 2 else { throw DeviceKeyError.malformedSignature }
            var value = 0
            for _ in 0..<count {
                value = value << 8 | Int(try byte())
            }
            return value
        }

        func integer() throws -> Data {
            guard try byte() == 0x02 else { throw DeviceKeyError.malformedSignature }
            let count = try length()
            guard count > 0, der.distance(from: index, to: der.endIndex) >= count else {
                throw DeviceKeyError.malformedSignature
            }
            let end = der.index(index, offsetBy: count)
            defer { index = end }

            // DER encodes a leading 0x00 when the high bit would make the value
            // negative; the raw form has no sign, so it is dropped.
            var value = Data(der[index..<end])
            while value.first == 0x00, value.count > 1 {
                value.removeFirst()
            }
            guard value.count <= 32 else { throw DeviceKeyError.malformedSignature }
            return value
        }

        guard try byte() == 0x30 else { throw DeviceKeyError.malformedSignature }
        let sequenceLength = try length()
        guard der.distance(from: index, to: der.endIndex) == sequenceLength else {
            throw DeviceKeyError.malformedSignature
        }

        let r = try integer()
        let s = try integer()
        guard index == der.endIndex else { throw DeviceKeyError.malformedSignature }

        return padded(r) + padded(s)
    }

    /// Left-pads to 32 bytes. A short R or S is a legitimate DER encoding, and
    /// forgetting to pad produces a 63-byte signature the server rejects — a
    /// bug that only surfaces for roughly one signature in 256.
    private static func padded(_ value: Data) -> Data {
        Data(repeating: 0, count: 32 - value.count) + value
    }
}
