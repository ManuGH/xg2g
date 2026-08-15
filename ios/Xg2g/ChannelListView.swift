// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

struct ChannelListView: View {

    @Bindable var model: AppModel
    @State private var selected: Channel?

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                VStack(spacing: 0) {
                    if !model.bouquets.isEmpty {
                        BouquetPicker(model: model)
                            .padding(.top, 8)
                            .padding(.bottom, 4)
                            .background(Theme.Colors.surfaceElevated.opacity(0.5))
                    }

                    // Time Window Filter Pills (Jetzt vs 20:15)
                    HStack(spacing: 8) {
                        ForEach(AppModel.TimeFilter.allCases) { filter in
                            Button {
                                model.selectedTimeFilter = filter
                            } label: {
                                HStack(spacing: 5) {
                                    if filter == .now {
                                        PulsingLiveDot(size: 4)
                                    } else {
                                        Image(systemName: "moon.stars.fill")
                                            .font(.caption2)
                                            .foregroundStyle(model.selectedTimeFilter == filter ? Theme.Colors.textPrimary : Theme.Colors.accentLive)
                                    }
                                    Text(filter.rawValue)
                                        .font(.caption.weight(model.selectedTimeFilter == filter ? .semibold : .regular))
                                }
                                .padding(.horizontal, 10)
                                .padding(.vertical, 4)
                                .background(
                                    model.selectedTimeFilter == filter
                                        ? Theme.Colors.accentAction
                                        : Theme.Colors.surfaceGlass,
                                    in: Capsule()
                                )
                                .foregroundStyle(model.selectedTimeFilter == filter ? Theme.Colors.textPrimary : Theme.Colors.textSecondary)
                                .overlay(
                                    Capsule().strokeBorder(model.selectedTimeFilter == filter ? Color.clear : Theme.Colors.borderSubtle, lineWidth: 1)
                                )
                            }
                            .buttonStyle(.plain)
                        }
                        Spacer()
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 6)
                    .background(Theme.Colors.surfaceElevated.opacity(0.3))

                    Group {
                        if model.channels.isEmpty && model.isLoadingChannels {
                            Spacer()
                            ProgressView("Lade Senderliste…")
                                .tint(Theme.Colors.accentAction)
                                .foregroundStyle(Theme.Colors.textSecondary)
                            Spacer()
                        } else if model.filteredChannels.isEmpty {
                            Spacer()
                            ContentUnavailableView(
                                model.searchQuery.isEmpty ? "Keine Sender" : "Keine Treffer",
                                systemImage: "tv.slash",
                                description: Text(model.searchQuery.isEmpty ? (model.lastError ?? "Keine Sender für dieses Bouquet gefunden.") : "Kein Sender entspricht deiner Suche.")
                            )
                            .foregroundStyle(Theme.Colors.textSecondary)
                            Spacer()
                        } else {
                            List(model.filteredChannels) { channel in
                                Button {
                                    selected = channel
                                } label: {
                                    ChannelRow(
                                        channel: channel,
                                        nowNext: model.schedule[channel.serviceRef],
                                        timeFilter: model.selectedTimeFilter,
                                        isFavorite: model.isFavorite(channel)
                                    )
                                }
                                .buttonStyle(.plain)
                                .listRowBackground(Theme.Colors.surfaceElevated)
                                .listRowSeparatorTint(Theme.Colors.borderSubtle)
                                .swipeActions(edge: .leading, allowsFullSwipe: true) {
                                    Button {
                                        model.toggleFavorite(channel)
                                    } label: {
                                        Label(
                                            model.isFavorite(channel) ? "Entfernen" : "Favorit",
                                            systemImage: model.isFavorite(channel) ? "star.slash.fill" : "star.fill"
                                        )
                                    }
                                    .tint(Theme.Colors.accentLive)
                                }
                                .swipeActions(edge: .trailing, allowsFullSwipe: false) {
                                    Button {
                                        Task {
                                            _ = await model.recordLiveNow(channel: channel)
                                        }
                                    } label: {
                                        Label("Aufnehmen", systemImage: "record.circle")
                                    }
                                    .tint(Theme.Colors.statusError)
                                }
                                .contextMenu {
                                    if let now = model.schedule[channel.serviceRef]?.now {
                                        Button {
                                            Task { _ = await model.scheduleProgramTimer(channel: channel, entry: now) }
                                        } label: {
                                            Label("„\(now.title)“ aufnehmen", systemImage: "record.circle")
                                        }
                                    }

                                    if let next = model.schedule[channel.serviceRef]?.next {
                                        Button {
                                            Task { _ = await model.scheduleProgramTimer(channel: channel, entry: next) }
                                        } label: {
                                            Label("„\(next.title)“ programmieren", systemImage: "clock.badge.checkmark")
                                        }
                                    }

                                    Button {
                                        model.toggleFavorite(channel)
                                    } label: {
                                        Label(
                                            model.isFavorite(channel) ? "Aus Favoriten entfernen" : "Zu Favoriten hinzufügen",
                                            systemImage: model.isFavorite(channel) ? "star.slash" : "star"
                                        )
                                    }
                                }
                            }
                            .listStyle(.plain)
                            .scrollContentBackground(.hidden)
                            .refreshable {
                                await model.loadChannels(bouquet: model.selectedBouquet?.name)
                            }
                        }
                    }
                }
            }
            .navigationTitle("Live TV")
            .searchable(text: $model.searchQuery, prompt: "Sender oder Sendung suchen…")
            .toolbar {
                if model.isLoadingChannels && !model.channels.isEmpty {
                    ProgressView()
                        .tint(Theme.Colors.accentAction)
                }
            }
        }
        .fullScreenCover(item: $selected) { channel in
            PlayerScreen(model: model, channel: channel)
        }
    }
}

