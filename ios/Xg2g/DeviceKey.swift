// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CryptoKit
import Foundation

/// Where the device's private key physically lives.
///
/// This is part of the API on purpose. A software key and a Secure Enclave key
/// are not interchangeable: only the latter is non-exportable and therefore
/// actually device-bound. Callers — and later the backend — must be able to
/// tell them apart and decide, rather than be handed "a key" and assume.
enum DeviceKeyProvenance: String, Sendable, Codable {
    /// Non-exportable, generated in and confined to the Secure Enclave.
    case hardwareBacked
    /// An ordinary Keychain key. Extractable in principle; development only.
    case software
}

/// Why a software key was permitted. Required so the reason is recorded rather
/// than inferred later from the absence of hardware.
enum SoftwareKeyReason: String, Sendable, Codable {
    case simulator
    case development
}

/// Whether a software key may be created at all.
enum DeviceKeyPolicy: Sendable, Equatable {
    /// Hardware only. Provisioning fails outright if the Secure Enclave is
    /// unavailable — no fallback, silent or otherwise.
    case requireHardware
    /// A software key is acceptable. Must be chosen explicitly.
    case allowSoftware(reason: SoftwareKeyReason)

    /// The policy for this build.
    ///
    /// The software case is behind `#if targetEnvironment(simulator)`, so a
    /// real iOS build cannot construct it at all: a development convenience
    /// cannot turn into a production weakening later, because the code that
    /// would allow it is not compiled into the product.
    static var forCurrentEnvironment: DeviceKeyPolicy {
        #if targetEnvironment(simulator)
        return .allowSoftware(reason: .simulator)
        #else
        return .requireHardware
        #endif
    }

    var permitsSoftware: Bool {
        if case .allowSoftware = self { return true }
        return false
    }
}

/// The public half of the device key, in JWK form.
struct ECPublicKeyJWK: Equatable, Sendable, Codable {
    let kty = "EC"
    let crv = "P-256"
    /// Base64url, unpadded, 32 bytes.
    let x: String
    /// Base64url, unpadded, 32 bytes.
    let y: String

    private enum CodingKeys: String, CodingKey {
        case kty, crv, x, y
    }

    init(x: String, y: String) {
        self.x = x
        self.y = y
    }

    /// RFC 7638 thumbprint.
    ///
    /// The canonical form is byte-identical to the server's
    /// `identity.ComputeJWKThumbprint`:
    /// `{"crv":"P-256","kty":"EC","x":…,"y":…}`, SHA-256, base64url unpadded.
    /// Members in lexicographic order, no whitespace — any deviation yields a
    /// different `jkt` and every proof is rejected.
    var thumbprint: String {
        let canonical = #"{"crv":"P-256","kty":"EC","x":"\#(x)","y":"\#(y)"}"#
        return Base64URL.encode(Data(SHA256.hash(data: Data(canonical.utf8))))
    }
}

/// What the app can say about the key it holds.
struct DeviceKeyAttestation: Equatable, Sendable {
    let provenance: DeviceKeyProvenance
    let publicKey: ECPublicKeyJWK

    var thumbprint: String { publicKey.thumbprint }
}

enum DeviceKeyError: Error, Equatable, Sendable {
    /// Policy demanded hardware and the Secure Enclave is not available here.
    case secureEnclaveUnavailable
    case keyNotProvisioned
    /// A key exists but its provenance is weaker than the policy allows. Never
    /// resolved by using it anyway.
    case provenanceMismatch(found: DeviceKeyProvenance, policy: DeviceKeyProvenance)
    case keychain(OSStatus)
    case keyGenerationFailed
    case signingFailed
    case malformedSignature
    case malformedPublicKey
}

/// Base64url without padding, as every JOSE field requires.
enum Base64URL {
    static func encode(_ data: Data) -> String {
        data.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }
}
