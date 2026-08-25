// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CryptoKit
import Foundation

/// Builds RFC 9449 DPoP proofs on top of the device key.
///
/// The private key never appears here: this assembles the JWS and asks
/// `DeviceKeyStore` to sign the bytes. Only the non-exportable key lives in the
/// Secure Enclave; `jti`, `htm`, `htu`, `iat` and `ath` are ordinary software.
///
/// The signature is raw `R ‖ S` — `DeviceKeyStore.sign` converts from the DER
/// that `SecKeyCreateSignature` returns. The server rejects anything that is not
/// exactly 64 bytes, which is what the shipping Android client got wrong.
enum DPoPProof {

    enum Failure: Error, Equatable, Sendable {
        /// The target could not be reduced to an `htu` the server will agree on.
        case unusableTarget(URL)
        case encodingFailed
    }

    /// Builds a proof for one request.
    ///
    /// - Parameter accessToken: when present, its SHA-256 goes into `ath`,
    ///   binding the proof to that specific token. Omitted for requests that
    ///   carry no access token, such as the refresh call.
    static func build(
        method: String,
        url: URL,
        accessToken: String? = nil,
        now: Date = Date(),
        jti: String = UUID().uuidString,
        using keyStore: DeviceKeyStore
    ) async throws -> String {
        let attestation = try await keyStore.provisionKey()

        guard let htu = normalizedHTU(for: url) else {
            throw Failure.unusableTarget(url)
        }

        let header = ProofHeader(jwk: attestation.publicKey)
        var payload = ProofPayload(
            jti: jti,
            htm: method.uppercased(),
            htu: htu,
            iat: Int(now.timeIntervalSince1970)
        )
        if let accessToken {
            payload.ath = Base64URL.encode(Data(SHA256.hash(data: Data(accessToken.utf8))))
        }

        let encoder = JSONEncoder()
        // Deterministic member order keeps proofs reproducible in tests; the
        // server does not care about order, but a reader of a captured proof
        // does.
        encoder.outputFormatting = [.sortedKeys]

        guard let headerData = try? encoder.encode(header),
              let payloadData = try? encoder.encode(payload)
        else { throw Failure.encodingFailed }

        let signingInput = Base64URL.encode(headerData) + "." + Base64URL.encode(payloadData)
        let signature = try await keyStore.sign(Data(signingInput.utf8))

        return signingInput + "." + Base64URL.encode(signature)
    }

    /// Reduces a URL to the `htu` the server derives.
    ///
    /// Must match `dpop.NormalizeHTU`: query and fragment removed, scheme and
    /// host lowercased, a default port omitted, path preserved byte for byte.
    /// A mismatch here is not a subtle bug — every proof is refused.
    static func normalizedHTU(for url: URL) -> String? {
        guard let origin = ServerOrigin(url: url),
              let components = URLComponents(url: url, resolvingAgainstBaseURL: false)
        else { return nil }

        let path = components.percentEncodedPath
        return "\(origin)\(path.isEmpty ? "/" : path)"
    }
}

// MARK: - Wire shapes

private struct ProofHeader: Encodable {
    let typ = "dpop+jwt"
    let alg = "ES256"
    let jwk: Xg2gContract.ECPublicKeyJWK

    private enum CodingKeys: String, CodingKey {
        case typ, alg, jwk
    }
}

private struct ProofPayload: Encodable {
    let jti: String
    let htm: String
    let htu: String
    let iat: Int
    var ath: String?
}
