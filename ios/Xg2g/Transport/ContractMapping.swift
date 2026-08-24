// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CryptoKit
import Foundation

// The mapping layer between the generated wire contract and this app's domain.
//
// It exists as its own file, in the transport zone, so the two sides stay
// separable: `Xg2gContract` is regenerated from api/openapi.yaml and must carry
// no behaviour, while the domain types must not be shaped by whatever the
// contract does next. Everything that bridges them is here, and nowhere else.

extension Xg2gContract.ECPublicKeyJWK {

    /// The device key as a JWK.
    ///
    /// `crv` and `kty` are fixed by the contract's enums, so this is the only
    /// constructor the app needs: the algorithm is P-256 because the device key
    /// is, and a call site that could choose would be a call site that could
    /// choose wrong.
    static func p256(x: String, y: String) -> Self {
        Self(crv: .p256, kty: .ec, x: x, y: y)
    }

    /// RFC 7638 thumbprint.
    ///
    /// Byte-identical to the server's `identity.ComputeJWKThumbprint`:
    /// `{"crv":"P-256","kty":"EC","x":…,"y":…}`, SHA-256, base64url unpadded.
    /// Members in lexicographic order, no whitespace — any deviation yields a
    /// different `jkt` and every proof is rejected.
    var thumbprint: String {
        let canonical = #"{"crv":"\#(crv.rawValue)","kty":"\#(kty.rawValue)","x":"\#(x)","y":"\#(y)"}"#
        return Base64URL.encode(Data(SHA256.hash(data: Data(canonical.utf8))))
    }
}

extension AccessSession {

    /// Builds the stored session from a pairing exchange.
    ///
    /// `expiresIn` becomes an absolute instant at the moment of receipt. Keeping
    /// the duration would make every later staleness check depend on when it
    /// happened to be evaluated.
    init(exchange: Xg2gContract.ExchangePairingResponse, receivedAt: Date) {
        self.init(
            sessionID: exchange.deviceId,
            token: exchange.accessToken,
            expiresAt: receivedAt.addingTimeInterval(TimeInterval(exchange.expiresIn)),
            policyVersion: exchange.policyVersion
        )
    }

    /// Builds the stored session from a rotated device grant.
    init(grant: Xg2gContract.DeviceGrantResponse, receivedAt: Date) {
        self.init(
            sessionID: grant.deviceId,
            token: grant.accessToken,
            expiresAt: receivedAt.addingTimeInterval(TimeInterval(grant.expiresIn)),
            policyVersion: nil
        )
    }
}
