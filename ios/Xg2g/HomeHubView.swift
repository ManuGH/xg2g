// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Modern "Für dich / Home" Hub: The intuitive, cinematic entry screen for daily TV viewing.
///
/// Combines the top live highlights, recent channels, 20:15 Prime Time preview, and DVR recordings
/// in a clean, glanceable layout that anyone can use effortlessly.
struct HomeHubView: View {

    @Bindable var model: AppModel
    @State private var selectedDetail: ProgramDetailPayload?
    @State private var recordConfirmationMessage: String?
    @State private var searchText = ""

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                if !searchText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    let searchResult = SmartSearchEngine.search(
                        query: searchText,
                        channels: model.channels,
                        schedule: model.schedule,
                        fullEpg: model.fullEpg,
                        now: Date.now
                    )
                    SmartSearchResultsView(
                        result: searchResult,
                        onPlayChannel: { channel in
                            Haptics.shared.impact(.light)
                            model.playingChannel = channel
                        },
                        onOpenShowDetail: { channel, entry in
                            selectedDetail = ProgramDetailPayload(channel: channel, entry: entry)
                        },
                        onRecordShow: { channel, entry in
                            scheduleTimer(channel: channel, entry: entry)
                        }
                    )
                } else {
                    mainScrollContent
                }

                // Toast Banner
                if let message = recordConfirmationMessage {
                    VStack {
                        Spacer()
                        HStack(spacing: 8) {
                            Image(systemName: "checkmark.circle.fill")
                                .foregroundStyle(Theme.Colors.statusSuccess)
                            Text(message)
                                .font(.subheadline.weight(.semibold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                        }
                        .padding(.horizontal, 16)
                        .padding(.vertical, 10)
                        .background(Theme.Colors.surfaceElevated, in: Capsule())
                        .overlay(Capsule().strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1))
                        .shadow(color: .black.opacity(0.35), radius: 10, y: 5)
                        .padding(.bottom, 20)
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                    }
                }
            }
            .navigationTitle("Für dich")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    HStack(spacing: 6) {
                        Image(systemName: "sparkles.tv")
                            .font(.system(size: 15, weight: .semibold))
                            .foregroundStyle(Theme.Colors.accentLive)
                        Text("xg2g TV")
                            .font(.headline.weight(.bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                    }
                }

                ToolbarItem(placement: .topBarTrailing) {
                    HStack(spacing: 8) {
                        // Playback Engine Badge
                        Text(model.playbackEngine == .native ? "NATIVE TS" : "HLS")
                            .font(.system(size: 9, weight: .bold, design: .monospaced))
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2.5)
                            .background(
                                model.playbackEngine == .native
                                    ? Theme.Colors.accentAction.opacity(0.2)
                                    : Theme.Colors.accentLive.opacity(0.2),
                                in: Capsule()
                            )
                            .foregroundStyle(
                                model.playbackEngine == .native
                                    ? Theme.Colors.accentAction
                                    : Theme.Colors.accentLive
                            )
                    }
                }
            }
            .searchable(text: $searchText, prompt: "Sendung, Film oder Sender suchen…")
            .sheet(item: $selectedDetail) { payload in
                ProgramDetailSheet(
                    channel: payload.channel,
                    entry: payload.entry,
                    channelSchedule: model.channelSchedule(for: payload.channel),
                    model: model,
                    onRecord: { entry in scheduleTimer(channel: payload.channel, entry: entry) }
                )
            }
            .refreshable {
                await model.refreshLiveContent()
            }
        }
    }

    private var mainScrollContent: some View {
        ScrollView {
            VStack(spacing: 22) {
                // 1. Spotlight Hero Banner
                if let hero = spotlightItem {
                    GoogleSpotlightHero(
                        channel: hero.channel,
                        entry: hero.entry,
                        model: model,
                        onPlay: {
                            Haptics.shared.impact(.medium)
                            model.playingChannel = hero.channel
                        },
                        onShowInfo: {
                            selectedDetail = ProgramDetailPayload(channel: hero.channel, entry: hero.entry)
                        },
                        onRecord: {
                            scheduleTimer(channel: hero.channel, entry: hero.entry)
                        }
                    )
                    .padding(.horizontal, 16)
                }

                // 2. Zuletzt geschaut (Quick Zap Rail)
                if !model.recentChannels.isEmpty {
                    RecentlyWatchedRail(
                        channels: model.recentChannels,
                        model: model,
                        onPlay: { channel in
                            Haptics.shared.impact(.light)
                            model.playingChannel = channel
                        },
                        onShowInfo: { channel, entry in
                            selectedDetail = ProgramDetailPayload(channel: channel, entry: entry)
                        }
                    )
                    .padding(.horizontal, 16)
                }

                // 3. Favoriten Quick Access Rail
                if !model.favoriteChannels.isEmpty {
                    FavoritesQuickRail(
                        channels: model.favoriteChannels,
                        model: model,
                        onPlay: { channel in
                            Haptics.shared.impact(.light)
                            model.playingChannel = channel
                        },
                        onShowInfo: { channel, entry in
                            selectedDetail = ProgramDetailPayload(channel: channel, entry: entry)
                        }
                    )
                    .padding(.horizontal, 16)
                }

                // 4. Prime Time Heute Abend (20:15 Uhr)
                primeTimeSection
                    .padding(.horizontal, 16)

                // 5. Neueste Aufnahmen (falls vorhanden)
                if !model.recordings.isEmpty {
                    recentRecordingsSection
                        .padding(.horizontal, 16)
                }
            }
            .padding(.top, 12)
            .safeAreaPadding(.bottom, 80)
        }
    }

    // MARK: - Prime Time Highlights Section (20:15 Uhr)

    private var primeTimeSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 6) {
                Image(systemName: "moon.stars.fill")
                    .font(.system(size: 12, weight: .bold))
                    .foregroundStyle(Theme.Colors.accentAction)
                Text("HEUTE 20:15 UHR")
                    .font(.system(size: 12, weight: .bold, design: .monospaced))
                    .foregroundStyle(Theme.Colors.textSecondary)

                Spacer()

                Text("Prime Time")
                    .font(.system(size: 11, weight: .medium))
                    .foregroundStyle(Theme.Colors.textTertiary)
            }

            let primeItems = primeTimePicks
            if primeItems.isEmpty {
                Text("Lade Prime-Time-Programm…")
                    .font(.footnote)
                    .foregroundStyle(Theme.Colors.textTertiary)
                    .padding(.vertical, 8)
            } else {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 12) {
                        ForEach(primeItems, id: \.entry.id) { pick in
                            HomePrimeTimeCard(pick: pick) {
                                Haptics.shared.impact(.light)
                                selectedDetail = ProgramDetailPayload(channel: pick.channel, entry: pick.entry)
                            } onRecord: {
                                scheduleTimer(channel: pick.channel, entry: pick.entry)
                            }
                        }
                    }
                }
            }
        }
    }

    // MARK: - Recent Recordings Section

    private var recentRecordingsSection: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 6) {
                Image(systemName: "play.rectangle.on.rectangle.fill")
                    .font(.system(size: 12, weight: .bold))
                    .foregroundStyle(Theme.Colors.accentAction)
                Text("AUFNAHMEN")
                    .font(.system(size: 12, weight: .bold, design: .monospaced))
                    .foregroundStyle(Theme.Colors.textSecondary)

                Spacer()

                Button {
                    model.selectedTab = .recordings
                } label: {
                    Text("Alle anzeigen")
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundStyle(Theme.Colors.accentAction)
                }
            }

            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 12) {
                    ForEach(Array(model.recordings.prefix(6))) { recording in
                        HomeRecordingCard(recording: recording) {
                            Haptics.shared.impact(.light)
                            model.playbackManager.play(recording: recording, startPosition: 0)
                        }
                    }
                }
            }
        }
    }

    // MARK: - Helpers

    private var spotlightItem: (channel: Channel, entry: NowNext.Entry)? {
        let pool = model.favoriteChannels.isEmpty ? Array(model.channels.prefix(10)) : model.favoriteChannels
        for channel in pool {
            if let show = model.show(for: channel, at: .now) {
                return (channel, show)
            }
        }
        return nil
    }

    private var primeTimePicks: [(channel: Channel, entry: NowNext.Entry)] {
        var picks: [(channel: Channel, entry: NowNext.Entry)] = []
        let pool = model.favoriteChannels.isEmpty ? Array(model.channels.prefix(12)) : model.favoriteChannels
        for channel in pool {
            if let show = model.show(for: channel, at: .primeTimeTonight) {
                picks.append((channel, show))
            }
            if picks.count >= 8 { break }
        }
        return picks
    }

    private func scheduleTimer(channel: Channel, entry: NowNext.Entry) {
        Task {
            let ok = await model.scheduleProgramTimer(channel: channel, entry: entry)
            if ok {
                Haptics.shared.impact(.medium)
                withAnimation {
                    recordConfirmationMessage = "„\(entry.title)“ programmiert"
                }
                try? await Task.sleep(for: .seconds(3))
                withAnimation {
                    recordConfirmationMessage = nil
                }
            }
        }
    }
}

