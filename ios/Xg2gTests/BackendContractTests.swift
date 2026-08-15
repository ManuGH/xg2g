// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

/// The real client against the real server.
///
/// Every other test in this target uses doubles, and doubles share the failure
/// mode that matters most here: a client and a server can each satisfy their
/// own mock of the other and still disagree on the wire. Only this file can
/// catch that — the `deviceGrantId` schema drift and the missing Apple device
/// types were both found this way, by writing a client against a contract
/// rather than against an idea of one.
///
/// ## How this suite decides whether to run
///
/// The address is fixed in the scheme rather than passed on the command line:
/// `xcodebuild` accepts build settings, but a scheme environment variable does
/// not expand them, so the value arrives as the literal `$(NAME)` — measured,
/// after it happened.
///
/// So the gate is reachability. An ordinary `xcodebuild test` on a machine with
/// no fixture running skips this suite cleanly, while
/// `ios/scripts/run-contract-tests.sh` starts the fixture and then *fails* if
/// the suite reports skipped. That last part is the important one: xcodebuild
/// reports a fully skipped suite as TEST SUCCEEDED, which is exactly how a
/// contract test comes to mean nothing while looking green.
let contractBaseURL: String? = {
    guard let raw = ProcessInfo.processInfo.environment["XG2G_CONTRACT_BASE_URL"],
          !raw.trimmingCharacters(in: .whitespaces).isEmpty,
          // An unexpanded build setting is not an address.
          !raw.contains("$(")
    else { return nil }
    return raw
}()

/// A synchronous TCP connect, because a suite trait cannot await.
///
/// Deliberately not a URLSession request: this asks only whether something is
/// listening, and must not be confused by an HTTP error from something else
/// that happens to hold the port.
private func canConnect(to urlString: String, timeout: TimeInterval = 0.5) -> Bool {
    guard let url = URL(string: urlString), let host = url.host else { return false }
    let port = UInt16(url.port ?? (url.scheme == "https" ? 443 : 80))

    var hints = addrinfo(
        ai_flags: 0, ai_family: AF_UNSPEC, ai_socktype: SOCK_STREAM,
        ai_protocol: 0, ai_addrlen: 0, ai_canonname: nil, ai_addr: nil, ai_next: nil
    )
    var result: UnsafeMutablePointer<addrinfo>?
    guard getaddrinfo(host, String(port), &hints, &result) == 0, let addresses = result else {
        return false
    }
    defer { freeaddrinfo(addresses) }

    let handle = socket(addresses.pointee.ai_family, addresses.pointee.ai_socktype, addresses.pointee.ai_protocol)
    guard handle >= 0 else { return false }
    defer { close(handle) }

    var tv = timeval(tv_sec: Int(timeout), tv_usec: Int32((timeout - floor(timeout)) * 1_000_000))
    setsockopt(handle, SOL_SOCKET, SO_SNDTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))

    return connect(handle, addresses.pointee.ai_addr, addresses.pointee.ai_addrlen) == 0
}

private let contractServerIsReachable = contractBaseURL.map { canConnect(to: $0) } ?? false

@Suite(.enabled(if: contractServerIsReachable, "no contract fixture listening; run ios/scripts/run-contract-tests.sh"))
struct BackendContractTests {

    // MARK: - Wiring

    /// One device's full client stack, assembled exactly as the app would.
    private struct Device {
        let identity: ServerIdentity
        let address: ServerAddress
        let keys: SecureEnclaveDeviceKeyStore
        let credentials: KeychainCredentialStore
        let enrollment: EnrollmentCoordinator
        let session: SessionCoordinator
        let revoke: RevokeCoordinator

        /// A client that signs with the device's live session — the pipeline
        /// ordinary API calls go through.
        let authorized: HTTPAPIClient
    }

