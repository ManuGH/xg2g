// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

struct RecordingsView: View {

    let model: AppModel

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                Group {
                    if model.recordings.isEmpty && model.isLoadingRecordings {
                        ProgressView("Lade Aufnahmen…")
                            .tint(Theme.Colors.accentAction)
                            .foregroundStyle(Theme.Colors.textSecondary)
                    } else if model.recordings.isEmpty {
                        ContentUnavailableView(
                            "Keine Aufnahmen",
                            systemImage: "play.rectangle.on.rectangle",
                            description: Text("Es wurden keine DVR-Aufnahmen auf dem Server gefunden.")
                        )
                        .foregroundStyle(Theme.Colors.textSecondary)
                    } else {
                        List(model.recordings) { recording in
                            RecordingRow(recording: recording)
                                .listRowBackground(Theme.Colors.surfaceElevated)
                                .listRowSeparatorTint(Theme.Colors.borderSubtle)
                        }
                        .listStyle(.plain)
                        .scrollContentBackground(.hidden)
                        .refreshable { await model.loadRecordings() }
                    }
                }
            }
            .navigationTitle("Aufnahmen")
            .toolbar {
                if model.isLoadingRecordings && !model.recordings.isEmpty {
                    ProgressView()
                        .tint(Theme.Colors.accentAction)
                }
            }
        }
    }
}

struct RecordingRow: View {

    let recording: Recording

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
        }
        .padding(.vertical, 4)
    }
}
