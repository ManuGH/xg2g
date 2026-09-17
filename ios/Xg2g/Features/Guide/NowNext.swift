// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// What is on now, and what follows.
struct NowNext: Equatable, Sendable {
    struct Entry: Identifiable, Equatable, Sendable {
        var id: String { "\(start.timeIntervalSince1970)_\(title)" }
        let title: String
        let description: String?
        let start: Date
        let end: Date

        private static let timeFormatter: DateFormatter = {
            let f = DateFormatter()
            f.dateFormat = "HH:mm"
            f.timeZone = .current
            return f
        }()

        /// How far through the programme we are, 0…1. `nil` before it starts or
        /// after it ends, so a caller cannot mistake "not on" for "just began".
        func progress(at now: Date) -> Double? {
            let total = end.timeIntervalSince(start)
            guard total > 0, now >= start, now <= end else { return nil }
            return now.timeIntervalSince(start) / total
        }

        /// Minutes left in the currently running programme.
        func remainingMinutes(at now: Date) -> Int? {
            guard now >= start, now <= end else { return nil }
            let secondsLeft = end.timeIntervalSince(now)
            return max(1, Int(secondsLeft / 60))
        }

        var formattedStartTime: String {
            Self.timeFormatter.string(from: start)
        }

        var formattedEndTime: String {
            Self.timeFormatter.string(from: end)
        }

        var formattedTimeRange: String {
            "\(Self.timeFormatter.string(from: start)) – \(Self.timeFormatter.string(from: end))"
        }

        var formattedDayHeader: String {
            let calendar = Calendar.current
            if calendar.isDateInToday(start) {
                return "HEUTE"
            } else if calendar.isDateInTomorrow(start) {
                return "MORGEN"
            } else {
                let f = DateFormatter()
                f.locale = Locale(identifier: "de_DE")
                f.dateFormat = "EEEE, d. MMMM"
                return f.string(from: start).uppercased()
            }
        }

        var dayIdentifier: String {
            let f = DateFormatter()
            f.dateFormat = "yyyy-MM-dd"
            return f.string(from: start)
        }

        var durationMinutes: Int {
            max(1, Int(end.timeIntervalSince(start) / 60))
        }

        var genre: EPGGenre {
            genre(channelName: nil)
        }

        func genre(channelName: String? = nil) -> EPGGenre {
            EPGGenreClassifier.classify(title: title, description: description, channelName: channelName)
        }

        func matches(genre: EPGGenre, channelName: String? = nil) -> Bool {
            if genre == .all { return true }
            return self.genre(channelName: channelName) == genre
        }
    }

    let serviceRef: String
    let now: Entry?
    let next: Entry?
}
