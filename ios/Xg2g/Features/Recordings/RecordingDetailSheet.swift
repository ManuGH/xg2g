// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import SwiftUI

// MARK: - Infuse-Style Rich Recording Detail Sheet

struct RecordingDetailSheet: View {
    let recording: Recording
    let model: AppModel
    let serverAddress: ServerAddress?
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
                                        Text("Fortsetzen bei \(RecordingTimeFormatter.string(from: resumePos))")
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
#if !os(tvOS)
            .navigationBarTitleDisplayMode(.inline)
#endif
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
