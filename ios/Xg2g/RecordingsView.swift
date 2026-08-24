// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import SwiftUI

// MARK: - Helper Formatting & Artwork Theme

private func formatRecordingTime(_ seconds: Double) -> String {
    let mins = Int(seconds) / 60
    let secs = Int(seconds) % 60
    return String(format: "%02d:%02d", mins, secs)
}

enum RecordingArtworkTheme {
    struct Palette {
        let gradient: LinearGradient
        let accent: Color
        let icon: String
        let label: String
    }

    static func palette(for recording: Recording) -> Palette {
        switch recording.genre {
        case .movie:
            return Palette(
                gradient: LinearGradient(
                    colors: [Color(red: 0.18, green: 0.05, blue: 0.10), Color(red: 0.05, green: 0.02, blue: 0.04)],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                ),
                accent: Color(red: 0.95, green: 0.35, blue: 0.45),
                icon: "film.stack",
                label: "Spielfilm"
            )
        case .series:
            return Palette(
                gradient: LinearGradient(
                    colors: [Color(red: 0.08, green: 0.12, blue: 0.24), Color(red: 0.02, green: 0.04, blue: 0.08)],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                ),
                accent: Color(red: 0.35, green: 0.65, blue: 1.0),
                icon: "tv",
                label: "Serie"
            )
        case .sport:
            return Palette(
                gradient: LinearGradient(
                    colors: [Color(red: 0.04, green: 0.16, blue: 0.12), Color(red: 0.01, green: 0.05, blue: 0.04)],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                ),
                accent: Color(red: 0.25, green: 0.85, blue: 0.55),
                icon: "sportscourt",
                label: "Sport"
            )
        case .docu:
            return Palette(
                gradient: LinearGradient(
                    colors: [Color(red: 0.04, green: 0.14, blue: 0.18), Color(red: 0.01, green: 0.04, blue: 0.06)],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                ),
                accent: Color(red: 0.20, green: 0.80, blue: 0.90),
                icon: "globe.europe.africa",
                label: "Doku"
            )
        case .news:
            return Palette(
                gradient: LinearGradient(
                    colors: [Color(red: 0.14, green: 0.10, blue: 0.04), Color(red: 0.04, green: 0.03, blue: 0.01)],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                ),
                accent: Color(red: 1.0, green: 0.70, blue: 0.25),
                icon: "newspaper",
                label: "Nachrichten"
            )
        case .kids:
            return Palette(
                gradient: LinearGradient(
                    colors: [Color(red: 0.16, green: 0.08, blue: 0.18), Color(red: 0.04, green: 0.02, blue: 0.05)],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                ),
                accent: Color(red: 0.90, green: 0.50, blue: 0.95),
                icon: "sparkles",
                label: "Kinder"
            )
        case .all, .show:
            return Palette(
                gradient: LinearGradient(
                    colors: [Color(red: 0.08, green: 0.10, blue: 0.16), Color(red: 0.02, green: 0.03, blue: 0.05)],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                ),
                accent: Theme.Colors.accentAction,
                icon: "play.rectangle.on.rectangle",
                label: "Aufnahme"
            )
        }
    }
}

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
            .navigationBarTitleDisplayMode(.inline)
            .searchable(text: $searchText, prompt: "Aufnahmen nach Titel oder Genre suchen…")
            .sheet(item: $selectedDetailRecording) { rec in
                RecordingDetailSheet(
                    recording: rec,
                    model: model,
                    serverAddress: model.serverURLString,
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
                    Button("Fortsetzen bei \(formatRecordingTime(resumePos))") {
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
                    Text("Zuletzt gesehen bis \(formatRecordingTime(resumePos)) (noch ca. \(remainingMin) Min.).")
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
                            serverAddress: model.serverURLString,
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
                                serverAddress: model.serverURLString,
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

// MARK: - Spotlight Hero Banner (Apple TV+ / Infuse Style)

struct RecordingSpotlightHero: View {
    let recording: Recording
    let model: AppModel
    let serverAddress: String
    var onPlay: () -> Void
    var onShowInfo: () -> Void
    var onDelete: () -> Void

    var body: some View {
        let resumePos = model.resumePosition(for: recording.id) ?? 0
        let hasResume = resumePos > 0 && recording.durationSeconds > 0
        let progress = hasResume ? min(1.0, resumePos / Double(recording.durationSeconds)) : 0.0
        let palette = RecordingArtworkTheme.palette(for: recording)

        VStack(alignment: .leading, spacing: 0) {
            ZStack(alignment: .bottomLeading) {
                // 16:9 Cinematic Stage Backdrop
                Rectangle()
                    .fill(palette.gradient)
                    .aspectRatio(16/9, contentMode: .fit)
                    .overlay(
                        // Ambient Watermark Icon
                        Image(systemName: palette.icon)
                            .font(.system(size: 140, weight: .ultraLight))
                            .foregroundStyle(palette.accent.opacity(0.12))
                            .offset(x: 80, y: -20),
                        alignment: .trailing
                    )
                    .overlay(
                        // Multi-stop Gradient Scrim for perfect contrast
                        LinearGradient(
                            stops: [
                                .init(color: Color.black.opacity(0.2), location: 0),
                                .init(color: Color.clear, location: 0.35),
                                .init(color: Color.black.opacity(0.80), location: 0.75),
                                .init(color: Color.black.opacity(0.96), location: 1.0)
                            ],
                            startPoint: .top,
                            endPoint: .bottom
                        )
                    )

                // Top Floating Badges
                VStack {
                    HStack(spacing: 8) {
                        // Badge: WEITERSCHAUEN / NEUESTE AUFNAHME
                        HStack(spacing: 5) {
                            Image(systemName: hasResume ? "play.circle.fill" : "sparkles.tv")
                                .font(.system(size: 10, weight: .bold))
                            Text(hasResume ? "WEITERSCHAUEN" : "NEUESTE AUFNAHME")
                                .font(.system(size: 10, weight: .bold, design: .monospaced))
                        }
                        .foregroundStyle(hasResume ? Theme.Colors.accentAction : palette.accent)
                        .padding(.horizontal, 9)
                        .padding(.vertical, 5)
                        .background(.ultraThinMaterial, in: Capsule())
                        .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))

                        Spacer()

                        // Tech Specs: 1080i HD • 5.1 Dolby
                        HStack(spacing: 4) {
                            Text("1080i")
                                .font(.system(size: 10, weight: .bold, design: .monospaced))
                            Text("•")
                                .foregroundStyle(Theme.Colors.textDisabled)
                            Text("5.1")
                                .font(.system(size: 10, weight: .bold, design: .monospaced))
                        }
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .padding(.horizontal, 8)
                        .padding(.vertical, 5)
                        .background(.ultraThinMaterial, in: Capsule())
                        .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                    .padding(16)

                    Spacer()
                }

                // Bottom Content Inside the Stage
                VStack(alignment: .leading, spacing: 10) {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(recording.title)
                            .font(.system(size: 22, weight: .heavy))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(2)
                            .shadow(color: .black.opacity(0.8), radius: 4, y: 2)

                        if let desc = recording.description, !desc.isEmpty {
                            Text(desc)
                                .font(.subheadline)
                                .foregroundStyle(Theme.Colors.textSecondary)
                                .lineLimit(2)
                                .lineSpacing(2)
                                .shadow(color: .black.opacity(0.8), radius: 2, y: 1)
                        }

                        HStack(spacing: 8) {
                            Text(recording.formattedDate)
                                .font(.system(size: 12, weight: .medium, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textTertiary)

                            Text("•")
                                .foregroundStyle(Theme.Colors.textDisabled)

                            Text(recording.formattedDuration)
                                .font(.system(size: 12, weight: .semibold, design: .monospaced))
                                .foregroundStyle(palette.accent)

                            if hasResume {
                                Text("•")
                                    .foregroundStyle(Theme.Colors.textDisabled)
                                let remainingMin = max(1, Int((Double(recording.durationSeconds) - resumePos) / 60))
                                Text("Noch \(remainingMin)m verbleibend")
                                    .font(.system(size: 12, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentAction)
                            }
                        }
                        .padding(.top, 2)
                    }

                    // Progress Bar (if in progress)
                    if hasResume {
                        GeometryReader { geo in
                            ZStack(alignment: .leading) {
                                Capsule()
                                    .fill(Color.white.opacity(0.18))
                                    .frame(height: 5)

                                Capsule()
                                    .fill(
                                        LinearGradient(
                                            colors: [Theme.Colors.accentAction, Theme.Colors.statusSuccess],
                                            startPoint: .leading,
                                            endPoint: .trailing
                                        )
                                    )
                                    .frame(width: max(8, geo.size.width * CGFloat(progress)), height: 5)
                                    .shadow(color: Theme.Colors.accentAction.opacity(0.6), radius: 4)
                            }
                        }
                        .frame(height: 5)
                        .padding(.vertical, 2)
                    }

                    // Action Button Row
                    HStack(spacing: 12) {
                        // Prominent Primary Action Button
                        Button(action: onPlay) {
                            HStack(spacing: 8) {
                                Image(systemName: hasResume ? "play.fill" : "play.circle.fill")
                                    .font(.system(size: 15, weight: .bold))
                                Text(hasResume ? "Fortsetzen bei \(formatRecordingTime(resumePos))" : "Jetzt abspielen")
                                    .font(.system(size: 14, weight: .bold))
                            }
                            .padding(.horizontal, 20)
                            .padding(.vertical, 12)
                            .background(Theme.Colors.accentAction, in: Capsule())
                            .foregroundStyle(.white)
                            .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1))
                            .shadow(color: Theme.Colors.accentAction.opacity(0.4), radius: 8, y: 3)
                        }
                        .buttonStyle(.plain)

                        // Info Button
                        Button(action: onShowInfo) {
                            HStack(spacing: 6) {
                                Image(systemName: "info.circle")
                                    .font(.system(size: 14, weight: .semibold))
                                Text("Details")
                                    .font(.system(size: 13, weight: .semibold))
                            }
                            .padding(.horizontal, 14)
                            .padding(.vertical, 12)
                            .background(.ultraThinMaterial, in: Capsule())
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                        }
                        .buttonStyle(.plain)

                        Spacer()

                        // Download Action Button
                        DownloadButton(
                            recording: recording,
                            serverAddress: serverAddress,
                            model: model,
                            status: DownloadManager.shared.status(for: recording.id)
                        )
                    }
                    .padding(.top, 4)
                }
                .padding(16)
            }
        }
        .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 20, style: .continuous)
                .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1.2)
        )
        .shadow(color: Color.black.opacity(0.45), radius: 14, y: 6)
    }
}

