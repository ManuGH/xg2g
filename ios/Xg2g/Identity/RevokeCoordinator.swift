// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Ends this device's enrollment: server first, local material second.
///
/// ## Why the order is not negotiable
///
/// The device key is what authenticates the revocation. Destroying it first
/// would leave a grant that is still valid on the server and no longer
/// revocable *from this device* — the user's "log out" would have produced
/// exactly the opposite of what they asked for, and the only remaining cure is
/// an admin removing the device from the household.
///
/// So: revoke remotely, wait for a definite answer, and only then clear local
/// state. This is the same rule `CredentialStore` refuses to let anyone break
/// by not offering a local `revoke` at all.
///
/// ## What counts as a definite answer
///
/// A 204 is the obvious one. A 401 is the other: the credential this call would
/// have used is precisely the one being retired, so "you are not authenticated"
/// means the server has already forgotten this device. Both are terminal
/// successes and both permit local destruction.
///
/// A transport failure is not an answer. Nothing is destroyed, and the caller
/// may try again — the endpoint is safe to retry by design.
actor RevokeCoordinator {

    enum Outcome: Equatable, Sendable {
        /// The server confirmed the revocation on this call.
        case revoked
        /// The server no longer recognised this device. Treated as success:
        /// the desired end state already holds.
        case alreadyRevoked
    }

    enum Failure: Error, Equatable, Sendable {
        /// Nothing to revoke.
        case notEnrolled
        /// The server could not be reached or refused for a reason that is not
        /// terminal. Local credentials are untouched and a retry is safe.
        case remoteRevocationFailed(APIError)
    }

    private let identity: ServerIdentity
    private let api: APIClient
    private let credentials: CredentialStore
    private let keyStore: DeviceKeyStore
    private let session: SessionCoordinator

    init(
        identity: ServerIdentity,
        api: APIClient,
        credentials: CredentialStore,
        keyStore: DeviceKeyStore,
        session: SessionCoordinator
    ) {
        self.identity = identity
        self.api = api
        self.credentials = credentials
        self.keyStore = keyStore
        self.session = session
    }

    /// Revokes this device and clears everything it held.
    ///
    /// - Parameter destroyingDeviceKey: whether to destroy the device key too.
    ///   `true` for "forget this server" and for a device being handed on;
    ///   `false` when the user will immediately pair again, where keeping the
    ///   key avoids a needless Secure Enclave round trip. Either way the key is
    ///   destroyed only after the server has confirmed.
    @discardableResult
    func revokeThisDevice(destroyingDeviceKey: Bool = true) async throws -> Outcome {
        guard try await credentials.deviceGrant(for: identity) != nil else {
            throw Failure.notEnrolled
        }

        let outcome: Outcome
        do {
            // Authorized with the live session, which the request pipeline
            // signs. If the token has expired, `SessionCoordinator` refreshes
            // once before this call — a revocation is worth one rotation.
            _ = try await api.send(
                APIRequest<EmptyResponse>(method: .post, path: "auth/device/revoke")
            )
            outcome = .revoked
        } catch let error as APIError {
            guard Self.meansAlreadyGone(error) else {
                // Deliberately nothing destroyed. A device that cannot reach
                // its server must not end up unable to reach it *and* unable to
                // prove who it was.
                throw Failure.remoteRevocationFailed(error)
            }
            outcome = .alreadyRevoked
        }

        // Past this line the server is certain, so local destruction is safe.
        try await credentials.forgetServer(identity)
        if destroyingDeviceKey {
            try await keyStore.destroyKey()
        }
        await session.resetAfterReenrollment()

        return outcome
    }

    /// A 401 from this endpoint means the credential being retired is already
    /// gone — the end state the caller wanted. Everything else (5xx, a proxy
    /// error, no network) leaves the question open.
    private static func meansAlreadyGone(_ error: APIError) -> Bool {
        switch error {
        case .problem(let problem):
            return problem.status == 401
        case .http(let status, _, _):
            return status == 401
        default:
            return false
        }
    }
}