    private func makeDevice() async throws -> Device {
        let base = try #require(contractBaseURL)
        let address = try ServerAddressParser.parseTrusted(base)
        let identity = ServerIdentity.address(address)

        let keys = SecureEnclaveDeviceKeyStore(
            policy: .allowSoftware(reason: .development),
            tag: "io.github.manugh.xg2g.contract.\(UUID().uuidString)",
            secureEnclaveProbe: { false }
        )
        let credentials = KeychainCredentialStore(
            backend: InMemoryKeychainBackend(),
            marker: FakeInstallationMarker(marked: true)
        )
        try await credentials.prepareForLaunch()

        // Pairing is unauthenticated by definition.
        let openClient = HTTPAPIClient(address: address, session: .shared)
        // Refresh authenticates the device key alone.
        let refreshClient = HTTPAPIClient(
            address: address, session: .shared,
            authorizer: DeviceProofAuthorizer(keyStore: keys)
        )
        let session = SessionCoordinator(identity: identity, api: refreshClient, credentials: credentials)
        let authorized = HTTPAPIClient(
            address: address, session: .shared,
            authorizer: DPoPRequestAuthorizer(identity: identity, credentials: credentials, keyStore: keys)
        )

        return Device(
            identity: identity,
            address: address,
            keys: keys,
            credentials: credentials,
            enrollment: EnrollmentCoordinator(
                identity: identity, api: openClient, keyStore: keys, credentials: credentials
            ),
            session: session,
            revoke: RevokeCoordinator(
                identity: identity, api: authorized, credentials: credentials,
                keyStore: keys, session: session
            ),
            authorized: authorized
        )
    }

    /// Stands in for the human approving the pairing in the web UI. The client
    /// never does this and must never be able to: a device that could approve
    /// its own pairing would make the whole handshake decorative.
    private func approveAsHuman(_ device: Device, pairingID: String) async throws {
        var request = URLRequest(
            url: device.address.apiBaseURL.appendingPathComponent("pairing/\(pairingID)/approve")
        )
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = Data(#"{"approvedPolicyProfile":"tv-default"}"#.utf8)

        let (_, response) = try await URLSession.shared.data(for: request)
        let status = try #require((response as? HTTPURLResponse)?.statusCode)
        #expect(status == 200, "the approval fixture must succeed, or nothing downstream means anything")
    }

    /// Any authenticated call. `EmptyResponse` skips decoding, so what is
    /// measured is authentication rather than a payload shape.
    private func probe(_ device: Device) async throws {
        _ = try await device.authorized.send(
            APIRequest<EmptyResponse>(method: .get, path: "auth/effective-permissions")
        )
    }

    private func enroll(_ device: Device) async throws -> EnrolledCredentials {
        let invitation = try await device.enrollment.startPairing(deviceName: "Contract iPhone", deviceType: .iPhone)
        #expect(!invitation.userCode.isEmpty)

        try await approveAsHuman(device, pairingID: invitation.pairingID)
        #expect(try await device.enrollment.pairingStatus() == .approved)

        return try await device.enrollment.completeEnrollment()
    }

    private func isUnauthorized(_ error: any Error) -> Bool {
        guard let apiError = error as? APIError else { return false }
        switch apiError {
        case .problem(let problem): return problem.status == 401
        case .http(let status, _, _): return status == 401
        default: return false
        }
    }

    // MARK: - The full lifecycle

    /// Pair → exchange → authorized request → refresh → rotation → second
    /// request → self-revoke → nothing works any more.
    @Test func theCompleteDeviceLifecycle() async throws {
        let device = try await makeDevice()

        // 1-3. Pair, approve, exchange.
        let enrolled = try await enroll(device)
        #expect(!enrolled.grant.id.isEmpty)
        #expect(!enrolled.session.token.isEmpty)
        #expect(!enrolled.grant.secret.isEmpty)

        // 4. A DPoP-authorized API request. This is the step that proves the
        //    proof, the htu normalisation and the raw R‖S signature all match
        //    what the server independently expects.
        try await probe(device)

        // 5. Refresh, and confirm both halves actually rotated.
        let rotated = try await device.session.refresh()
        #expect(rotated.session.token != enrolled.session.token, "the access token must rotate")
        #expect(rotated.grant.secret != enrolled.grant.secret, "the refresh token must rotate")

        // 6. A second request, now on the rotated token.
        try await probe(device)

        // 7. Self-revoke.
        #expect(try await device.revoke.revokeThisDevice(destroyingDeviceKey: false) == .revoked)

        // 8. Nothing works afterwards. The credential store was cleared, so the
        //    authorizer refuses before reaching the network — which is the
        //    correct client behaviour, and the server-side half of this is
        //    asserted in the Go E2E.
        await #expect(throws: (any Error).self) { try await probe(device) }
        await #expect(throws: (any Error).self) { _ = try await device.session.refresh() }
    }

    // MARK: - Negative cases

    /// A revoked device's tokens are dead on the server too, not merely
    /// forgotten locally. Checked by keeping a copy of the credentials and
    /// putting them back after the revocation.
    @Test func revokedTokensAreDeadServerSide() async throws {
        let device = try await makeDevice()
        let enrolled = try await enroll(device)

        try await probe(device)
        _ = try await device.revoke.revokeThisDevice(destroyingDeviceKey: false)

        // Restore the exact credentials the server just retired.
        try await device.credentials.commit(enrolled, for: device.identity)

        await #expect(throws: (any Error).self) { try await probe(device) }

        // And the refresh token cannot mint a replacement.
        await #expect(throws: SessionCoordinator.Failure.reauthenticationRequired(.refreshRejected)) {
            _ = try await device.session.refresh()
        }
    }

