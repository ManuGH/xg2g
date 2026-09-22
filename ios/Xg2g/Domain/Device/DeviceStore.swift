// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Observation

/// UI-facing store managing backend connectivity, pairing status, and session credentials.
///
/// Strictly bound to `@MainActor` for thread-safe SwiftUI updates, delegating all
/// background handshake and network coordination to an injected `DeviceSession`.
@MainActor
@Observable
final class DeviceStore {
    private(set) var isConnected: Bool = false
    private(set) var serverAddress: ServerAddress?
    private(set) var isConnecting: Bool = false
    private(set) var errorMessage: String?

    private let session: any DeviceSession

    init(session: any DeviceSession) {
        self.session = session
        self.isConnected = session.isConnected
        self.serverAddress = session.activeServerAddress
    }

    /// Connects to a specific server address.
    func connect(to address: ServerAddress) async {
        isConnecting = true
        defer { isConnecting = false }
        do {
            try await session.connect(address: address)
            serverAddress = address
            isConnected = true
            errorMessage = nil
        } catch {
            errorMessage = error.localizedDescription
            isConnected = false
        }
    }

    /// Disconnects from the server.
    func disconnect() async {
        await session.disconnect()
        serverAddress = nil
        isConnected = false
    }

    /// Synchronizes state from external session changes.
    func updateState(isConnected: Bool, address: ServerAddress?) {
        self.isConnected = isConnected
        self.serverAddress = address
    }
}
