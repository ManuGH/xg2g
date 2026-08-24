// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Observation

/// Manages background offline downloads for DVR recordings (Plex/Netflix style).
/// Supports multi-profile quality selection (Original 1:1, 720p HD, and HEVC Airplane compact).
@Observable
@MainActor
final class DownloadManager: NSObject {

    static let shared = DownloadManager()

    enum DownloadStatus: Equatable, Sendable {
        case notDownloaded
        case downloading(progress: Double)
        case downloaded(OfflineRecording)
        case failed(String)
    }

    private(set) var offlineRecordings: [OfflineRecording] = []
    private(set) var activeDownloads: [String: Double] = [:]

    var defaultQuality: DownloadQuality {
        get {
            let raw = UserDefaults.standard.string(forKey: "xg2g.downloadQuality") ?? DownloadQuality.original.rawValue
            return DownloadQuality(rawValue: raw) ?? .original
        }
        set {
            UserDefaults.standard.set(newValue.rawValue, forKey: "xg2g.downloadQuality")
        }
    }

    var wifiOnly: Bool {
        get {
            UserDefaults.standard.object(forKey: "xg2g.downloadWifiOnly") as? Bool ?? true
        }
        set {
            UserDefaults.standard.set(newValue, forKey: "xg2g.downloadWifiOnly")
        }
    }

    private struct DownloadTaskPayload: Codable, Sendable {
        let recordingId: String
        let title: String
        let channelName: String?
        let durationSeconds: Int
        let quality: DownloadQuality
    }

    private var downloadTasks: [String: URLSessionDownloadTask] = [:]
    private var taskToRecordingMap: [Int: Recording] = [:]
    private var taskToQualityMap: [Int: DownloadQuality] = [:]
    private var session: URLSession!

    override private init() {
        super.init()
        loadPersistedRecordings()
        let config = URLSessionConfiguration.background(withIdentifier: "de.matrixcentral.xg2g.downloads")
        config.isDiscretionary = false
        config.sessionSendsLaunchEvents = true
        config.allowsCellularAccess = !wifiOnly
        self.session = URLSession(configuration: config, delegate: self, delegateQueue: nil)

        // Reattach to running background tasks across app relaunches
        session.getAllTasks { [weak self] tasks in
            Task { @MainActor [weak self] in
                guard let self else { return }
                for task in tasks {
                    guard let downloadTask = task as? URLSessionDownloadTask,
                          let desc = downloadTask.taskDescription,
                          let data = desc.data(using: .utf8),
                          let payload = try? JSONDecoder().decode(DownloadTaskPayload.self, from: data) else {
                        continue
                    }
                    self.downloadTasks[payload.recordingId] = downloadTask
                    if self.activeDownloads[payload.recordingId] == nil {
                        self.activeDownloads[payload.recordingId] = 0.05
                    }
                }
            }
        }
    }

    func status(for recordingId: String) -> DownloadStatus {
        if let offline = offlineRecordings.first(where: { $0.recordingId == recordingId }) {
            return .downloaded(offline)
        }
        if let progress = activeDownloads[recordingId] {
            return .downloading(progress: progress)
        }
        return .notDownloaded
    }

    static func safeFilename(for recordingId: String) -> String {
        let sanitized = recordingId
            .replacingOccurrences(of: "[^a-zA-Z0-9._-]", with: "_", options: .regularExpression)
            .trimmingCharacters(in: CharacterSet(charactersIn: "._"))
        let safeName = sanitized.isEmpty ? UUID().uuidString : sanitized
        return "\(safeName).mp4"
    }

