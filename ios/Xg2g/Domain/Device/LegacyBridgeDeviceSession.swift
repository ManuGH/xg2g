// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Adapter bridging the Greenfield `DeviceSession` protocol to `AppModel`.
@MainActor
final class LegacyBridgeDeviceSession: DeviceSession {
    private weak var appModel: AppModel?

    init(appModel: AppModel) {
        self.appModel = appModel
    }

    var isConnected: Bool {
        appModel?.state == .ready
    }

    var activeServerAddress: ServerAddress? {
        appModel?.serverAddress
    }

    func connect(address: ServerAddress) async throws {
        // AppModel connection / pairing logic
    }

    func disconnect() async {
        await appModel?.disconnectServer()
    }
}
