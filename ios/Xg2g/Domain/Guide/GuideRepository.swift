// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// A time interval window for querying EPG broadcast schedules.
struct GuideWindow: Sendable, Equatable {
    let start: Date
    let end: Date

    init(start: Date, end: Date) {
        self.start = start
        self.end = end
    }

    /// Convenience window starting from `now` with a given hour duration.
    static func fromNow(hours: Double = 6) -> GuideWindow {
        let now = Date()
        return GuideWindow(start: now, end: now.addingTimeInterval(hours * 3600))
    }
}

/// Domain contract for channel metadata and electronic programme guide (EPG) schedules.
protocol GuideRepository: Sendable {
    /// Loads all available broadcast channels.
    func channels() async throws -> [Channel]

    /// Loads the current and upcoming show for all known channels.
    func nowNext() async throws -> [String: NowNext]

    /// Loads the schedule for the specified channels within the time window.
    func schedule(for channels: [Channel], in window: GuideWindow) async throws -> [GuideChannelSchedule]
}
