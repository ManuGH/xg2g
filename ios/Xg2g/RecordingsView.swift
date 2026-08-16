// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import SwiftUI

/// Recordings library with Plex/Netflix-style offline download & playback.
/// Supports responsive multi-column iPadOS grid and smooth iOS cards.
struct RecordingsView: View {

    let model: AppModel

    enum TabFilter: String, CaseIterable, Identifiable {
        case all = "Alle Aufnahmen"
        case offline = "Downloads"

        var id: String { rawValue }
    }

    @State private var selectedFilter: TabFilter = .all
    @State private var playingOffline: OfflineRecording?
    @State private var recordingToDelete: Recording?

    private var downloadManager: DownloadManager {
        DownloadManager.shared
    }

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                VStack(spacing: 0) {
                    // Filter Pills (Alle vs Downloads)
                    HStack(spacing: 8) {
                        ForEach(TabFilter.allCases) { filter in
                            Button {
                                triggerHaptic(.light)
                                selectedFilter = filter
                            } label: {
                                HStack(spacing: 6) {
                                    if filter == .offline {
                                        Image(systemName: "arrow.down.circle.fill")
                                            .font(.caption2)
                                    }
                                    Text(filter.rawValue)
                                        .font(.subheadline.weight(selectedFilter == filter ? .semibold : .regular))

                                    if filter == .offline && !downloadManager.offlineRecordings.isEmpty {
                                        Text("\(downloadManager.offlineRecordings.count)")
                                            .font(.caption2.monospacedDigit())
                                            .foregroundStyle(selectedFilter == filter ? Theme.Colors.textPrimary : Theme.Colors.accentAction)
                                    }
                                }
                                .padding(.horizontal, 14)
                                .padding(.vertical, 6)
                                .background(
                                    selectedFilter == filter ? Theme.Colors.accentAction : Theme.Colors.surfaceGlass,
                                    in: Capsule()
                                )
                                .foregroundStyle(selectedFilter == filter ? Theme.Colors.textPrimary : Theme.Colors.textSecondary)
                                .overlay(
                                    Capsule().strokeBorder(selectedFilter == filter ? Color.clear : Theme.Colors.borderSubtle, lineWidth: 1)
                                )
                            }
                            .buttonStyle(.plain)
                        }

                        Spacer()

                        if !downloadManager.offlineRecordings.isEmpty {
                            Text(downloadManager.formattedTotalStorage)
                                .font(.caption.monospacedDigit())
                                .foregroundStyle(Theme.Colors.textTertiary)
                        }
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 8)
                    .background(Theme.Colors.surfaceElevated.opacity(0.5))

                    // Content
                    if selectedFilter == .offline {
                        if downloadManager.offlineRecordings.isEmpty {
                            Spacer()
                            ContentUnavailableView(
                                "Keine Downloads",
                                systemImage: "arrow.down.circle",
                                description: Text("Tippe bei einer beliebigen Aufnahme auf das Download-Symbol, um sie offline im Flugzeug oder unterwegs anzusehen.")
                            )
                            .foregroundStyle(Theme.Colors.textSecondary)
                            Spacer()
                        } else {
                            ScrollView {
                                VStack(spacing: 12) {
                                    // Storage Breakdown Card (Netflix/Apple TV+ Style)
                                    HStack {
                                        Label("Offline-Speicher", systemImage: "internaldrive.fill")
                                            .font(.caption.bold())
                                            .foregroundStyle(Theme.Colors.textPrimary)

                                        Spacer()

                                        Text("\(downloadManager.formattedTotalStorage) belegt")
                                            .font(.caption.monospacedDigit().bold())
                                            .foregroundStyle(Theme.Colors.accentAction)
                                    }
                                    .padding(.horizontal, 14)
                                    .padding(.vertical, 8)
                                    .background(Theme.Colors.surfaceElevated, in: RoundedRectangle(cornerRadius: 10))

                                    LazyVGrid(
                                        columns: [GridItem(.adaptive(minimum: 340, maximum: 540), spacing: 14)],
                                        spacing: 14
                                    ) {
                                        ForEach(downloadManager.offlineRecordings) { offline in
                                            Button {
                                                playingOffline = offline
                                            } label: {
                                                OfflineRecordingRow(offline: offline)
                                            }
                                            .buttonStyle(.plain)
                                            .contextMenu {
                                                Button(role: .destructive) {
                                                    downloadManager.deleteOfflineRecording(id: offline.id)
                                                } label: {
                                                    Label("Download löschen", systemImage: "trash")
                                                }
                                            }
                                        }
                                    }
                                }
                                .padding(.horizontal, 16)
                                .padding(.vertical, 10)
                            }
                        }
                    } else {
                        Group {
                            if model.recordings.isEmpty && model.isLoadingRecordings {
                                Spacer()
                                ProgressView("Lade Aufnahmen…")
                                    .tint(Theme.Colors.accentAction)
                                    .foregroundStyle(Theme.Colors.textSecondary)
                                Spacer()
                            } else if model.recordings.isEmpty {
                                Spacer()
                                ContentUnavailableView(
                                    "Keine Aufnahmen",
                                    systemImage: "play.rectangle.on.rectangle",
                                    description: Text("Es wurden keine DVR-Aufnahmen auf dem Server gefunden.")
                                )
                                .foregroundStyle(Theme.Colors.textSecondary)
                                Spacer()
                            } else {
                                ScrollView {
                                    LazyVGrid(
                                        columns: [GridItem(.adaptive(minimum: 340, maximum: 540), spacing: 14)],
                                        spacing: 14
                                    ) {
                                        ForEach(model.recordings) { recording in
                                            RecordingRow(
                                                recording: recording,
                                                serverAddress: model.serverURLString,
                                                model: model
                                            )
                                            .contextMenu {
                                                Button(role: .destructive) {
                                                    recordingToDelete = recording
                                                } label: {
                                                    Label("Vom Server löschen", systemImage: "trash")
                                                }
                                            }
                                        }
                                    }
                                    .padding(.horizontal, 16)
                                    .padding(.vertical, 10)
                                }
                                .refreshable { await model.loadRecordings() }
                            }
                        }
                    }
                }
            }
            .navigationTitle("Aufnahmen")
            .fullScreenCover(item: $playingOffline) { offline in
                OfflinePlayerScreen(offlineRecording: offline)
            }
            .confirmationDialog(
                "Aufnahme wirklich vom Server löschen?",
                isPresented: Binding(
                    get: { recordingToDelete != nil },
                    set: { if !$0 { recordingToDelete = nil } }
                ),
                titleVisibility: .visible
            ) {
                Button("Löschen", role: .destructive) {
                    if let target = recordingToDelete {
                        Task { await model.deleteRecording(target) }
                    }
                }
                Button("Abbrechen", role: .cancel) {
                    recordingToDelete = nil
                }
            } message: {
                if let target = recordingToDelete {
                    Text("„\(target.title)“ wird unwiderruflich von der Festplatte gelöscht.")
                }
            }
        }
        .task {
            if model.recordings.isEmpty {
                await model.loadRecordings()
            }
        }
    }

    private func triggerHaptic(_ style: UIImpactFeedbackGenerator.FeedbackStyle) {
        let generator = UIImpactFeedbackGenerator(style: style)
        generator.impactOccurred()
    }
}

