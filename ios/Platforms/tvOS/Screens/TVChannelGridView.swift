// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Live TV as a grid of channel tiles.
///
/// A phone lists channels in rows because a thumb scrolls a column; a
/// television shows them as tiles because a remote moves in two directions
/// and a logo is recognised faster than a name is read.
struct TVChannelGridView: View {

    @Bindable var model: AppModel

    private let columns = Array(
        repeating: GridItem(.fixed(TVDesign.Layout.tileWidth), spacing: TVDesign.Layout.gutter),
        count: 5
    )

    var body: some View {
        ZStack {
            Theme.Colors.bgBase.ignoresSafeArea()

            if model.channels.isEmpty {
                ContentUnavailableView(
                    model.isLoadingChannels ? "Lade Sender…" : "Keine Sender",
                    systemImage: "tv"
                )
                .foregroundStyle(Theme.Colors.textSecondary)
            } else {
                ScrollView(.vertical, showsIndicators: false) {
                    VStack(alignment: .leading, spacing: 16) {
                        HStack(alignment: .firstTextBaseline) {
                            Text(model.selectedBouquet?.name ?? "Alle Sender")
                                .font(TVDesign.Font.heading)
                                .foregroundStyle(Theme.Colors.textPrimary)
                            Text("\(model.channels.count) Sender")
                                .font(TVDesign.Font.meta)
                                .foregroundStyle(Theme.Colors.textSecondary)
                        }

                        LazyVGrid(columns: columns, alignment: .leading, spacing: TVDesign.Layout.gutter) {
                            ForEach(model.channels) { channel in
                                TVChannelTile(
                                    channel: channel,
                                    now: model.show(for: channel, at: .now),
                                    isFavorite: model.isFavorite(channel),
                                    onPlay: { model.playingChannel = channel },
                                    onToggleFavorite: { model.toggleFavorite(channel) }
                                )
                            }
                        }
                        .padding(.vertical, 16)
                    }
                    .padding(.vertical, 20)
                }
            }
        }
    }
}

struct TVChannelTile: View {
    let channel: Channel
    let now: NowNext.Entry?
    let isFavorite: Bool
    var onPlay: () -> Void
    var onToggleFavorite: () -> Void

    var body: some View {
        Button(action: onPlay) {
            VStack(spacing: 10) {
                HStack {
                    if let number = channel.number {
                        Text(number)
                            .font(TVDesign.Font.meta)
                            .foregroundStyle(Theme.Colors.textSecondary)
                    }
                    Spacer(minLength: 0)
                    if isFavorite {
                        Image(systemName: "star.fill")
                            .font(TVDesign.Font.meta)
                            .foregroundStyle(Theme.Colors.statusWarning)
                    }
                }

                Spacer(minLength: 0)

                ChannelLogo(url: channel.logoURL, name: channel.name, size: 72)

                Spacer(minLength: 0)

                VStack(spacing: 4) {
                    Text(channel.name)
                        .font(TVDesign.Font.body)
                        .foregroundStyle(Theme.Colors.textPrimary)
                        .lineLimit(1)
                    Text(now?.title ?? " ")
                        .font(TVDesign.Font.meta)
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .lineLimit(1)
                }
                .frame(maxWidth: .infinity)

                TVProgressLine(progress: now?.progress(at: Date.now) ?? 0)
                    .opacity(now?.progress(at: Date.now) == nil ? 0 : 1)
            }
            .padding(18)
            .frame(width: TVDesign.Layout.tileWidth, height: TVDesign.Layout.tileHeight)
        }
        .buttonStyle(TVCardButtonStyle())
        .contextMenu {
            Button("Live schauen", systemImage: "play.fill", action: onPlay)
            Button(
                isFavorite ? "Aus Favoriten entfernen" : "Zu Favoriten hinzufügen",
                systemImage: isFavorite ? "star.slash" : "star",
                action: onToggleFavorite
            )
        }
    }
}
