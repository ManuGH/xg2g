// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Path canonicalization shared by everything that compares or joins URL paths.
///
/// Every function here operates on the **percent-encoded** path and never
/// decodes it. That is deliberate: `%2e%2e` and `%2f` are not traversal for
/// WebKit, for `URLSession`, or for the server, so decoding them here would
/// invent a second parser opinion — the classic parser-differential bug. What
/// this code sees must stay what the network stack sees.
enum URLPath {

    /// Resolves `.` and `..` segments so a prefix comparison cannot be walked
    /// out of. Percent-encoding is preserved byte for byte.
    static func removingDotSegments(_ path: String) -> String {
        let hadTrailingSlash = path.hasSuffix("/")
        var stack: [String] = []

        for segment in path.split(separator: "/", omittingEmptySubsequences: true) {
            switch segment {
            case ".":
                continue
            case "..":
                if !stack.isEmpty { stack.removeLast() }
            default:
                stack.append(String(segment))
            }
        }

        var resolved = "/" + stack.joined(separator: "/")
        if hadTrailingSlash, resolved != "/" {
            resolved += "/"
        }
        return resolved
    }

    /// Canonical *directory* path: always one leading and one trailing slash,
    /// dot segments resolved. An empty or root path becomes `"/"`.
    ///
    /// Directory form matters because every derived path is produced by plain
    /// concatenation; a missing trailing slash would silently drop the last
    /// segment under RFC 3986 relative resolution.
    static func canonicalDirectory(_ path: String) -> String {
        let trimmed = path.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty { return "/" }

        let withLeadingSlash = trimmed.hasPrefix("/") ? trimmed : "/" + trimmed
        let withTrailingSlash = withLeadingSlash.hasSuffix("/") ? withLeadingSlash : withLeadingSlash + "/"
        return removingDotSegments(withTrailingSlash)
    }
}
