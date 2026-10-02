// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Quality profiles for offline downloads (Netflix / Plex style).
enum DownloadQuality: String, CaseIterable, Identifiable, Codable, Sendable {
    case original = "original"
    case av1 = "av1"
    case compact = "compact"
    case high = "high"

    var id: String { rawValue }

    /// Returns only the qualities supported by the local device's hardware decoder.
    static var supportedQualities: [DownloadQuality] {
        if DeviceCapabilities.supportsAV1 {
            return [.original, .av1, .compact, .high]
        } else {
            return [.original, .compact, .high]
        }
    }

    var title: String {
        switch self {
        case .original: return String(localized: "Original HD (1:1)")
        case .av1: return String(localized: "Ultra Compact (AV1)")
        case .compact: return String(localized: "Airplane / Compact (HEVC)")
        case .high: return String(localized: "720p HD (Balanced)")
        }
    }

    var localizedTitle: LocalizedStringResource {
        switch self {
        case .original: return LocalizedStringResource("Original HD (1:1)")
        case .av1: return LocalizedStringResource("Ultra Compact (AV1)")
        case .compact: return LocalizedStringResource("Airplane / Compact (HEVC)")
        case .high: return LocalizedStringResource("720p HD (Balanced)")
        }
    }

    var subtitle: String {
        switch self {
        case .original: return String(localized: "Best bitrate (~3.5 GB / 90m)")
        case .av1: return String(localized: "Next-gen AV1 efficiency (~550 MB / 90m)")
        case .compact: return String(localized: "Save up to 75% storage (~800 MB / 90m)")
        case .high: return String(localized: "High image quality (~1.5 GB / 90m)")
        }
    }

    var icon: String {
        switch self {
        case .original: return "sparkles"
        case .av1: return "bolt.shield.fill"
        case .compact: return "airplane"
        case .high: return "tv"
        }
    }

    func estimateSizeBytes(durationSeconds: Int) -> Int64 {
        let hours = max(0.25, Double(durationSeconds) / 3600.0)
        switch self {
        case .original:
            return Int64(hours * 3.5 * 1024 * 1024 * 1024)
        case .av1:
            return Int64(hours * 0.4 * 1024 * 1024 * 1024)
        case .compact:
            return Int64(hours * 0.6 * 1024 * 1024 * 1024)
        case .high:
            return Int64(hours * 1.4 * 1024 * 1024 * 1024)
        }
    }

    func formattedEstimatedSize(durationSeconds: Int) -> String {
        let formatter = ByteCountFormatter()
        formatter.allowedUnits = [.useMB, .useGB]
        formatter.countStyle = .file
        return formatter.string(fromByteCount: estimateSizeBytes(durationSeconds: durationSeconds))
    }
}

/// A locally downloaded recording for offline playback (Plex/Netflix style).
struct OfflineRecording: Identifiable, Codable, Equatable, Sendable {
    let id: String
    let recordingId: String
    let title: String
    let channelName: String?
    let durationSeconds: Int
    let fileSize: Int64
    let downloadDate: Date
    let localRelativePath: String
    var quality: DownloadQuality? = .original

    var formattedDuration: String {
        let hours = durationSeconds / 3600
        let minutes = (durationSeconds % 3600) / 60
        if hours > 0 {
            return "\(hours)h \(minutes)m"
        }
        return "\(minutes) Min."
    }

    var formattedSize: String {
        let formatter = ByteCountFormatter()
        formatter.allowedUnits = [.useMB, .useGB]
        formatter.countStyle = .file
        return formatter.string(fromByteCount: fileSize)
    }

    var formattedDate: String {
        let formatter = DateFormatter()
        formatter.dateStyle = .medium
        formatter.timeStyle = .short
        return formatter.string(from: downloadDate)
    }

    func localFileURL() -> URL {
        let docs = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0]
        return docs.appendingPathComponent(localRelativePath)
    }
}
