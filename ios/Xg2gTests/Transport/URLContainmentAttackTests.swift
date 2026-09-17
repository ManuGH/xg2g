// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

/// Containment is only meaningful if the URL we judge is the URL that gets
/// sent. Every case here therefore asserts twice: that `URLRequest` carries the
/// URL unchanged, and what the verdict is. If Foundation ever starts rewriting
/// one of these, the first assertion fails and we find out before the second
/// assertion becomes a lie.
///
/// The guiding rule is that this layer **never decodes**. `%2e%2e`, `%2f` and
/// `%5c` are not traversal for `URLSession` or for the server, so treating them
/// as traversal here would be a second parser opinion — the differential this
/// layer exists to prevent. Being "more careful" than the network stack is not
/// safer; it is just different, and different is the bug.
struct URLContainmentAttackTests {

    private let root = try! ServerAddressParser.parseTrusted("https://tv.example/xg2g/")

    /// Asserts that the judged URL is byte-for-byte the URL that would be sent,
    /// then asserts the containment verdict.
    private func check(_ raw: String, contained expected: Bool, _ note: Comment) throws {
        let url = try #require(URL(string: raw), "URL(string:) rejected \(raw)")
        let sent = try #require(URLRequest(url: url).url)

        #expect(
            sent.absoluteString == url.absoluteString,
            "URLRequest rewrote the URL: \(url.absoluteString) -> \(sent.absoluteString)"
        )
        #expect(root.contains(sent) == expected, note)
    }

    // MARK: - Real traversal is rejected

    @Test func literalDotSegmentsEscapeAndAreRejected() throws {
        try check("https://tv.example/xg2g/../admin", contained: false, "'..' really does escape the root")
        try check("https://tv.example/xg2g/../../admin", contained: false, "repeated traversal")
        try check("https://tv.example/xg2g/a/../../admin", contained: false, "traversal after a real segment")
    }

    @Test func harmlessDotSegmentsStayInside() throws {
        try check("https://tv.example/xg2g/./api/v3/", contained: true, "'.' is a no-op")
        try check("https://tv.example/xg2g/a/../api/", contained: true, "traversal that stays under the root")
    }

    // MARK: - Percent-encoded separators are NOT separators

    /// `%2f` is a literal slash inside one segment. Decoding it here would
    /// invent a segment boundary that neither URLSession nor the server sees.
    @Test func encodedSlashIsNotASegmentBoundary() throws {
        try check(
            "https://tv.example/xg2g/a%2F..%2F..%2Fadmin",
            contained: true,
            "%2F must not be read as a separator, so this never leaves the root"
        )
        try check(
            "https://tv.example/%2Fxg2g/admin",
            contained: false,
            "a leading %2F is part of the first segment, so this is not under /xg2g/"
        )
    }

    /// `%5c` is a literal backslash. Browsers normalize `\` to `/`, which is why
    /// the backend rejects it in `normalizeWebBootstrapTargetPath`; URLSession
    /// does not, and this layer feeds URLSession.
    @Test func encodedBackslashIsNotASegmentBoundary() throws {
        try check(
            "https://tv.example/xg2g/a%5C..%5Cadmin",
            contained: true,
            "%5C stays inside one segment"
        )
        try check(
            "https://tv.example/%5Cxg2g/admin",
            contained: false,
            "not under the root"
        )
    }

    // MARK: - Encoded dots are literal, in any spelling

    @Test func encodedDotsAreNotTraversal() throws {
        try check("https://tv.example/xg2g/%2e%2e/admin", contained: true, "lowercase %2e%2e is a literal segment")
        try check("https://tv.example/xg2g/%2E%2E/admin", contained: true, "uppercase %2E%2E likewise")
        try check("https://tv.example/xg2g/%2e%2E/admin", contained: true, "mixed case likewise")
    }

    @Test func doubleEncodedDotsAreNotTraversal() throws {
        try check(
            "https://tv.example/xg2g/%252e%252e/admin",
            contained: true,
            "%252e%252e decodes once to %2e%2e, which is still not traversal"
        )
    }

    /// The canonical example from the review: a UI path with encoded dots must
    /// not be read as reaching the API root.
    @Test func encodedDotsDoNotReachTheAPIFromTheUI() throws {
        let uiRoot = try ServerAddressParser.parseTrusted("https://tv.example/ui/")
        let url = try #require(URL(string: "https://tv.example/ui/%2e%2e/api/v3/services"))

        #expect(uiRoot.contains(url), "stays a literal segment under /ui/")

        let deploymentRoot = try ServerAddressParser.parseTrusted("https://tv.example/")
        #expect(
            deploymentRoot.apiBaseURL.absoluteString == "https://tv.example/api/v3/",
            "the real API is reached by derivation from the root, never by traversal"
        )
    }

    // MARK: - Prefix confusion

    @Test func siblingPathsSharingAPrefixAreRejected() throws {
        try check("https://tv.example/xg2gX/api", contained: false, "/xg2gX is not under /xg2g/")
        try check("https://tv.example/xg2g-other/", contained: false, "hyphenated sibling")
    }

    @Test func theRootItselfIsContainedWithOrWithoutTrailingSlash() throws {
        try check("https://tv.example/xg2g/", contained: true, "exact root")
        try check("https://tv.example/xg2g", contained: true, "root without trailing slash")
    }

    // MARK: - Origin confusion

    @Test func otherOriginsAreRejected() throws {
        try check("http://tv.example/xg2g/", contained: false, "scheme differs")
        try check("https://tv.example:8443/xg2g/", contained: false, "port differs")
        try check("https://attacker.example/xg2g/", contained: false, "host differs")
    }

    @Test func equivalentOriginSpellingsAreAccepted() throws {
        try check("https://TV.EXAMPLE/xg2g/", contained: true, "host case is not significant")
        try check("https://tv.example.:443/xg2g/", contained: true, "trailing root dot and default port")
    }

    /// Reads as `tv.example` and resolves to `attacker.example`.
    @Test func userInfoDisguiseIsRejected() throws {
        try check("https://tv.example@attacker.example/xg2g/", contained: false, "userinfo host disguise")
        try check("https://tv.example:pass@attacker.example/xg2g/", contained: false, "with a password too")
    }

    /// A Unicode host cannot be compared safely, so it can never satisfy
    /// containment against an ASCII-canonical root.
    @Test func unicodeHostIsRejected() throws {
        let url = try #require(URL(string: "https://tv.exämple/xg2g/"))
        #expect(!root.contains(url))
    }
}
