// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// What a stored secret is.
///
/// The kinds are separate so that destroying one class of secret cannot take
/// another with it — notably, clearing credentials must never destroy the
/// device key, which is a cryptographic identity with its own lifecycle.
enum CredentialKind: String, Sendable, CaseIterable {
    case deviceGrant = "device_grant"
    case accessToken = "access_token"
    case accessSession = "access_session"
}

/// Serializes domain identities into stable Keychain account strings.
///
/// Kept out of `ServerIdentity` on purpose. If the domain type carried its own
/// storage format, every storage change would become a domain change and
/// business logic would start depending on an Apple persistence detail. This is
/// the one place that knows what a key looks like.
///
/// The format is versioned. Changing how keys are built is a migration, not an
/// edit: bump `version`, keep the old reader, and move entries deliberately.
enum CredentialKeyEncoding {

    /// Bump only together with a migration path for previously written keys.
    static let version = 1

    /// Keychain service under which *all* of this app's secrets live.
    ///
    /// A single owned service is what makes a wipe-on-reinstall possible: at
    /// first launch after reinstall the app does not yet know which identities
    /// were stored, so it can only purge by service, not by enumerating them.
    static let service = "io.github.manugh.xg2g.credentials"

    /// Account string for one secret of one identity.
    ///
    /// The identity discriminator (`addr` / `inst`) keeps the two namespaces
    /// disjoint, so an address-bound entry can never collide with an
    /// instance-bound one even if a server minted an ID that looks like a URL.
    static func account(for identity: ServerIdentity, kind: CredentialKind) -> String {
        "v\(version).\(namespace(of: identity)).\(kind.rawValue)"
    }

    /// Namespace component for an identity. Internal to the encoding.
    private static func namespace(of identity: ServerIdentity) -> String {
        switch identity {
        case .address(let address):
            // The canonical address string is already collision-free across
            // spellings; percent-encoding keeps the "." separator unambiguous.
            return "addr.\(escaped(address.description))"
        case .instance(let id):
            // InstanceID is validated to [A-Za-z0-9._-] at construction, so it
            // cannot introduce a separator here.
            return "inst.\(id.rawValue)"
        }
    }

    private static let separatorSafe: CharacterSet = {
        CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "-_~"))
    }()

    private static func escaped(_ value: String) -> String {
        value.addingPercentEncoding(withAllowedCharacters: separatorSafe) ?? value
    }
}