    func startDownload(
        recording: Recording,
        serverBaseURL: URL,
        quality: DownloadQuality? = nil,
        sessionCookie: String? = nil,
        authToken: String? = nil
    ) {
        guard status(for: recording.id) == .notDownloaded else { return }

        let q = quality ?? defaultQuality
        var components = URLComponents(
            url: serverBaseURL.appendingPathComponent("api/v3/recordings/\(recording.id)/stream.mp4"),
            resolvingAgainstBaseURL: true
        )

        var queryItems: [URLQueryItem] = []
        switch q {
        case .original:
            break
        case .av1:
            queryItems.append(URLQueryItem(name: "quality", value: "compact"))
            queryItems.append(URLQueryItem(name: "video_codec", value: "av1"))
        case .compact:
            queryItems.append(URLQueryItem(name: "quality", value: "compact"))
            queryItems.append(URLQueryItem(name: "video_codec", value: "hevc"))
        case .high:
            queryItems.append(URLQueryItem(name: "quality", value: "high"))
        }

        if !queryItems.isEmpty {
            components?.queryItems = queryItems
        }

        guard let downloadURL = components?.url ?? serverBaseURL.appendingPathComponent("api/v3/recordings/\(recording.id)/stream.mp4") as URL? else {
            return
        }

        var request = URLRequest(url: downloadURL)
        request.httpMethod = "GET"
        if let cookie = sessionCookie, !cookie.isEmpty {
            request.setValue("xg2g_session=\(cookie)", forHTTPHeaderField: "Cookie")
        }
        if let token = authToken, !token.isEmpty {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }

        let task = session.downloadTask(with: request)
        let payload = DownloadTaskPayload(
            recordingId: recording.id,
            title: recording.title,
            channelName: recording.serviceRef ?? recording.filename,
            durationSeconds: recording.durationSeconds,
            quality: q
        )
        if let data = try? JSONEncoder().encode(payload),
           let jsonString = String(data: data, encoding: .utf8) {
            task.taskDescription = jsonString
        }

        taskToRecordingMap[task.taskIdentifier] = recording
        taskToQualityMap[task.taskIdentifier] = q
        downloadTasks[recording.id] = task
        activeDownloads[recording.id] = 0.01
        task.resume()
    }

    func cancelDownload(recordingId: String) {
        if let task = downloadTasks[recordingId] {
            task.cancel()
            downloadTasks.removeValue(forKey: recordingId)
        }
        activeDownloads.removeValue(forKey: recordingId)
    }

    func deleteOfflineRecording(id: String) {
        guard let idx = offlineRecordings.firstIndex(where: { $0.id == id || $0.recordingId == id }) else { return }
        let recording = offlineRecordings[idx]
        let fileURL = recording.localFileURL()
        try? FileManager.default.removeItem(at: fileURL)
        offlineRecordings.remove(at: idx)
        persistRecordings()
    }

    var totalStorageUsed: Int64 {
        offlineRecordings.reduce(0) { $0 + $1.fileSize }
    }

    var formattedTotalStorage: String {
        let formatter = ByteCountFormatter()
        formatter.allowedUnits = [.useMB, .useGB]
        formatter.countStyle = .file
        return formatter.string(fromByteCount: totalStorageUsed)
    }

    // MARK: - Persistence

    private func loadPersistedRecordings() {
        let url = metadataFileURL()
        guard let data = try? Data(contentsOf: url),
              let list = try? JSONDecoder().decode([OfflineRecording].self, from: data) else {
            return
        }
        self.offlineRecordings = list
    }

    private func persistRecordings() {
        let url = metadataFileURL()
        guard let data = try? JSONEncoder().encode(offlineRecordings) else { return }
        try? data.write(to: url, options: .atomic)
    }

    private func metadataFileURL() -> URL {
        let docs = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
        return docs.appendingPathComponent("offline_recordings_metadata.json")
    }

    private func ensureOfflineDirectory() -> URL {
        let docs = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
        var dir = docs.appendingPathComponent("OfflineRecordings", isDirectory: true)
        if !FileManager.default.fileExists(atPath: dir.path) {
            try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        }
        var values = URLResourceValues()
        values.isExcludedFromBackup = true
        try? dir.setResourceValues(values)
        return dir
    }

