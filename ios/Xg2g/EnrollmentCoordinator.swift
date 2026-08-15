// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Drives one device enrollment from first contact to stored credentials.
///
/// ## The sequence, and why it is this one
///
/// 1. `startPairing` — asks the server for a pairing **and provisions the
///    device key**. The key comes first so that a device which cannot produce a
///    hardware-backed key fails here, before a human has been sent off to
///    approve anything. Discovering it after the approval would waste the
///    user's effort and burn a pairing that can no longer be completed.
/// 2. The user approves, out of band. Nothing happens here.
/// 3. `pairingStatus` — polled by whoever owns the screen. This type runs no
///    loop of its own: how often to ask, and how long to keep asking, is a UI
///    decision, and a hidden retry loop inside a coordinator is the kind of
///    network behaviour that becomes impossible to reason about later.
/// 4. `completeEnrollment` — exchanges the pairing for credentials, validates
///    the response in full, and commits.
///
/// ## Where the atomicity actually lives
///
/// Nothing is written to the `CredentialStore` until the exchange has both
/// succeeded and passed validation. Before that point the only durable side
/// effect is the device key itself — which is deliberate: the key is an
/// identity this device owns rather than a credential the server issued, it is
/// worthless to anyone without a matching grant, and re-provisioning it on
/// every attempt would be worse than keeping it.
///
/// The pairing secret lives in memory only, for exactly as long as the pairing
/// does. It is cleared on success so a second `completeEnrollment` cannot
/// replay an exchange that already happened.
actor EnrollmentCoordinator {

    /// What the user has to act on. `qrPayload` and `userCode` are two
    /// presentations of the same pairing, not alternatives with different
    /// meanings.
    struct Invitation: Equatable, Sendable {
        let pairingID: String
        let userCode: String
        let qrPayload: String
        let expiresAt: Date
    }

    enum Failure: Error, Equatable, Sendable {

        /// `completeEnrollment` or `pairingStatus` was called with no pairing
        /// in flight.
        case noPairingInProgress

        /// The exchange answered 2xx with something that is not a usable
        /// credential set. Treated as a failed enrollment rather than
        /// half-trusted, because every one of these defects would otherwise
        /// surface much later as an unexplainable 401.
        case malformedExchange(Defect)

        enum Defect: String, Equatable, Sendable {
            /// The response describes a different pairing than the one asked
            /// about.
            case pairingMismatch
            /// `tokenType` was not `DPoP`. A `Bearer` token is not
            /// sender-constrained, so accepting one would silently drop the
            /// device binding this whole design exists to provide.
            case notSenderConstrained
            case emptyAccessToken
            case emptyRefreshToken
            case emptyDeviceID
            /// A lifetime of zero or less would make the session unusable the
            /// moment it was stored.
            case nonPositiveLifetime
        }
    }

    private struct PendingPairing {
        let id: String
        let secret: String
    }

    private let identity: ServerIdentity
    private let api: APIClient
    private let keyStore: DeviceKeyStore
    private let credentials: CredentialStore
    private let now: @Sendable () -> Date

    private var pending: PendingPairing?

    init(
        identity: ServerIdentity,
        api: APIClient,
        keyStore: DeviceKeyStore,
        credentials: CredentialStore,
        now: @escaping @Sendable () -> Date = { Date() }
    ) {
        self.identity = identity
        self.api = api
        self.keyStore = keyStore
        self.credentials = credentials
        self.now = now
    }

    // MARK: - 1. Start

    /// Starts a pairing and makes sure this device has a usable key.
    ///
    /// The key is provisioned first: `provisionKey` enforces the hardware
    /// policy and throws when it cannot be met, and that answer is worth having
    /// before the user is asked to go and approve something.
    @discardableResult
    func startPairing(deviceName: String, deviceType: DeviceType) async throws -> Invitation {
        _ = try await keyStore.provisionKey()

        let started: PairingWire.StartResponse = try await api.send(
            APIRequest(
                method: .post,
                path: "pairing/start",
                body: try Self.encode(
                    PairingWire.StartRequest(deviceName: deviceName, deviceType: deviceType.rawValue)
                ),
                contentType: "application/json"
            )
        )

        pending = PendingPairing(id: started.pairingID, secret: started.pairingSecret)

        return Invitation(
            pairingID: started.pairingID,
            userCode: started.userCode,
            qrPayload: started.qrPayload,
            expiresAt: started.expiresAt
        )
    }

    // MARK: - 2. Poll

    /// Reads where the pairing stands. Runs no loop and sleeps for nobody.
    func pairingStatus() async throws -> PairingStatus {
        guard let pending else { throw Failure.noPairingInProgress }

        let response: PairingWire.StatusResponse = try await api.send(
            APIRequest(
                method: .post,
                path: "pairing/\(pending.id)/status",
                body: try await secretBody(pending.secret),
                contentType: "application/json"
            )
        )
        return response.status
    }

    // MARK: - 3. Complete

    /// Exchanges an approved pairing for credentials and stores them.
    ///
    /// On any failure the credential store is left exactly as it was. The
    /// pairing stays pending so a caller can retry a call that failed for a
    /// transport reason; the server itself refuses a second exchange of a
    /// pairing it already consumed, which is the authority on whether a retry
    /// is legitimate — this type does not try to guess that locally.
    @discardableResult
    func completeEnrollment() async throws -> EnrolledCredentials {
        guard let pending else { throw Failure.noPairingInProgress }

        // Re-read rather than reuse an attestation from `startPairing`: the
        // JWK that goes on the wire has to describe the key that will actually
        // sign, and `provisionKey` re-checks provenance on the way out.
        let attestation = try await keyStore.provisionKey()

        let response: PairingWire.ExchangeResponse = try await api.send(
            APIRequest(
                method: .post,
                path: "pairing/\(pending.id)/exchange",
                body: try Self.encode(
                    PairingWire.SecretRequest(
                        pairingSecret: pending.secret,
                        deviceJwk: attestation.publicKey
                    )
                ),
                contentType: "application/json"
            )
        )

        try Self.validate(response, against: pending.id)

        let receivedAt = now()
        let enrolled = EnrolledCredentials(
            grant: DeviceGrant(exchange: response),
            session: AccessSession(exchange: response, receivedAt: receivedAt)
        )

        // First and only durable credential write of the whole flow.
        try await credentials.commit(enrolled, for: identity)

        // The secret has done its job and the server has consumed the pairing;
        // holding on to it could only enable a replay of our own.
        self.pending = nil

        return enrolled
    }

    /// Abandons a pairing without touching credentials or the device key.
    func cancelPairing() {
        pending = nil
    }

    // MARK: - Validation

    /// Everything the client must be sure of before treating a 200 as an
    /// enrollment. Each check corresponds to a way the credentials could look
    /// fine now and fail inexplicably later.
    private static func validate(_ response: PairingWire.ExchangeResponse, against pairingID: String) throws {
        guard response.pairingID == pairingID else {
            throw Failure.malformedExchange(.pairingMismatch)
        }
        guard response.tokenType.caseInsensitiveCompare("DPoP") == .orderedSame else {
            throw Failure.malformedExchange(.notSenderConstrained)
        }
        guard !response.deviceID.isEmpty else {
            throw Failure.malformedExchange(.emptyDeviceID)
        }
        guard !response.accessToken.isEmpty else {
            throw Failure.malformedExchange(.emptyAccessToken)
        }
        guard !response.refreshToken.isEmpty else {
            throw Failure.malformedExchange(.emptyRefreshToken)
        }
        guard response.expiresIn > 0 else {
            throw Failure.malformedExchange(.nonPositiveLifetime)
        }
    }

    // MARK: - Internals

    /// The status route requires the JWK too, so this is the same body the
    /// exchange sends.
    private func secretBody(_ secret: String) async throws -> Data {
        let attestation = try await keyStore.provisionKey()
        return try Self.encode(
            PairingWire.SecretRequest(pairingSecret: secret, deviceJwk: attestation.publicKey)
        )
    }

    private static func encode<T: Encodable>(_ value: T) throws -> Data {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        return try encoder.encode(value)
    }
}

/// How this device describes itself when pairing.
///
/// The raw values are the server's `DeviceAuthDeviceType`. Anything the server
/// does not recognise is folded to `unknown` on its side, so an unrepresentable
/// device would be silently mislabelled rather than rejected — which is why
/// these are pinned to the wire enum rather than derived from `UIDevice`.
enum DeviceType: String, Equatable, Sendable {
    case iPhone = "ios_phone"
    case iPad = "ios_tablet"
    case appleTV = "apple_tv"
}
