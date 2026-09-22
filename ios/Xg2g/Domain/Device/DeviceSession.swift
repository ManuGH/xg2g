// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Domain contract for backend server connectivity, authentication session, and device health.
///
/// - Important: Currently isolated to `@MainActor` as a **transitional legacy constraint**
///   because the existing `AppModel` maintains session state, credentials, and pairing status
///   directly on the main actor.
/// - Follow-up: In Apple C / Step 9 (API v4 client), `DeviceSession` will be decoupled from `AppModel`
///   into a background `Sendable` actor boundary (`protocol DeviceSession: Sendable`).
@MainActor
protocol DeviceSession: AnyObject {
    /// Whether a secure authenticated session with the backend is active.
    var isConnected: Bool { get }

    /// The currently active server address.
    var activeServerAddress: ServerAddress? { get }

    /// Connects to a backend server.
    func connect(address: ServerAddress) async throws

    /// Disconnects and releases active session state.
    func disconnect() async
}
