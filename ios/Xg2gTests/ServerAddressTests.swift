// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

struct ServerAddressTests {

    // MARK: - Endpoint derivation

    @Test func endpointsAreDerivedFromTheRoot() throws {
        let address = try ServerAddressParser.parseTrusted("https://tv.example/")

        #expect(address.rootURL.absoluteString == "https://tv.example/")
        #expect(address.apiBaseURL.absoluteString == "https://tv.example/api/v3/")
        #expect(address.webUIURL.absoluteString == "https://tv.example/ui/")
    }

    @Test func endpointsRespectADeploymentSubPath() throws {
        let address = try ServerAddressParser.parseTrusted("https://proxy.example/xg2g/")

        #expect(address.apiBaseURL.absoluteString == "https://proxy.example/xg2g/api/v3/")
        #expect(address.webUIURL.absoluteString == "https://proxy.example/xg2g/ui/")
    }

    @Test func nonDefaultPortSurvivesIntoEveryEndpoint() throws {
        let address = try ServerAddressParser.parseTrusted("http://192.168.1.10:8080/")

        #expect(address.apiBaseURL.absoluteString == "http://192.168.1.10:8080/api/v3/")
    }

    @Test func ipv6AddressProducesABracketedURL() throws {
        let address = try ServerAddressParser.parseTrusted("http://[::1]:8080/")

        #expect(address.origin.host == "::1")
        #expect(address.apiBaseURL.absoluteString == "http://[::1]:8080/api/v3/")
    }

    @Test func missingPathBecomesTheRoot() throws {
        #expect(try ServerAddressParser.parseTrusted("https://tv.example").rootPath == "/")
    }

    // MARK: - The Android modelling trap

    /// Pasting the Web UI URL yields a root one level too deep, which is exactly
    /// how the Android native transport ends up requesting `/ui/api/v3/...`.
    /// The parser must not silently "fix" this — it reports what it was given,
    /// and offers the alternative as a probe candidate.
    @Test func webUIURLIsNotSilentlyReinterpreted() throws {
        let address = try ServerAddressParser.parseUserEntered("https://tv.example/ui/")

        #expect(address.rootPath == "/ui/")
        #expect(address.apiBaseURL.absoluteString == "https://tv.example/ui/api/v3/")

        let candidates = address.rootCandidates
        #expect(candidates.count == 2)
        #expect(candidates[0].rootPath == "/ui/")
        #expect(candidates[1].rootPath == "/")
        #expect(candidates[1].apiBaseURL.absoluteString == "https://tv.example/api/v3/")
    }

    @Test func rootAddressOffersOnlyItself() throws {
        #expect(try ServerAddressParser.parseTrusted("https://tv.example/").rootCandidates.count == 1)
    }

    /// Candidates are a UX affordance, not discovery. Nothing but a trailing
    /// `/ui/` may generate an alternative, and never more than one.
    @Test func deepPathsDoNotGenerateACandidateLadder() throws {
        let address = try ServerAddressParser.parseTrusted("https://tv.example/a/b/c/")

        #expect(address.rootCandidates.count == 1)
        #expect(address.rootCandidates[0] == address)
    }

    @Test func onlyOneUISegmentIsEverRemoved() throws {
        let address = try ServerAddressParser.parseTrusted("https://tv.example/ui/ui/")

        #expect(address.rootCandidates.count == 2)
        #expect(address.rootCandidates[1].rootPath == "/ui/")
    }

    @Test func aPathMerelyContainingUIIsNotACandidateSource() throws {
        for path in ["/ui-admin/", "/guide/", "/uix/"] {
            let address = try ServerAddressParser.parseTrusted("https://tv.example\(path)")
            #expect(address.rootCandidates.count == 1, "unexpected candidate for \(path)")
        }
    }

    // MARK: - Deployment containment (the single same-origin check)

    private func url(_ raw: String) throws -> URL {
        try #require(URL(string: raw))
    }

    @Test func containsURLsUnderTheDeploymentRoot() throws {
        let address = try ServerAddressParser.parseTrusted("https://tv.example/xg2g/")

        #expect(address.contains(try url("https://tv.example/xg2g/api/v3/services")))
        #expect(address.contains(try url("https://tv.example/xg2g/ui/live")))
        // Exact root without the trailing slash.
        #expect(address.contains(try url("https://tv.example/xg2g")))
    }

    @Test func rejectsSiblingPathsOutsideTheRoot() throws {
        let address = try ServerAddressParser.parseTrusted("https://tv.example/xg2g/")

        #expect(!address.contains(try url("https://tv.example/xg2gother/")))
        #expect(!address.contains(try url("https://tv.example/admin")))
        #expect(!address.contains(try url("https://tv.example/")))
    }

    @Test func rejectsOtherOrigins() throws {
        let address = try ServerAddressParser.parseTrusted("https://tv.example/")

        #expect(!address.contains(try url("http://tv.example/api/v3/")))
        #expect(!address.contains(try url("https://tv.example:8443/api/v3/")))
        #expect(!address.contains(try url("https://other.example/api/v3/")))
    }

    @Test func acceptsEquivalentOriginSpellings() throws {
        let address = try ServerAddressParser.parseTrusted("https://tv.example/")

        #expect(address.contains(try url("https://TV.EXAMPLE:443/api/v3/")))
    }

