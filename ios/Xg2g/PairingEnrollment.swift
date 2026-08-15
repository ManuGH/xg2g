// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Where a pairing stands. Mirrors the server's `PairingStatus`.
///
/// Only `approved` may be exchanged. The other terminal values are kept
/// distinct rather than folded into one failure, because they mean genuinely
/// different things to the user: `expired` invites another attempt, `revoked`
/// says someone declined this device, and `consumed` says the pairing already
/// produced credentials — which, if this device does not have them, means it
/// was not the device that completed it.
enum PairingStatus: String, Decodable, Sendable, CaseIterable {
    case pending
    case approved
    case expired
    case consumed
    case revoked
}

/// Wire shapes for the converged pairing exchange.
///
/// The response is identity-shaped: the server issues a DPoP-bound access token
/// and a rotating refresh token. The former `deviceGrantId`, `deviceGrant` and
/// `accessSessionId` are gone because the concepts are — the rotating secret is
/// now a refresh family entry, and the access token is bound to this device's
/// key rather than tied to a separate session row.
enum PairingWire {

    /// `POST /pairing/start` — unauthenticated by definition; the pairing is
    /// worthless until a human approves it.
    struct StartRequest: Encodable, Sendable {
        let deviceName: String
        let deviceType: String
    }

    struct StartResponse: Decodable, Sendable {
        let pairingID: String
        let pairingSecret: String
        let userCode: String
        let qrPayload: String
        let expiresAt: Date

        private enum CodingKeys: String, CodingKey {
            case pairingID = "pairingId"
            case pairingSecret, userCode, qrPayload, expiresAt
        }
    }

    /// Both status and exchange post the same body shape; the secret is what
    /// proves the caller is the device that started this pairing.
    struct StatusResponse: Decodable, Sendable {
        let pairingID: String
        let status: PairingStatus
        let userCode: String
        let expiresAt: Date

        private enum CodingKeys: String, CodingKey {
            case pairingID = "pairingId"
            case status, userCode, expiresAt
        }
    }

    /// Body for both `/status` and `/exchange` — the server declares one
    /// `PairingSecretRequest` shape for the pair, and both fields are required
    /// on both routes. The secret proves the caller started this pairing; the
    /// JWK is what the exchange binds the credentials to.
    struct SecretRequest: Encodable, Sendable {
        let pairingSecret: String
        let deviceJwk: ECPublicKeyJWK
    }

    struct ExchangeResponse: Decodable, Sendable {
        let pairingID: String
        let deviceID: String
        let tokenType: String
        let accessToken: String
        let expiresIn: Int
        let refreshToken: String
        let scope: String
        let policyVersion: String?

        private enum CodingKeys: String, CodingKey {
            case pairingID = "pairingId"
            case deviceID = "deviceId"
            case tokenType, accessToken, expiresIn, refreshToken, scope, policyVersion
        }
    }

    /// `/auth/device/refresh` — the only refresh path. Requires a DPoP proof
    /// from the same device key the grant is bound to.
    struct RefreshRequest: Encodable, Sendable {
        let refreshToken: String

        private enum CodingKeys: String, CodingKey {
            case refreshToken = "refresh_token"
        }
    }

    struct RefreshResponse: Decodable, Sendable {
        let tokenType: String
        let accessToken: String
        let refreshToken: String
        let expiresIn: Int
        let deviceID: String?
        let scope: String?

        private enum CodingKeys: String, CodingKey {
            case tokenType = "token_type"
            case accessToken = "access_token"
            case refreshToken = "refresh_token"
            case expiresIn = "expires_in"
            case deviceID = "device_id"
            case scope
        }
    }
}

extension AccessSession {
    /// Builds the stored session from an exchange response.
    ///
    /// `expiresIn` is converted to an absolute instant at the moment of receipt.
    /// Keeping a duration would make every later staleness check depend on when
    /// it happened to be evaluated.
    init(exchange: PairingWire.ExchangeResponse, receivedAt: Date) {
        self.init(
            sessionID: exchange.deviceID,
            token: exchange.accessToken,
            expiresAt: receivedAt.addingTimeInterval(TimeInterval(exchange.expiresIn)),
            policyVersion: exchange.policyVersion
        )
    }

    init(refresh: PairingWire.RefreshResponse, deviceID: String, receivedAt: Date) {
        self.init(
            sessionID: deviceID,
            token: refresh.accessToken,
            expiresAt: receivedAt.addingTimeInterval(TimeInterval(refresh.expiresIn)),
            policyVersion: nil
        )
    }
}

extension DeviceGrant {
    /// The refresh token is the durable half of the pair, so it is what the
    /// grant record holds. It rotates on every refresh; the old value must never
    /// be presented again, because the server treats that as a replay and
    /// revokes the whole family.
    init(exchange: PairingWire.ExchangeResponse) {
        self.init(id: exchange.deviceID, secret: exchange.refreshToken, expiresAt: nil)
    }

    func rotated(to refreshToken: String) -> DeviceGrant {
        DeviceGrant(id: id, secret: refreshToken, expiresAt: expiresAt)
    }
}
