// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CryptoKit
import Foundation
import Security
import Testing
@testable import Xg2g

// MARK: - Signature encoding

/// The conversion the server's `verifyES256Signature` depends on: it rejects
/// anything that is not exactly 64 bytes, so a DER blob never validates.
struct ECDSASignatureTests {

    /// `SEQUENCE { INTEGER r, INTEGER s }` with no high-bit padding.
    private func der(r: [UInt8], s: [UInt8]) -> Data {
        var body: [UInt8] = [0x02, UInt8(r.count)] + r + [0x02, UInt8(s.count)] + s
        body = [0x30, UInt8(body.count)] + body
        return Data(body)
    }

    @Test func convertsAFullLengthSignature() throws {
        let r = [UInt8](repeating: 0x11, count: 32)
        let s = [UInt8](repeating: 0x22, count: 32)

        let raw = try ECDSASignature.rawFromDER(der(r: r, s: s))

        #expect(raw.count == 64)
        #expect(Array(raw.prefix(32)) == r)
        #expect(Array(raw.suffix(32)) == s)
    }

    /// DER prepends 0x00 when the high bit is set; the raw form is unsigned.
    @Test func stripsTheSignPaddingByte() throws {
        let r: [UInt8] = [0x00] + [UInt8](repeating: 0xFF, count: 32)
        let s = [UInt8](repeating: 0x22, count: 32)

        let raw = try ECDSASignature.rawFromDER(der(r: r, s: s))

        #expect(raw.count == 64)
        #expect(Array(raw.prefix(32)) == [UInt8](repeating: 0xFF, count: 32))
    }

    /// A short R is legal DER. Forgetting to left-pad yields a 63-byte
    /// signature the server rejects — and it only happens for about one
    /// signature in 256, so it survives casual testing.
    @Test func leftPadsAShortInteger() throws {
        let r = [UInt8](repeating: 0x11, count: 31)
        let s = [UInt8](repeating: 0x22, count: 32)

        let raw = try ECDSASignature.rawFromDER(der(r: r, s: s))

        #expect(raw.count == 64)
        #expect(raw.first == 0x00)
        #expect(Array(raw.prefix(32).dropFirst()) == r)
    }

    @Test func leftPadsAVeryShortInteger() throws {
        let raw = try ECDSASignature.rawFromDER(der(r: [0x07], s: [UInt8](repeating: 0x22, count: 32)))

        #expect(raw.count == 64)
        #expect(Array(raw.prefix(32)) == [UInt8](repeating: 0x00, count: 31) + [0x07])
    }

    @Test func rejectsMalformedInput() {
        for bad: [UInt8] in [
            [],
            [0x31, 0x06, 0x02, 0x01, 0x01, 0x02, 0x01, 0x01],   // not a SEQUENCE
            [0x30, 0x06, 0x03, 0x01, 0x01, 0x02, 0x01, 0x01],   // not an INTEGER
            [0x30, 0x20, 0x02, 0x01, 0x01],                     // truncated
        ] {
            #expect(throws: DeviceKeyError.malformedSignature) {
                _ = try ECDSASignature.rawFromDER(Data(bad))
            }
        }
    }

    @Test func rejectsAnOversizedInteger() {
        let r = [UInt8](repeating: 0x11, count: 33)
        #expect(throws: DeviceKeyError.malformedSignature) {
            _ = try ECDSASignature.rawFromDER(der(r: r, s: [UInt8](repeating: 0x22, count: 32)))
        }
    }
}

// MARK: - JWK

struct ECPublicKeyJWKTests {

    /// Oracle computed independently of this implementation:
    /// sha256(`{"crv":"P-256","kty":"EC","x":"dGVzdC14","y":"dGVzdC15"}`),
    /// base64url, unpadded. Any change to the canonical form — member order,
    /// whitespace, padding — changes `jkt` and makes the server reject every
    /// proof, so it is pinned rather than described.
    @Test func thumbprintMatchesTheServerCanonicalForm() {
        let jwk = ECPublicKeyJWK(x: "dGVzdC14", y: "dGVzdC15")

        #expect(jwk.thumbprint == "KPkQZ9JAzMQ212Ca4HYCcvg3sX_zN-AldCezxFGLkH4")
    }

