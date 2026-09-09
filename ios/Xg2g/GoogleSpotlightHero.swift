// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

struct GoogleSpotlightHero: View {
    let channel: Channel
    let entry: NowNext.Entry
    let model: AppModel
    var onPlay: () -> Void
    var onShowInfo: () -> Void
    var onRecord: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            // Header: "HIGHLIGHT" + Channel Logo + Live Pulsing Badge
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
                            .font(.system(size: 14, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)

                        Image(systemName: "star.fill")
                            .font(.system(size: 10))
                            .foregroundStyle(.yellow)
                    }

                    HStack(spacing: 5) {
                        PulsingLiveDot(size: 6)
                        Text(model.selectedTimeFilter == .now ? "FAVORIT JETZT LIVE" : "FAVORIT HIGHLIGHT")
                            .font(.system(size: 10, weight: .bold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.accentLive)
                    }
                }

                Spacer()

                let g = entry.genre(channelName: channel.name)
                if g != .all {
                    Label(g.rawValue, systemImage: g.icon)
                        .font(.system(size: 11, weight: .bold))
                        .foregroundStyle(Theme.Colors.accentLive)
                        .padding(.horizontal, 9)
                        .padding(.vertical, 4)
                        .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())
                }
            }

            // Title & Synopsis
            VStack(alignment: .leading, spacing: 4) {
                Text(entry.title)
                    .font(.title2.weight(.bold))
                    .foregroundStyle(Theme.Colors.textPrimary)
                    .lineLimit(2)

                if let desc = entry.description, !desc.isEmpty {
                    Text(desc)
                        .font(.subheadline)
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .lineLimit(2)
                }
            }

            // Live Scrubber (if on air)
            if let progress = entry.progress(at: .now) {
                InfuseScrubber(
                    progress: progress,
                    startTime: entry.formattedStartTime,
                    endTime: entry.formattedEndTime,
                    remainingText: entry.remainingMinutes(at: .now).map { "noch \($0) Min" }
                )
            }

            // Google-Style Action Buttons
            HStack(spacing: 10) {
                // Big 1-Tap Play Button
                Button(action: onPlay) {
                    HStack(spacing: 6) {
                        Image(systemName: "play.fill")
                            .font(.system(size: 13, weight: .bold))
                        Text("Jetzt ansehen")
                            .font(.system(size: 14, weight: .bold))
                    }
                    .padding(.horizontal, 18)
                    .padding(.vertical, 10)
                    .background(Theme.Colors.accentAction, in: Capsule())
                    .foregroundStyle(.white)
                }
                .buttonStyle(.plain)

                // 1-Tap Record Button
                Button(action: onRecord) {
                    HStack(spacing: 5) {
                        Image(systemName: "record.circle")
                            .font(.system(size: 13, weight: .semibold))
                        Text("Aufnehmen")
                            .font(.system(size: 13, weight: .semibold))
                    }
                    .padding(.horizontal, 14)
                    .padding(.vertical, 10)
                    .background(Theme.Colors.surfaceElevated, in: Capsule())
                    .foregroundStyle(Theme.Colors.textPrimary)
                    .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                }
                .buttonStyle(.plain)

                // Info / Wiederholungen Button
                Button(action: onShowInfo) {
                    Image(systemName: "info.circle")
                        .font(.system(size: 16))
                        .padding(10)
                        .background(Theme.Colors.surfaceElevated, in: Circle())
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                }
                .buttonStyle(.plain)
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
                            Color(red: 0.08, green: 0.12, blue: 0.18)
                        ],
                        startPoint: .topLeading,
                        endPoint: .bottomTrailing
                    )
                )
        )
        .overlay(
            RoundedRectangle(cornerRadius: 18, style: .continuous)
                .strokeBorder(Theme.Gradients.liveAuraBorder, lineWidth: 1.2)
        )
        .shadow(color: Theme.Colors.accentLive.opacity(0.12), radius: 12, y: 4)
    }
}
