// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Signs outgoing requests with the device's DPoP-bound access token.
///
/// Two headers travel together and neither works alone:
///
/// - `Authorization: DPoP <access token>` — the server reads this scheme and
///   tags the credential as sender-constrained (`token.go`).
/// - `DPoP: <proof>` — a fresh proof over this exact method and URL, with `ath`
///   binding it to that access token.
///
/// The proof is per-request on purpose: `jti` is replay-cached server side, and
/// `htu`/`htm` tie the proof to one target. Reusing a proof across requests
/// would be refused, and caching one would be a replay of our own making.
actor DPoPRequestAuthorizer: RequestAuthorizer {

    enum Failure: Error, Equatable, Sendable {
        /// No usable access token is stored. The caller must refresh or
        /// re-pair; signing without one would produce a request that is
        /// guaranteed to fail.
        case noUsableSession
        case malformedRequest
    }

    private let identity: ServerIdentity
    private let credentials: CredentialStore
    private let keyStore: DeviceKeyStore
    private let now: @Sendable () -> Date

    init(
        identity: ServerIdentity,
        credentials: CredentialStore,
        keyStore: DeviceKeyStore,
        now: @escaping @Sendable () -> Date = { Date() }
    ) {
        self.identity = identity
        self.credentials = credentials
        self.keyStore = keyStore
        self.now = now
    }

    func authorized(_ request: URLRequest) async throws -> URLRequest {
        guard let url = request.url, let method = request.httpMethod else {
            throw Failure.malformedRequest
        }

        guard let session = try await credentials.accessSession(for: identity),
              session.isUsable(at: now())
        else {
            // Deliberately not "sign anyway and let the server decide": an
            // expired token produces a guaranteed 401, and the caller needs to
            // distinguish "refresh first" from "the server rejected us".
            throw Failure.noUsableSession
        }

        var authorized = request
        authorized.setValue("DPoP \(session.token)", forHTTPHeaderField: "Authorization")
        authorized.setValue(
            try await DPoPProof.build(
                method: method,
                url: url,
                accessToken: session.token,
                now: now(),
                using: keyStore
            ),
            forHTTPHeaderField: "DPoP"
        )
        return authorized
    }
}