// MARK: - Infuse-Style 16:9 Media Card

struct RecordingMediaCard: View {
    let recording: Recording
    let model: AppModel
    let serverAddress: String
    var onPlay: () -> Void
    var onShowInfo: () -> Void

    var body: some View {
        let resumePos = model.resumePosition(for: recording.id) ?? 0
        let hasResume = resumePos > 0 && recording.durationSeconds > 0
        let progress = hasResume ? min(1.0, resumePos / Double(recording.durationSeconds)) : 0.0
        let palette = RecordingArtworkTheme.palette(for: recording)

        Button(action: onPlay) {
            VStack(alignment: .leading, spacing: 0) {
                // 16:9 Poster / Thumbnail Stage
                ZStack(alignment: .bottomLeading) {
                    Rectangle()
                        .fill(palette.gradient)
                        .aspectRatio(16/9, contentMode: .fit)
                        .overlay(
                            // Watermark Genre Icon
                            Image(systemName: palette.icon)
                                .font(.system(size: 72, weight: .ultraLight))
                                .foregroundStyle(palette.accent.opacity(0.15))
                                .offset(x: 35, y: -10),
                            alignment: .trailing
                        )
                        .overlay(
                            // Multi-stop Gradient Scrim
                            LinearGradient(
                                stops: [
                                    .init(color: Color.black.opacity(0.15), location: 0),
                                    .init(color: Color.clear, location: 0.35),
                                    .init(color: Color.black.opacity(0.75), location: 0.70),
                                    .init(color: Color.black.opacity(0.95), location: 1.0)
                                ],
                                startPoint: .top,
                                endPoint: .bottom
                            )
                        )

                    // Top Floating Badges
                    VStack {
                        HStack(spacing: 6) {
                            // Genre Badge
                            Text(palette.label)
                                .font(.system(size: 9, weight: .bold))
                                .foregroundStyle(palette.accent)
                                .padding(.horizontal, 7)
                                .padding(.vertical, 3)
                                .background(.ultraThinMaterial, in: Capsule())
                                .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.6))

                            Spacer()

                            // Format Badge
                            Text("1080i")
                                .font(.system(size: 9, weight: .bold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textSecondary)
                                .padding(.horizontal, 6)
                                .padding(.vertical, 3)
                                .background(.ultraThinMaterial, in: Capsule())
                                .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.6))
                        }
                        .padding(10)

                        Spacer()
                    }

                    // Center Glass Play Button
                    ZStack {
                        Circle()
                            .fill(.ultraThinMaterial)
                            .frame(width: 44, height: 44)
                            .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1))
                            .shadow(color: Color.black.opacity(0.35), radius: 6, y: 2)

