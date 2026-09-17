// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// A scheduled or active DVR timer.
struct DVRTimer: Identifiable, Equatable, Sendable {
    let id: String
    let name: String
    let description: String?
    let serviceRef: String
    let serviceName: String?
    let beginDate: Date
    let endDate: Date
    let state: String

    var formattedTimeRange: String {
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .short
        let endFormatter = DateFormatter()
        endFormatter.dateStyle = .none
        endFormatter.timeStyle = .short
        return "\(formatter.string(from: beginDate)) – \(endFormatter.string(from: endDate))"
    }

    var isRunning: Bool {
        state.lowercased() == "running" || state.lowercased() == "active" || state.lowercased() == "recording"
    }
}