    /// A proof signed by a key the access token is not bound to must not
    /// authenticate, however valid the token itself is.
    @Test func aForeignKeyCannotUseAValidToken() async throws {
        let device = try await makeDevice()
        _ = try await enroll(device)
        try await probe(device)

        let foreignKeys = SecureEnclaveDeviceKeyStore(
            policy: .allowSoftware(reason: .development),
            tag: "io.github.manugh.xg2g.contract.foreign.\(UUID().uuidString)",
            secureEnclaveProbe: { false }
        )
        let impostor = HTTPAPIClient(
            address: device.address, session: .shared,
            authorizer: DPoPRequestAuthorizer(
                identity: device.identity, credentials: device.credentials, keyStore: foreignKeys
            )
        )

        do {
            _ = try await impostor.send(
                APIRequest<EmptyResponse>(method: .get, path: "auth/effective-permissions")
            )
            Issue.record("a proof from a foreign key must not authenticate")
        } catch {
            #expect(isUnauthorized(error), "expected 401, got \(error)")
        }
    }

    /// Presenting a superseded refresh token is replay. The server revokes the
    /// whole family for it — which is why the client rotates atomically and
    /// never keeps the old value.
    @Test func aSupersededRefreshTokenIsRejectedAsReplay() async throws {
        let device = try await makeDevice()
        let enrolled = try await enroll(device)

        let rotated = try await device.session.refresh()
        #expect(rotated.grant.secret != enrolled.grant.secret)

        // Put the superseded token back, as a client with a stale copy would.
        try await device.credentials.commit(enrolled, for: device.identity)

        await #expect(throws: SessionCoordinator.Failure.reauthenticationRequired(.refreshRejected)) {
            _ = try await device.session.refresh()
        }
        #expect(await device.session.requiresReauthentication, "replay is terminal, never a retry loop")
    }

    /// A pairing that was already exchanged cannot be exchanged again.
    @Test func aConsumedPairingCannotBeReused() async throws {
        let device = try await makeDevice()
        let invitation = try await device.enrollment.startPairing(
            deviceName: "Contract iPhone", deviceType: .iPhone
        )
        try await approveAsHuman(device, pairingID: invitation.pairingID)
        _ = try await device.enrollment.completeEnrollment()

        // The coordinator clears the spent secret, so the client cannot even
        // attempt it — the replay is impossible one layer before the server.
        await #expect(throws: EnrollmentCoordinator.Failure.noPairingInProgress) {
            _ = try await device.enrollment.completeEnrollment()
        }
    }

    /// A pairing nobody approved must not be exchangeable.
    @Test func anUnapprovedPairingCannotBeExchanged() async throws {
        let device = try await makeDevice()
        _ = try await device.enrollment.startPairing(deviceName: "Contract iPhone", deviceType: .iPhone)

        #expect(try await device.enrollment.pairingStatus() == .pending)
        await #expect(throws: (any Error).self) { _ = try await device.enrollment.completeEnrollment() }
    }

    /// Two devices are two identities, with independent credentials.
    @Test func twoDevicesEnrollIndependently() async throws {
        let first = try await makeDevice()
        let second = try await makeDevice()

        let a = try await enroll(first)
        let b = try await enroll(second)

        #expect(a.grant.id != b.grant.id, "each enrollment must yield its own device identity")
        try await probe(first)
        try await probe(second)

        // Revoking one must not touch the other.
        _ = try await first.revoke.revokeThisDevice(destroyingDeviceKey: false)
        try await probe(second)
    }
}