                        Image(systemName: hasResume ? "play.fill" : "play.fill")
                            .font(.system(size: 16, weight: .bold))
                            .foregroundStyle(hasResume ? Theme.Colors.accentAction : Color.white)
                            .offset(x: 1.5)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)

                    // Bottom Content Overlay
                    VStack(alignment: .leading, spacing: 4) {
                        Text(recording.title)
                            .font(.system(size: 14, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(1)
                            .shadow(color: .black.opacity(0.9), radius: 3, y: 1)

                        HStack(spacing: 6) {
                            Text(recording.formattedDate)
                                .font(.system(size: 10, weight: .medium, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textTertiary)

                            Text("•")
                                .foregroundStyle(Theme.Colors.textDisabled)

                            Text(recording.formattedDuration)
                                .font(.system(size: 10, weight: .semibold, design: .monospaced))
                                .foregroundStyle(palette.accent)

                            if hasResume {
                                Text("•")
                                    .foregroundStyle(Theme.Colors.textDisabled)
                                let remainingMin = max(1, Int((Double(recording.durationSeconds) - resumePos) / 60))
                                Text("Noch \(remainingMin)m")
                                    .font(.system(size: 10, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentAction)
                            }
                        }
                    }
                    .padding(10)

                    // Bottom Edge Progress Bar
                    if hasResume {
                        GeometryReader { geo in
                            ZStack(alignment: .leading) {
                                Rectangle()
                                    .fill(Color.white.opacity(0.15))
                                    .frame(height: 3.5)

                                Rectangle()
                                    .fill(
                                        LinearGradient(
                                            colors: [Theme.Colors.accentAction, Theme.Colors.statusSuccess],
                                            startPoint: .leading,
                                            endPoint: .trailing
                                        )
                                    )
                                    .frame(width: max(6, geo.size.width * CGFloat(progress)), height: 3.5)
                                    .shadow(color: Theme.Colors.accentAction.opacity(0.8), radius: 3)
                            }
                        }
                        .frame(height: 3.5)
                    }
                }

                // Bottom Action Strip (Details & Download)
                HStack(spacing: 8) {
                    if let desc = recording.description, !desc.isEmpty {
                        Text(desc)
                            .font(.system(size: 11))
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .lineLimit(1)
                    } else {
                        Text("Aufnahme bereit")
                            .font(.system(size: 11))
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }

                    Spacer()

                    // Info Button
                    Button(action: onShowInfo) {
                        Image(systemName: "info.circle")
                            .font(.system(size: 15))
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .padding(4)
                    }
                    .buttonStyle(.plain)

                    // Download Button
                    DownloadButton(
                        recording: recording,
                        serverAddress: serverAddress,
                        model: model,
                        status: DownloadManager.shared.status(for: recording.id)
                    )
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 8)
                .background(Theme.Colors.surfaceElevated.opacity(0.75))
            }
        }
        .buttonStyle(.plain)
        .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1)
        )
        .shadow(color: Color.black.opacity(0.3), radius: 8, y: 3)
    }
}

