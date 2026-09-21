// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

// MARK: - Favorites Quick Access Rail (Top Highlight Carousel)

struct FavoritesQuickRail: View {
    let channels: [Channel]
    let model: AppModel
    var onPlay: (Channel) -> Void
    var onShowInfo: (Channel, NowNext.Entry) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 6) {
                Image(systemName: "star.fill")
                    .font(.app(size: 13, weight: .bold))
                    .foregroundStyle(.yellow)
                Text("Meine Favoriten")
                    .font(.headline.weight(.bold))
                    .foregroundStyle(Theme.Colors.textPrimary)

                Spacer()

                Text("\(channels.count) Sender")
                    .font(.subheadline)
                    .foregroundStyle(Theme.Colors.textTertiary)
            }
            .padding(.horizontal, 2)

            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 12) {
                    ForEach(channels) { channel in
                        let nowNext = model.schedule[channel.serviceRef]
                        Button {
                            Haptics.shared.impact(.light)
                            onPlay(channel)
                        } label: {
                            HStack(spacing: 10) {
                                ChannelLogo(url: channel.logoURL, name: channel.name, size: 36)

                                VStack(alignment: .leading, spacing: 2) {
                                    HStack(spacing: 4) {
                                        if let number = channel.number {
                                            Text(number)
                                                .font(.app(size: 9, weight: .bold, design: .monospaced))
                                                .foregroundStyle(Theme.Colors.accentAction)
                                                .padding(.horizontal, 4)
                                                .padding(.vertical, 1)
                                                .background(Theme.Colors.accentAction.opacity(0.15), in: RoundedRectangle(cornerRadius: 3))
                                        }

                                        Text(channel.name)
                                            .font(.app(size: 14, weight: .bold))
                                            .foregroundStyle(Theme.Colors.textPrimary)
                                            .lineLimit(1)
                                    }

                                    if let now = nowNext?.now {
                                        Text(now.title)
                                            .font(.app(size: 12, weight: .medium))
                                            .foregroundStyle(Theme.Colors.textTertiary)
                                            .lineLimit(1)
                                    } else {
                                        Text("Live TV")
                                            .font(.app(size: 12))
                                            .foregroundStyle(Theme.Colors.textTertiary)
                                    }
                                }

                                Image(systemName: "play.circle.fill")
                                    .font(.app(size: 18))
                                    .foregroundStyle(Theme.Colors.accentAction)
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
