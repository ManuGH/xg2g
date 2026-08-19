// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

/// The fixtures are the bytes the Go handlers actually write, as validated
/// against api/openapi.yaml by TestV3Contract_PairingFlowResponsesMatchOpenAPI.
///
/// The point of these tests is not that Codable works. It is that the generated
/// types and `JSONDecoder.xg2g` together accept the real response — including
/// its timestamp format, which the server got wrong for as long as nobody
/// decoded it strictly.
struct Xg2gContractTests {

    private func decode<T: Decodable>(_ type: T.Type, _ json: String) throws -> T {
        try JSONDecoder.xg2g.decode(type, from: Data(json.utf8))
    }

    @Test func theExchangeResponseDecodesIntoTheIdentityShapedContract() throws {
        let response = try decode(Xg2gContract.ExchangePairingResponse.self, """
        {
          "pairingId": "pair_999",
          "deviceId": "dev_tv_100",
          "tokenType": "DPoP",
          "accessToken": "at_tv_dpop",
          "expiresIn": 900,
          "refreshToken": "rt_tv_rotating",
          "scope": "v3:read v3:stream",
          "policyVersion": "v1",
          "endpoints": [
            {
              "url": "https://public.example",
              "kind": "public_https",
              "priority": 10,
              "tlsMode": "required",
              "allowPairing": true,
              "allowStreaming": true,
              "allowWeb": true,
              "allowNative": true,
              "advertiseReason": "public reverse proxy",
              "source": "config"
            }
          ]
        }
        """)

        #expect(response.deviceId == "dev_tv_100")
        #expect(response.tokenType == "DPoP")
        #expect(response.expiresIn == 900)
        #expect(response.refreshToken == "rt_tv_rotating")
        #expect(response.endpoints.count == 1)
        #expect(response.endpoints[0].kind == .publicHttps)
        #expect(response.endpoints[0].tlsMode == .required)
        #expect(response.endpoints[0].source == .config)
    }

    @Test func pairingTimestampsAreRfc3339InBothOfGoDsForms() throws {
        // Go's time.Time emits fractional seconds only when they are non-zero,
        // so the same field alternates between these two spellings.
        let withoutFraction = try decode(Xg2gContract.StartPairingResponse.self, """
        {
          "pairingId": "pair_1",
          "pairingSecret": "sec_1",
          "userCode": "ABCD-EFGH",
          "qrPayload": "https://tv.example/pair?code=ABCD-EFGH",
          "expiresAt": "2026-08-13T14:00:00Z"
        }
        """)

        let withFraction = try decode(Xg2gContract.StartPairingResponse.self, """
        {
          "pairingId": "pair_1",
          "pairingSecret": "sec_1",
          "userCode": "ABCD-EFGH",
          "qrPayload": "https://tv.example/pair?code=ABCD-EFGH",
          "expiresAt": "2026-08-13T14:00:00.529Z"
        }
        """)

        #expect(withoutFraction.expiresAt.timeIntervalSince1970 == 1786629600)
        #expect(abs(withFraction.expiresAt.timeIntervalSince1970 - 1786629600.529) < 0.001)
    }

    @Test func anHttpHeaderDateIsRejectedRatherThanQuietlyAccepted() {
        // What the server used to send. A client that shrugs this off is a
        // client that cannot tell a working pairing from a broken one.
        #expect(throws: DecodingError.self) {
            _ = try decode(Xg2gContract.StartPairingResponse.self, """
            {
              "pairingId": "pair_1",
              "pairingSecret": "sec_1",
              "userCode": "ABCD-EFGH",
              "qrPayload": "https://tv.example/pair?code=ABCD-EFGH",
              "expiresAt": "Thu, 13 Aug 2026 14:00:00 GMT"
            }
            """)
        }
    }

    @Test func optionalContractFieldsStayOptionalAndRequiredOnesDoNot() throws {
        // approvedAt and consumedAt are absent while a pairing is pending.
        let pending = try decode(Xg2gContract.PairingStatusResponse.self, """
        {
          "pairingId": "pair_1",
          "status": "pending",
          "userCode": "ABCD-EFGH",
          "deviceName": "Living Room TV",
          "deviceType": "android_tv",
          "expiresAt": "2026-08-13T14:00:00Z"
        }
        """)

        #expect(pending.status == .pending)
        #expect(pending.deviceType == .androidTv)
        #expect(pending.approvedAt == nil)
        #expect(pending.consumedAt == nil)

        #expect(throws: DecodingError.self) {
            _ = try decode(Xg2gContract.PairingStatusResponse.self, """
            {
              "pairingId": "pair_1",
              "status": "pending",
              "userCode": "ABCD-EFGH",
              "expiresAt": "2026-08-13T14:00:00Z"
            }
            """)
        }
    }

    @Test func theRefreshResponseDecodesDespiteItsSnakeCaseWireNames() throws {
        // /auth/device/refresh is snake_case while the pairing exchange is
        // camelCase — the same six concepts, two spellings. The generated type
        // absorbs that in CodingKeys so no call site has to know.
        let grant = try decode(Xg2gContract.DeviceGrantResponse.self, """
        {
          "token_type": "DPoP",
          "access_token": "at_rotated",
          "refresh_token": "rt_rotated",
          "expires_in": 900,
          "device_id": "dev_tv_100",
          "scope": "v3:read v3:stream"
        }
        """)

        #expect(grant.tokenType == "DPoP")
        #expect(grant.accessToken == "at_rotated")
        #expect(grant.refreshToken == "rt_rotated")
        #expect(grant.expiresIn == 900)
        #expect(grant.deviceId == "dev_tv_100")
        #expect(grant.scope == "v3:read v3:stream")
    }

    @Test func aRefreshResponseMissingARequiredFieldIsRejected() {
        // Every field is required, so a truncated response must fail rather than
        // leave the device holding a grant it cannot rotate.
        #expect(throws: DecodingError.self) {
            _ = try decode(Xg2gContract.DeviceGrantResponse.self, """
            {
              "token_type": "DPoP",
              "access_token": "at_rotated",
              "expires_in": 900,
              "device_id": "dev_tv_100",
              "scope": "v3:read"
            }
            """)
        }
    }

    @Test func theRefreshRequestEncodesTheWireKeyNotTheSwiftName() throws {
        let request = Xg2gContract.DeviceRefreshRequest(refreshToken: "rt_current")

        let encoded = try JSONSerialization.jsonObject(
            with: JSONEncoder().encode(request)
        ) as? [String: Any]

        #expect(encoded?["refresh_token"] as? String == "rt_current")
        #expect(encoded?["refreshToken"] == nil)
    }

    @Test func theDeviceKeyEncodesInTheShapeTheExchangeExpects() throws {
        let request = Xg2gContract.PairingSecretRequest(
            deviceJwk: Xg2gContract.ECPublicKeyJWK(crv: .p256, kty: .ec, x: "eA", y: "eQ"),
            pairingSecret: "sec_1"
        )

        let encoded = try JSONSerialization.jsonObject(
            with: JSONEncoder().encode(request)
        ) as? [String: Any]

        let jwk = try #require(encoded?["deviceJwk"] as? [String: Any])
        #expect(jwk["kty"] as? String == "EC")
        #expect(jwk["crv"] as? String == "P-256")
    }
}
