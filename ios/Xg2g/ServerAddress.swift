// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// A configured xg2g server: a canonical origin plus the path the whole
/// deployment is mounted under.
///
/// ## Why the root, and not the Web UI path
///
/// The backend mounts `/api/v3` and `/ui` as **siblings** at the deployment
/// root (`V3BaseURL = "/api/v3"`, and `/ui` behind `http.StripPrefix("/ui", …)`).
/// The Android client instead persists the *UI* base (`https://host/ui/`) and
/// has to reconstruct the API from it. Its two transports once did that
/// differently: one replaced the path with `/api/v3/`, the other appended
/// segments and so requested `https://host/ui/api/v3/…` — the SPA handler, not
/// the API. Both share an origin-rooted helper now; the reconstruction is what
/// made two answers possible in the first place.
///
/// Modelling the **root** removes that whole class of mistake: every endpoint
/// is derived downward from one stored value, never reconstructed sideways out
/// of another endpoint's path.
struct ServerAddress: Hashable, Sendable, CustomStringConvertible {

    enum ParseFailure: Error, Equatable, Sendable {
        case empty
        /// The strict parser was handed something that is not an absolute URL.
        case notAbsolute(String)
        case malformed(String)
        case origin(ServerOrigin.Failure)
        /// A server address is not a request; a query or fragment is meaningless
        /// and would be silently dropped on the first join.
        case carriesQueryOrFragment(String)
        /// `https://trusted.example@attacker.example/` reads as one host to a
        /// human and resolves to another. Never accepted, never repaired.
        case carriesUserInfo(String)
    }

    let origin: ServerOrigin
    /// Deployment root, in canonical directory form (`"/"`, `"/xg2g/"`, …).
    let rootPath: String

    var rootURL: URL { url(appending: "") }

    /// The v3 API base. Always derived from the root.
    var apiBaseURL: URL { url(appending: "api/v3/") }

    /// The Web UI, for the day an isolated `WebContentView` needs it.
    var webUIURL: URL { url(appending: "ui/") }

    var description: String { "\(origin)\(rootPath)" }

    init(origin: ServerOrigin, rootPath: String) {
        self.origin = origin
        self.rootPath = URLPath.canonicalDirectory(rootPath)
    }

    private func url(appending suffix: String) -> URL {
        // Built from the canonical serialization rather than through
        // URLComponents: assigning an unbracketed IPv6 literal to
        // `URLComponents.host` yields a nil `url`, and `origin.description`
        // already carries the bracketing and the default-port elision.
        // `rootPath` is percent-encoded and `suffix` is an ASCII literal, so
        // the result is well-formed by construction.
        let string = "\(origin)\(rootPath)\(suffix)"
        guard let url = URL(string: string) else {
            preconditionFailure("non-representable ServerAddress: \(string)")
        }
        return url
    }

    /// This deployment scoped down to its API base.
    ///
    /// Containment against the deployment root is too weak for API calls: a
    /// path like `api/v3/../../admin` resolves back inside the deployment and
    /// would pass, while having left the API entirely. Callers that must stay
    /// within the API check against this instead.
    var apiScope: ServerAddress {
        ServerAddress(origin: origin, rootPath: rootPath + "api/v3/")
    }

    /// Whether `url` is served by this deployment: identical canonical origin,
    /// and a path lying under the deployment root.
    ///
    /// The path is compared in its **percent-encoded** form with only dot
    /// segments resolved. Decoding first would make this disagree with what
    /// `URLSession` and the server actually see — the parser differential this
    /// whole layer exists to avoid.
    func contains(_ url: URL) -> Bool {
        guard let urlOrigin = ServerOrigin(url: url), urlOrigin == origin else { return false }
        guard let components = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return false }

        let path = URLPath.removingDotSegments(components.percentEncodedPath)