// MARK: - Subviews

private struct HomePrimeTimeCard: View {
    let pick: (channel: Channel, entry: NowNext.Entry)
    let onSelect: () -> Void
    let onRecord: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                ChannelLogo(url: pick.channel.logoURL, name: pick.channel.name, size: 30)
                VStack(alignment: .leading, spacing: 1) {
                    Text(pick.channel.name)
                        .font(.system(size: 12, weight: .bold))
                        .foregroundStyle(Theme.Colors.textPrimary)
                        .lineLimit(1)
                    Text("20:15 Uhr")
                        .font(.system(size: 10, weight: .semibold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.accentAction)
                }
                Spacer()
            }

            Text(pick.entry.title)
                .font(.system(size: 13, weight: .bold))
                .foregroundStyle(Theme.Colors.textPrimary)
                .lineLimit(2)
                .frame(height: 34, alignment: .topLeading)

            if let desc = pick.entry.description, !desc.isEmpty {
                Text(desc)
                    .font(.system(size: 11))
                    .foregroundStyle(Theme.Colors.textTertiary)
                    .lineLimit(2)
                    .frame(height: 28, alignment: .topLeading)
            } else {
                Spacer().frame(height: 28)
            }

            Button(action: onRecord) {
                HStack(spacing: 5) {
                    Image(systemName: "record.circle")
                        .font(.system(size: 11, weight: .bold))
                    Text("Aufnehmen")
                        .font(.system(size: 11, weight: .bold))
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 6)
                .background(Theme.Colors.accentAction.opacity(0.15), in: Capsule())
                .foregroundStyle(Theme.Colors.accentAction)
            }
            .buttonStyle(.plain)
        }
        .padding(12)
        .frame(width: 200)
        .background(Theme.Gradients.cardSurface, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8)
        )
        .contentShape(Rectangle())
        .onTapGesture {
            onSelect()
        }
    }
}