// MARK: - Bouquet Picker Pills

struct BouquetPicker: View {

    let model: AppModel

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                if !model.favoriteChannelIDs.isEmpty {
                    BouquetPill(
                        title: "Favoriten",
                        icon: "star.fill",
                        count: model.favoriteChannelIDs.count,
                        isSelected: model.selectedBouquet?.id == AppModel.favoritesBouquetID
                    ) {
                        model.selectedBouquet = Bouquet(
                            id: AppModel.favoritesBouquetID,
                            name: "Favoriten",
                            servicesCount: model.favoriteChannelIDs.count
                        )
                    }
                }

                BouquetPill(
                    title: "Alle Sender",
                    isSelected: model.selectedBouquet == nil
                ) {
                    Task { await model.selectBouquet(nil) }
                }

                ForEach(model.bouquets) { bouquet in
                    BouquetPill(
                        title: bouquet.name,
                        count: bouquet.servicesCount > 0 ? bouquet.servicesCount : nil,
                        isSelected: model.selectedBouquet?.id == bouquet.id
                    ) {
                        Task { await model.selectBouquet(bouquet) }
                    }
                }
            }
            .padding(.horizontal, 16)
        }
    }
}

struct BouquetPill: View {

    let title: String
    var icon: String? = nil
    var count: Int?
    let isSelected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                if let icon {
                    Image(systemName: icon)
                        .font(.caption2)
                        .foregroundStyle(isSelected ? Theme.Colors.textPrimary : Theme.Colors.accentLive)
                }

                Text(title)
                    .font(.subheadline.weight(isSelected ? .semibold : .regular))

                if let count {
                    Text("\(count)")
                        .font(.caption2.monospacedDigit())
                        .foregroundStyle(isSelected ? Theme.Colors.textPrimary.opacity(0.8) : Theme.Colors.textTertiary)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .background(
                isSelected ? Theme.Colors.accentAction : Theme.Colors.surfaceGlass,
                in: Capsule()
            )
            .foregroundStyle(isSelected ? Theme.Colors.textPrimary : Theme.Colors.textSecondary)
            .overlay(
                Capsule()
                    .strokeBorder(isSelected ? Color.clear : Theme.Colors.borderSubtle, lineWidth: 1)
            )
        }
        .buttonStyle(.plain)
    }
}