    fileprivate func handleDownloadCompletion(task: URLSessionDownloadTask, stagedLocation: URL) {
        defer { try? FileManager.default.removeItem(at: stagedLocation) }
        var recId: String? = taskToRecordingMap[task.taskIdentifier]?.id
        var recTitle: String? = taskToRecordingMap[task.taskIdentifier]?.title
        var recChannel: String? = taskToRecordingMap[task.taskIdentifier]?.serviceRef ?? taskToRecordingMap[task.taskIdentifier]?.filename
        var recDuration: Int = taskToRecordingMap[task.taskIdentifier]?.durationSeconds ?? 0
        var quality: DownloadQuality = taskToQualityMap[task.taskIdentifier] ?? .compact

        if (recId == nil || recTitle == nil),
           let desc = task.taskDescription,
           let data = desc.data(using: .utf8),
           let payload = try? JSONDecoder().decode(DownloadTaskPayload.self, from: data) {
            recId = payload.recordingId
            recTitle = payload.title
            recChannel = payload.channelName
            recDuration = payload.durationSeconds
            quality = payload.quality
        }

        guard let recordingId = recId, let title = recTitle else { return }

        let offlineDir = ensureOfflineDirectory()
        let filename = Self.safeFilename(for: recordingId)
        var destURL = offlineDir.appendingPathComponent(filename)

        try? FileManager.default.removeItem(at: destURL)
        do {
            try FileManager.default.moveItem(at: stagedLocation, to: destURL)
            var values = URLResourceValues()
            values.isExcludedFromBackup = true
            try? destURL.setResourceValues(values)

            let fileSize = (try? FileManager.default.attributesOfItem(atPath: destURL.path)[.size] as? Int64) ?? 0

            let offline = OfflineRecording(
                id: UUID().uuidString,
                recordingId: recordingId,
                title: title,
                channelName: recChannel,
                durationSeconds: recDuration,
                fileSize: fileSize,
                downloadDate: Date(),
                localRelativePath: "OfflineRecordings/\(filename)",
                quality: quality
            )

            self.activeDownloads.removeValue(forKey: recordingId)
            self.downloadTasks.removeValue(forKey: recordingId)
            self.taskToRecordingMap.removeValue(forKey: task.taskIdentifier)
            self.taskToQualityMap.removeValue(forKey: task.taskIdentifier)
            self.offlineRecordings.insert(offline, at: 0)
            self.persistRecordings()
        } catch {
            self.activeDownloads.removeValue(forKey: recordingId)
        }
    }

    fileprivate func handleDownloadFailure(task: URLSessionDownloadTask) {
        var recId: String? = taskToRecordingMap[task.taskIdentifier]?.id
        if recId == nil,
           let desc = task.taskDescription,
           let data = desc.data(using: .utf8),
           let payload = try? JSONDecoder().decode(DownloadTaskPayload.self, from: data) {
            recId = payload.recordingId
        }
        if let recId {
            activeDownloads.removeValue(forKey: recId)
            downloadTasks.removeValue(forKey: recId)
        }
    }

    fileprivate func handleProgressUpdate(task: URLSessionDownloadTask, bytesWritten: Int64, totalBytes: Int64) {
        var recId: String? = taskToRecordingMap[task.taskIdentifier]?.id
        if recId == nil,
           let desc = task.taskDescription,
           let data = desc.data(using: .utf8),
           let payload = try? JSONDecoder().decode(DownloadTaskPayload.self, from: data) {
            recId = payload.recordingId
        }

        guard let recordingId = recId else { return }
        let progress = totalBytes > 0 ? Double(bytesWritten) / Double(totalBytes) : 0.05
        self.activeDownloads[recordingId] = min(0.99, max(0.02, progress))
    }
}

// MARK: - URLSessionDownloadDelegate

extension DownloadManager: URLSessionDownloadDelegate {

    nonisolated func urlSession(
        _ session: URLSession,
        downloadTask: URLSessionDownloadTask,
        didFinishDownloadingTo location: URL
    ) {
        let stagingURL = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString + ".mp4")
        do {
            try FileManager.default.moveItem(at: location, to: stagingURL)
            Task { @MainActor in
                DownloadManager.shared.handleDownloadCompletion(task: downloadTask, stagedLocation: stagingURL)
            }
        } catch {
            Task { @MainActor in
                DownloadManager.shared.handleDownloadFailure(task: downloadTask)
            }
        }
    }

    nonisolated func urlSession(
        _ session: URLSession,
        downloadTask: URLSessionDownloadTask,
        didWriteData bytesWritten: Int64,
        totalBytesWritten: Int64,
        totalBytesExpectedToWrite: Int64
    ) {
        Task { @MainActor in
            DownloadManager.shared.handleProgressUpdate(
                task: downloadTask,
                bytesWritten: totalBytesWritten,
                totalBytes: totalBytesExpectedToWrite
            )
        }
    }
}
