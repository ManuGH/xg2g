// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Adds a DPoP proof and nothing else.
///
/// The refresh endpoint authenticates the *device key*, not a session: the
/// server validates the proof with an empty access token, so the proof carries
/// no `ath` and the request carries no `Authorization` header at all. Sending
/// one would be meaningless at best — and at the moment refresh matters most,
/// the access token it would carry is the expired one.
actor DeviceProofAuthorizer: RequestAuthorizer {

    enum Failure: Error, Equatable, Sendable {
        case malformedRequest
    }

    private let keyStore: DeviceKeyStore
    private let now: @Sendable () -> Date

    init(keyStore: DeviceKeyStore, now: @escaping @Sendable () -> Date = { Date() }) {
        self.keyStore = keyStore
        self.now = now
    }

    func authorized(_ request: URLRequest) async throws -> URLRequest {
        guard let url = request.url, let method = request.httpMethod else {
            throw Failure.malformedRequest
        }

        var authorized = request
        authorized.setValue(
            try await DPoPProof.build(method: method, url: url, now: now(), using: keyStore),
            forHTTPHeaderField: "DPoP"
        )
        return authorized
    }
}

/// Owns the question "may this device still talk to this server, and with what
/// token" — and every decision about refreshing.
///
/// ## The division of labour with `DPoPRequestAuthorizer`
///
/// The authorizer signs a request with a session that is *already* valid and
/// throws `noUsableSession` otherwise. It never refreshes. That boundary is
/// deliberate: an authorizer that quietly performs a network round trip in the
/// middle of signing makes every request a potential refresh, turns one failure
/// into two, and hides the rotation that this design depends on. Deciding
/// *when* to refresh lives here, where it is one visible call.
///
/// ## Why refresh is single-flight
///
/// The server rotates the refresh token on every use and treats a superseded
/// token as replay — which revokes the entire family, not just that request.
/// Two concurrent refreshes with the same stored token would therefore not
/// merely race: the loser would revoke the winner's credentials and log the
/// device out for good. Concurrent callers join one in-flight refresh instead.
///
/// ## Why re-authentication is terminal
///
/// `409 DEVICE_REAUTH_REQUIRED` and a rejected refresh grant both mean the same
/// thing — no token this credential set can produce will be accepted. Retrying
/// is not merely useless; a refresh loop against a revoked family is
/// indistinguishable from an attack. Once seen, this coordinator refuses
/// locally without touching the network until the device is paired again.
actor SessionCoordinator {

    enum Failure: Error, Equatable, Sendable {
        /// No stored grant. The device was never enrolled here, or was
        /// forgotten.
        case notEnrolled

        /// Terminal for this credential set: pair again. Never a retry, never
        /// a refresh loop.
        case reauthenticationRequired(Reason)

        /// The refresh answered 2xx with something unusable. Same reasoning as
        /// the enrollment exchange: better a named failure now than an
        /// inexplicable 401 later.
        case malformedRefresh(Defect)

        enum Reason: String, Equatable, Sendable {
            /// The server said `DEVICE_REAUTH_REQUIRED` on an ordinary request.
            case deviceRepairRequired
            /// The refresh token was rejected — expired, revoked, or detected
            /// as a replay, which revokes the whole family.
            case refreshRejected
        }

        enum Defect: String, Equatable, Sendable {
            case notSenderConstrained
            case emptyAccessToken
            case emptyRefreshToken
            case nonPositiveLifetime
        }
    }

    private let identity: ServerIdentity
    private let api: APIClient
    private let credentials: CredentialStore
    private let now: @Sendable () -> Date

    private var terminalReason: Failure.Reason?
    private var inFlightRefresh: Task<EnrolledCredentials, any Error>?

    /// - Parameter api: must be built with a ``DeviceProofAuthorizer``. The
    ///   refresh route authenticates the device key alone.
    init(
        identity: ServerIdentity,
        api: APIClient,
        credentials: CredentialStore,
        now: @escaping @Sendable () -> Date = { Date() }
    ) {
        self.identity = identity
        self.api = api
        self.credentials = credentials
        self.now = now
    }

    // MARK: - Reading a usable session

    /// A session that is valid right now, refreshing only if the stored one is
    /// not.
    func validSession() async throws -> AccessSession {
        if let terminalReason { throw Failure.reauthenticationRequired(terminalReason) }

        if let stored = try await credentials.accessSession(for: identity), stored.isUsable(at: now()) {
            return stored
        }
        return try await refresh().session
    }

    /// Forces a rotation regardless of the stored session's remaining life.
    ///
    /// Concurrent callers join the same refresh rather than starting a second
    /// one — see the note on single-flight above.
    @discardableResult
    func refresh() async throws -> EnrolledCredentials {
        if let terminalReason { throw Failure.reauthenticationRequired(terminalReason) }

        if let inFlightRefresh {
            return try await inFlightRefresh.value
        }

        // Unstructured on purpose: a refresh must not be cancelled halfway.
        // The server rotates the moment it answers, so abandoning the call
        // between the response and the commit would leave this device holding a
        // token the server has already retired — the exact state that looks
        // like replay on the next attempt.
        let task = Task { try await self.performRefresh() }
        inFlightRefresh = task
        return try await task.value
    }

    // MARK: - Reacting to a request that was refused

    /// Records that the server refused a request in a way no refresh can fix.
    ///
    /// Call this with the error from any authorized request. Anything that is
    /// not a terminal refusal is ignored, so callers do not have to classify
    /// server errors themselves — that knowledge belongs in one place.
    func noteRequestFailure(_ error: APIError) {
        guard case .problem(let problem) = error else { return }
        if problem.code == Self.deviceReauthRequiredCode {
            terminalReason = .deviceRepairRequired
        }
    }

    /// Whether this credential set is finished. The UI uses this to route to
    /// re-pairing rather than to a retry.
    var requiresReauthentication: Bool { terminalReason != nil }

    /// Clears the terminal state after the device has been paired again.
    /// Enrollment is the only legitimate way out.
    func resetAfterReenrollment() {
        terminalReason = nil
    }

    // MARK: - Internals

    /// The problem code from `problemcode.CodeDeviceReauthRequired`. Pinned as
    /// a literal because it is a wire contract, not a local constant.
    static let deviceReauthRequiredCode = "DEVICE_REAUTH_REQUIRED"

    private func performRefresh() async throws -> EnrolledCredentials {
        // Cleared by the work, not by whoever happened to be waiting on it. If
        // a caller walks away — cancelled, timed out, screen dismissed — the
        // rotation is still in flight, and a second one started in that window
        // would present a token the server is about to retire. That reads as
        // replay, and the server answers replay by revoking the whole family.
        defer { inFlightRefresh = nil }

        guard let grant = try await credentials.deviceGrant(for: identity) else {
            throw Failure.notEnrolled
        }

        let response: Xg2gContract.DeviceGrantResponse
        do {
            response = try await api.send(
                APIRequest(
                    method: .post,
                    path: "auth/device/refresh",
                    body: try Self.encode(Xg2gContract.DeviceRefreshRequest(refreshToken: grant.secret)),
                    contentType: "application/json"
                )
            )
        } catch let error as APIError {
            if Self.isRejectedGrant(error) {
                // The family is gone server-side. Nothing this device holds can
                // recover it, so stop here rather than retrying into a
                // revocation that has already happened.
                terminalReason = .refreshRejected
                throw Failure.reauthenticationRequired(.refreshRejected)
            }
            throw error
        }

        try Self.validate(response)

        let rotated = EnrolledCredentials(
            grant: grant.rotated(to: response.refreshToken),
            session: AccessSession(grant: response, receivedAt: now())
        )

        // The old refresh token is dead the moment the server answered. It is
        // replaced in the same commit that stores the new session, so no state
        // exists in which this device would present the superseded value again.
        try await credentials.commit(rotated, for: identity)

        return rotated
    }

    /// A refused grant is a 401 from this endpoint. The handler answers 401
    /// `auth/invalid_grant` both for an invalid token and for a detected
    /// replay; they differ in severity but not in what this device can do next.
    private static func isRejectedGrant(_ error: APIError) -> Bool {
        switch error {
        case .problem(let problem):
            return problem.status == 401
        case .http(let status, _, _):
            return status == 401
        default:
            return false
        }
    }

    private static func validate(_ response: Xg2gContract.DeviceGrantResponse) throws {
        guard response.tokenType.caseInsensitiveCompare("DPoP") == .orderedSame else {
            throw Failure.malformedRefresh(.notSenderConstrained)
        }
        guard !response.accessToken.isEmpty else {
            throw Failure.malformedRefresh(.emptyAccessToken)
        }
        guard !response.refreshToken.isEmpty else {
            throw Failure.malformedRefresh(.emptyRefreshToken)
        }
        guard response.expiresIn > 0 else {
            throw Failure.malformedRefresh(.nonPositiveLifetime)
        }
    }

    private static func encode<T: Encodable>(_ value: T) throws -> Data {
        try JSONEncoder().encode(value)
    }
}