        // rootPath always carries a trailing slash, so dropping it yields the
        // "/xg2g" exact-match case for a root of "/xg2g/".
        if path == String(rootPath.dropLast()) { return true }
        return path.hasPrefix(rootPath)
    }

    /// The one path segment this client is allowed to interpret as "you pasted
    /// the Web UI, the deployment root is one level up".
    private static let webUISegment = "/ui/"

    /// Ordered candidates for a setup-time reachability probe.
    ///
    /// A user pasting the address out of their browser will most likely paste
    /// the Web UI URL (`https://host/ui/`), whose root is one level up. Rather
    /// than *guessing* that in the parser — which would be exactly the kind of
    /// reinterpretation the strict/lenient split exists to prevent — the
    /// candidates are handed to the setup flow, which resolves the ambiguity by
    /// asking the server instead of by assuming.
    ///
    /// - Important: This is a UX affordance, **not** discovery. The guardrails
    ///   are deliberate and must stay: at most **one** alternative, produced by
    ///   removing exactly **one** trailing `/ui/` segment. Paths are never
    ///   walked up generally. Without that bound this quietly turns into a
    ///   prober that tries more than anyone intended, and every extra candidate
    ///   is another address the setup flow will send a request to.
    var rootCandidates: [ServerAddress] {
        guard rootPath.hasSuffix(Self.webUISegment) else { return [self] }

        let parent = String(rootPath.dropLast(Self.webUISegment.count - 1))
        return [self, ServerAddress(origin: origin, rootPath: parent)]
    }
}

/// The single boundary between "text a human typed" and "a URL we trust".
///
/// The two entry points are intentionally not one function with a flag. Once a
/// repairing parser is reachable from internal code paths, some later call will
/// take the convenient route and a machine-supplied string will get reinterpreted
/// rather than validated.
enum ServerAddressParser {

    /// Lenient — **only** for text a human typed into the setup field.
    ///
    /// The sole repair is supplying a missing `https://`. If the input already
    /// declares a scheme, it is validated and never reinterpreted: `ftp://host`
    /// is rejected, not rewritten. Nothing else is inferred — no host guessing,
    /// no path fixing, no protocol downgrade.
    static func parseUserEntered(_ input: String) throws(ServerAddress.ParseFailure) -> ServerAddress {
        let trimmed = input.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { throw .empty }

        let candidate = declaredScheme(of: trimmed) == nil ? "https://\(trimmed)" : trimmed
        return try parseAbsolute(candidate)
    }

    /// Strict — for persisted values and trusted backend responses such as
    /// published endpoints. Requires an absolute `http(s)` URL and repairs
    /// nothing at all.
    static func parseTrusted(_ input: String) throws(ServerAddress.ParseFailure) -> ServerAddress {
        let trimmed = input.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { throw .empty }
        guard declaredScheme(of: trimmed) != nil else { throw .notAbsolute(input) }

        return try parseAbsolute(trimmed)
    }

    // MARK: - Internals

    private static func parseAbsolute(_ input: String) throws(ServerAddress.ParseFailure) -> ServerAddress {
        guard let components = URLComponents(string: input) else {
            throw .malformed(input)
        }
        guard components.user == nil, components.password == nil else {
            throw .carriesUserInfo(input)
        }
        guard components.percentEncodedQuery == nil, components.percentEncodedFragment == nil else {
            throw .carriesQueryOrFragment(input)
        }
        guard let scheme = components.scheme else {
            throw .notAbsolute(input)
        }

        // URLComponents.host strips IPv6 brackets; ServerOrigin accepts either.
        guard let host = components.host else {
            throw .origin(.emptyHost)
        }

        let origin: ServerOrigin
        do {
            origin = try ServerOrigin(scheme: scheme, host: host, port: components.port)
        } catch {
            throw .origin(error)
        }

        return ServerAddress(origin: origin, rootPath: components.percentEncodedPath)
    }

    /// Returns the lowercased scheme when `input` actually declares one.
    ///
    /// Keyed on `://`, never on a bare colon: `.` and `-` are legal scheme
    /// characters, so a colon test would read the ordinary `demo.example:8080`
    /// as scheme `demo.example`.
    static func declaredScheme(of input: String) -> String? {
        guard let separator = input.range(of: "://") else { return nil }

        let candidate = String(input[input.startIndex..<separator.lowerBound])
        guard let first = candidate.unicodeScalars.first,
              CharacterSet.letters.contains(first)
        else { return nil }

        let allowed = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "+-."))
        guard candidate.unicodeScalars.allSatisfy(allowed.contains) else { return nil }

        return candidate.lowercased()
    }
}