// MARK: - Infuse-Style Rich Recording Detail Sheet

struct RecordingDetailSheet: View {
    let recording: Recording
    let model: AppModel
    let serverAddress: String
    var onPlay: (Double) -> Void
    var onDelete: () -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        let resumePos = model.resumePosition(for: recording.id) ?? 0
        let hasResume = resumePos > 0 && recording.durationSeconds > 0
        let palette = RecordingArtworkTheme.palette(for: recording)

        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                ScrollView {
                    VStack(alignment: .leading, spacing: 20) {
                        // Hero Header with 16:9 Backdrop
                        ZStack(alignment: .bottomLeading) {
                            Rectangle()
                                .fill(palette.gradient)
                                .aspectRatio(16/9, contentMode: .fit)
                                .overlay(
                                    Image(systemName: palette.icon)
                                        .font(.system(size: 120, weight: .ultraLight))
                                        .foregroundStyle(palette.accent.opacity(0.18))
                                        .offset(x: 60, y: -10),
                                    alignment: .trailing
                                )
                                .overlay(
                                    LinearGradient(
                                        stops: [
                                            .init(color: Color.clear, location: 0.3),
                                            .init(color: Color.black.opacity(0.85), location: 0.8),
                                            .init(color: Theme.Colors.bgBase, location: 1.0)
                                        ],
                                        startPoint: .top,
                                        endPoint: .bottom
                                    )
                                )

                            VStack(alignment: .leading, spacing: 6) {
                                Text(recording.title)
                                    .font(.title2.weight(.heavy))
                                    .foregroundStyle(Theme.Colors.textPrimary)
                                    .lineLimit(2)
                                    .shadow(color: .black.opacity(0.8), radius: 4, y: 2)

                                HStack(spacing: 8) {
                                    Text(recording.formattedDate)
                                        .font(.subheadline.monospaced())
                                        .foregroundStyle(Theme.Colors.textSecondary)

                                    Text("•")
                                        .foregroundStyle(Theme.Colors.textDisabled)

                                    Text(recording.formattedDuration)
                                        .font(.subheadline.bold().monospaced())
                                        .foregroundStyle(palette.accent)
                                }
                            }
                            .padding(16)
                        }
                        .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
                        .overlay(
                            RoundedRectangle(cornerRadius: 16, style: .continuous)
                                .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1)
                        )

                        // Quick Tech Specs Grid (Infuse Style)
                        HStack(spacing: 8) {
                            specPill(label: "AUFLÖSUNG", value: "1080i50 HD")
                            specPill(label: "AUDIO", value: "5.1 AC3 / Stereo")
                            specPill(label: "CONTAINER", value: "MP4 / TS")
                        }

                        // Synopsis
                        VStack(alignment: .leading, spacing: 8) {
                            Text("INHALTSANGABE")
                                .font(.caption.weight(.bold).monospaced())
                                .foregroundStyle(Theme.Colors.textTertiary)

                            if let desc = recording.description, !desc.isEmpty {
                                Text(desc)
                                    .font(.body)
                                    .foregroundStyle(Theme.Colors.textSecondary)
                                    .lineSpacing(4)
                            } else {
                                Text("Keine detaillierte Beschreibung für diese Aufnahme verfügbar.")
                                    .font(.subheadline)
                                    .foregroundStyle(Theme.Colors.textTertiary)
                            }
                        }

                        // Technical Metadata Cards
                        VStack(alignment: .leading, spacing: 8) {
                            Text("METADATEN")
                                .font(.caption.weight(.bold).monospaced())
                                .foregroundStyle(Theme.Colors.textTertiary)

                            VStack(spacing: 8) {
                                if let filename = recording.filename {
                                    HStack {
                                        Text("Dateiname")
                                            .font(.caption)
                                            .foregroundStyle(Theme.Colors.textTertiary)
                                        Spacer()
                                        Text(filename)
                                            .font(.caption.monospaced())
                                            .foregroundStyle(Theme.Colors.textSecondary)
                                            .lineLimit(1)
                                    }
                                }

                                if let sref = recording.serviceRef {
                                    HStack {
                                        Text("Service-Ref")
                                            .font(.caption)
                                            .foregroundStyle(Theme.Colors.textTertiary)
                                        Spacer()
                                        Text(sref)
                                            .font(.caption.monospaced())
                                            .foregroundStyle(Theme.Colors.textSecondary)
                                            .lineLimit(1)
                                    }
                                }
                            }
                            .padding(14)
                            .background(Theme.Colors.surfaceElevated, in: RoundedRectangle(cornerRadius: 12))
                            .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                        }

                        Spacer(minLength: 12)

                        // Action Buttons
                        VStack(spacing: 12) {
                            if hasResume {
                                Button {
                                    onPlay(resumePos)
                                } label: {
                                    HStack {
                                        Spacer()
                                        Image(systemName: "play.fill")
                                        Text("Fortsetzen bei \(formatRecordingTime(resumePos))")
                                            .font(.headline)
                                        Spacer()
                                    }
                                    .padding(.vertical, 14)
                                    .background(Theme.Colors.accentAction, in: RoundedRectangle(cornerRadius: 12))
                                    .foregroundStyle(.white)
                                    .shadow(color: Theme.Colors.accentAction.opacity(0.35), radius: 8, y: 2)
                                }

                                Button {
                                    model.updateRecordingProgress(
                                        id: recording.id,
                                        currentTime: 0,
                                        totalDuration: Double(recording.durationSeconds),
                                        title: recording.title
                                    )
                                    onPlay(0)
                                } label: {
                                    HStack {
                                        Spacer()
                                        Image(systemName: "arrow.counterclockwise")
                                        Text("Von Beginn an abspielen")
                                            .font(.headline)
                                        Spacer()
                                    }
                                    .padding(.vertical, 14)
                                    .background(Theme.Colors.surfaceElevated, in: RoundedRectangle(cornerRadius: 12))
                                    .foregroundStyle(Theme.Colors.textPrimary)
                                    .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                                }
                            } else {
                                Button {
                                    onPlay(0)
                                } label: {
                                    HStack {
                                        Spacer()
                                        Image(systemName: "play.fill")
                                        Text("Aufnahme abspielen")
                                            .font(.headline)
                                        Spacer()
                                    }
                                    .padding(.vertical, 14)
                                    .background(Theme.Colors.accentAction, in: RoundedRectangle(cornerRadius: 12))
                                    .foregroundStyle(.white)
                                    .shadow(color: Theme.Colors.accentAction.opacity(0.35), radius: 8, y: 2)
                                }
                            }

                            Button(role: .destructive) {
                                onDelete()
                            } label: {
                                HStack {
                                    Spacer()
                                    Image(systemName: "trash")
                                    Text("Vom Server löschen")
                                        .font(.subheadline.bold())
                                    Spacer()
                                }
                                .padding(.vertical, 12)
                                .background(Theme.Colors.statusError.opacity(0.15), in: RoundedRectangle(cornerRadius: 12))
                                .foregroundStyle(Theme.Colors.statusError)
                            }
                        }
                    }
                    .padding(20)
                }
            }
            .navigationTitle("Aufnahmedetails")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Schließen") { dismiss() }
                        .foregroundStyle(Theme.Colors.accentAction)
                }
            }
        }
    }

    private func specPill(label: String, value: String) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(label)
                .font(.system(size: 9, weight: .bold, design: .monospaced))
                .foregroundStyle(Theme.Colors.textTertiary)
            Text(value)
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(Theme.Colors.textPrimary)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(10)
        .background(Theme.Colors.surfaceElevated, in: RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
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
                    if let q = offline.quality {
                        Label(q.title, systemImage: q.icon)
                            .font(.system(size: 10, weight: .bold))
                            .foregroundStyle(Theme.Colors.accentLive)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Theme.Colors.surfaceElevated, in: Capsule())

                        Text("•")
                            .font(.caption2)
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }

                    Text(offline.formattedSize)
                        .font(.caption.monospaced())
                        .foregroundStyle(Theme.Colors.textTertiary)

                    Text("•")
                        .font(.caption2)
                        .foregroundStyle(Theme.Colors.textTertiary)

                    Text(offline.formattedDuration)
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
        .background(Theme.Gradients.cardSurface, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 16, style: .continuous).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1))
        .shadow(color: Color.black.opacity(0.18), radius: 6, y: 2)
    }
}

// MARK: - Download Button (Plex/Netflix Multi-Quality Menu)

struct DownloadButton: View {

    let recording: Recording
    let serverAddress: String
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
                    .font(.system(size: 22))
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
                        .font(.system(size: 8))
                        .foregroundStyle(Theme.Colors.accentAction)
                }
                .frame(width: 22, height: 22)
                .frame(width: 36, height: 36)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

        case .downloaded:
            Image(systemName: "arrow.down.circle.fill")
                .font(.system(size: 22))
                .foregroundStyle(Theme.Colors.statusSuccess)
                .frame(width: 36, height: 36)
        }
    }

    private func start(quality: DownloadQuality) {
        let base = serverAddress.starts(with: "http") ? serverAddress : "https://\(serverAddress)"
        guard let url = URL(string: base) else { return }

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
