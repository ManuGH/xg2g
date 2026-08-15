// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Recordings library with Plex/Netflix-style offline download & playback.
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
                            List(downloadManager.offlineRecordings) { offline in
                                Button {
                                    playingOffline = offline
                                } label: {
                                    OfflineRecordingRow(offline: offline)
                                }
                                .buttonStyle(.plain)
                                .listRowBackground(Theme.Colors.surfaceElevated)
                                .listRowSeparatorTint(Theme.Colors.borderSubtle)
                                .swipeActions(edge: .trailing, allowsFullSwipe: true) {
                                    Button(role: .destructive) {
                                        downloadManager.deleteOfflineRecording(id: offline.id)
                                    } label: {
                                        Label("Löschen", systemImage: "trash")
                                    }
                                }
                            }
                            .listStyle(.plain)
                            .scrollContentBackground(.hidden)
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
                                List(model.recordings) { recording in
                                    RecordingRow(
                                        recording: recording,
                                        serverAddress: model.serverURLString
                                    )
                                    .listRowBackground(Theme.Colors.surfaceElevated)
                                    .listRowSeparatorTint(Theme.Colors.borderSubtle)
                                    .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                                        Button(role: .destructive) {
                                            recordingToDelete = recording
                                        } label: {
                                            Label("Löschen", systemImage: "trash")
                                        }
                                    }
                                }
                                .listStyle(.plain)
                                .scrollContentBackground(.hidden)
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
                if let rec = recordingToDelete {
                    Button("Aufnahme löschen", role: .destructive) {
                        Task { await model.deleteRecording(rec) }
                    }
                }
                Button("Abbrechen", role: .cancel) {
                    recordingToDelete = nil
                }
            } message: {
                if let rec = recordingToDelete {
                    Text("„\(rec.title)“ wird dauerhaft von der Festplatte der Vu+ Uno 4K gelöscht.")
                }
            }
            .toolbar {
                if model.isLoadingRecordings && !model.recordings.isEmpty {
                    ProgressView()
                        .tint(Theme.Colors.accentAction)
                }
            }
        }
    }
}

// MARK: - Online Recording Row with Download Button

struct RecordingRow: View {

    let recording: Recording
    let serverAddress: String

    private var downloadManager: DownloadManager {
        DownloadManager.shared
    }

    var body: some View {
        HStack(spacing: 14) {
            Image(systemName: "video.fill")
                .font(.title2)
                .foregroundStyle(Theme.Colors.accentLive)
                .frame(width: 44, height: 44)
                .background(Theme.Colors.surfaceGlass, in: RoundedRectangle(cornerRadius: 8))

            VStack(alignment: .leading, spacing: 4) {
                Text(recording.title)
                    .font(.body.weight(.semibold))
                    .foregroundStyle(Theme.Colors.textPrimary)
                    .lineLimit(1)

                if let description = recording.description {
                    Text(description)
                        .font(.caption)
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .lineLimit(2)
                }

                HStack(spacing: 8) {
                    Text(recording.formattedDate)
                        .font(.caption2.monospacedDigit())
                        .foregroundStyle(Theme.Colors.textTertiary)

                    Text("•")
                        .foregroundStyle(Theme.Colors.textTertiary)

                    Text(recording.formattedDuration)
                        .font(.caption2.monospacedDigit())
                        .foregroundStyle(Theme.Colors.textTertiary)
                }
            }

            Spacer()

            // Plex / Netflix Download Button
            DownloadActionButton(recording: recording, serverAddress: serverAddress)
        }
        .padding(.vertical, 4)
    }
}

// MARK: - Download Action Button

struct DownloadActionButton: View {

    let recording: Recording
    let serverAddress: String

    private var downloadManager: DownloadManager {
        DownloadManager.shared
    }

    var body: some View {
        let status = downloadManager.status(for: recording.id)

        switch status {
        case .downloaded:
            Image(systemName: "checkmark.circle.fill")
                .font(.title2)
                .foregroundStyle(Theme.Colors.accentAction)

        case .downloading(let progress):
            ZStack {
                CircularProgressRing(progress: progress)
                    .frame(width: 28, height: 28)

                Button {
                    downloadManager.cancelDownload(recordingId: recording.id)
                } label: {
                    Image(systemName: "stop.fill")
                        .font(.system(size: 9))
                        .foregroundStyle(Theme.Colors.accentAction)
                }
            }

        case .notDownloaded, .failed:
            Button {
                if let url = URL(string: serverAddress) {
                    downloadManager.startDownload(recording: recording, serverBaseURL: url)
                }
            } label: {
                Image(systemName: "arrow.down.circle")
                    .font(.title2)
                    .foregroundStyle(Theme.Colors.textSecondary)
            }
            .buttonStyle(.plain)
        }
    }
}

struct CircularProgressRing: View {
    let progress: Double

    var body: some View {
        ZStack {
            Circle()
                .stroke(Theme.Colors.borderSubtle, lineWidth: 3)
            Circle()
                .trim(from: 0, to: progress)
                .stroke(Theme.Colors.accentAction, style: StrokeStyle(lineWidth: 3, lineCap: .round))
                .rotationEffect(.degrees(-90))
        }
    }
}

// MARK: - Offline Recording Row

struct OfflineRecordingRow: View {

    let offline: OfflineRecording

    var body: some View {
        HStack(spacing: 14) {
            Image(systemName: "play.circle.fill")
                .font(.title)
                .foregroundStyle(Theme.Colors.accentAction)
                .frame(width: 44, height: 44)

            VStack(alignment: .leading, spacing: 4) {
                Text(offline.title)
                    .font(.body.weight(.semibold))
                    .foregroundStyle(Theme.Colors.textPrimary)
                    .lineLimit(1)

                if let channel = offline.channelName {
                    Text(channel)
                        .font(.caption)
                        .foregroundStyle(Theme.Colors.textSecondary)
                }

                HStack(spacing: 8) {
                    Text(offline.formattedDuration)
                        .font(.caption2.monospacedDigit())
                        .foregroundStyle(Theme.Colors.textTertiary)

                    Text("•")
                        .foregroundStyle(Theme.Colors.textTertiary)

                    Text(offline.formattedSize)
                        .font(.caption2.monospacedDigit())
                        .foregroundStyle(Theme.Colors.accentLive)
                }
            }

            Spacer()

            Image(systemName: "chevron.right")
                .font(.caption)
                .foregroundStyle(Theme.Colors.textTertiary)
        }
        .padding(.vertical, 4)
    }
}
