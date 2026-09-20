// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Television Electronic Programme Guide (EPG).
///
/// Designed for 10-foot distance with Siri Remote focus navigation:
/// Replaces the mobile 3-mode segmented picker with a clear, readable TV schedule.
/// Each channel row shows the current broadcast with live progress line and
/// the upcoming program. Focus illuminates the row; pressing Select plays live TV.
struct TVGuideView: View {

    @Bindable var model: AppModel

    enum TVGuideFilter: String, CaseIterable, Identifiable {
        case now = "Jetzt & Danach"
        case primeTime = "Heute 20:15"
        case lateNight = "Heute 22:00"

        var id: String { rawValue }
    }

    @State private var activeFilter: TVGuideFilter = .now
    @State private var toastMessage: String?

    private var channels: [Channel] {
        model.selectedBouquet?.id == AppModel.favoritesBouquetID
            ? model.favoriteChannels
            : model.channels
    }

    private func showFor(_ channel: Channel) -> (current: NowNext.Entry?, upcoming: NowNext.Entry?) {
        switch activeFilter {
        case .now:
            let nowEntry = model.show(for: channel, at: .now)
            let nextEntry = model.show(for: channel, at: .next)
            return (nowEntry, nextEntry)
        case .primeTime:
            let primeEntry = model.show(for: channel, at: .primeTimeTonight)
            return (primeEntry, nil)
        case .lateNight:
            let lateEntry = model.show(for: channel, at: .lateNightTonight)
            return (lateEntry, nil)
        }
    }

    var body: some View {
        ZStack {
            Theme.Colors.bgBase.ignoresSafeArea()

            if channels.isEmpty {
                ContentUnavailableView(
                    model.isLoadingChannels ? "Lade Fernsehprogramm…" : "Keine Sender verfügbar",
                    systemImage: "tv",
                    description: Text("Sender und EPG-Daten werden geladen.")
                )
                .foregroundStyle(Theme.Colors.textSecondary)
            } else {
                ScrollView(.vertical, showsIndicators: false) {
                    VStack(alignment: .leading, spacing: 20) {
                        // Header
                        HStack(alignment: .firstTextBaseline) {
                            Text(model.selectedBouquet?.name ?? "Programm")
                                .font(TVDesign.Font.heading)
                                .foregroundStyle(Theme.Colors.textPrimary)

                            Text("\(channels.count) Sender")
                                .font(TVDesign.Font.meta)
                                .foregroundStyle(Theme.Colors.textSecondary)

                            Spacer()

                            // Time Filter Anchors
                            HStack(spacing: 12) {
                                ForEach(TVGuideFilter.allCases) { filter in
                                    let isSelected = activeFilter == filter
                                    Button {
                                        withAnimation(.easeInOut(duration: 0.2)) {
                                            activeFilter = filter
                                        }
                                    } label: {
                                        Text(filter.rawValue)
                                            .font(TVDesign.Font.meta)
                                            .padding(.horizontal, 16)
                                            .padding(.vertical, 8)
                                    }
                                    .buttonStyle(TVFilterChipButtonStyle(isSelected: isSelected))
                                }
                            }
                        }
                        .padding(.horizontal, 48)
                        .padding(.top, 24)

                        // Channel EPG Rows
                        LazyVStack(spacing: 16) {
                            ForEach(channels) { channel in
                                let shows = showFor(channel)
                                TVGuideChannelRow(
                                    channel: channel,
                                    currentEntry: shows.current,
                                    upcomingEntry: shows.upcoming,
                                    isLiveMode: activeFilter == .now,
                                    isFavorite: model.isFavorite(channel),
                                    onPlay: {
                                        model.playingChannel = channel
                                    },
                                    onRecord: { entry in
                                        scheduleRecord(entry, on: channel)
                                    },
                                    onToggleFavorite: {
                                        model.toggleFavorite(channel)
                                    }
                                )
                            }
                        }
                        .padding(.horizontal, 48)
                        .padding(.bottom, 40)
                    }
                }
            }

            if let toast = toastMessage {
                VStack {
                    Spacer()
                    HStack(spacing: 10) {
                        Image(systemName: "checkmark.circle.fill")
                            .foregroundStyle(Theme.Colors.statusSuccess)
                        Text(toast)
                            .font(TVDesign.Font.body)
                            .foregroundStyle(Theme.Colors.textPrimary)
                    }
                    .padding(.horizontal, 24)
                    .padding(.vertical, 14)
                    .background(.ultraThinMaterial, in: Capsule())
                    .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1))
                    .shadow(color: .black.opacity(0.4), radius: 12, y: 6)
                    .padding(.bottom, 48)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                }
            }
        }
        .onExitCommand {
            model.selectedTab = .home
        }
        .task {
            if model.channels.isEmpty {
                await model.loadChannels()
            } else if let last = model.lastDataRefreshTime,
                      Date().timeIntervalSince(last) > 300 {
                await model.refreshLiveContent()
            }
        }
    }

    private func scheduleRecord(_ entry: NowNext.Entry, on channel: Channel) {
        Task {
            let success = await model.scheduleProgramTimer(channel: channel, entry: entry)
            if success {
                withAnimation {
                    toastMessage = "„\(entry.title)“ programmiert"
                }
                try? await Task.sleep(for: .seconds(3))
                withAnimation {
                    toastMessage = nil
                }
            }
        }
    }
}

// MARK: - Row Component

struct TVGuideChannelRow: View {
    let channel: Channel
    let currentEntry: NowNext.Entry?
    let upcomingEntry: NowNext.Entry?
    let isLiveMode: Bool
    let isFavorite: Bool
    var onPlay: () -> Void
    var onRecord: (NowNext.Entry) -> Void
    var onToggleFavorite: () -> Void