private struct HomeRecordingCard: View {
    let recording: Recording
    let onPlay: () -> Void

    var body: some View {
        Button(action: onPlay) {
            VStack(alignment: .leading, spacing: 6) {
                HStack {
                    Text(recording.formattedDuration)
                        .font(.system(size: 10, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.accentAction)
                        .padding(.horizontal, 5)
                        .padding(.vertical, 2)
                        .background(Theme.Colors.accentAction.opacity(0.15), in: RoundedRectangle(cornerRadius: 4))
                    Spacer()
                    Image(systemName: "play.circle.fill")
                        .font(.system(size: 16))
                        .foregroundStyle(Theme.Colors.accentAction)
                }

                Text(recording.title)
                    .font(.system(size: 13, weight: .bold))
                    .foregroundStyle(Theme.Colors.textPrimary)
                    .lineLimit(2)
                    .frame(height: 34, alignment: .topLeading)

                Text(recording.formattedDate)
                    .font(.system(size: 10, weight: .medium, design: .monospaced))
                    .foregroundStyle(Theme.Colors.textTertiary)
            }
            .padding(12)
            .frame(width: 170)
            .background(Theme.Gradients.cardSurface, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            .overlay(
                RoundedRectangle(cornerRadius: 14, style: .continuous)
                    .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8)
            )
        }
        .buttonStyle(.plain)
    }
}
