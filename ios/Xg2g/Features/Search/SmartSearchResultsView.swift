// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Clean, categorized search results view that intelligently classifies items:
/// 1. Sender (Channels matching query)
/// 2. Jetzt Live (Shows on air right now with live indicator & play button)
/// 3. Demnächst im Programm (Upcoming shows sorted chronologically with record button)
struct SmartSearchResultsView: View {

    let result: SmartSearchResult
    var onPlayChannel: (Channel) -> Void
    var onOpenShowDetail: (Channel, NowNext.Entry) -> Void
    var onRecordShow: (Channel, NowNext.Entry) -> Void

    var body: some View {
        if result.isEmpty {
            VStack(spacing: 16) {
                Spacer()
                ContentUnavailableView(
                    "Keine Treffer für „\(result.query)“",
                    systemImage: "magnifyingglass",
                    description: Text("Weder Sender noch laufende oder kommende Sendungen entsprechen deiner Suche.")
                )
                .foregroundStyle(Theme.Colors.textSecondary)
                Spacer()
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 22) {
                    // MARK: - 1. SENDER MATCHES
                    if !result.channels.isEmpty {
                        VStack(alignment: .leading, spacing: 10) {
                            sectionHeader(
                                title: "SENDER",
                                icon: "tv",
                                count: result.channels.count,
                                color: Theme.Colors.accentAction
                            )

                            ScrollView(.horizontal, showsIndicators: false) {
                                HStack(spacing: 10) {
                                    ForEach(result.channels) { channel in
                                        Button {
                                            triggerHaptic(.light)
                                            onPlayChannel(channel)
                                        } label: {
                                            HStack(spacing: 8) {
                                                ChannelLogo(url: channel.logoURL, name: channel.name, size: 28)

                                                VStack(alignment: .leading, spacing: 1) {
                                                    if let number = channel.number {
                                                        Text("CH \(number)")
                                                            .font(.system(size: 9, weight: .bold, design: .monospaced))
                                                            .foregroundStyle(Theme.Colors.accentAction)
                                                    }
                                                    Text(channel.name)
                                                        .font(.system(size: 13, weight: .bold))
                                                        .foregroundStyle(Theme.Colors.textPrimary)
                                                        .lineLimit(1)
                                                }

                                                Image(systemName: "play.circle.fill")
                                                    .font(.system(size: 18))
                                                    .foregroundStyle(Theme.Colors.accentAction)
                                                    .padding(.leading, 4)
                                            }
                                            .padding(.horizontal, 12)
                                            .padding(.vertical, 8)
                                            .background(Theme.Colors.surfaceElevated, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                                            .overlay(
                                                RoundedRectangle(cornerRadius: 12, style: .continuous)
                                                    .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8)
                                            )
                                        }
                                        .buttonStyle(.plain)
                                    }
                                }
                                .padding(.horizontal, 16)
                            }
                        }
                    }

                    // MARK: - 2. JETZT LIVE AUF SENDUNG
                    if !result.liveShows.isEmpty {
                        VStack(alignment: .leading, spacing: 12) {
                            sectionHeader(
                                title: "JETZT LIVE",
                                icon: "dot.radiowaves.left.and.right",
                                count: result.liveShows.count,
                                color: Theme.Colors.accentLive
                            )

                            VStack(spacing: 10) {
                                ForEach(result.liveShows) { item in
                                    LiveSearchResultCard(
                                        item: item,
                                        onPlay: {
                                            triggerHaptic(.medium)
                                            onPlayChannel(item.channel)
                                        },
                                        onOpenDetail: {
                                            onOpenShowDetail(item.channel, item.entry)
                                        },
                                        onRecord: {
                                            onRecordShow(item.channel, item.entry)
                                        }
                                    )
                                }
                            }
                            .padding(.horizontal, 16)
                        }
                    }

                    // MARK: - 3. DEMNÄCHST IM PROGRAMM
                    if !result.upcomingShows.isEmpty {
                        VStack(alignment: .leading, spacing: 12) {
                            sectionHeader(
                                title: "DEMNÄCHST IM PROGRAMM",
                                icon: "calendar.badge.clock",
                                count: result.upcomingShows.count,
                                color: Theme.Colors.accentAction
                            )

                            VStack(spacing: 10) {
                                ForEach(result.upcomingShows) { item in
                                    UpcomingSearchResultCard(
                                        item: item,
                                        onOpenDetail: {
                                            onOpenShowDetail(item.channel, item.entry)
                                        },
                                        onRecord: {
                                            onRecordShow(item.channel, item.entry)
                                        }
                                    )
                                }
                            }
                            .padding(.horizontal, 16)
                        }
                    }
                }
                .padding(.vertical, 14)
                .safeAreaPadding(.bottom, 80)
            }
        }
    }

    private func sectionHeader(title: String, icon: String, count: Int, color: Color) -> some View {
        HStack(spacing: 6) {
            Image(systemName: icon)
                .font(.system(size: 11, weight: .bold))
                .foregroundStyle(color)

            Text(title)
                .font(.system(size: 11, weight: .bold, design: .monospaced))
                .foregroundStyle(Theme.Colors.textSecondary)

            Spacer()

            Text("\(count)")
                .font(.system(size: 10, weight: .bold, design: .monospaced))
                .foregroundStyle(color)
                .padding(.horizontal, 7)
                .padding(.vertical, 2)
                .background(color.opacity(0.15), in: Capsule())
        }
        .padding(.horizontal, 16)
    }

    private func triggerHaptic(_ style: UIImpactFeedbackGenerator.FeedbackStyle) {
        Haptics.shared.impact(style)
    }
}