    var body: some View {
        Button(action: onPlay) {
            HStack(spacing: 24) {
                // 1. Channel Identity Column
                HStack(spacing: 16) {
                    if let number = channel.number {
                        Text(number)
                            .font(TVDesign.Font.meta)
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .frame(width: 44, alignment: .trailing)
                    } else {
                        Spacer().frame(width: 44)
                    }

                    ChannelLogo(url: channel.logoURL, name: channel.name, size: 54)

                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 6) {
                            Text(channel.name)
                                .font(TVDesign.Font.body)
                                .foregroundStyle(Theme.Colors.textPrimary)
                                .lineLimit(1)
                            if isFavorite {
                                Image(systemName: "star.fill")
                                    .font(.caption)
                                    .foregroundStyle(Theme.Colors.statusWarning)
                            }
                        }
                    }
                    .frame(width: 200, alignment: .leading)
                }

                Divider()
                    .frame(height: 50)
                    .background(Theme.Colors.borderSubtle)

                // 2. Current Broadcast (Jetzt / PrimeTime)
                VStack(alignment: .leading, spacing: 6) {
                    if let entry = currentEntry {
                        HStack(spacing: 8) {
                            if isLiveMode {
                                Circle()
                                    .fill(Theme.Colors.accentLive)
                                    .frame(width: 6, height: 6)
                            }
                            Text("\(entry.formattedStartTime) – \(entry.formattedEndTime)")
                                .font(TVDesign.Font.meta)
                                .foregroundStyle(isLiveMode ? Theme.Colors.accentLive : Theme.Colors.textSecondary)

                            if let remaining = entry.remainingMinutes(at: .now), isLiveMode {
                                Text("• noch \(remaining) Min")
                                    .font(TVDesign.Font.meta)
                                    .foregroundStyle(Theme.Colors.textSecondary)
                            }
                        }

                        Text(entry.title)
                            .font(TVDesign.Font.body)
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(1)

                        if let desc = entry.description, !desc.isEmpty {
                            Text(desc)
                                .font(TVDesign.Font.meta)
                                .foregroundStyle(Theme.Colors.textSecondary)
                                .lineLimit(1)
                        }

                        if isLiveMode, let progress = entry.progress(at: .now) {
                            TVProgressLine(progress: progress)
                                .padding(.top, 2)
                        }
                    } else {
                        Text("Keine Programminformationen")
                            .font(TVDesign.Font.meta)
                            .foregroundStyle(Theme.Colors.textSecondary)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)

                // 3. Upcoming Broadcast (Danach)
                if isLiveMode, let upcoming = upcomingEntry {
                    Divider()
                        .frame(height: 50)
                        .background(Theme.Colors.borderSubtle)

                    VStack(alignment: .leading, spacing: 4) {
                        Text("Danach ab \(upcoming.formattedStartTime)")
                            .font(TVDesign.Font.meta)
                            .foregroundStyle(Theme.Colors.textSecondary)

                        Text(upcoming.title)
                            .font(TVDesign.Font.body)
                            .foregroundStyle(Theme.Colors.textPrimary.opacity(0.85))
                            .lineLimit(1)
                    }
                    .frame(width: 360, alignment: .leading)
                }
            }
            .padding(.horizontal, 24)
            .padding(.vertical, 16)
        }
        .buttonStyle(TVGuideRowButtonStyle())
        .contextMenu {
            Button("Live schauen", systemImage: "play.fill", action: onPlay)
            if let entry = currentEntry {
                Button("Sendung aufnehmen", systemImage: "record.circle") {
                    onRecord(entry)
                }
            }
            if let upcoming = upcomingEntry {
                Button("„\(upcoming.title)“ aufnehmen", systemImage: "record.circle") {
                    onRecord(upcoming)
                }
            }
            Button(
                isFavorite ? "Aus Favoriten entfernen" : "Zu Favoriten hinzufügen",
                systemImage: isFavorite ? "star.slash" : "star",
                action: onToggleFavorite
            )
        }
    }
}

// MARK: - Button Styles

struct TVGuideRowButtonStyle: ButtonStyle {
    @Environment(\.isFocused) private var isFocused

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(
                RoundedRectangle(cornerRadius: 16, style: .continuous)
                    .fill(isFocused ? Theme.Colors.surfaceElevated : Theme.Colors.surfaceElevated.opacity(0.5))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 16, style: .continuous)
                    .strokeBorder(isFocused ? Theme.Colors.accentAction : Theme.Colors.borderSubtle, lineWidth: isFocused ? 3 : 1)
            )
            .scaleEffect(isFocused ? 1.02 : 1.0)
            .shadow(color: .black.opacity(isFocused ? 0.45 : 0), radius: 16, y: 8)
            .animation(.easeOut(duration: 0.18), value: isFocused)
    }
}

struct TVFilterChipButtonStyle: ButtonStyle {
    let isSelected: Bool
    @Environment(\.isFocused) private var isFocused

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(
                Capsule()
                    .fill(isSelected ? Theme.Colors.accentAction : (isFocused ? Theme.Colors.surfaceElevated : Theme.Colors.surfaceElevated.opacity(0.6)))
            )
            .overlay(
                Capsule()
                    .strokeBorder(isFocused ? Color.white : Color.clear, lineWidth: isFocused ? 2 : 0)
            )
            .scaleEffect(isFocused ? 1.06 : 1.0)
            .foregroundStyle(isSelected ? Color.white : (isFocused ? Color.white : Theme.Colors.textSecondary))
            .animation(.easeOut(duration: 0.15), value: isFocused)
    }
}
