// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Television DVR recordings library.
///
/// Clean, television-grade media center interface using the TVDesign system:
/// A spotlight hero for the newest or in-progress recording, followed by
/// uniform 340×200 cards with readable typography and Siri Remote focus.
/// No mobile search keyboards, no offline-download chips.
struct TVRecordingsView: View {

    @Bindable var model: AppModel
    @State private var confirmDeleteRecording: Recording?

    private var recordings: [Recording] {
        model.recordings
    }

    private var inProgressRecordings: [Recording] {
        recordings.filter { rec in
            let pos = model.resumePosition(for: rec.id) ?? 0
            return pos > 10 && (Double(rec.durationSeconds) <= 0 || pos < Double(rec.durationSeconds) * 0.95)
        }
    }

    private func play(_ recording: Recording, fromBeginning: Bool = false) {
        let resume = fromBeginning ? 0 : (model.resumePosition(for: recording.id) ?? 0)
        model.playbackManager.play(recording: recording, startPosition: resume)
    }

    var body: some View {
        ZStack {
            Theme.Colors.bgBase.ignoresSafeArea()

            if recordings.isEmpty {
                ContentUnavailableView(
                    model.isLoadingRecordings ? "Lade Aufnahmen…" : "Keine Aufnahmen",
                    systemImage: "film.stack",
                    description: Text("Aufnahmen von deiner VU+ Box erscheinen hier.")
                )
                .foregroundStyle(Theme.Colors.textSecondary)
            } else {
                ScrollView(.vertical, showsIndicators: false) {
                    VStack(alignment: .leading, spacing: TVDesign.Layout.gutter) {
                        // 1. Header
                        HStack(alignment: .firstTextBaseline) {
                            Text("Aufnahmen")
                                .font(TVDesign.Font.heading)
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Text("\(recordings.count) Aufnahmen")
                                .font(TVDesign.Font.meta)
                                .foregroundStyle(Theme.Colors.textSecondary)
                        }

                        // 2. Spotlight Hero (Newest or resumed recording)
                        if let hero = inProgressRecordings.first ?? recordings.first {
                            TVRecordingHero(
                                recording: hero,
                                resumePosition: model.resumePosition(for: hero.id),
                                onPlay: { play(hero) },
                                onPlayFromBeginning: { play(hero, fromBeginning: true) },
                                onDelete: { confirmDeleteRecording = hero }
                            )
                        }

                        // 3. In-Progress Shelf
                        if inProgressRecordings.count > 1 {
                            TVShelf(title: "Weiterschauen") {
                                ForEach(inProgressRecordings) { rec in
                                    TVRecordingCard(
                                        recording: rec,
                                        resumePos: model.resumePosition(for: rec.id),
                                        onPlay: { play(rec) },
                                        onDelete: { confirmDeleteRecording = rec }
                                    )
                                }
                            }
                        }

                        // 4. All Recordings Shelf / Grid
                        TVShelf(title: "Alle Aufnahmen") {
                            ForEach(recordings) { rec in
                                TVRecordingCard(
                                    recording: rec,
                                    resumePos: model.resumePosition(for: rec.id),
                                    onPlay: { play(rec) },
                                    onDelete: { confirmDeleteRecording = rec }
                                )
                            }
                        }
                    }
                    .padding(.horizontal, 48)
                    .padding(.vertical, 24)
                }
            }
        }
        .onExitCommand {
            // Pressing Menu on secondary tabs returns cleanly to the Home tab
            model.selectedTab = .home
        }
        .task {
            if model.recordings.isEmpty {
                await model.loadRecordings()
            }
        }
        .confirmationDialog(
            "Aufnahme löschen?",
            isPresented: Binding(
                get: { confirmDeleteRecording != nil },
                set: { if !$0 { confirmDeleteRecording = nil } }
            ),
            titleVisibility: .visible
        ) {
            if let rec = confirmDeleteRecording {
                Button("„\(rec.title)“ löschen", role: .destructive) {
                    Task {
                        await model.deleteRecording(rec)
                        confirmDeleteRecording = nil
                    }
                }
                Button("Abbrechen", role: .cancel) {
                    confirmDeleteRecording = nil
                }
            }
        } message: {
            if confirmDeleteRecording != nil {
                Text("Möchtest du diese Aufnahme wirklich von der Festplatte entfernen?")
            }
        }
    }
}

// MARK: - Hero Component

struct TVRecordingHero: View {
    let recording: Recording
    let resumePosition: Double?
    var onPlay: () -> Void
    var onPlayFromBeginning: () -> Void
    var onDelete: () -> Void

    var body: some View {
        let resumeSecs = resumePosition ?? 0
        let hasResume = resumeSecs > 10
        let duration = Double(recording.durationSeconds)
        let progress = duration > 0 ? min(1.0, resumeSecs / duration) : 0

        VStack(alignment: .leading, spacing: 18) {
            HStack(spacing: 14) {
                Image(systemName: "film.stack.fill")
                    .font(TVDesign.Font.meta)
                    .foregroundStyle(Theme.Colors.accentAction)
                Text("Aufnahme")
                    .font(TVDesign.Font.meta)
                    .foregroundStyle(Theme.Colors.accentAction)
                Text("•")
                    .font(TVDesign.Font.meta)
                    .foregroundStyle(Theme.Colors.textSecondary)
                Text(recording.formattedDate)
                    .font(TVDesign.Font.meta)
                    .foregroundStyle(Theme.Colors.textSecondary)
                Text("•")
                    .font(TVDesign.Font.meta)
                    .foregroundStyle(Theme.Colors.textSecondary)
                Text(recording.formattedDuration)
                    .font(TVDesign.Font.meta)
                    .foregroundStyle(Theme.Colors.textSecondary)
            }

            Text(recording.title)
                .font(TVDesign.Font.hero)
                .foregroundStyle(Theme.Colors.textPrimary)
                .lineLimit(2)

            if let description = recording.description, !description.isEmpty {
                Text(description)
                    .font(TVDesign.Font.meta)
                    .foregroundStyle(Theme.Colors.textSecondary)
                    .lineLimit(2)
                    .frame(maxWidth: 1100, alignment: .leading)
            }

            if hasResume {
                HStack(spacing: 16) {
                    let mins = Int(resumeSecs / 60)
                    Text("Fortsetzen bei \(mins) Min")
                    TVProgressLine(progress: progress)
                        .frame(width: 320)
                    let remaining = max(0, Int((duration - resumeSecs) / 60))
                    Text("noch \(remaining) Min")
                }
                .font(TVDesign.Font.meta)
                .foregroundStyle(Theme.Colors.textSecondary)
            }

            HStack(spacing: 20) {
                Button(action: onPlay) {
                    Label(hasResume ? "Fortsetzen" : "Abspielen", systemImage: "play.fill")
                        .font(TVDesign.Font.body)
                }

                if hasResume {
                    Button(action: onPlayFromBeginning) {
                        Label("Von Beginn", systemImage: "arrow.counterclockwise")
                            .font(TVDesign.Font.body)
                    }
                }

                Button(action: onDelete) {
                    Label("Löschen", systemImage: "trash")
                        .font(TVDesign.Font.body)
                }
            }
            .padding(.top, 6)
        }
        .padding(36)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(
            RoundedRectangle(cornerRadius: 24, style: .continuous)
                .fill(Theme.Gradients.cardSurface)
        )
        .overlay(
            RoundedRectangle(cornerRadius: 24, style: .continuous)
                .strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1)
        )
    }
}
