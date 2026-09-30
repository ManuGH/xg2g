// Package sourceref provides parsing, normalization, and confidential handling
// of Enigma2 IPTV service references (service types 4097, 5001, and 5002).
//
// IPTV service references embed provider URLs, paths, and credentials directly
// in the service reference string. This package ensures that provider URLs are
// never exposed to clients, APIs, WebUIs, M3U exports, logs, or error strings.
// Instead, every valid IPTV reference is resolved to a stable, opaque, HMAC-derived
// identifier (e.g. "iptv_<base32>").
//
// URL Canonicalization:
// Canonicalization is strictly lossless with respect to path and query components.
// Only provably equivalent elements are normalized (lowercasing scheme and hostname,
// stripping default ports 80/443, ensuring non-empty path defaults to "/").
// The raw path and raw query string are preserved byte-for-byte without re-encoding,
// sorting, or parameter dropping. This guarantees deterministic IDs for tokenized
// URLs and URLs with semicolons in query parameters.
//
// Port Colon Heuristic (Fail-Closed):
// Enigma2 references split fields using colons. Hand-edited bouquet files may fail
// to percent-encode the colon preceding a port number (e.g. "http%3a//host:8080/path:Name"
// or "http%3a//host:8080:Name"). In such references, the colon split slices the authority
// at the port. The parser detects this situation when the decoded URL is host-only
// (path is empty or "/", and query is empty), the decoded URL has no explicit port,
// and the subsequent field starts with 1 to 5 digits (^[0-9]{1,5}(/|$)), immediately
// rejecting the reference with ErrInvalidRef to prevent connecting to an unintended default port.
// Residual ambiguity (documented, fail-closed): A host-only URL followed by a purely numeric
// channel name is rejected.
//
// Registry Collision Guard & Equivalent Sources:
// Registry.Replace checks incoming snapshots for ID collisions. A collision is defined
// as two sources producing the same opaque ID but differing canonical URLs (a true HMAC
// collision), in which case Replace aborts and returns ErrCollision without modifying the active snapshot.
// Sources with identical IDs and matching canonical URLs are treated as equivalent.
// Equivalent variants include differences in scheme or host casing (e.g. http://H.INVALID vs http://h.invalid),
// default port numbers (e.g. http://h.invalid:80 vs http://h.invalid), empty path vs root slash
// (e.g. http://h.invalid vs http://h.invalid/), and URL fragments (e.g. http://h.invalid/x vs http://h.invalid/x#frag).
// The fragment is not part of the canonical URL used for ID generation; RevealURL() of the winning
// (first) entry may still contain it. For equivalent sources, the first occurrence's raw URL is kept
// and subsequent occurrences are safely deduplicated without error.
//
// Outbound Policy Boundary:
// This package is strictly a pure domain parser and registry. Outbound network
// policy decisions (such as platformnet.ValidateOutboundURL and
// platformnet.PointsAtReceiverContext) remain the caller's explicit responsibility
// when consuming or activating the underlying stream.
//
// Secret Management:
// HMAC keys for ID derivation are provided as constructor parameters to NewParser.
// Key persistence and configuration binding are decoupled from this package.
package sourceref
