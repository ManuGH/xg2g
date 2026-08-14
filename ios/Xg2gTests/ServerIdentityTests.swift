// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Testing
@testable import Xg2g

struct InstanceIDTests {

    @Test func acceptsAPlausibleServerMintedIdentifier() throws {
        #expect(try InstanceID("xg2g-01H9Z_abc.DEF").rawValue == "xg2g-01H9Z_abc.DEF")
    }

    @Test func trimsSurroundingWhitespace() throws {
        #expect(try InstanceID("  abcdefgh  ").rawValue == "abcdefgh")
    }

    @Test func rejectsEmpty() {
        #expect(throws: InstanceID.Failure.empty) { try InstanceID("   ") }
    }

    @Test func rejectsTooShortAndTooLong() {
        #expect(throws: InstanceID.Failure.tooShort(3)) { try InstanceID("abc") }
        #expect(throws: InstanceID.Failure.tooLong(129)) { try InstanceID(String(repeating: "a", count: 129)) }
    }

    /// The value becomes part of a Keychain account key, so anything that could
    /// act as a separator or escape the key format is refused at the boundary.
    @Test func rejectsCharactersThatCouldEscapeAStorageKey() {
        for hostile in ["abcdefgh:ij", "abcdefgh/ij", "abcdefgh ij", "abcdefgh\nij", "abcdefgh@ij", "abcdefgh\u{0}ij"] {
            #expect(throws: InstanceID.Failure.self) { try InstanceID(hostile) }
        }
    }
}

struct ServerIdentityTests {

    private func address(_ raw: String = "https://tv.example/") throws -> ServerAddress {
        try ServerAddressParser.parseTrusted(raw)
    }

    private func instanceID(_ raw: String = "instance-aaaa") throws -> InstanceID {
        try InstanceID(raw)
    }

    // MARK: - Identity equality

    /// Base paths distinguish deployments, so two installations behind one
    /// origin must remain distinct identities.
    @Test func deploymentsUnderOneOriginAreDistinctIdentities() throws {
        let home = ServerIdentity.address(try address("https://example.com/home/"))
        let lab = ServerIdentity.address(try address("https://example.com/lab/"))

        #expect(home != lab)
    }

    @Test func spellingVariantsAreOneIdentity() throws {
        let loud = ServerIdentity.address(try address("HTTPS://TV.EXAMPLE:443/"))
        let quiet = ServerIdentity.address(try address("https://tv.example/"))

        #expect(loud == quiet)
    }

    // MARK: - Reconciliation

    @Test func addressBindingIsUpgradedWhenTheServerReportsAnInstance() throws {
        let bound = ServerIdentity.address(try address())
        let observed = try instanceID()

        let resolution = ServerIdentity.reconcile(bound: bound, observedInstance: observed)

        #expect(resolution == .upgraded(from: bound, to: .instance(observed)))
        #expect(resolution.identity == .instance(observed))
        #expect(resolution.requiresRekeying)
        #expect(!resolution.blocksCredentialUse)
    }

    @Test func addressBindingStaysWhenNoInstanceIsReported() throws {
        let bound = ServerIdentity.address(try address())

        let resolution = ServerIdentity.reconcile(bound: bound, observedInstance: nil)

        #expect(resolution == .unchanged(bound))
        #expect(!resolution.requiresRekeying)
    }

    @Test func matchingInstanceIsUnchanged() throws {
        let bound = ServerIdentity.instance(try instanceID())

        let resolution = ServerIdentity.reconcile(bound: bound, observedInstance: try instanceID())

        #expect(resolution == .unchanged(bound))
        #expect(!resolution.blocksCredentialUse)
    }

    /// A familiar address now answering with a different installation. The
    /// credentials belong to the old one and must not be offered to the new.
    @Test func differentInstanceIsAConflictAndBlocksCredentialUse() throws {
        let bound = ServerIdentity.instance(try instanceID("instance-aaaa"))
        let observed = try instanceID("instance-bbbb")

        let resolution = ServerIdentity.reconcile(bound: bound, observedInstance: observed)

        #expect(resolution == .conflict(bound: bound, observed: observed))
        #expect(resolution.blocksCredentialUse)
        #expect(resolution.identity == bound, "a conflict must not adopt the observed identity")
    }

    /// The case the type exists for: the server failed to report an instance
    /// while we are instance-bound. A missing value is not evidence of a
    /// different server, so the stronger binding is kept.
    @Test func missingInstanceDoesNotWeakenAnInstanceBinding() throws {
        let bound = ServerIdentity.instance(try instanceID())

        let resolution = ServerIdentity.reconcile(bound: bound, observedInstance: nil)

        #expect(resolution == .instanceUnavailable(bound))
        #expect(resolution.identity == bound)
        #expect(resolution.identity.strength == .instance)
        #expect(!resolution.blocksCredentialUse)
    }

    // MARK: - The invariant itself

    /// Exhaustive over every reconciliation shape: no input can produce an
    /// identity weaker than the one already bound.
    @Test func reconciliationNeverWeakensABinding() throws {
        let bindings: [ServerIdentity] = [
            .address(try address("https://tv.example/")),
            .address(try address("https://tv.example/lab/")),
            .instance(try instanceID("instance-aaaa")),
        ]
        let observations: [InstanceID?] = [
            nil,
            try instanceID("instance-aaaa"),
            try instanceID("instance-bbbb"),
        ]

        for bound in bindings {
            for observed in observations {
                let resolution = ServerIdentity.reconcile(bound: bound, observedInstance: observed)
                #expect(
                    resolution.identity.strength >= bound.strength,
                    "reconcile(\(bound), \(String(describing: observed))) weakened the binding"
                )
            }
        }
    }
}
