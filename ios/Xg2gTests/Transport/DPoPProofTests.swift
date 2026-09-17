// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CryptoKit
import Foundation
import Testing
@testable import Xg2g

/// The proof has to satisfy a server that was built and tested independently,
/// so these assertions mirror what `dpop.ValidateProof` actually checks.
struct DPoPProofTests {

    private func makeStore() -> SecureEnclaveDeviceKeyStore {
        SecureEnclaveDeviceKeyStore(
            policy: .allowSoftware(reason: .development),
            tag: "io.github.manugh.xg2g.tests.proof.\(UUID().uuidString)",
            // Forced to software so the test exercises the same path on any
            // host, including CI without a Secure Enclave.
            secureEnclaveProbe: { false }
        )
    }

    private func segments(_ proof: String) throws -> (header: [String: Any], payload: [String: Any], signature: Data) {
        let parts = proof.split(separator: ".", omittingEmptySubsequences: false)
        try #require(parts.count == 3)

        func decode(_ part: Substring) throws -> Data {
            var s = String(part).replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
            while s.count % 4 != 0 { s += "=" }
            return try #require(Data(base64Encoded: s))
        }

        let header = try #require(try JSONSerialization.jsonObject(with: decode(parts[0])) as? [String: Any])
        let payload = try #require(try JSONSerialization.jsonObject(with: decode(parts[1])) as? [String: Any])
        return (header, payload, try decode(parts[2]))
    }

    @Test func proofCarriesTheRequiredHeaderAndClaims() async throws {
        let store = makeStore()
        defer { Task { try? await store.destroyKey() } }

        let url = try #require(URL(string: "https://tv.example/api/v3/auth/device/refresh"))
        let proof = try await DPoPProof.build(method: "post", url: url, using: store)
        let parts = try segments(proof)

        #expect(parts.header["typ"] as? String == "dpop+jwt")
        #expect(parts.header["alg"] as? String == "ES256")

        let jwk = try #require(parts.header["jwk"] as? [String: Any])
        #expect(jwk["kty"] as? String == "EC")
        #expect(jwk["crv"] as? String == "P-256")

        #expect(parts.payload["htm"] as? String == "POST", "htm must be upper-case")
        #expect(parts.payload["htu"] as? String == "https://tv.example/api/v3/auth/device/refresh")
        #expect(parts.payload["iat"] as? Int != nil)
        #expect((parts.payload["jti"] as? String)?.isEmpty == false)
    }

    /// The server refuses any ES256 signature that is not exactly 64 bytes.
    /// Shipping DER here is the defect the Android client has.
    ///
    /// Length alone settles it: DER for P-256 is 68–72 bytes, so exactly 64
    /// already excludes it. Inspecting the first byte for DER's 0x30 SEQUENCE
    /// tag would be worse than useless — in a raw R‖S signature that byte is
    /// simply the top byte of R, legitimately 0x30 about once in 256, so such a
    /// check would reject correct signatures at random.
    @Test func signatureIsRawSixtyFourBytes() async throws {
        let store = makeStore()
        defer { Task { try? await store.destroyKey() } }
        let url = try #require(URL(string: "https://tv.example/api/v3/services"))

        for _ in 0..<64 {
            let proof = try await DPoPProof.build(method: "GET", url: url, using: store)
            let parts = try segments(proof)
            #expect(parts.signature.count == 64)
        }
    }

