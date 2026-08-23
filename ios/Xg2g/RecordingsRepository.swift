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

    var genre: EpgGenre {
        EpgGenreClassifier.classify(title: title, description: description, channelName: nil)
    }
}

/// Reads recordings from the DVR library and manages server-side resume state.
actor RecordingsRepository {

    private let api: APIClient

    init(api: APIClient) {
        self.api = api
    }

    func recordings(root: String? = nil, path: String? = nil) async throws -> [Recording] {
        var query: [URLQueryItem] = []
        if let root { query.append(URLQueryItem(name: "root", value: root)) }
        if let path { query.append(URLQueryItem(name: "path", value: path)) }

        let response: RecordingWire.Response = try await api.send(
            APIRequest(method: .get, path: "recordings", query: query)
        )

        return (response.recordings ?? [])
            .compactMap { $0.toDomain() }
            .sorted { $0.beginDate > $1.beginDate }
    }

    func deleteRecording(id: String) async throws {
        let _: EmptyResponse = try await api.send(
            APIRequest(method: .delete, path: "recordings/\(id)")
        )
    }

    func saveResume(id: String, position: Double, total: Double, finished: Bool, title: String, channel: String) async throws {
        struct ResumeRequest: Encodable {
            let position: Double
            let total: Double
            let finished: Bool
            let title: String
            let channel: String
        }

        let body = try JSONEncoder().encode(ResumeRequest(
            position: position,
            total: total,
            finished: finished,
            title: title,
            channel: channel
        ))

        let _: EmptyResponse = try await api.send(
            APIRequest(method: .put, path: "recordings/\(id)/resume", body: body, contentType: "application/json")
        )
    }
}

// MARK: - Wire

enum RecordingWire {

    struct Response: Decodable, Sendable {
        let requestId: String
        let currentRoot: String?
        let currentPath: String?
        let recordings: [Item]?
        let directories: [DirectoryItem]?
        let roots: [RootItem]?
        let breadcrumbs: [BreadcrumbItem]?
    }

    struct DirectoryItem: Decodable, Sendable {
        let name: String?
        let path: String?
    }

    struct RootItem: Decodable, Sendable {
        let id: String?
        let name: String?
    }

    struct BreadcrumbItem: Decodable, Sendable {
        let name: String?
        let path: String?
    }

    struct ResumeInfo: Decodable, Sendable {
        let posSeconds: Int64?
        let durationSeconds: Int64?
        let finished: Bool?
        let updatedAt: String?
    }

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
        let resume: ResumeInfo?

        func toDomain() -> Recording? {
            guard let title = title?.trimmingCharacters(in: .whitespaces), !title.isEmpty,
                  let id = (recordingId ?? filename)?.trimmingCharacters(in: .whitespaces), !id.isEmpty
            else { return nil }

            let startSeconds = beginUnixSeconds ?? 0
            let duration = Int(durationSeconds ?? 0)

            var serverResume: Double? = nil
            if let r = resume, let pos = r.posSeconds, pos > 0, !(r.finished ?? false) {
                serverResume = Double(pos)
            }

            return Recording(
                id: id,
                title: title,
                description: description?.trimmingCharacters(in: .whitespaces).isEmpty == false ? description : nil,
                beginDate: Date(timeIntervalSince1970: TimeInterval(startSeconds)),
                durationSeconds: duration,
                serviceRef: serviceRef,
                filename: filename,
                status: status ?? "completed",
                serverResumePos: serverResume
            )
        }
    }
}
