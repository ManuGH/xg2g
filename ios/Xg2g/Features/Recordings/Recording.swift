// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// A recorded programme in the DVR library.
struct Recording: Identifiable, Equatable, Sendable {
    let id: String
    let title: String
    let description: String?
    let beginDate: Date
    let durationSeconds: Int
    let serviceRef: String?
    let filename: String?
    let status: String
    let serverResumePos: Double?

    var formattedDuration: String {
        let minutes = durationSeconds / 60
        let hours = minutes / 60
        let remainingMinutes = minutes % 60
        if hours > 0 {
            return "\(hours)h \(remainingMinutes)m"
        }
        return "\(minutes)m"
    }

    var formattedDate: String {
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .short
        return formatter.string(from: beginDate)
    }

    var genre: EPGGenre {
        EPGGenreClassifier.classify(title: title, description: description, channelName: nil)
    }
}
