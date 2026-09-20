// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import SwiftUI

// MARK: - Spotlight Hero Banner (Apple TV+ / Infuse Style)

struct RecordingSpotlightHero: View {
    let recording: Recording
    let model: AppModel
    let serverAddress: ServerAddress?
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
                            .font(.app(size: 140, weight: .ultraLight))
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
                                .font(.app(size: 10, weight: .bold))
                            Text(hasResume ? "WEITERSCHAUEN" : "NEUESTE AUFNAHME")
                                .font(.app(size: 10, weight: .bold, design: .monospaced))
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
                                .font(.app(size: 10, weight: .bold, design: .monospaced))
                            Text("•")
                                .foregroundStyle(Theme.Colors.textDisabled)
                            Text("5.1")
                                .font(.app(size: 10, weight: .bold, design: .monospaced))
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
                            .font(.app(size: 22, weight: .heavy))
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
                                .font(.app(size: 12, weight: .medium, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textTertiary)

                            Text("•")
                                .foregroundStyle(Theme.Colors.textDisabled)

                            Text(recording.formattedDuration)
                                .font(.app(size: 12, weight: .semibold, design: .monospaced))
                                .foregroundStyle(palette.accent)

                            if hasResume {
                                Text("•")
                                    .foregroundStyle(Theme.Colors.textDisabled)
                                let remainingMin = max(1, Int((Double(recording.durationSeconds) - resumePos) / 60))
                                Text("Noch \(remainingMin)m verbleibend")
                                    .font(.app(size: 12, weight: .bold, design: .monospaced))
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
                                    .font(.app(size: 15, weight: .bold))
                                Text(hasResume ? "Fortsetzen bei \(RecordingTimeFormatter.string(from: resumePos))" : "Jetzt abspielen")
                                    .font(.app(size: 14, weight: .bold))
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
                                    .font(.app(size: 14, weight: .semibold))
                                Text("Details")
                                    .font(.app(size: 13, weight: .semibold))
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
