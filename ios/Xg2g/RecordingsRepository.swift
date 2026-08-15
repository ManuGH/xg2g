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
}

/// Reads recordings from the DVR library.
actor RecordingsRepository {

    private let api: APIClient

    init(api: APIClient) {
        self.api = api
    }

    func recordings(root: String? = nil, path: String? = nil) async throws -> [Recording] {
        var query: [URLQueryItem] = []
        if let root { query.append(URLQueryItem(name: "root", value: root)) }
        if let path { query.append(URLQueryItem(name: "path", value: path)) }

        let items: [RecordingWire.Item] = try await api.send(
            APIRequest(method: .get, path: "recordings", query: query)
        )

        return items
            .compactMap { $0.toDomain() }
            .sorted { $0.beginDate > $1.beginDate }
    }
}

// MARK: - Wire

enum RecordingWire {

    struct Item: Decodable, Sendable {
        let recordingId: String?
        let title: String?
        let description: String?
        let beginUnixSeconds: Int64?
        let durationSeconds: Int64?
        let length: String?
        let filename: String?
        let serviceRef: String?
        let status: String?

        func toDomain() -> Recording? {
            guard let title = title?.trimmingCharacters(in: .whitespaces), !title.isEmpty,
                  let id = (recordingId ?? filename)?.trimmingCharacters(in: .whitespaces), !id.isEmpty
            else { return nil }

            let startSeconds = beginUnixSeconds ?? 0
            let duration = Int(durationSeconds ?? 0)

            return Recording(
                id: id,
                title: title,
                description: description?.trimmingCharacters(in: .whitespaces).isEmpty == false ? description : nil,
                beginDate: Date(timeIntervalSince1970: TimeInterval(startSeconds)),
                durationSeconds: duration,
                serviceRef: serviceRef,
                filename: filename,
                status: status ?? "completed"
            )
        }
    }
}