    /// End-to-end: the signature must verify against the key advertised in the
    /// proof's own header, which is what the server does.
    @Test func signatureVerifiesAgainstTheAdvertisedKey() async throws {
        let store = makeStore()
        defer { Task { try? await store.destroyKey() } }

        let url = try #require(URL(string: "https://tv.example/api/v3/services"))
        let proof = try await DPoPProof.build(method: "GET", url: url, using: store)
        let parts = try segments(proof)

        let jwk = try #require(parts.header["jwk"] as? [String: Any])
        func coordinate(_ name: String) throws -> Data {
            var s = try #require(jwk[name] as? String)
            s = s.replacingOccurrences(of: "-", with: "+").replacingOccurrences(of: "_", with: "/")
            while s.count % 4 != 0 { s += "=" }
            return try #require(Data(base64Encoded: s))
        }

        let publicKey = try P256.Signing.PublicKey(rawRepresentation: try coordinate("x") + coordinate("y"))
        let signature = try P256.Signing.ECDSASignature(rawRepresentation: parts.signature)

        let signingInput = proof.split(separator: ".", omittingEmptySubsequences: false).prefix(2).joined(separator: ".")
        #expect(publicKey.isValidSignature(signature, for: Data(signingInput.utf8)))
    }

    @Test func accessTokenBindsTheProofThroughATH() async throws {
        let store = makeStore()
        defer { Task { try? await store.destroyKey() } }
        let url = try #require(URL(string: "https://tv.example/api/v3/services"))

        let withToken = try await DPoPProof.build(method: "GET", url: url, accessToken: "at_example", using: store)
        let bound = try segments(withToken)
        let expected = Base64URL.encode(Data(SHA256.hash(data: Data("at_example".utf8))))
        #expect(bound.payload["ath"] as? String == expected)

        // A request with no access token carries no ath rather than an empty one.
        let withoutToken = try await DPoPProof.build(method: "GET", url: url, using: store)
        #expect(try segments(withoutToken).payload["ath"] == nil)
    }

    @Test func jtiIsFreshPerProof() async throws {
        let store = makeStore()
        defer { Task { try? await store.destroyKey() } }
        let url = try #require(URL(string: "https://tv.example/api/v3/services"))

        var seen = Set<String>()
        for _ in 0..<20 {
            let proof = try await DPoPProof.build(method: "GET", url: url, using: store)
            let jti = try #require(try segments(proof).payload["jti"] as? String)
            #expect(seen.insert(jti).inserted, "jti must not repeat; the server replay-caches it")
        }
    }

    // MARK: - htu must match the server's normalisation

    /// `dpop.NormalizeHTU` drops query and fragment, lowercases scheme and host
    /// and omits a default port. A client that disagrees has every proof
    /// refused, so these are pinned rather than assumed.
    @Test func htuMatchesTheServerNormalisation() throws {
        let cases: [(String, String)] = [
            ("https://tv.example/api/v3/services", "https://tv.example/api/v3/services"),
            ("https://tv.example/api/v3/services?bouquet=1", "https://tv.example/api/v3/services"),
            ("https://tv.example/api/v3/services#frag", "https://tv.example/api/v3/services"),
            ("https://TV.EXAMPLE/api/v3/services", "https://tv.example/api/v3/services"),
            ("https://tv.example:443/api/v3/services", "https://tv.example/api/v3/services"),
            ("http://tv.example:80/api/v3/services", "http://tv.example/api/v3/services"),
            ("https://tv.example:8443/api/v3/services", "https://tv.example:8443/api/v3/services"),
            ("https://tv.example", "https://tv.example/"),
        ]

        for (input, expected) in cases {
            let url = try #require(URL(string: input))
            #expect(DPoPProof.normalizedHTU(for: url) == expected, "htu for \(input)")
        }
    }

    /// Percent-encoding in the path is preserved: the server compares the path
    /// unchanged, so normalising it here would break every such request.
    @Test func htuPreservesPercentEncodedPaths() throws {
        let url = try #require(URL(string: "https://tv.example/api/v3/recordings/a%2Fb/status"))

        #expect(DPoPProof.normalizedHTU(for: url) == "https://tv.example/api/v3/recordings/a%2Fb/status")
    }

    @Test func unusableTargetsAreRejected() async throws {
        let store = makeStore()
        defer { Task { try? await store.destroyKey() } }
        let url = try #require(URL(string: "ftp://tv.example/api"))

        await #expect(throws: DPoPProof.Failure.unusableTarget(url)) {
            _ = try await DPoPProof.build(method: "GET", url: url, using: store)
        }
    }
}
