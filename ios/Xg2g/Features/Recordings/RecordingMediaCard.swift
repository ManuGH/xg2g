// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import SwiftUI

// MARK: - Infuse-Style 16:9 Media Card

struct RecordingMediaCard: View {
    let recording: Recording
    let model: AppModel
    let serverAddress: ServerAddress?
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
                                .font(.app(size: 72, weight: .ultraLight))
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
                                .font(.app(size: 9, weight: .bold))
                                .foregroundStyle(palette.accent)
                                .padding(.horizontal, 7)
                                .padding(.vertical, 3)
                                .background(.ultraThinMaterial, in: Capsule())
                                .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.6))

                            Spacer()

                            // Format Badge
                            Text("1080i")
                                .font(.app(size: 9, weight: .bold, design: .monospaced))
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
                            .font(.app(size: 16, weight: .bold))
                            .foregroundStyle(hasResume ? Theme.Colors.accentAction : Color.white)
                            .offset(x: 1.5)
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity)

                    // Bottom Content Overlay
                    VStack(alignment: .leading, spacing: 4) {
                        Text(recording.title)
                            .font(.app(size: 14, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(1)
                            .shadow(color: .black.opacity(0.9), radius: 3, y: 1)

                        HStack(spacing: 6) {
                            Text(recording.formattedDate)
                                .font(.app(size: 10, weight: .medium, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textTertiary)

                            Text("•")
                                .foregroundStyle(Theme.Colors.textDisabled)

                            Text(recording.formattedDuration)
                                .font(.app(size: 10, weight: .semibold, design: .monospaced))
                                .foregroundStyle(palette.accent)

                            if hasResume {
                                Text("•")
                                    .foregroundStyle(Theme.Colors.textDisabled)
                                let remainingMin = max(1, Int((Double(recording.durationSeconds) - resumePos) / 60))
                                Text("Noch \(remainingMin)m")
                                    .font(.app(size: 10, weight: .bold, design: .monospaced))
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
                            .font(.app(size: 11))
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .lineLimit(1)
                    } else {
                        Text("Aufnahme bereit")
                            .font(.app(size: 11))
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }

                    Spacer()

                    // Info Button
                    Button(action: onShowInfo) {
                        Image(systemName: "info.circle")
                            .font(.app(size: 15))
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
