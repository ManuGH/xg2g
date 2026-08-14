// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Launch-time device auth material carried by an onboarding link.
///
/// Mirrors `DeviceAuthLaunchCredentials` on Android; the query keys are part of
/// the wire contract and must stay in step with it.
struct DeviceAuthLaunchCredentials: Equatable, Sendable {
    let deviceGrantID: String?
    let deviceGrant: String?
    let accessToken: String?
    let accessTokenExpiresAtEpochMs: Int64?
}

/// Reads onboarding links. Nothing else.
///
/// This type deliberately owns **no** URL semantics: no normalization, no
/// origin comparison, no path containment. `ServerOrigin` owns origin
/// semantics, `ServerAddress` owns deployment and path semantics, and anything
/// this parser extracts goes through `ServerAddressParser.parseTrusted`. A
/// second normalizer living here is what let the Android client end up with two
/// disagreeing opinions about the same URL.
enum DeepLinkParser {

    static let customScheme = "xg2g"

    private enum QueryKey {
        static let baseURL = "base_url"
        static let authToken = "auth_token"
        static let deviceGrantID = "device_grant_id"
        static let deviceGrant = "device_grant"
        static let launchAccessToken = "launch_access_token"
        static let launchAccessTokenExpiresAt = "launch_access_token_expires_at"
        static let legacyAccessToken = "access_token"
        static let legacyAccessTokenExpiresAt = "access_token_expires_at"
    }

    /// The server an onboarding link configures, if any.
    ///
    /// ## Security invariant
    ///
    /// **Only the `xg2g://` scheme may change server configuration.** An
    /// `https://` link configures nothing, ever. Such a link is a content link
    /// into a server the user already bound; letting one point the app at a
    /// *new* server would mean an arbitrary web page could nominate the host
    /// this client sends credentials to. Android permits it behind an
    /// `isServerSwitch` confirmation, so a dialog is the only thing standing in
    /// the way. If universal links are added later they may open content within
    /// an already-bound server — they must never bind a new one.
    ///
    /// ## Transport form of `base_url`
    ///
    /// The `base_url` on the wire is the **Web UI URL**, not the deployment
    /// root: `apps/webui/src/components/Settings.tsx` mints it as
    /// `new URL('/ui/', endpoint)`, yielding `https://host/ui/`.
    ///
    /// That form is accepted here and converted to a deployment root exactly
    /// once, at this boundary. Past this function nothing in the app deals in
    /// UI URLs. This is not the guessing that `rootCandidates` exists to avoid:
    /// it is a documented transport format with a deterministic, total
    /// conversion, applied at the edge rather than smeared through the core.
    static func configuredAddress(from url: URL) -> ServerAddress? {
        guard let components = components(of: url),
              components.scheme?.lowercased() == customScheme,
              let rawBaseURL = queryParameter(components.percentEncodedQuery, named: QueryKey.baseURL),
              let transported = try? ServerAddressParser.parseTrusted(rawBaseURL)
        else { return nil }

        return deploymentRoot(ofTransported: transported)
    }

    /// Converts the link's Web-UI-shaped `base_url` into a deployment root.
    ///
    /// A trailing `/ui/` is removed exactly once. A link that already carries a
    /// root — which a future link generator may well mint — passes through
    /// unchanged, so both forms work without the caller having to know which
    /// one it got.
    private static func deploymentRoot(ofTransported address: ServerAddress) -> ServerAddress {
        address.rootCandidates.last ?? address
    }

    static func authToken(from url: URL) -> String? {
        guard let components = customSchemeComponents(of: url) else { return nil }
        return trimmedNonEmpty(queryParameter(components.percentEncodedQuery, named: QueryKey.authToken))
    }

    static func accessToken(from url: URL) -> String? {
        guard let components = customSchemeComponents(of: url) else { return nil }
        return trimmedNonEmpty(
            queryParameter(
                components.percentEncodedQuery,
                namedAnyOf: [QueryKey.launchAccessToken, QueryKey.legacyAccessToken]
            )
        )
    }

    static func launchCredentials(from url: URL) -> DeviceAuthLaunchCredentials? {
        guard let components = customSchemeComponents(of: url) else { return nil }
        let rawQuery = components.percentEncodedQuery

        let deviceGrantID = trimmedNonEmpty(queryParameter(rawQuery, named: QueryKey.deviceGrantID))
        let deviceGrant = trimmedNonEmpty(queryParameter(rawQuery, named: QueryKey.deviceGrant))
        let accessToken = accessToken(from: url)
        let expiresAtEpochMs = expiryEpochMs(
            queryParameter(
                rawQuery,
                namedAnyOf: [QueryKey.launchAccessTokenExpiresAt, QueryKey.legacyAccessTokenExpiresAt]
            )
        )

        if deviceGrantID == nil, deviceGrant == nil, accessToken == nil, expiresAtEpochMs == nil {
            return nil
        }

        return DeviceAuthLaunchCredentials(
            deviceGrantID: deviceGrantID,
            deviceGrant: deviceGrant,
            accessToken: accessToken,
            accessTokenExpiresAtEpochMs: expiresAtEpochMs
        )
    }

    /// A bare number is epoch milliseconds; anything else is tried as an
    /// RFC 1123 HTTP date. Same two-step contract as Android's
    /// `parseDeviceAuthExpiryEpochMs`.
    static func expiryEpochMs(_ value: String?) -> Int64? {
        guard let trimmed = trimmedNonEmpty(value) else { return nil }

        if let numeric = Int64(trimmed) {
            return numeric > 0 ? numeric : nil
        }

        guard let date = rfc1123Formatter.date(from: trimmed) else { return nil }
        return Int64((date.timeIntervalSince1970 * 1000).rounded())
    }

    // MARK: - Internals

    private static func components(of url: URL) -> URLComponents? {
        URLComponents(url: url, resolvingAgainstBaseURL: false)
    }

    private static func customSchemeComponents(of url: URL) -> URLComponents? {
        guard let components = components(of: url),
              components.scheme?.lowercased() == customScheme
        else { return nil }
        return components
    }

    private static func trimmedNonEmpty(_ value: String?) -> String? {
        guard let trimmed = value?.trimmingCharacters(in: .whitespacesAndNewlines), !trimmed.isEmpty else {
            return nil
        }
        return trimmed
    }

    private static func queryParameter(_ rawQuery: String?, named name: String) -> String? {
        guard let rawQuery, !rawQuery.isEmpty else { return nil }

        for part in rawQuery.split(separator: "&", omittingEmptySubsequences: true) {
            let piece = String(part)
            let key: String
            let value: String

            if let separator = piece.firstIndex(of: "=") {
                key = String(piece[piece.startIndex..<separator])
                value = String(piece[piece.index(after: separator)...])
            } else {
                key = piece
                value = ""
            }

            if percentDecoded(key) == name {
                return percentDecoded(value)
            }
        }
        return nil
    }

    private static func queryParameter(_ rawQuery: String?, namedAnyOf names: [String]) -> String? {
        for name in names {
            if let value = queryParameter(rawQuery, named: name) { return value }
        }
        return nil
    }

    private static func percentDecoded(_ value: String) -> String {
        // `+` means space in a query component, matching Android's URLDecoder.
        let plusDecoded = value.replacingOccurrences(of: "+", with: " ")
        return plusDecoded.removingPercentEncoding ?? plusDecoded
    }

    private static let rfc1123Formatter: DateFormatter = {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = TimeZone(secondsFromGMT: 0)
        formatter.dateFormat = "EEE, dd MMM yyyy HH:mm:ss zzz"
        return formatter
    }()
}
