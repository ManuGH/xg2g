// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import SwiftUI

// MARK: - Download Button (Plex/Netflix Multi-Quality Menu)

struct DownloadButton: View {

    let recording: Recording
    let serverAddress: ServerAddress?
    var model: AppModel? = nil
    let status: DownloadManager.DownloadStatus

    private var downloadManager: DownloadManager {
        DownloadManager.shared
    }

    var body: some View {
        switch status {
        case .notDownloaded, .failed:
            Menu {
                Section("Download-Qualität für Offline:") {
                    ForEach(DownloadQuality.supportedQualities) { q in
                        Button {
                            triggerHaptic(.medium)
                            start(quality: q)
                        } label: {
                            Label(
                                "\(q.title) • \(q.formattedEstimatedSize(durationSeconds: recording.durationSeconds))",
                                systemImage: q.icon
                            )
                        }
                    }
                }
            } label: {
                Image(systemName: "arrow.down.circle")
                    .font(.app(size: 22))
                    .foregroundStyle(Theme.Colors.textTertiary)
                    .frame(width: 36, height: 36)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

        case .downloading(let progress):
            Button {
                triggerHaptic(.light)
                downloadManager.cancelDownload(recordingId: recording.id)
            } label: {
                ZStack {
                    Circle()
                        .stroke(Theme.Colors.borderSubtle, lineWidth: 2.5)
                    Circle()
                        .trim(from: 0, to: CGFloat(progress))
                        .stroke(Theme.Colors.accentAction, lineWidth: 2.5)
                        .rotationEffect(.degrees(-90))
                    Image(systemName: "stop.fill")
                        .font(.app(size: 8))
                        .foregroundStyle(Theme.Colors.accentAction)
                }
                .frame(width: 22, height: 22)
                .frame(width: 36, height: 36)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

        case .downloaded:
            Image(systemName: "arrow.down.circle.fill")
                .font(.app(size: 22))
                .foregroundStyle(Theme.Colors.statusSuccess)
                .frame(width: 36, height: 36)
        }
    }

    private func start(quality: DownloadQuality) {
        // No configured deployment, nothing to download from. The repair this
        // used to attempt — prepending a scheme to whatever text was stored —
        // belonged to the address parser and now lives there.
        guard let url = serverAddress?.rootURL else { return }

        Task {
            let sessionCookie = try? await model?.mediaSessionCookie()
            await MainActor.run {
                downloadManager.startDownload(
                    recording: recording,
                    serverBaseURL: url,
                    quality: quality,
                    sessionCookie: sessionCookie
                )
            }
        }
    }

    private func triggerHaptic(_ style: UIImpactFeedbackGenerator.FeedbackStyle) {
        Haptics.shared.impact(style)
    }
}
