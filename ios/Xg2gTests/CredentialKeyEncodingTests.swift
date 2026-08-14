// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Testing
@testable import Xg2g

struct CredentialKeyEncodingTests {

    private func address(_ raw: String) throws -> ServerIdentity {
        .address(try ServerAddressParser.parseTrusted(raw))
    }

    private func instance(_ raw: String) throws -> ServerIdentity {
        .instance(try InstanceID(raw))
    }

    @Test func addressAndInstanceKeyspacesAreDisjoint() throws {
        let fromAddress = CredentialKeyEncoding.account(
            for: try address("https://tv.example/"),
            kind: .deviceGrant
        )
        let fromInstance = CredentialKeyEncoding.account(
            for: try instance("instance-aaaa"),
            kind: .deviceGrant
        )

        #expect(fromAddress != fromInstance)
        #expect(fromAddress.contains(".addr."))
        #expect(fromInstance.contains(".inst."))
    }

    /// Two deployments behind one origin must not share a key.
    @Test func deploymentsUnderOneOriginGetSeparateKeys() throws {
        let home = CredentialKeyEncoding.account(for: try address("https://example.com/home/"), kind: .accessToken)
        let lab = CredentialKeyEncoding.account(for: try address("https://example.com/lab/"), kind: .accessToken)

        #expect(home != lab)
    }

    @Test func equivalentSpellingsProduceOneKey() throws {
        let loud = CredentialKeyEncoding.account(for: try address("HTTPS://TV.EXAMPLE:443/"), kind: .accessToken)
        let quiet = CredentialKeyEncoding.account(for: try address("https://tv.example/"), kind: .accessToken)

        #expect(loud == quiet)
    }

    @Test func credentialKindsDoNotCollide() throws {
        let identity = try address("https://tv.example/")
        let keys = Set(CredentialKind.allCases.map { CredentialKeyEncoding.account(for: identity, kind: $0) })

        #expect(keys.count == CredentialKind.allCases.count)
    }

    /// The separator must not be forgeable out of an address, or one server
    /// could be made to address another server's entry.
    @Test func addressSeparatorsAreEscaped() throws {
        let key = CredentialKeyEncoding.account(for: try address("https://tv.example/a.b/"), kind: .deviceGrant)
        let namespace = key.replacingOccurrences(of: "v\(CredentialKeyEncoding.version).addr.", with: "")

        // Everything after the fixed prefix is one escaped component plus the
        // kind suffix; the address' own dots must not survive as separators.
        #expect(!namespace.dropLast(CredentialKind.deviceGrant.rawValue.count + 1).contains("."))
    }

    @Test func keysCarryTheFormatVersion() throws {
        let key = CredentialKeyEncoding.account(for: try address("https://tv.example/"), kind: .accessToken)

        #expect(key.hasPrefix("v\(CredentialKeyEncoding.version)."))
    }

    @Test func keysAreStableAcrossCalls() throws {
        let identity = try address("https://tv.example/")

        #expect(
            CredentialKeyEncoding.account(for: identity, kind: .deviceGrant)
                == CredentialKeyEncoding.account(for: identity, kind: .deviceGrant)
        )
    }
}
