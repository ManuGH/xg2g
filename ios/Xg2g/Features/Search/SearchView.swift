// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Search as a destination of its own.
///
/// iPhone and iPad keep the search field in the Home hub's navigation bar. On
/// tvOS that field would take first focus and unfold the inline keyboard over
/// the hub, so search is a tab here, the way the system apps do it. The view
/// is the hub's search branch lifted out: same engine, same result list, same
/// actions.
struct SearchView: View {

    @Bindable var model: AppModel
    @State private var searchText = ""
    @State private var selectedDetail: ProgramDetailPayload?
    @State private var recordConfirmationMessage: String?

    private var trimmedQuery: String {
        searchText.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                if trimmedQuery.isEmpty {
                    searchDiscoveryView
                } else {
                    SmartSearchResultsView(
                        result: SmartSearchEngine.search(
                            query: searchText,
                            channels: model.channels,
                            schedule: model.schedule,
                            fullEpg: model.fullEpg,
                            now: Date.now
                        ),
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
                }

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
                        .padding(.bottom, 20)
                        .transition(.move(edge: .bottom).combined(with: .opacity))
                    }
                }
            }
            .navigationTitle(Tab.search.rawValue)
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
#if os(tvOS)
            .onExitCommand {
                model.selectedTab = .home
            }
#endif
        }
    }

    private func scheduleTimer(channel: Channel, entry: NowNext.Entry) {
        Task {
            let ok = await model.scheduleProgramTimer(channel: channel, entry: entry)
            if ok {
                Haptics.shared.impact(.medium)
            } else {
                Haptics.shared.notification(.error)
            }
            withAnimation {
                recordConfirmationMessage = ok ? "„\(entry.title)“ programmiert" : "Aufnahme fehlgeschlagen: \(model.lastError ?? "Receiver beschäftigt")"
            }
            try? await Task.sleep(for: .seconds(3))
            withAnimation {
                recordConfirmationMessage = nil
            }
        }
    }

    // MARK: - Discovery Content for Empty Search State

    private var searchDiscoveryView: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                // 1. Kategorien & Genres zum Entdecken
                VStack(alignment: .leading, spacing: 12) {
                    HStack(spacing: 6) {
                        Image(systemName: "sparkles")
                            .font(.app(size: 14, weight: .bold))
                            .foregroundStyle(Theme.Colors.accentAction)
                        Text("Kategorien entdecken")
                            .font(.headline.weight(.bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                    }

                    LazyVGrid(
                        columns: [GridItem(.adaptive(minimum: 145, maximum: 220), spacing: 12)],
                        spacing: 12
                    ) {
                        ForEach(discoveryGenres, id: \.title) { item in
                            Button {
                                Haptics.shared.impact(.light)
                                searchText = item.title
                            } label: {
                                HStack(spacing: 10) {
                                    Image(systemName: item.icon)
                                        .font(.app(size: 16, weight: .semibold))
                                        .foregroundStyle(item.color)
                                        .frame(width: 32, height: 32)
                                        .background(item.color.opacity(0.15), in: Circle())

                                    Text(item.title)
                                        .font(.app(size: 13, weight: .semibold))
                                        .foregroundStyle(Theme.Colors.textPrimary)

                                    Spacer()
                                }
                                .padding(.horizontal, 12)
                                .padding(.vertical, 10)
                                .background(Theme.Gradients.cardSurface, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                                .overlay(
                                    RoundedRectangle(cornerRadius: 12, style: .continuous)
                                        .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8)
                                )
                            }
                            .buttonStyle(.plain)
                            .contentShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                            .appHoverEffect(.highlight)
                        }
                    }
                }

                // 2. Beliebte Sender / Favoriten
                let topChannels = model.favoriteChannels.isEmpty ? Array(model.channels.prefix(6)) : model.favoriteChannels
                if !topChannels.isEmpty {
                    VStack(alignment: .leading, spacing: 12) {
                        HStack(spacing: 6) {
                            Image(systemName: "tv.fill")
                                .font(.app(size: 14, weight: .bold))
                                .foregroundStyle(Theme.Colors.accentLive)
                            Text(model.favoriteChannels.isEmpty ? "Beliebte Sender" : "Deine Favoriten")
                                .font(.headline.weight(.bold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                        }

                        ScrollView(.horizontal, showsIndicators: false) {
                            HStack(spacing: 12) {
                                ForEach(topChannels) { channel in
                                    let nowTitle = model.schedule[channel.serviceRef]?.now?.title ?? "Live TV"
                                    Button {
                                        Haptics.shared.impact(.light)
                                        model.playingChannel = channel
                                    } label: {
                                        HStack(spacing: 10) {
                                            ChannelLogo(url: channel.logoURL, name: channel.name, size: 36)
                                            VStack(alignment: .leading, spacing: 2) {
                                                Text(channel.name)
                                                    .font(.app(size: 14, weight: .bold))
                                                    .foregroundStyle(Theme.Colors.textPrimary)
                                                    .lineLimit(1)
                                                Text(nowTitle)
                                                    .font(.app(size: 11, weight: .medium))
                                                    .foregroundStyle(Theme.Colors.textTertiary)
                                                    .lineLimit(1)
                                            }
                                            Image(systemName: "play.circle.fill")
                                                .font(.app(size: 18))
                                                .foregroundStyle(Theme.Colors.accentLive)
                                                .padding(.leading, 2)
                                        }
                                        .padding(.horizontal, 12)
                                        .padding(.vertical, 8)
                                        .background(Theme.Gradients.cardSurface, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                                        .overlay(
                                            RoundedRectangle(cornerRadius: 12, style: .continuous)
                                                .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8)
                                        )
                                    }
                                    .buttonStyle(.plain)
                                    .contentShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                                    .appHoverEffect(.highlight)
                                }
                            }
                        }
                    }
                }
            }
            .padding(16)
        }
    }

    private struct DiscoveryGenre {
        let title: String
        let icon: String
        let color: Color
    }

    private var discoveryGenres: [DiscoveryGenre] {
        [
            DiscoveryGenre(title: "Spielfilme", icon: "film", color: Color.purple),
            DiscoveryGenre(title: "Serien", icon: "tv", color: Color.blue),
            DiscoveryGenre(title: "Sport", icon: "figure.run", color: Color.green),
            DiscoveryGenre(title: "Dokus", icon: "globe.europe.africa.fill", color: Color.teal),
            DiscoveryGenre(title: "Nachrichten", icon: "newspaper", color: Color.orange),
            DiscoveryGenre(title: "Kinder", icon: "balloon.2.fill", color: Color.pink)
        ]
    }
}
