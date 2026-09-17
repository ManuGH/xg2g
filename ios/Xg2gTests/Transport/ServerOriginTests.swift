// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Testing
@testable import Xg2g

/// These tests are the contract for credential scoping: any two spellings of the
/// same server must collapse to one value, or the same installation ends up
/// owning two separate credential namespaces.
struct ServerOriginTests {

    private func origin(_ scheme: String, _ host: String, _ port: Int? = nil) throws -> ServerOrigin {
        try ServerOrigin(scheme: scheme, host: host, port: port)
    }

    // MARK: - The case that motivated the type

    @Test func uppercaseAndDefaultPortCollapseToTheSameOrigin() throws {
        let loud = try origin("HTTPS", "EXAMPLE.COM", 443)
        let quiet = try origin("https", "example.com", nil)

        #expect(loud == quiet)
        #expect(loud.description == "https://example.com")
        #expect(Set([loud, quiet]).count == 1)
    }

    @Test func httpDefaultPortCollapses() throws {
        #expect(try origin("http", "example.com", 80) == (try origin("http", "example.com", nil)))
        #expect(try origin("http", "example.com", 80).description == "http://example.com")
    }

    @Test func nonDefaultPortIsPartOfTheIdentity() throws {
        let a = try origin("https", "example.com", 8443)
        let b = try origin("https", "example.com", nil)

        #expect(a != b)
        #expect(a.description == "https://example.com:8443")
    }

    @Test func schemeIsPartOfTheIdentity() throws {
        #expect(try origin("http", "example.com", nil) != (try origin("https", "example.com", nil)))
    }

    // MARK: - Host canonicalization

    @Test func trailingRootDotIsRemoved() throws {
        #expect(try origin("https", "example.com.", nil) == (try origin("https", "example.com", nil)))
    }

    @Test func onlyOneTrailingDotIsStripped() throws {
        // "example.com.." is not a legal name; it must not silently become
        // "example.com".
        #expect(try origin("https", "example.com..", nil).host == "example.com.")
    }

    @Test func ipv6IsCompressedToItsCanonicalForm() throws {
        let long = try origin("https", "0:0:0:0:0:0:0:1", nil)
        let short = try origin("https", "::1", nil)
        let bracketed = try origin("https", "[::1]", nil)

        #expect(long == short)
        #expect(bracketed == short)
        #expect(short.host == "::1")
        #expect(short.isIPv6Literal)
    }

    @Test func ipv6IsBracketedInSerialization() throws {
        #expect(try origin("https", "::1", nil).description == "https://[::1]")
        #expect(try origin("https", "::1", 8080).description == "https://[::1]:8080")
    }

    /// A zone index is device-local. Keeping it would make the same server look
    /// like two different identities depending on which interface was used.
    @Test func ipv6ZoneIndexIsNotPartOfTheIdentity() throws {
        #expect(try origin("https", "fe80::1%en0", nil) == (try origin("https", "fe80::1", nil)))
    }

    @Test func ipv6MixedCaseCollapses() throws {
        #expect(try origin("https", "FE80::ABCD", nil) == (try origin("https", "fe80::abcd", nil)))
    }

    @Test func ipv4LiteralIsAccepted() throws {
        #expect(try origin("http", "192.168.1.10", 8080).host == "192.168.1.10")
    }

    @Test func hostIsLowercased() throws {
        #expect(try origin("https", "Tv-Server.Local", nil).host == "tv-server.local")
    }

    // MARK: - Refusals

    /// IDNA is a homograph problem, not a formatting problem. A caller that
    /// needs an internationalized host supplies punycode.
    @Test func nonASCIIHostIsRejected() {
        #expect(throws: ServerOrigin.Failure.nonASCIIHost("münchen.example")) {
            try ServerOrigin(scheme: "https", host: "münchen.example", port: nil)
        }
    }

    @Test func punycodeHostIsAccepted() throws {
        #expect(try origin("https", "xn--mnchen-3ya.example", nil).host == "xn--mnchen-3ya.example")
    }

    @Test func unsupportedSchemeIsRejected() {
        #expect(throws: ServerOrigin.Failure.unsupportedScheme("ftp")) {
            try ServerOrigin(scheme: "ftp", host: "example.com", port: nil)
        }
    }

    @Test func emptyHostIsRejected() {
        #expect(throws: ServerOrigin.Failure.emptyHost) {
            try ServerOrigin(scheme: "https", host: "   ", port: nil)
        }
    }

    @Test func hostCarryingAuthorityDelimitersIsRejected() {
        for bad in ["example.com/ui", "user@example.com", "example.com\\x", "exa mple.com", "example.com#f"] {
            #expect(throws: ServerOrigin.Failure.self) {
                try ServerOrigin(scheme: "https", host: bad, port: nil)
            }
        }
    }

    @Test func outOfRangePortIsRejected() {
        #expect(throws: ServerOrigin.Failure.invalidPort(0)) {
            try ServerOrigin(scheme: "https", host: "example.com", port: 0)
        }
        #expect(throws: ServerOrigin.Failure.invalidPort(70000)) {
            try ServerOrigin(scheme: "https", host: "example.com", port: 70000)
        }
    }

    @Test func malformedIPv6IsRejected() {
        #expect(throws: ServerOrigin.Failure.self) {
            try ServerOrigin(scheme: "https", host: "[::zz::1]", port: nil)
        }
    }
}
