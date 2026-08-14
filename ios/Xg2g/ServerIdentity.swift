// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// An opaque, server-minted installation identifier.
///
/// Validated on the way in because it becomes part of a Keychain account key.
/// A server-supplied string that ends up in a storage key must not be able to
/// carry separators, whitespace or control characters that could collide with,
/// or escape from, the key format.
struct InstanceID: Hashable, Sendable, CustomStringConvertible {

    enum Failure: Error, Equatable, Sendable {
        case empty
        case tooShort(Int)
        case tooLong(Int)
        case illegalCharacter(String)
    }

    /// Lower bound is a sanity check against degenerate values, not a security
    /// guarantee — the entropy has to come from the server.
    static let minimumLength = 8
    static let maximumLength = 128

    let rawValue: String

    var description: String { rawValue }

    init(_ rawValue: String) throws(Failure) {
        let trimmed = rawValue.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { throw .empty }
        guard trimmed.count >= Self.minimumLength else { throw .tooShort(trimmed.count) }
        guard trimmed.count <= Self.maximumLength else { throw .tooLong(trimmed.count) }

        let allowed = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "._-"))
        guard trimmed.unicodeScalars.allSatisfy(allowed.contains) else {
            throw .illegalCharacter(rawValue)
        }

        self.rawValue = trimmed
    }
}

/// What a set of credentials is bound to.
///
/// The two cases are **not** interchangeable spellings of the same thing.
/// `.instance` is strictly the stronger binding: it distinguishes two
/// deployments that happen to share an origin, and it survives a hostname
/// change. `.address` is what we can know before pairing, or against a backend
/// that does not yet mint an instance identifier.
///
/// - Important: There is deliberately no operation that produces a weaker
///   identity from a stronger one. See ``reconcile(bound:observedInstance:)``.
///   An automatic downgrade would let a transient server failure permanently
///   weaken a binding while looking like a successful fallback.
enum ServerIdentity: Hashable, Sendable, CustomStringConvertible {
    case address(ServerAddress)
    case instance(InstanceID)

    enum Strength: Int, Comparable, Sendable {
        case address = 0
        case instance = 1

        static func < (lhs: Strength, rhs: Strength) -> Bool { lhs.rawValue < rhs.rawValue }
    }

    var strength: Strength {
        switch self {
        case .address: return .address
        case .instance: return .instance
        }
    }

    /// Human-readable form for logs and diagnostics.
    ///
    /// - Note: This is **not** a storage key. Serializing an identity for the
    ///   Keychain is `CredentialKeyEncoding`'s job, deliberately kept out of
    ///   this type so domain logic never depends on an Apple storage format —
    ///   and so a storage-format change cannot become a domain change.
    var description: String {
        switch self {
        case .address(let address): return "address(\(address))"
        case .instance(let id): return "instance(\(id))"
        }
    }

    /// Outcome of reconciling a stored binding with what the server just said.
    ///
    /// Every case either keeps the current identity or strengthens it. None
    /// returns something weaker — that is the point of the type.
    enum Resolution: Hashable, Sendable {
        /// Nothing to do.
        case unchanged(ServerIdentity)

        /// Address binding was upgraded to instance binding. The caller must
        /// re-key stored credentials from the old namespace to the new one.
        case upgraded(from: ServerIdentity, to: ServerIdentity)

        /// The server reported a *different* instance than the one we are bound
        /// to. This is a different installation reachable at a familiar
        /// address; credentials must not be reused and re-pairing is required.
        case conflict(bound: ServerIdentity, observed: InstanceID)

        /// We are instance-bound but the server did not report an instance ID
        /// this time. The binding is kept, explicitly and on purpose: a missing
        /// value is not evidence of a different server, and treating it as one
        /// would downgrade the binding on any transient failure.
        case instanceUnavailable(ServerIdentity)

        /// The identity to use after this reconciliation.
        var identity: ServerIdentity {
            switch self {
            case .unchanged(let identity): return identity
            case .upgraded(_, let identity): return identity
            case .conflict(let bound, _): return bound
            case .instanceUnavailable(let identity): return identity
            }
        }

        /// True when stored credentials must be moved to a new namespace.
        var requiresRekeying: Bool {
            if case .upgraded = self { return true }
            return false
        }

        /// True when the existing credentials must not be used.
        var blocksCredentialUse: Bool {
            if case .conflict = self { return true }
            return false
        }
    }

    /// Reconciles a stored binding with an instance identifier the server just
    /// reported, if any.
    ///
    /// This is the only supported way to change a binding, and by construction
    /// it never returns an identity weaker than `bound`.
    static func reconcile(bound: ServerIdentity, observedInstance: InstanceID?) -> Resolution {
        switch (bound, observedInstance) {
        case (.address, .none):
            return .unchanged(bound)

        case (.address, .some(let observed)):
            return .upgraded(from: bound, to: .instance(observed))

        case (.instance, .none):
            return .instanceUnavailable(bound)

        case (.instance(let boundID), .some(let observed)):
            return boundID == observed
                ? .unchanged(bound)
                : .conflict(bound: bound, observed: observed)
        }
    }
}
