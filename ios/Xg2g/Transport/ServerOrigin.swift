// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Network

/// A canonical `http(s)` origin: scheme, host, effective port. Never a path.
///
/// Two textually different spellings of the same server must produce one value,
/// because this feeds credential scoping — `HTTPS://EXAMPLE.COM:443` and
/// `https://example.com` are the same origin and must not end up owning two
/// separate sets of credentials.
///
/// Canonicalization rules, all enforced in `init`:
///
/// - scheme and host are lowercased;
/// - the port is always the *effective* port, so a default port is not a
///   distinguishing feature;
/// - a single trailing root dot (`example.com.`) is removed;
/// - IPv6 literals are compressed per RFC 5952 by `Network.IPv6Address` rather
///   than by hand;
/// - IPv4 literals are canonicalized, so shorthand forms cannot alias;
/// - a non-ASCII host is **rejected**, not guessed at. See `Host encoding`.
///
/// ## Host encoding
///
/// Unicode hosts are refused rather than mapped to punycode here. Getting IDNA
/// wrong is a homograph problem, not a formatting problem, and Foundation does
/// not expose a UTS-46 mapping we could rely on. A caller that genuinely needs
/// an internationalized host supplies the punycode (`xn--`) form, which is
/// plain ASCII and canonicalizes unambiguously.
struct ServerOrigin: Hashable, Sendable, CustomStringConvertible {

    enum Scheme: String, Sendable, CaseIterable {
        case http
        case https

        var defaultPort: Int {
            switch self {
            case .http: return 80
            case .https: return 443
            }
        }
    }

    /// Why a host could not be canonicalized. Surfaced so setup UI can explain
    /// the refusal instead of silently failing.
    enum Failure: Error, Equatable, Sendable {
        case unsupportedScheme(String)
        case emptyHost
        case nonASCIIHost(String)
        case invalidIPv6Literal(String)
        case invalidPort(Int)
        case illegalHostCharacter(String)
    }

    let scheme: Scheme
    /// Canonical host. IPv6 literals are stored *without* brackets.
    let host: String
    /// Effective port, with the scheme default filled in.
    let port: Int

    var isDefaultPort: Bool { port == scheme.defaultPort }

    /// True when `host` is an IPv6 literal and therefore needs brackets in URLs.
    var isIPv6Literal: Bool { host.contains(":") }

    /// Host as it must appear inside a URL authority.
    var bracketedHost: String { isIPv6Literal ? "[\(host)]" : host }

    /// Canonical serialization. A default port is omitted so it cannot become a
    /// distinguishing character in a credential key.
    var description: String {
        isDefaultPort
            ? "\(scheme.rawValue)://\(bracketedHost)"
            : "\(scheme.rawValue)://\(bracketedHost):\(port)"
    }

    init(scheme rawScheme: String, host rawHost: String, port rawPort: Int?) throws(Failure) {
        guard let scheme = Scheme(rawValue: rawScheme.lowercased()) else {
            throw .unsupportedScheme(rawScheme)
        }
        self.scheme = scheme

        self.host = try Self.canonicalHost(rawHost)

        let effectivePort = rawPort ?? scheme.defaultPort
        guard (1...65535).contains(effectivePort) else {
            throw .invalidPort(effectivePort)
        }
        self.port = effectivePort
    }

    // MARK: - Host canonicalization

    private static func canonicalHost(_ rawHost: String) throws(Failure) -> String {
        var host = rawHost.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        guard !host.isEmpty else { throw .emptyHost }

        // An IPv6 literal may arrive bracketed or bare depending on which
        // Foundation accessor produced it.
        let wasBracketed = host.hasPrefix("[") && host.hasSuffix("]")
        if wasBracketed {
            host = String(host.dropFirst().dropLast())
            guard !host.isEmpty else { throw .emptyHost }
        }

        if wasBracketed || host.contains(":") {
            guard let canonical = canonicalIPv6(host) else {
                throw .invalidIPv6Literal(rawHost)
            }
            return canonical
        }

        // A single trailing root dot is legal DNS and means the same name.
        if host.hasSuffix("."), host.count > 1 {
            host.removeLast()
        }
        guard !host.isEmpty, host != "." else { throw .emptyHost }

        guard host.allSatisfy(\.isASCII) else {
            throw .nonASCIIHost(rawHost)
        }

        // Reject anything that could not appear in an authority, so a stray
        // slash, space, credential marker or port separator cannot be smuggled
        // into what callers treat as a bare host.
        let illegal = CharacterSet(charactersIn: "/?#@\\ \t")
        guard host.rangeOfCharacter(from: illegal) == nil else {
            throw .illegalHostCharacter(rawHost)
        }

        if let canonical = canonicalIPv4(host) {
            return canonical
        }
        return host
    }

    /// Canonical origin of an already-parsed URL, or `nil` when it carries none
    /// this client accepts.
    ///
    /// This is the **only** same-origin implementation in the app. Comparing
    /// two `ServerOrigin` values is the same-origin check; there is no second
    /// one to drift out of step with it.
    init?(url: URL) {
        guard let components = URLComponents(url: url, resolvingAgainstBaseURL: false),
              let scheme = components.scheme,
              let host = components.host,
              // `https://trusted.example@attacker.example/` must never compare
              // equal to the trusted origin.
              components.user == nil,
              components.password == nil,
              let origin = try? ServerOrigin(scheme: scheme, host: host, port: components.port)
        else { return nil }

        self = origin
    }

    /// RFC 5952 compression via Network, so `0:0:0:0:0:0:0:1` and `::1` cannot
    /// become two different credential namespaces.
    private static func canonicalIPv6(_ literal: String) -> String? {
        // A scope/zone id is device-local and must not be part of an identity.
        let withoutZone = literal.split(separator: "%", maxSplits: 1).first.map(String.init) ?? literal
        guard let address = IPv6Address(withoutZone) else { return nil }
        return address.debugDescription.lowercased()
    }

    private static func canonicalIPv4(_ literal: String) -> String? {
        guard let address = IPv4Address(literal) else { return nil }
        return address.debugDescription
    }
}