// MARK: - Recording Row

struct RecordingRow: View {

    let recording: Recording
    let serverAddress: String
    var model: AppModel? = nil

    @State private var showPlayer = false
    @State private var downloadManager = DownloadManager.shared

    private var downloadStatus: DownloadManager.DownloadStatus {
        downloadManager.status(for: recording.id)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 12) {
                // Play Icon Container
                Button {
                    showPlayer = true
                } label: {
                    ZStack {
                        RoundedRectangle(cornerRadius: 10)
                            .fill(Theme.Colors.surfaceElevated)
                            .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1))

                        Image(systemName: "play.fill")
                            .font(.title3)
                            .foregroundStyle(Theme.Colors.accentAction)
                    }
                    .frame(width: 46, height: 46)
                }
                .buttonStyle(.plain)

                VStack(alignment: .leading, spacing: 3) {
                    Text(recording.title)
                        .font(.system(size: 15, weight: .bold))
                        .foregroundStyle(Theme.Colors.textPrimary)
                        .lineLimit(1)

                    HStack(spacing: 6) {
                        Text(recording.formattedDate)
                            .font(.caption.monospaced())
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                }

                Spacer(minLength: 4)

                // Download Button (Plex/Netflix Style)
                DownloadButton(
                    recording: recording,
                    serverAddress: serverAddress,
                    model: model,
                    status: downloadStatus
                )
            }

            if let description = recording.description, !description.isEmpty {
                Text(description)
                    .font(.caption)
                    .foregroundStyle(Theme.Colors.textSecondary)
                    .lineLimit(2)
            }

            HStack {
                Label(recording.formattedDuration, systemImage: "clock")
                    .font(.system(size: 10, design: .monospaced))
                    .foregroundStyle(Theme.Colors.textTertiary)

                Spacer()
            }
        }
        .padding(14)
        .background(Theme.Colors.surfaceElevated, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1))
        .fullScreenCover(isPresented: $showPlayer) {
            RecordingPlayerScreen(recording: recording, serverAddress: serverAddress)
        }
    }
}

