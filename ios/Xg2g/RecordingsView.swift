// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import SwiftUI

/// Recordings library with Netflix / Infuse / Apple TV+ inspired aesthetics.
///
/// Features:
/// - "Weiterschauen / Spotlight" Hero Banner with instant resume playback
/// - Multi-Category Filter Chips (Alle, Downloads, Spielfilme, Serien, Sport, Dokus)
/// - Real-time Title & Synopsis Search
/// - Offline Background Downloads with Apple-style Storage Breakdown
private func formatRecordingTime(_ seconds: Double) -> String {
    let mins = Int(seconds) / 60
    let secs = Int(seconds) % 60
    return String(format: "%02d:%02d", mins, secs)
}

/// - Rich Recording Detail Sheet with 1-Tap Playback & Delete Confirmation
/// - Persistent playback progress syncing (Resume at exact timestamp)
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
    @State private var activePlaybackItem: PlayingRecordingItem?
    @State private var promptResumeRecording: Recording?
    @State private var selectedDetailRecording: Recording?
    @State private var playingOffline: OfflineRecording?
    @State private var recordingToDelete: Recording?

    private func play(recording: Recording, startPosition: Double) {
        activePlaybackItem = PlayingRecordingItem(
            id: recording.id,
            recording: recording,
            initialPosition: startPosition
        )
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
                        // Offline Downloads Tab
                        offlineContentView
                    } else {
                        // Server Recordings Tab
                        serverRecordingsContentView
                    }
                }
            }
            .navigationTitle("Aufnahmen")
            .navigationBarTitleDisplayMode(.inline)
            .searchable(text: $searchText, prompt: "Aufnahmen nach Titel oder Genre suchen…")
            .fullScreenCover(item: $activePlaybackItem) { item in
                RecordingPlayerScreen(
                    recording: item.recording,
                    serverAddress: model.serverURLString,
                    initialPosition: item.initialPosition,
                    onProgressUpdate: { current, total in
                        model.updateRecordingProgress(
                            id: item.recording.id,
                            currentTime: current,
                            totalDuration: total,
                            title: item.recording.title
                        )
                    }
                )
            }
            .fullScreenCover(item: $playingOffline) { offline in
                OfflinePlayerScreen(offlineRecording: offline)
            }
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
                VStack(spacing: 20) {
                    // 1. Spotlight / Weiterschauen Hero (if not searching)
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
                            .font(.headline.weight(.bold))
                            .foregroundStyle(Theme.Colors.textPrimary)

                        Spacer()

                        Text("\(filtered.count) Einträge")
                            .font(.system(size: 11, weight: .semibold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                    .padding(.horizontal, 2)

                    // 3. Multi-Column Grid of Recordings
                    LazyVGrid(
                        columns: [
                            GridItem(.adaptive(minimum: isRegular ? 320 : 280, maximum: 460), spacing: isRegular ? 16 : 12)
                        ],
                        spacing: isRegular ? 16 : 12
                    ) {
                        ForEach(filtered) { recording in
                            RecordingCard(
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
                .padding(.horizontal, isRegular ? 20 : 12)
                .padding(.vertical, isRegular ? 16 : 12)
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

// MARK: - Recording Spotlight Hero Banner (Netflix / Apple TV+ Style)

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

        VStack(alignment: .leading, spacing: 14) {
            // Top Badge: "WEITERSCHAUEN" or "NEUESTE AUFNAHME"
            HStack(spacing: 8) {
                HStack(spacing: 5) {
                    Image(systemName: hasResume ? "play.circle.fill" : "sparkles.tv")
                        .font(.system(size: 11, weight: .bold))
                    Text(hasResume ? "WEITERSCHAUEN" : "NEUESTE AUFNAHME")
                        .font(.system(size: 10, weight: .bold, design: .monospaced))
                }
                .foregroundStyle(Theme.Colors.accentAction)
                .padding(.horizontal, 9)
                .padding(.vertical, 4)
                .background(Theme.Colors.accentAction.opacity(0.18), in: Capsule())

                Spacer()

                Text("\(recording.formattedDate) • \(recording.formattedDuration)")
                    .font(.system(size: 11, weight: .medium, design: .monospaced))
                    .foregroundStyle(Theme.Colors.textTertiary)
            }

            // Title & Synopsis
            VStack(alignment: .leading, spacing: 4) {
                Text(recording.title)
                    .font(.title2.weight(.bold))
                    .foregroundStyle(Theme.Colors.textPrimary)
                    .lineLimit(2)

                if let desc = recording.description, !desc.isEmpty {
                    Text(desc)
                        .font(.subheadline)
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .lineLimit(2)
                }
            }

            // Resume Progress Bar (if in progress)
            if hasResume {
                VStack(alignment: .leading, spacing: 4) {
                    HStack {
                        let remainingMin = max(1, Int((Double(recording.durationSeconds) - resumePos) / 60))
                        Text("Noch \(remainingMin) Min verbleibend")
                            .font(.system(size: 11, weight: .semibold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.accentAction)

                        Spacer()

                        Text("\(Int(progress * 100))%")
                            .font(.system(size: 11, weight: .bold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }

                    GeometryReader { geo in
                        ZStack(alignment: .leading) {
                            Capsule()
                                .fill(Color.white.opacity(0.15))
                                .frame(height: 5)

                            Capsule()
                                .fill(Theme.Colors.accentAction)
                                .frame(width: max(8, geo.size.width * CGFloat(progress)), height: 5)
                        }
                    }
                    .frame(height: 5)
                }
                .padding(.vertical, 2)
            }

            // Action Buttons
            HStack(spacing: 10) {
                // 1-Tap Play Button
                Button(action: onPlay) {
                    HStack(spacing: 6) {
                        Image(systemName: hasResume ? "play.fill" : "play.circle.fill")
                            .font(.system(size: 14, weight: .bold))
                        Text(hasResume ? "Fortsetzen" : "Jetzt abspielen")
                            .font(.system(size: 14, weight: .bold))
                    }
                    .padding(.horizontal, 18)
                    .padding(.vertical, 10)
                    .background(Theme.Colors.accentAction, in: Capsule())
                    .foregroundStyle(.white)
                }
                .buttonStyle(.plain)

                // Info Button
                Button(action: onShowInfo) {
                    Image(systemName: "info.circle")
                        .font(.system(size: 16))
                        .padding(10)
                        .background(Theme.Colors.surfaceElevated, in: Circle())
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                }
                .buttonStyle(.plain)

                Spacer()

                // Download Button
                DownloadButton(
                    recording: recording,
                    serverAddress: serverAddress,
                    model: model,
                    status: DownloadManager.shared.status(for: recording.id)
                )
            }
            .padding(.top, 4)
        }
        .padding(18)
        .background(
            RoundedRectangle(cornerRadius: 18, style: .continuous)
                .fill(
                    LinearGradient(
                        colors: [
                            Theme.Colors.surfaceElevated.opacity(0.95),
                            Color(red: 0.07, green: 0.11, blue: 0.17)
                        ],
                        startPoint: .topLeading,
                        endPoint: .bottomTrailing
                    )
                )
        )
        .overlay(
            RoundedRectangle(cornerRadius: 18, style: .continuous)
                .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1.2)
        )
        .shadow(color: Color.black.opacity(0.35), radius: 10, y: 4)
    }
}

// MARK: - Recording Card (Modern Responsive Grid Item)

struct RecordingCard: View {
    let recording: Recording
    let model: AppModel
    let serverAddress: String
    var onPlay: () -> Void
    var onShowInfo: () -> Void

    var body: some View {
        let resumePos = model.resumePosition(for: recording.id) ?? 0
        let hasResume = resumePos > 0 && recording.durationSeconds > 0
        let progress = hasResume ? min(1.0, resumePos / Double(recording.durationSeconds)) : 0.0

        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 12) {
                // Play Icon Container
                Button(action: onPlay) {
                    ZStack {
                        RoundedRectangle(cornerRadius: 10, style: .continuous)
                            .fill(Theme.Colors.surfaceElevated)
                            .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1))

                        Image(systemName: hasResume ? "play.fill" : "play.circle.fill")
                            .font(.system(size: 20))
                            .foregroundStyle(Theme.Colors.accentAction)
                    }
                    .frame(width: 44, height: 44)
                }
                .buttonStyle(.plain)

                // Title & Date
                Button(action: onShowInfo) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(recording.title)
                            .font(.system(size: 15, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(1)

                        HStack(spacing: 6) {
                            Text(recording.formattedDate)
                                .font(.system(size: 11, weight: .medium, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textTertiary)

                            Text("•")
                                .font(.caption2)
                                .foregroundStyle(Theme.Colors.textDisabled)

                            Text(recording.formattedDuration)
                                .font(.system(size: 11, weight: .semibold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentAction)
                        }
                    }
                }
                .buttonStyle(.plain)

                Spacer(minLength: 4)

                // Download Button (Plex/Netflix Style)
                DownloadButton(
                    recording: recording,
                    serverAddress: serverAddress,
                    model: model,
                    status: DownloadManager.shared.status(for: recording.id)
                )
            }

            // Synopsis
            if let description = recording.description, !description.isEmpty {
                Text(description)
                    .font(.caption)
                    .foregroundStyle(Theme.Colors.textSecondary)
                    .lineLimit(2)
                    .lineSpacing(2)
            }

            // Playback Progress Bar (if in progress)
            if hasResume {
                VStack(alignment: .leading, spacing: 3) {
                    GeometryReader { geo in
                        ZStack(alignment: .leading) {
                            Capsule()
                                .fill(Color.white.opacity(0.12))
                                .frame(height: 4)

                            Capsule()
                                .fill(Theme.Colors.accentAction)
                                .frame(width: max(6, geo.size.width * CGFloat(progress)), height: 4)
                        }
                    }
                    .frame(height: 4)

                    HStack {
                        let remainingMin = max(1, Int((Double(recording.durationSeconds) - resumePos) / 60))
                        Text("Noch \(remainingMin)m")
                            .font(.system(size: 10, weight: .semibold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.accentAction)

                        Spacer()

                        Text("\(Int(progress * 100))%")
                            .font(.system(size: 10, weight: .bold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                }
                .padding(.top, 2)
            }
        }
        .padding(14)
        .background(
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .fill(Theme.Gradients.cardSurface)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1)
        )
        .shadow(color: Color.black.opacity(0.2), radius: 6, y: 2)
    }
}

struct PlayingRecordingItem: Identifiable, Sendable {
    let id: String
    let recording: Recording
    let initialPosition: Double
}

// MARK: - Recording Detail Sheet

struct RecordingDetailSheet: View {
    let recording: Recording
    let model: AppModel
    let serverAddress: String
    var onPlay: (Double) -> Void
    var onDelete: () -> Void
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                ScrollView {
                    VStack(alignment: .leading, spacing: 20) {
                        // Header
                        VStack(alignment: .leading, spacing: 6) {
                            Text(recording.title)
                                .font(.title2.weight(.bold))
                                .foregroundStyle(Theme.Colors.textPrimary)

                            HStack(spacing: 8) {
                                Text(recording.formattedDate)
                                    .font(.subheadline.monospaced())
                                    .foregroundStyle(Theme.Colors.textSecondary)

                                Text("•")
                                    .foregroundStyle(Theme.Colors.textDisabled)

                                Text(recording.formattedDuration)
                                    .font(.subheadline.bold().monospaced())
                                    .foregroundStyle(Theme.Colors.accentAction)
                            }
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

                        // Technical Metadata
                        VStack(alignment: .leading, spacing: 8) {
                            Text("DETAILS")
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
                            .padding(12)
                            .background(Theme.Colors.surfaceElevated, in: RoundedRectangle(cornerRadius: 10))
                        }

                        Spacer(minLength: 20)

                        // Action Buttons
                        VStack(spacing: 10) {
                            let resumePos = model.resumePosition(for: recording.id) ?? 0
                            if resumePos > 0 {
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

    private func formatTime(_ seconds: Double) -> String {
        let mins = Int(seconds) / 60
        let secs = Int(seconds) % 60
        return String(format: "%02d:%02d", mins, secs)
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
