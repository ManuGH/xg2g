// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

// MARK: - Recently Played Rail ("ZULETZT GESPIELT" - Premium Frosted Glass Cards)

struct RecentlyWatchedRail: View {
    let channels: [Channel]
    let model: AppModel
    var onPlay: (Channel) -> Void
    var onShowInfo: (Channel, NowNext.Entry) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(spacing: 6) {
                Image(systemName: "bolt.fill")
                    .font(.app(size: 13, weight: .bold))
                    .foregroundStyle(Theme.Colors.accentAction)
                Text("Zuletzt geschaut")
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
                        RecentChannelCard(
                            channel: channel,
                            nowNext: nowNext,
                            onPlay: { onPlay(channel) },
                            onShowInfo: { entry in onShowInfo(channel, entry) }
                        )
                    }
                }
            }
        }
    }
}