// MARK: - Live Search Result Card

private struct LiveSearchResultCard: View {
    let item: SmartSearchShowItem
    var onPlay: () -> Void
    var onOpenDetail: () -> Void
    var onRecord: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            // Header: Channel logo + channel name + LIVE badge
            HStack(spacing: 8) {
                ChannelLogo(url: item.channel.logoURL, name: item.channel.name, size: 26)

                Text(item.channel.name)
                    .font(.system(size: 13, weight: .bold))
                    .foregroundStyle(Theme.Colors.textSecondary)

                Spacer()

                HStack(spacing: 4) {
                    PulsingLiveDot(size: 6)
                    Text("LIVE")
                        .font(.system(size: 10, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.accentLive)
                }
                .padding(.horizontal, 7)
                .padding(.vertical, 3)
                .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())
            }

            // Title & description
            VStack(alignment: .leading, spacing: 3) {
                Text(item.entry.title)
                    .font(.system(size: 16, weight: .bold))
                    .foregroundStyle(Theme.Colors.textPrimary)
                    .lineLimit(2)

                if let desc = item.entry.description, !desc.isEmpty {
                    Text(desc)
                        .font(.system(size: 12))
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .lineLimit(2)
                }
            }

            // Scrubber / Progress
            if let progress = item.progress {
                VStack(spacing: 4) {
                    GuideProgressBar(progress: progress, height: 3)

                    HStack {
                        Text(item.entry.formattedTimeRange)
                            .font(.system(size: 10, weight: .medium, design: .monospaced))
                            .foregroundStyle(Theme.Colors.textTertiary)

                        Spacer()

                        if let rem = item.remainingMinutes {
                            Text("noch \(rem) Min")
                                .font(.system(size: 10, weight: .semibold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentLive)
                        }
                    }
                }
            }

            // Action Buttons
            HStack(spacing: 8) {
                Button(action: onPlay) {
                    HStack(spacing: 5) {
                        Image(systemName: "play.fill")
                            .font(.system(size: 11, weight: .bold))
                        Text("Live ansehen")
                            .font(.system(size: 13, weight: .bold))
                    }
                    .padding(.horizontal, 14)
                    .padding(.vertical, 7)
                    .background(Theme.Colors.accentAction, in: Capsule())
                    .foregroundStyle(.white)
                }
                .buttonStyle(.plain)

                Button(action: onRecord) {
                    HStack(spacing: 4) {
                        Image(systemName: "record.circle")
                            .font(.system(size: 12))
                        Text("Aufnehmen")
                            .font(.system(size: 12, weight: .medium))
                    }
                    .padding(.horizontal, 11)
                    .padding(.vertical, 7)
                    .background(Theme.Colors.surfaceElevated, in: Capsule())
                    .foregroundStyle(Theme.Colors.textPrimary)
                    .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                }
                .buttonStyle(.plain)

                Spacer()

                Button(action: onOpenDetail) {
                    Image(systemName: "info.circle")
                        .font(.system(size: 15))
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .padding(6)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(14)
        .background(
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .fill(Theme.Colors.surfaceElevated.opacity(0.9))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .strokeBorder(Theme.Colors.accentLive.opacity(0.35), lineWidth: 1)
        )
    }
}

// MARK: - Upcoming Search Result Card

private struct UpcomingSearchResultCard: View {
    let item: SmartSearchShowItem
    var onOpenDetail: () -> Void
    var onRecord: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            ChannelLogo(url: item.channel.logoURL, name: item.channel.name, size: 36)
                .padding(.top, 2)

            VStack(alignment: .leading, spacing: 4) {
                // Time & Channel Header
                HStack(spacing: 6) {
                    Text(item.formattedBadge)
                        .font(.system(size: 11, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.accentAction)
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(Theme.Colors.accentAction.opacity(0.12), in: RoundedRectangle(cornerRadius: 4))

                    Text("•")
                        .foregroundStyle(Theme.Colors.textDisabled)

                    Text(item.channel.name)
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .lineLimit(1)

                    Spacer()
                }

                // Show Title
                Text(item.entry.title)
                    .font(.system(size: 15, weight: .bold))
                    .foregroundStyle(Theme.Colors.textPrimary)
                    .lineLimit(2)

                if let desc = item.entry.description, !desc.isEmpty {
                    Text(desc)
                        .font(.system(size: 11))
                        .foregroundStyle(Theme.Colors.textTertiary)
                        .lineLimit(1)
                }
            }

            Spacer(minLength: 4)

            // Right Actions: Record & Info
            VStack(spacing: 6) {
                Button(action: onRecord) {
                    HStack(spacing: 4) {
                        Image(systemName: "record.circle")
                            .font(.system(size: 12, weight: .bold))
                        Text("Timer")
                            .font(.system(size: 11, weight: .bold))
                    }
                    .padding(.horizontal, 9)
                    .padding(.vertical, 6)
                    .background(Color.red.opacity(0.18), in: Capsule())
                    .foregroundStyle(.red)
                    .overlay(Capsule().strokeBorder(Color.red.opacity(0.4), lineWidth: 0.8))
                }
                .buttonStyle(.plain)

                Button(action: onOpenDetail) {
                    Image(systemName: "info.circle")
                        .font(.system(size: 14))
                        .foregroundStyle(Theme.Colors.textTertiary)
                        .padding(4)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(12)
        .background(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .fill(Theme.Colors.surfaceElevated.opacity(0.65))
        )
        .overlay(
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8)
        )
    }
}