// MARK: - Channel Row

struct ChannelRow: View {

    let channel: Channel
    let nowNext: NowNext?
    var timeFilter: AppModel.TimeFilter = .now
    var isFavorite: Bool = false

    var body: some View {
        HStack(spacing: 12) {
            ChannelLogo(url: channel.logoURL, name: channel.name)

            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    if let number = channel.number {
                        Text(number)
                            .font(.caption.monospacedDigit().weight(.semibold))
                            .foregroundStyle(Theme.Colors.accentAction)
                            .frame(minWidth: 24, alignment: .leading)
                    }
                    Text(channel.name)
                        .font(.body.weight(.medium))
                        .foregroundStyle(Theme.Colors.textPrimary)
                        .lineLimit(1)

                    if isFavorite {
                        Image(systemName: "star.fill")
                            .font(.caption2)
                            .foregroundStyle(Theme.Colors.accentLive)
                    }
                }

                if timeFilter == .primeTime, let next = nowNext?.next {
                    VStack(alignment: .leading, spacing: 2) {
                        HStack(spacing: 5) {
                            Text("20:15 / DANACH")
                                .font(.system(size: 8, weight: .bold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentLive)
                                .padding(.horizontal, 4)
                                .padding(.vertical, 1)
                                .background(Theme.Colors.accentLive.opacity(0.15), in: RoundedRectangle(cornerRadius: 3))

                            Text(next.title)
                                .font(.caption.weight(.medium))
                                .foregroundStyle(Theme.Colors.textSecondary)
                                .lineLimit(1)
                        }

                        Text(next.formattedTimeRange)
                            .font(.system(size: 10, design: .monospaced))
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                } else if let now = nowNext?.now {
                    HStack(spacing: 5) {
                        PulsingLiveDot(size: 5)
                        Text(now.title)
                            .font(.caption.weight(.medium))
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .lineLimit(1)
                    }

                    if let fraction = now.progress(at: .now) {
                        HStack(spacing: 6) {
                            ProgressView(value: fraction)
                                .progressViewStyle(.linear)
                                .tint(Theme.Colors.accentLive)

                            if let remaining = now.remainingMinutes(at: .now) {
                                Text("noch \(remaining)m")
                                    .font(.system(size: 9, weight: .medium, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentLive)
                            } else {
                                Text("\(Int(fraction * 100))%")
                                    .font(.system(size: 9, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.textTertiary)
                            }
                        }
                    }
                } else {
                    Text("Keine Programminformationen")
                        .font(.caption)
                        .foregroundStyle(Theme.Colors.textTertiary)
                }
            }

            Spacer(minLength: 0)

            Image(systemName: "play.circle.fill")
                .font(.title3)
                .foregroundStyle(Theme.Colors.accentAction)
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
    }
}

// MARK: - Channel Logo

struct ChannelLogo: View {

    let url: URL?
    let name: String

    var body: some View {
        AsyncImage(url: url) { phase in
            switch phase {
            case .success(let image):
                image.resizable().scaledToFit()
            default:
                Text(initials)
                    .font(.caption2.bold().monospaced())
                    .foregroundStyle(Theme.Colors.textSecondary)
            }
        }
        .frame(width: 48, height: 36)
        .background(Theme.Colors.surfaceGlass, in: RoundedRectangle(cornerRadius: 6))
        .overlay(
            RoundedRectangle(cornerRadius: 6)
                .strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1)
        )
    }

    private var initials: String {
        name.split(separator: " ").prefix(2).compactMap { $0.first.map(String.init) }.joined()
    }
}