// MARK: - Offline Recording Row

struct OfflineRecordingRow: View {

    let offline: OfflineRecording

    var body: some View {
        HStack(spacing: 12) {
            ZStack {
                RoundedRectangle(cornerRadius: 10)
                    .fill(Theme.Colors.statusSuccess.opacity(0.15))
                    .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.Colors.statusSuccess.opacity(0.3), lineWidth: 1))

                Image(systemName: "arrow.down.circle.fill")
                    .font(.title3)
                    .foregroundStyle(Theme.Colors.statusSuccess)
            }
            .frame(width: 46, height: 46)

            VStack(alignment: .leading, spacing: 3) {
                Text(offline.title)
                    .font(.system(size: 15, weight: .bold))
                    .foregroundStyle(Theme.Colors.textPrimary)
                    .lineLimit(1)

                HStack(spacing: 6) {
                    Text(offline.channelName ?? "Aufnahme")
                        .font(.caption.weight(.medium))
                        .foregroundStyle(Theme.Colors.accentLive)

                    Text("•")
                        .font(.caption2)
                        .foregroundStyle(Theme.Colors.textTertiary)

                    Text(offline.formattedSize)
                        .font(.caption.monospaced())
                        .foregroundStyle(Theme.Colors.textTertiary)
                }
            }

            Spacer()

            Image(systemName: "play.circle.fill")
                .font(.title2)
                .foregroundStyle(Theme.Colors.accentAction)
        }
        .padding(14)
        .background(Theme.Colors.surfaceElevated, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1))
    }
}

// MARK: - Download Button (Plex/Netflix Offline Toggle)

struct DownloadButton: View {

    let recording: Recording
    let serverAddress: String
    var model: AppModel? = nil
    let status: DownloadManager.DownloadStatus

    private var downloadManager: DownloadManager {
        DownloadManager.shared
    }

    var body: some View {
        Button {
            handleTap()
        } label: {
            ZStack {
                switch status {
                case .notDownloaded, .failed:
                    Image(systemName: "arrow.down.circle")
                        .font(.system(size: 22))
                        .foregroundStyle(Theme.Colors.textTertiary)

                case .downloading(let progress):
                    ZStack {
                        Circle()
                            .stroke(Theme.Colors.borderSubtle, lineWidth: 2.5)
                        Circle()
                            .trim(from: 0, to: CGFloat(progress))
                            .stroke(Theme.Colors.accentAction, lineWidth: 2.5)
                            .rotationEffect(.degrees(-90))
                        Image(systemName: "stop.fill")
                            .font(.system(size: 8))
                            .foregroundStyle(Theme.Colors.accentAction)
                    }
                    .frame(width: 22, height: 22)

                case .downloaded:
                    Image(systemName: "arrow.down.circle.fill")
                        .font(.system(size: 22))
                        .foregroundStyle(Theme.Colors.statusSuccess)
                }
            }
            .frame(width: 36, height: 36)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private func handleTap() {
        switch status {
        case .notDownloaded, .failed:
            let base = serverAddress.starts(with: "http") ? serverAddress : "https://\(serverAddress)"
            if let url = URL(string: base) {
                Task {
                    let token = try? await model?.currentAccessToken()
                    await MainActor.run {
                        downloadManager.startDownload(
                            recording: recording,
                            serverBaseURL: url,
                            authToken: token
                        )
                    }
                }
            }
        case .downloading:
            downloadManager.cancelDownload(recordingId: recording.id)
        case .downloaded:
            break
        }
    }
}

// MARK: - Recording Player Screen

struct RecordingPlayerScreen: View {

    let recording: Recording
    let serverAddress: String
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        ZStack {
            Theme.Colors.bgVideoStage.ignoresSafeArea()

            let base = serverAddress.starts(with: "http") ? serverAddress : "https://\(serverAddress)"
            if let baseURL = URL(string: base) {
                let streamURL = baseURL.appendingPathComponent("api/v3/recordings/\(recording.id)/stream.mp4")
                NativeVideoPlayerView(
                    player: AVPlayer(url: streamURL),
                    onDismiss: { dismiss() }
                )
                .ignoresSafeArea()
            } else {
                VStack(spacing: 16) {
                    Image(systemName: "exclamationmark.triangle")
                        .font(.system(size: 48))
                        .foregroundStyle(Theme.Colors.statusWarning)

                    Text("Keine Streaming-URL für diese Aufnahme verfügbar.")
                        .font(.headline)
                        .foregroundStyle(Theme.Colors.textPrimary)

                    Button("Schließen") {
                        dismiss()
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(Theme.Colors.accentAction)
                }
            }
        }
    }
}
