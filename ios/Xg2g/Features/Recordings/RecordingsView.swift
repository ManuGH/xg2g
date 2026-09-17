// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import SwiftUI

// MARK: - Main Recordings View

struct RecordingsView: View {

    let model: AppModel

    enum CategoryFilter: String, CaseIterable, Identifiable {
        case all = "Alle"
        case offline = "Downloads"
        case movies = "🎬 Spielfilme"
        case series = "📺 Serien"
        case sport = "⚽️ Sport"
        case docus = "🌍 Dokus"

        var id: String { rawValue }
    }

    @Environment(\.horizontalSizeClass) private var sizeClass

    @State private var selectedFilter: CategoryFilter = .all
    @State private var searchText = ""
    @State private var promptResumeRecording: Recording?
    @State private var selectedDetailRecording: Recording?
    @State private var recordingToDelete: Recording?
    @State private var showTimersSheet = false

    private func play(recording: Recording, startPosition: Double) {
        model.playbackManager.play(recording: recording, startPosition: startPosition)
    }

    private func handlePlayAction(for recording: Recording) {
        let resumePos = model.resumePosition(for: recording.id) ?? 0
        if resumePos > 5 {
            promptResumeRecording = recording
        } else {
            play(recording: recording, startPosition: 0)
        }
    }

    private var downloadManager: DownloadManager {
        DownloadManager.shared
    }

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                VStack(spacing: 0) {
                    // MARK: - 1. Category Filter Carousel
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(spacing: 8) {
                            ForEach(CategoryFilter.allCases) { filter in
                                let isSelected = selectedFilter == filter
                                Button {
                                    triggerHaptic(.light)
                                    withAnimation(.spring(response: 0.25, dampingFraction: 0.85)) {
                                        selectedFilter = filter
                                    }
                                } label: {
                                    HStack(spacing: 6) {
                                        if filter == .offline {
                                            Image(systemName: "arrow.down.circle.fill")
                                                .font(.caption2)
                                        }
                                        Text(filter.rawValue)
                                            .font(.system(size: 13, weight: isSelected ? .bold : .medium))

                                        // Badge count for all or downloads
                                        if filter == .all && !model.recordings.isEmpty {
                                            Text("\(model.recordings.count)")
                                                .font(.system(size: 10, weight: .bold, design: .monospaced))
                                                .padding(.horizontal, 5)
                                                .padding(.vertical, 1)
                                                .background(isSelected ? Theme.Colors.bgBase.opacity(0.3) : Theme.Colors.surfaceElevated, in: Capsule())
                                        } else if filter == .offline && !downloadManager.offlineRecordings.isEmpty {
                                            Text("\(downloadManager.offlineRecordings.count)")
                                                .font(.system(size: 10, weight: .bold, design: .monospaced))
                                                .padding(.horizontal, 5)
                                                .padding(.vertical, 1)
                                                .background(isSelected ? Theme.Colors.bgBase.opacity(0.3) : Theme.Colors.statusSuccess.opacity(0.25), in: Capsule())
                                        }
                                    }
                                    .padding(.horizontal, 14)
                                    .padding(.vertical, 7)
                                    .background(
                                        isSelected
                                            ? (filter == .offline ? Theme.Colors.statusSuccess : Theme.Colors.accentAction)
                                            : Theme.Colors.surfaceElevated.opacity(0.85),
                                        in: Capsule()
                                    )
                                    .foregroundStyle(isSelected ? Color.white : Theme.Colors.textPrimary)
                                    .overlay {
                                        if !isSelected {
                                            Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8)
                                        }
                                    }
                                }
                                .buttonStyle(.plain)
                            }
                        }
                        .padding(.horizontal, 16)
                        .padding(.vertical, 8)
                    }
                    .background(Theme.Colors.surfaceElevated.opacity(0.25))

                    // MARK: - 2. Content View
                    if selectedFilter == .offline {
                        offlineContentView
                    } else {
                        serverRecordingsContentView
                    }
                }
            }
            .navigationTitle("Aufnahmen")
#if !os(tvOS)
            .navigationBarTitleDisplayMode(.inline)