    /// Deployment-root containment is too weak for API calls: a traversal can
    /// leave the API and still land inside the deployment.
    @Test func apiScopeIsTighterThanTheDeploymentRoot() throws {
        let address = try ServerAddressParser.parseTrusted("https://tv.example/xg2g/")
        let escaped = try url("https://tv.example/xg2g/api/v3/../../admin")

        #expect(address.contains(escaped), "it never leaves the deployment")
        #expect(!address.apiScope.contains(escaped), "but it does leave the API")
    }

    @Test func apiScopeAcceptsOrdinaryAPIPaths() throws {
        let address = try ServerAddressParser.parseTrusted("https://tv.example/xg2g/")

        #expect(address.apiScope.contains(try url("https://tv.example/xg2g/api/v3/services/bouquets")))
        #expect(!address.apiScope.contains(try url("https://tv.example/xg2g/ui/live")))
    }

    @Test func rejectsDotSegmentTraversalOutOfTheRoot() throws {
        let address = try ServerAddressParser.parseTrusted("https://tv.example/xg2g/")

        #expect(!address.contains(try url("https://tv.example/xg2g/../admin")))
    }

    /// A userinfo host reads as the trusted one and resolves to another, so it
    /// must never satisfy a containment check.
    @Test func rejectsUserInfoDisguisedOrigin() throws {
        let address = try ServerAddressParser.parseTrusted("https://tv.example/")

        #expect(!address.contains(try url("https://tv.example@attacker.example/api/v3/")))
    }

    // MARK: - Lenient parser: exactly one repair

    @Test func userInputMayOmitTheScheme() throws {
        let address = try ServerAddressParser.parseUserEntered("tv.example")

        #expect(address.origin.scheme == .https)
        #expect(address.rootPath == "/")
    }

    @Test func userInputMayOmitTheSchemeWithAPort() throws {
        let address = try ServerAddressParser.parseUserEntered("tv-server.local:8080")

        #expect(address.origin.scheme == .https)
        #expect(address.origin.port == 8080)
    }

    /// A declared scheme is validated, never rewritten.
    @Test func userInputWithForeignSchemeIsRejectedNotRepaired() {
        #expect(throws: ServerAddress.ParseFailure.origin(.unsupportedScheme("ftp"))) {
            try ServerAddressParser.parseUserEntered("ftp://tv.example/")
        }
    }

    @Test func userInputKeepsAnExplicitHTTPScheme() throws {
        #expect(try ServerAddressParser.parseUserEntered("http://tv.example/").origin.scheme == .http)
    }

    @Test func emptyUserInputIsRejected() {
        #expect(throws: ServerAddress.ParseFailure.empty) {
            try ServerAddressParser.parseUserEntered("   ")
        }
    }

    // MARK: - Strict parser: no repair at all

    @Test func trustedParserRefusesASchemelessString() {
        #expect(throws: ServerAddress.ParseFailure.notAbsolute("tv.example")) {
            try ServerAddressParser.parseTrusted("tv.example")
        }
    }

    @Test func trustedParserRefusesAForeignScheme() {
        #expect(throws: ServerAddress.ParseFailure.self) {
            try ServerAddressParser.parseTrusted("xg2g://connect")
        }
    }

    // MARK: - Refusals shared by both parsers

    /// Reads as `trusted.example` to a human, resolves to `attacker.example`.
    @Test func userInfoIsRejected() {
        #expect(throws: ServerAddress.ParseFailure.carriesUserInfo("https://trusted.example@attacker.example/")) {
            try ServerAddressParser.parseTrusted("https://trusted.example@attacker.example/")
        }
        #expect(throws: ServerAddress.ParseFailure.self) {
            try ServerAddressParser.parseUserEntered("https://trusted.example@attacker.example/")
        }
    }

    @Test func queryOrFragmentIsRejected() {
        #expect(throws: ServerAddress.ParseFailure.self) {
            try ServerAddressParser.parseTrusted("https://tv.example/?next=/admin")
        }
        #expect(throws: ServerAddress.ParseFailure.self) {
            try ServerAddressParser.parseTrusted("https://tv.example/#/live")
        }
    }

    @Test func nonASCIIHostIsRejected() {
        #expect(throws: ServerAddress.ParseFailure.origin(.nonASCIIHost("münchen.example"))) {
            try ServerAddressParser.parseUserEntered("münchen.example")
        }
    }

    // MARK: - Canonicalization reaches through the address

    @Test func spellingVariantsCollapseToOneAddress() throws {
        let loud = try ServerAddressParser.parseTrusted("HTTPS://TV.EXAMPLE:443/")
        let quiet = try ServerAddressParser.parseTrusted("https://tv.example/")

        #expect(loud == quiet)
        #expect(Set([loud, quiet]).count == 1)
    }

    @Test func dotSegmentsInTheRootAreResolved() throws {
        let address = try ServerAddressParser.parseTrusted("https://tv.example/xg2g/../other/")

        #expect(address.rootPath == "/other/")
        #expect(address.apiBaseURL.absoluteString == "https://tv.example/other/api/v3/")
    }

    /// `%2e%2e` is not traversal for the network stack, so it must stay a
    /// literal segment here too. Decoding it would be the parser-differential.
    @Test func percentEncodedDotsStayLiteral() throws {
        let address = try ServerAddressParser.parseTrusted("https://tv.example/a/%2e%2e/b/")

        #expect(address.rootPath == "/a/%2e%2e/b/")
    }

    @Test func aMissingTrailingSlashIsAddedToTheRoot() throws {
        let address = try ServerAddressParser.parseTrusted("https://tv.example/xg2g")

        #expect(address.rootPath == "/xg2g/")
        #expect(address.apiBaseURL.absoluteString == "https://tv.example/xg2g/api/v3/")
    }
}
