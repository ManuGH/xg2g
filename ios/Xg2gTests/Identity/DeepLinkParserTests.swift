// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

struct DeepLinkParserTests {

    private func link(_ raw: String) throws -> URL {
        try #require(URL(string: raw))
    }

    // MARK: - Configuring a server

    @Test func customSchemeLinkConfiguresTheDeploymentRoot() throws {
        let url = try link("xg2g://connect?base_url=https%3A%2F%2Ftv.example%2F")

        let address = try #require(DeepLinkParser.configuredAddress(from: url))
        #expect(address.rootPath == "/")
        #expect(address.apiBaseURL.absoluteString == "https://tv.example/api/v3/")
    }

    /// The form the Web UI actually mints today: `new URL('/ui/', endpoint)`.
    /// It is converted to a deployment root once, here at the edge.
    @Test func legacyWebUIBaseURLIsConvertedToTheDeploymentRoot() throws {
        let url = try link("xg2g://connect?base_url=https%3A%2F%2Ftv.example%2Fui%2F")

        let address = try #require(DeepLinkParser.configuredAddress(from: url))
        #expect(address.rootPath == "/")
        #expect(address.apiBaseURL.absoluteString == "https://tv.example/api/v3/")
        #expect(address.webUIURL.absoluteString == "https://tv.example/ui/")
    }

    @Test func legacyWebUIBaseURLUnderASubPathKeepsTheSubPath() throws {
        let url = try link("xg2g://connect?base_url=https%3A%2F%2Ftv.example%2Fxg2g%2Fui%2F")

        let address = try #require(DeepLinkParser.configuredAddress(from: url))
        #expect(address.rootPath == "/xg2g/")
        #expect(address.apiBaseURL.absoluteString == "https://tv.example/xg2g/api/v3/")
    }

    /// A future generator emitting a root must keep working unchanged.
    @Test func rootShapedBaseURLPassesThroughUnchanged() throws {
        let url = try link("xg2g://connect?base_url=https%3A%2F%2Ftv.example%2Fxg2g%2F")

        let address = try #require(DeepLinkParser.configuredAddress(from: url))
        #expect(address.rootPath == "/xg2g/")
    }

    @Test func customSchemeLinkSupportsADeploymentSubPath() throws {
        let url = try link("xg2g://connect?base_url=https%3A%2F%2Ftv.example%2Fxg2g%2F")

        let address = try #require(DeepLinkParser.configuredAddress(from: url))
        #expect(address.apiBaseURL.absoluteString == "https://tv.example/xg2g/api/v3/")
    }

    /// An https link is a content link into a server the user already set up.
    /// Letting one nominate a *new* server would mean an arbitrary web page
    /// could choose the host this client sends credentials to.
    @Test func httpsLinkConfiguresNothing() throws {
        let url = try link("https://demo.example/ui/live/channel-1")

        #expect(DeepLinkParser.configuredAddress(from: url) == nil)
    }

    @Test func customSchemeLinkWithoutBaseURLConfiguresNothing() throws {
        let url = try link("xg2g://connect?auth_token=token-123")

        #expect(DeepLinkParser.configuredAddress(from: url) == nil)
    }

    /// `base_url` goes through the strict parser, so nothing about it is
    /// repaired — a scheme-less value is refused rather than assumed https.
    @Test func schemelessBaseURLIsRefused() throws {
        let url = try link("xg2g://connect?base_url=tv.example")

        #expect(DeepLinkParser.configuredAddress(from: url) == nil)
    }

    @Test func baseURLCarryingUserInfoIsRefused() throws {
        let url = try link("xg2g://connect?base_url=https%3A%2F%2Ftrusted.example%40attacker.example%2F")

        #expect(DeepLinkParser.configuredAddress(from: url) == nil)
    }

    @Test func baseURLWithForeignSchemeIsRefused() throws {
        let url = try link("xg2g://connect?base_url=ftp%3A%2F%2Ftv.example%2F")

        #expect(DeepLinkParser.configuredAddress(from: url) == nil)
    }

    // MARK: - Tokens

    @Test func readsAuthTokenFromCustomSchemeLink() throws {
        let url = try link("xg2g://connect?base_url=https%3A%2F%2Ftv.example%2F&auth_token=token-123")

        #expect(DeepLinkParser.authToken(from: url) == "token-123")
    }

    @Test func readsAccessToken() throws {
        let url = try link("xg2g://connect?launch_access_token=device-access-token")

        #expect(DeepLinkParser.accessToken(from: url) == "device-access-token")
    }

    @Test func stillAcceptsTheLegacyAccessTokenKey() throws {
        let legacyAccessTokenKey = ["access", "token"].joined(separator: "_")
        let url = try link("xg2g://connect?\(legacyAccessTokenKey)=device-access-token")

        #expect(DeepLinkParser.accessToken(from: url) == "device-access-token")
    }

    /// Tokens are only ever read out of our own scheme.
    @Test func tokensAreIgnoredOnHTTPSLinks() throws {
        let url = try link("https://demo.example/ui/?auth_token=token-123&launch_access_token=t")

        #expect(DeepLinkParser.authToken(from: url) == nil)
        #expect(DeepLinkParser.accessToken(from: url) == nil)
        #expect(DeepLinkParser.launchCredentials(from: url) == nil)
    }

    // MARK: - Launch credentials

    @Test func readsGrantAndExpiry() throws {
        let url = try link(
            "xg2g://connect?device_grant_id=dgr-123&device_grant=grant-secret"
                + "&launch_access_token=device-access-token"
                + "&launch_access_token_expires_at=Thu,%2009%20Apr%202026%2012:00:00%20GMT"
        )

        let credentials = try #require(DeepLinkParser.launchCredentials(from: url))
        #expect(credentials.deviceGrantID == "dgr-123")
        #expect(credentials.deviceGrant == "grant-secret")
        #expect(credentials.accessToken == "device-access-token")
        #expect(credentials.accessTokenExpiresAtEpochMs == 1_775_736_000_000)
    }

    @Test func returnsNilWhenNoCredentialParameterIsPresent() throws {
        let url = try link("xg2g://connect?base_url=https%3A%2F%2Ftv.example%2F")

        #expect(DeepLinkParser.launchCredentials(from: url) == nil)
    }

    // MARK: - Expiry parsing

    @Test func expiryAcceptsEpochMillis() {
        #expect(DeepLinkParser.expiryEpochMs("1775736000000") == 1_775_736_000_000)
    }

    @Test func expiryRejectsNonPositiveEpoch() {
        #expect(DeepLinkParser.expiryEpochMs("0") == nil)
        #expect(DeepLinkParser.expiryEpochMs("-1") == nil)
    }

    @Test func expiryRejectsGarbage() {
        #expect(DeepLinkParser.expiryEpochMs("not-a-date") == nil)
        #expect(DeepLinkParser.expiryEpochMs("   ") == nil)
        #expect(DeepLinkParser.expiryEpochMs(nil) == nil)
    }
}
