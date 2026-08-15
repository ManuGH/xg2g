// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Observation

/// Manages background offline downloads for DVR recordings (Plex/Netflix style).
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

    private var downloadTasks: [String: URLSessionDownloadTask] = [:]
    private var taskToRecordingMap: [Int: Recording] = [:]
    private var session: URLSession!

    override private init() {
        super.init()
        loadPersistedRecordings()
        let config = URLSessionConfiguration.background(withIdentifier: "de.matrixcentral.xg2g.downloads")
        config.isDiscretionary = false
        config.sessionSendsLaunchEvents = true
        self.session = URLSession(configuration: config, delegate: self, delegateQueue: nil)
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

    func startDownload(recording: Recording, serverBaseURL: URL) {
        guard status(for: recording.id) == .notDownloaded else { return }

        let downloadURL = serverBaseURL.appendingPathComponent("api/v3/recordings/\(recording.id)/stream.mp4")
        let task = session.downloadTask(with: downloadURL)
        taskToRecordingMap[task.taskIdentifier] = recording
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
        let dir = docs.appendingPathComponent("OfflineRecordings", isDirectory: true)
        if !FileManager.default.fileExists(atPath: dir.path) {
            try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        }
        return dir
    }

    fileprivate func handleDownloadCompletion(task: URLSessionDownloadTask, location: URL) {
        guard let recording = taskToRecordingMap[task.taskIdentifier] else { return }

        let offlineDir = ensureOfflineDirectory()
        let filename = "\(recording.id).mp4"
        let destURL = offlineDir.appendingPathComponent(filename)

        try? FileManager.default.removeItem(at: destURL)
        do {
            try FileManager.default.moveItem(at: location, to: destURL)
            let fileSize = (try? FileManager.default.attributesOfItem(atPath: destURL.path)[.size] as? Int64) ?? 0

            let offline = OfflineRecording(
                id: UUID().uuidString,
                recordingId: recording.id,
                title: recording.title,
                channelName: recording.serviceRef ?? recording.filename,
                durationSeconds: recording.durationSeconds,
                fileSize: fileSize,
                downloadDate: Date(),
                localRelativePath: "OfflineRecordings/\(filename)"
            )

            Task { @MainActor in
                self.activeDownloads.removeValue(forKey: recording.id)
                self.downloadTasks.removeValue(forKey: recording.id)
                self.taskToRecordingMap.removeValue(forKey: task.taskIdentifier)
                self.offlineRecordings.insert(offline, at: 0)
                self.persistRecordings()
            }
        } catch {
            Task { @MainActor in
                self.activeDownloads.removeValue(forKey: recording.id)
            }
        }
    }

    fileprivate func handleProgressUpdate(task: URLSessionDownloadTask, bytesWritten: Int64, totalBytes: Int64) {
        guard let recording = taskToRecordingMap[task.taskIdentifier] else { return }
        let progress = totalBytes > 0 ? Double(bytesWritten) / Double(totalBytes) : 0.05
        Task { @MainActor in
            self.activeDownloads[recording.id] = min(0.99, max(0.02, progress))
        }
    }
}

// MARK: - URLSessionDownloadDelegate

extension DownloadManager: URLSessionDownloadDelegate {

    nonisolated func urlSession(
        _ session: URLSession,
        downloadTask: URLSessionDownloadTask,
        didFinishDownloadingTo location: URL
    ) {
        Task { @MainActor in
            DownloadManager.shared.handleDownloadCompletion(task: downloadTask, location: location)
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
