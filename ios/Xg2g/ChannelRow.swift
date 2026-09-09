// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

struct ChannelRow: View {

    let channel: Channel
    let nowNext: NowNext?
    let fullSchedule: [NowNext.Entry]
    var previewHours: AppModel.EpgPreviewHours = .fourHours
    var timeFilter: AppModel.TimeFilter = .now
    var targetShow: NowNext.Entry? = nil
    var isFavorite: Bool = false
    var onPlay: () -> Void = {}
    var onShowInfo: (NowNext.Entry) -> Void = { _ in }
    var onRecord: (NowNext.Entry) -> Void = { _ in }

    var body: some View {
        let displayedShow = targetShow ?? nowNext?.now

        Button {
            Haptics.shared.impact(.light)
            onPlay()
        } label: {
            VStack(alignment: .leading, spacing: 9) {
                // MARK: - Header: Logo + Channel Name & Number + Genre/Live Badge
                HStack(spacing: 10) {
                    ChannelLogo(url: channel.logoURL, name: channel.name, size: 38)

                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 5) {
                            if let number = channel.number {
                                Text(number)
                                    .font(.system(size: 10, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentAction)
                                    .padding(.horizontal, 5)
                                    .padding(.vertical, 1.5)
                                    .background(Theme.Colors.accentAction.opacity(0.15), in: RoundedRectangle(cornerRadius: 4))
                            }

                            Text(channel.name)
                                .font(.system(size: 15, weight: .bold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                                .lineLimit(1)

                            if isFavorite {
                                Image(systemName: "star.fill")
                                    .font(.system(size: 10))
                                    .foregroundStyle(.yellow)
                            }
                        }
                    }

                    Spacer(minLength: 4)

                    if let show = displayedShow {
                        let currentGenre = show.genre(channelName: channel.name)
                        HStack(spacing: 6) {
                            if timeFilter == .now {
                                PulsingLiveDot(size: 5)
                            }
                            if currentGenre != .all {
                                Text(currentGenre.rawValue)
                                    .font(.system(size: 10, weight: .semibold))
                                    .foregroundStyle(timeFilter == .now ? Theme.Colors.accentLive : Theme.Colors.textSecondary)
                                    .padding(.horizontal, 7)
                                    .padding(.vertical, 2.5)
                                    .background(
                                        timeFilter == .now ? Theme.Colors.accentLive.opacity(0.14) : Theme.Colors.surfaceElevated,
                                        in: Capsule()
                                    )
                            }
                        }
                    }
                }

                // MARK: - Show Details & Clean Timing
                if let show = displayedShow {
                    VStack(alignment: .leading, spacing: 3) {
                        HStack(alignment: .top) {
                            Text(show.title)
                                .font(.system(size: 14, weight: .bold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                                .lineLimit(1)

                            Spacer(minLength: 4)

                            // Quick Info Button to open Detail Sheet
                            Button {
                                Haptics.shared.impact(.light)
                                onShowInfo(show)
                            } label: {
                                Image(systemName: "info.circle")
                                    .font(.system(size: 14))
                                    .foregroundStyle(Theme.Colors.textTertiary)
                                    .padding(2)
                            }
                            .buttonStyle(.plain)
                        }

                        HStack(spacing: 6) {
                            Text(show.formattedTimeRange)
                                .font(.system(size: 11, weight: .medium, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textSecondary)

                            if timeFilter == .now, let remaining = show.remainingMinutes(at: .now) {
                                Text("• noch \(remaining) Min")
                                    .font(.system(size: 11, weight: .semibold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentLive)
                            } else {
                                Text("• \(show.durationMinutes) Min")
                                    .font(.system(size: 11, weight: .regular))
                                    .foregroundStyle(Theme.Colors.textTertiary)
                            }
                        }

                        if let desc = show.description, !desc.isEmpty {
                            Text(desc)
                                .font(.system(size: 12))
                                .foregroundStyle(Theme.Colors.textTertiary)
                                .lineLimit(1)
                                .padding(.top, 1)
                        }
                    }

                    // Sleek Live Progress Bar
                    if timeFilter == .now, let fraction = show.progress(at: .now) {
                        GeometryReader { geo in
                            ZStack(alignment: .leading) {
                                Capsule()
                                    .fill(Color.white.opacity(0.12))
                                    .frame(height: 3)

                                Capsule()
                                    .fill(
                                        LinearGradient(
                                            colors: [Theme.Colors.accentAction, Theme.Colors.accentLive],
                                            startPoint: .leading,
                                            endPoint: .trailing
                                        )
                                    )
                                    .frame(width: max(0, min(geo.size.width, geo.size.width * CGFloat(fraction))), height: 3)
                            }
                        }
                        .frame(height: 3)
                        .padding(.top, 2)
                    }
                } else {
                    Text("Keine Programminformationen verfügbar")
                        .font(.caption)
                        .foregroundStyle(Theme.Colors.textTertiary)
                        .padding(.vertical, 4)
                }

                // MARK: - Next Show Preview ("Danach")
                if timeFilter == .now, let next = nowNext?.next {
                    HStack(spacing: 6) {
                        Text("DANACH:")
                            .font(.system(size: 9, weight: .bold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.textTertiary)

                        Text(next.formattedStartTime)
                            .font(.system(size: 11, weight: .semibold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.accentAction)

                        Text(next.title)
                            .font(.system(size: 11))
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .lineLimit(1)

                        Spacer()
                    }
                    .padding(.top, 2)
                }
            }
            .padding(12)
            .background(
                RoundedRectangle(cornerRadius: 14, style: .continuous)
                    .fill(Theme.Gradients.cardSurface)
            )
            .overlay(
                RoundedRectangle(cornerRadius: 14, style: .continuous)
                    .strokeBorder(timeFilter == .now ? Theme.Gradients.liveAuraBorder : Theme.Gradients.specularBorder, lineWidth: 0.8)
            )
            .shadow(
                color: timeFilter == .now ? Theme.Colors.accentLive.opacity(0.12) : Color.black.opacity(0.18),
                radius: 6,
                y: 2
            )
            .contentShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        }
        .buttonStyle(.plain)
    }
}
