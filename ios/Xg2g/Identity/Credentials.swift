// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// A complete credential set, as issued by one exchange or one refresh.
///
/// Grant and session are never independent: a single server response mints
/// both, a refresh replaces both, and a revoke ends both. Modelling them as one
/// value is what makes "half a credential set" unrepresentable at the API
/// level rather than merely discouraged — a caller cannot commit a session
/// without the grant that renews it, because there is no call that would let
/// them.
struct EnrolledCredentials: Equatable, Sendable {
    let grant: DeviceGrant
    let session: AccessSession
}

/// Long-lived proof that this device was paired with a server.
///
/// Separate from the DPoP device key on purpose: the grant is a *credential*
/// the server issued, the key is a cryptographic *identity* this device owns.
/// Clearing one must never take the other with it.
struct DeviceGrant: Equatable, Sendable, Codable {
    let id: String
    let secret: String
    let expiresAt: Date?

    /// The server may rotate a grant on refresh; this keeps the call site
    /// honest about which fields changed.
    func rotated(id newID: String?, secret newSecret: String?, expiresAt newExpiry: Date?) -> DeviceGrant {
        DeviceGrant(
            id: newID ?? id,
            secret: newSecret ?? secret,
            expiresAt: newExpiry ?? expiresAt
        )
    }
}

/// A short-lived access session obtained with a `DeviceGrant`.
struct AccessSession: Equatable, Sendable, Codable {
    let sessionID: String
    let token: String
    let expiresAt: Date
    let policyVersion: String?

    /// Treat a token as expired this long before it actually is, so a request
    /// cannot be issued with a token that dies in flight.
    ///
    /// Android has this value twice — `ACCESS_TOKEN_EXPIRY_SKEW_MS` in
    /// `DeviceAuthStore` and an inline `30_000L` in
    /// `NativeDeviceAuthRepository`. Same number today, two places to change.
    /// Here it exists once.
    static let expirySkew: TimeInterval = 30

    func isUsable(at now: Date) -> Bool {
        guard !token.isEmpty else { return false }
        return now.addingTimeInterval(Self.expirySkew) < expiresAt
    }
}
