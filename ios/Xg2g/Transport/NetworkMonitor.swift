// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Network

/// Monitors network interface transitions (WiFi ↔ Cellular 5G/4G)
/// and informs the stream capability planner for optimal bandwidth adaptation.
@Observable
final class NetworkMonitor: @unchecked Sendable {

    static let shared = NetworkMonitor()

    enum ConnectionType: String, Sendable {
        case wifi = "wifi"
        case cellular = "cellular"
        case wired = "wired"
        case other = "other"
    }

    private(set) var currentType: ConnectionType = .wifi
    private(set) var isExpensive: Bool = false
    private(set) var isConstrained: Bool = false

    private let monitor: NWPathMonitor
    private let queue = DispatchQueue(label: "de.matrixcentral.xg2g.network-monitor")

    private init() {
        self.monitor = NWPathMonitor()
        self.monitor.pathUpdateHandler = { [weak self] path in
            guard let self else { return }
            let type: ConnectionType
            if path.usesInterfaceType(.wifi) {
                type = .wifi
            } else if path.usesInterfaceType(.cellular) {
                type = .cellular
            } else if path.usesInterfaceType(.wiredEthernet) {
                type = .wired
            } else {
                type = .other
            }

            Task { @MainActor in
                self.currentType = type
                self.isExpensive = path.isExpensive
                self.isConstrained = path.isConstrained
            }
        }
        self.monitor.start(queue: queue)
    }

    deinit {
        monitor.cancel()
    }
}