#endif
            .toolbar {
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        showTimersSheet = true
                    } label: {
                        Label("Timer", systemImage: "clock.badge")
                    }
                }
            }
            .sheet(isPresented: $showTimersSheet) {
                TimersView(model: model)
            }
            .searchable(text: $searchText, prompt: "Aufnahmen nach Titel oder Genre suchen…")
            .sheet(item: $selectedDetailRecording) { rec in
                RecordingDetailSheet(
                    recording: rec,
                    model: model,
                    serverAddress: model.serverAddress,
                    onPlay: { startPos in
                        selectedDetailRecording = nil
                        play(recording: rec, startPosition: startPos)
                    },
                    onDelete: {
                        selectedDetailRecording = nil
                        recordingToDelete = rec
                    }
                )
            }
            .confirmationDialog(
                "„\(promptResumeRecording?.title ?? "Aufnahme")“ abspielen",
                isPresented: Binding(
                    get: { promptResumeRecording != nil },
                    set: { if !$0 { promptResumeRecording = nil } }
                ),
                titleVisibility: .visible
            ) {
                if let rec = promptResumeRecording {
                    let resumePos = model.resumePosition(for: rec.id) ?? 0
                    Button("Fortsetzen bei \(RecordingTimeFormatter.string(from: resumePos))") {
                        let target = rec
                        promptResumeRecording = nil
                        play(recording: target, startPosition: resumePos)
                    }

                    Button("Von Beginn an abspielen") {
                        let target = rec
                        promptResumeRecording = nil
                        model.updateRecordingProgress(
                            id: target.id,
                            currentTime: 0,
                            totalDuration: Double(target.durationSeconds),
                            title: target.title
                        )
                        play(recording: target, startPosition: 0)
                    }

                    Button("Abbrechen", role: .cancel) {
                        promptResumeRecording = nil
                    }
                }
            } message: {
                if let rec = promptResumeRecording {
                    let resumePos = model.resumePosition(for: rec.id) ?? 0
                    let remainingMin = max(1, Int((Double(rec.durationSeconds) - resumePos) / 60))
                    Text("Zuletzt gesehen bis \(RecordingTimeFormatter.string(from: resumePos)) (noch ca. \(remainingMin) Min.).")
                }
            }
            .confirmationDialog(
                "Aufnahme wirklich vom Server löschen?",
                isPresented: Binding(
                    get: { recordingToDelete != nil },
                    set: { if !$0 { recordingToDelete = nil } }
                ),
                titleVisibility: .visible
            ) {
                Button("Vom Server löschen", role: .destructive) {
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

    // MARK: - Server Recordings Content View

    @ViewBuilder
    private var serverRecordingsContentView: some View {
        let filtered = filteredRecordings
        let isRegular = sizeClass == .regular

        if model.recordings.isEmpty && model.isLoadingRecordings {
            Spacer()
            ProgressView("Lade DVR-Aufnahmen…")
                .tint(Theme.Colors.accentAction)
                .foregroundStyle(Theme.Colors.textSecondary)
            Spacer()
        } else if filtered.isEmpty {
            Spacer()
            ContentUnavailableView(
                searchText.isEmpty ? "Keine Aufnahmen" : "Keine Treffer für „\(searchText)“",
                systemImage: "play.rectangle.on.rectangle",
                description: Text(searchText.isEmpty ? "Es wurden keine DVR-Aufnahmen auf dem Server gefunden." : "Passe deine Suchanfrage oder den Filter an.")
            )
            .foregroundStyle(Theme.Colors.textSecondary)
            Spacer()
        } else {
            ScrollView {
                VStack(spacing: 24) {
                    // 1. Spotlight / Weiterschauen Hero (Apple TV+ / Infuse Style)
                    if searchText.isEmpty, let spotlight = spotlightRecording {
                        RecordingSpotlightHero(
                            recording: spotlight,
                            model: model,
                            serverAddress: model.serverAddress,
                            onPlay: { handlePlayAction(for: spotlight) },
                            onShowInfo: { selectedDetailRecording = spotlight },
                            onDelete: { recordingToDelete = spotlight }
                        )
                    }

                    // 2. Section Header
                    HStack {
                        Text(selectedFilter == .all ? "Alle Aufnahmen" : selectedFilter.rawValue)
                            .font(.title3.weight(.bold))
                            .foregroundStyle(Theme.Colors.textPrimary)

                        Spacer()

                        Text("\(filtered.count) Videos")
                            .font(.system(size: 12, weight: .semibold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .padding(.horizontal, 2)

                    // 3. Infuse-Style 16:9 Media Cards Grid
                    LazyVGrid(
                        columns: [
                            GridItem(.adaptive(minimum: isRegular ? 320 : 280, maximum: 480), spacing: isRegular ? 18 : 14)
                        ],
                        spacing: isRegular ? 18 : 14
                    ) {
                        ForEach(filtered) { recording in
                            RecordingMediaCard(
                                recording: recording,
                                model: model,
                                serverAddress: model.serverAddress,
                                onPlay: { handlePlayAction(for: recording) },
                                onShowInfo: { selectedDetailRecording = recording }
                            )
                            .contextMenu {
                                Button {
                                    handlePlayAction(for: recording)
                                } label: {
                                    Label("Abspielen", systemImage: "play.fill")
                                }

                                Button {
                                    selectedDetailRecording = recording
                                } label: {
                                    Label("Details ansehen", systemImage: "info.circle")
                                }

                                Button(role: .destructive) {
                                    recordingToDelete = recording
                                } label: {
                                    Label("Vom Server löschen", systemImage: "trash")
                                }
                            }
                        }
                    }
                }
                .padding(.horizontal, isRegular ? 20 : 14)
                .padding(.vertical, isRegular ? 18 : 14)
                .safeAreaPadding(.bottom, 80)
            }
            .refreshable { await model.loadRecordings() }
        }
    }

    // MARK: - Offline Content View

    @ViewBuilder
    private var offlineContentView: some View {
        let isRegular = sizeClass == .regular

        if downloadManager.offlineRecordings.isEmpty {
            Spacer()
            ContentUnavailableView(
                "Keine Downloads",
                systemImage: "arrow.down.circle",
                description: Text("Tippe bei einer beliebigen Aufnahme auf das Download-Symbol, um sie offline im Flugzeug oder unterwegs ohne Internet anzusehen.")
            )
            .foregroundStyle(Theme.Colors.textSecondary)
            Spacer()
        } else {
            ScrollView {
                VStack(spacing: 16) {
                    // Storage Breakdown Card (Apple TV+ / Netflix Style)
                    VStack(spacing: 10) {
                        HStack {
                            Label("Offline-Speicher", systemImage: "internaldrive.fill")
                                .font(.caption.bold())
                                .foregroundStyle(Theme.Colors.textPrimary)

                            Spacer()

                            Text("\(downloadManager.formattedTotalStorage) belegt")
                                .font(.caption.monospacedDigit().bold())
                                .foregroundStyle(Theme.Colors.statusSuccess)
                        }

                        // Storage Capacity Bar
                        GeometryReader { geo in
                            ZStack(alignment: .leading) {
                                Capsule()
                                    .fill(Color.white.opacity(0.12))
                                    .frame(height: 7)

                                Capsule()
                                    .fill(
                                        LinearGradient(
                                            colors: [Theme.Colors.statusSuccess, Theme.Colors.accentAction],
                                            startPoint: .leading,
                                            endPoint: .trailing
                                        )
                                    )
                                    .frame(width: max(14, min(geo.size.width, geo.size.width * 0.4)), height: 7)
                            }
                        }
                        .frame(height: 7)
                    }
                    .padding(14)
                    .background(Theme.Gradients.cardSurface, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                    .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1))

                    // Offline Recordings Grid
                    LazyVGrid(
                        columns: [
                            GridItem(.adaptive(minimum: isRegular ? 320 : 280, maximum: 460), spacing: isRegular ? 16 : 12)
                        ],
                        spacing: isRegular ? 16 : 12
                    ) {
                        ForEach(downloadManager.offlineRecordings) { offline in
                            Button {
                                model.playbackManager.play(offline: offline)
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
                .padding(.horizontal, isRegular ? 20 : 12)
                .padding(.vertical, isRegular ? 16 : 12)
                .safeAreaPadding(.bottom, 80)
            }
        }
    }

    // MARK: - Computed Properties

    private var filteredRecordings: [Recording] {
        var list = model.recordings

        // Filter by category
        switch selectedFilter {
        case .all, .offline:
            break
        case .movies:
            list = list.filter { $0.genre == .movie }
        case .series:
            list = list.filter { $0.genre == .series }
        case .sport:
            list = list.filter { $0.genre == .sport }
        case .docus:
            list = list.filter { $0.genre == .docu }
        }

        // Filter by Search Query
        let query = searchText.trimmingCharacters(in: .whitespaces).lowercased()
        if !query.isEmpty {
            list = list.filter {
                $0.title.lowercased().contains(query) ||
                ($0.description?.lowercased().contains(query) ?? false)
            }
        }

        return list
    }

    private var spotlightRecording: Recording? {
        let filtered = filteredRecordings
        // 1. Look for a partially watched recording to resume
        if let inProgress = filtered.first(where: { (model.resumePosition(for: $0.id) ?? 0) > 0 }) {
            return inProgress
        }
        // 2. Or the latest recording
        return filtered.first
    }

    private func triggerHaptic(_ style: UIImpactFeedbackGenerator.FeedbackStyle) {
        Haptics.shared.impact(style)
    }
}