    @Test func thumbprintIsUnpaddedBase64URL() {
        let jwk = ECPublicKeyJWK(x: "dGVzdC14", y: "dGVzdC15")

        #expect(jwk.thumbprint.count == 43)
        #expect(!jwk.thumbprint.contains("="))
        #expect(!jwk.thumbprint.contains("+"))
        #expect(!jwk.thumbprint.contains("/"))
    }
}

// MARK: - Store

struct DeviceKeyStoreTests {

    private func makeStore(policy: DeviceKeyPolicy) -> (SecureEnclaveDeviceKeyStore, String) {
        let tag = "io.github.manugh.xg2g.tests.key.\(UUID().uuidString)"
        return (SecureEnclaveDeviceKeyStore(policy: policy, tag: tag), tag)
    }

    private func permissivePolicy() -> DeviceKeyPolicy {
        SecureEnclaveDeviceKeyStore.isSecureEnclaveAvailable()
            ? .requireHardware
            : .allowSoftware(reason: .simulator)
    }

    private func base64URLDecode(_ value: String) throws -> Data {
        var s = value.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
        while s.count % 4 != 0 { s += "=" }
        return try #require(Data(base64Encoded: s))
    }

    // MARK: Environment

    /// Records what this environment can actually do, rather than inferring it
    /// from the platform. The rest of the suite keys off this answer.
    @Test func secureEnclaveAvailabilityIsSelfConsistent() async throws {
        let available = SecureEnclaveDeviceKeyStore.isSecureEnclaveAvailable()
        let (store, _) = makeStore(policy: .requireHardware)

        if available {
            let attestation = try await store.provisionKey()
            #expect(attestation.provenance == .hardwareBacked)
            try await store.destroyKey()
        } else {
            await #expect(throws: DeviceKeyError.secureEnclaveUnavailable) {
                _ = try await store.provisionKey()
            }
        }
    }

    /// A real device build cannot construct the software case at all.
    @Test func environmentPolicyPermitsSoftwareOnlyOnSimulator() {
        #if targetEnvironment(simulator)
        #expect(DeviceKeyPolicy.forCurrentEnvironment == .allowSoftware(reason: .simulator))
        #else
        #expect(DeviceKeyPolicy.forCurrentEnvironment == .requireHardware)
        #endif
    }

    // MARK: Provisioning

    @Test func provisioningIsIdempotent() async throws {
        let (store, _) = makeStore(policy: permissivePolicy())
        defer { Task { try? await store.destroyKey() } }

        let first = try await store.provisionKey()
        let second = try await store.provisionKey()

        #expect(first == second, "a second provision must return the same key, not mint a new identity")
    }

    @Test func attestationIsNilBeforeProvisioning() async throws {
        let (store, _) = makeStore(policy: permissivePolicy())

        #expect(try await store.attestation() == nil)
    }

    @Test func destroyingRemovesTheIdentity() async throws {
        let (store, _) = makeStore(policy: permissivePolicy())
        _ = try await store.provisionKey()

        try await store.destroyKey()

        #expect(try await store.attestation() == nil)
    }

    @Test func destroyingIsIdempotent() async throws {
        let (store, _) = makeStore(policy: permissivePolicy())

        try await store.destroyKey()
        try await store.destroyKey()
    }

    // MARK: Signing

    /// End-to-end proof that the DER conversion is right: the raw signature
    /// verifies against the very public key the store reports.
    @Test func signatureIsRawAndVerifiesAgainstTheReportedPublicKey() async throws {
        let (store, _) = makeStore(policy: permissivePolicy())
        defer { Task { try? await store.destroyKey() } }

        let attestation = try await store.provisionKey()
        let message = Data("DPoP signing input".utf8)

        let raw = try await store.sign(message)

        #expect(raw.count == 64, "the server rejects any length other than 64")

        let x = try base64URLDecode(attestation.publicKey.x)
        let y = try base64URLDecode(attestation.publicKey.y)
        let publicKey = try P256.Signing.PublicKey(rawRepresentation: x + y)
        let signature = try P256.Signing.ECDSASignature(rawRepresentation: raw)

        #expect(publicKey.isValidSignature(signature, for: message))
    }

    @Test func signaturesAreAlwaysSixtyFourBytes() async throws {
        let (store, _) = makeStore(policy: permissivePolicy())
        defer { Task { try? await store.destroyKey() } }
        _ = try await store.provisionKey()

        // Repeated because a short R or S only occurs occasionally; a missing
        // left-pad would show up as a 63-byte signature here.
        for iteration in 0..<32 {
            let raw = try await store.sign(Data("message-\(iteration)".utf8))
            #expect(raw.count == 64)
        }
    }

    @Test func signingWithoutAKeyFails() async throws {
        let (store, _) = makeStore(policy: permissivePolicy())

        await #expect(throws: DeviceKeyError.keyNotProvisioned) {
            _ = try await store.sign(Data("x".utf8))
        }
    }

    // MARK: The substitution invariant

    /// A software key must never be usable where hardware is required — not at
    /// provisioning, and not later at signing time either.
    ///
    /// The Secure Enclave probe is forced to `false` so a real software key is
    /// created regardless of the host. Gating this test on actual hardware
    /// availability made it silently no-op on the simulator, which now *does*
    /// have a Secure Enclave — a security test that skips itself is worse than
    /// no test at all.
    @Test func aSoftwareKeyIsRejectedUnderAHardwarePolicy() async throws {
        let tag = "io.github.manugh.xg2g.tests.key.\(UUID().uuidString)"
        let permissive = SecureEnclaveDeviceKeyStore(
            policy: .allowSoftware(reason: .simulator),
            tag: tag,
            secureEnclaveProbe: { false }
        )
        let strict = SecureEnclaveDeviceKeyStore(policy: .requireHardware, tag: tag)
        defer { Task { try? await permissive.destroyKey() } }

        let attestation = try await permissive.provisionKey()
        #expect(attestation.provenance == .software, "the seam must really produce a software key")

        await #expect(throws: DeviceKeyError.provenanceMismatch(found: .software, policy: .hardwareBacked)) {
            _ = try await strict.provisionKey()
        }
        await #expect(throws: DeviceKeyError.provenanceMismatch(found: .software, policy: .hardwareBacked)) {
            _ = try await strict.sign(Data("x".utf8))
        }
    }

    /// A build that requires hardware must fail loudly where there is none,
    /// never quietly downgrade itself.
    @Test func hardwarePolicyRefusesToCreateASoftwareKey() async throws {
        let (_, tag) = makeStore(policy: .requireHardware)
        let store = SecureEnclaveDeviceKeyStore(
            policy: .requireHardware,
            tag: tag,
            secureEnclaveProbe: { false }
        )

        await #expect(throws: DeviceKeyError.secureEnclaveUnavailable) {
            _ = try await store.provisionKey()
        }
        #expect(try await store.attestation() == nil, "nothing may be left behind by a refused provisioning")
    }

    /// A software key still has to be a working P-256 key, or the fallback is
    /// useless for development.
    @Test func aSoftwareKeySignsVerifiably() async throws {
        let tag = "io.github.manugh.xg2g.tests.key.\(UUID().uuidString)"
        let store = SecureEnclaveDeviceKeyStore(
            policy: .allowSoftware(reason: .development),
            tag: tag,
            secureEnclaveProbe: { false }
        )
        defer { Task { try? await store.destroyKey() } }

        let attestation = try await store.provisionKey()
        let message = Data("software signing input".utf8)
        let raw = try await store.sign(message)

        #expect(attestation.provenance == .software)
        #expect(raw.count == 64)

        let x = try base64URLDecode(attestation.publicKey.x)
        let y = try base64URLDecode(attestation.publicKey.y)
        let publicKey = try P256.Signing.PublicKey(rawRepresentation: x + y)
        let signature = try P256.Signing.ECDSASignature(rawRepresentation: raw)
        #expect(publicKey.isValidSignature(signature, for: message))
    }
}
