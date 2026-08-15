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
                            .padding(.vertical, 8)
                            .background(Theme.Colors.surfaceElevated.opacity(0.5))
                    }

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
                                    ChannelRow(channel: channel, nowNext: model.schedule[channel.serviceRef])
                                }
                                .buttonStyle(.plain)
                                .listRowBackground(Theme.Colors.surfaceElevated)
                                .listRowSeparatorTint(Theme.Colors.borderSubtle)
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
    var count: Int?
    let isSelected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
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
                }

                if let now = nowNext?.now {
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
