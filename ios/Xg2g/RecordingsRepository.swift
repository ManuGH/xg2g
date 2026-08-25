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

        let response: Xg2gContract.RecordingResponse = try await api.send(
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
        let body = try JSONEncoder().encode(Xg2gContract.RecordingResumeRequest(
            position: position,
            channel: channel,
            finished: finished,
            title: title,
            total: total
        ))

        let _: EmptyResponse = try await api.send(
            APIRequest(method: .put, path: "recordings/\(id)/resume", body: body, contentType: "application/json")
        )
    }

    func playbackUrl(for id: String) async throws -> String? {
        struct CapabilitiesPayload: Encodable, Sendable {
            let capabilitiesVersion: Int
            let supportsHls: Bool
            let allowTranscode: Bool
            let container: [String]
            let videoCodecs: [String]
            let audioCodecs: [String]
        }

        struct PlaybackInfoResponse: Decodable, Sendable {
            let url: String?
            let mode: String?
            let durationSeconds: Double?
            let decision: PlaybackDecision?

            struct PlaybackDecision: Decodable, Sendable {
                let selectedOutputUrl: String?
                let mode: String?
            }
        }

        let payload = CapabilitiesPayload(
            capabilitiesVersion: 1,
            supportsHls: true,
            allowTranscode: true,
            container: ["m3u8", "ts", "mp4"],
            videoCodecs: ["h264", "hevc"],
            audioCodecs: ["aac", "ac3", "eac3"]
        )
        let body = try JSONEncoder().encode(payload)

        for attempt in 0..<3 {
            do {
                let response: PlaybackInfoResponse = try await api.send(
                    APIRequest(method: .post, path: "recordings/\(id)/stream-info", body: body, contentType: "application/json")
                )
                if let directUrl = response.url ?? response.decision?.selectedOutputUrl, !directUrl.isEmpty {
                    return directUrl
                }
                break
            } catch {
                if attempt < 2 {
                    try? await Task.sleep(nanoseconds: 1_500_000_000)
                    continue
                }
                throw error
            }
        }

        // Nothing negotiated. The canonical playlist path is the transport's to
        // know, not this repository's; `nil` says "the backend named no URL".
        return nil
    }
}

// MARK: - Contract Domain Mapping

extension Xg2gContract.RecordingItem {
    func toDomain() -> Recording? {
        guard let title = title?.trimmingCharacters(in: .whitespaces), !title.isEmpty,
              let id = (recordingId ?? filename)?.trimmingCharacters(in: .whitespaces), !id.isEmpty
        else { return nil }

        let startSeconds = beginUnixSeconds ?? 0
        let duration = Int(durationSeconds ?? 0)

        var serverResume: Double? = nil
        if let r = resume, r.posSeconds > 0, !(r.finished ?? false) {
            serverResume = Double(r.posSeconds)
        }

        return Recording(
            id: id,
            title: title,
            description: description?.trimmingCharacters(in: .whitespaces).isEmpty == false ? description : nil,
            beginDate: Date(timeIntervalSince1970: TimeInterval(startSeconds)),
            durationSeconds: duration,
            serviceRef: serviceRef,
            filename: filename,
            status: status.rawValue,
            serverResumePos: serverResume
        )
    }
}
