// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

struct ChannelListView: View {

    let model: AppModel
    @State private var selected: Channel?

    var body: some View {
        NavigationStack {
            Group {
                if model.channels.isEmpty && model.isLoadingChannels {
                    ProgressView("Loading channels…")
                } else if model.channels.isEmpty {
                    ContentUnavailableView(
                        "No channels",
                        systemImage: "tv.slash",
                        description: Text(model.lastError ?? "The server returned no channels.")
                    )
                } else {
                    List(model.channels) { channel in
                        Button {
                            selected = channel
                        } label: {
                            ChannelRow(channel: channel, nowNext: model.schedule[channel.serviceRef])
                        }
                        .buttonStyle(.plain)
                    }
                    .listStyle(.plain)
                    .refreshable { await model.loadChannels() }
                }
            }
            .navigationTitle("Live TV")
            .toolbar {
                if model.isLoadingChannels && !model.channels.isEmpty {
                    ProgressView()
                }
            }
        }
        .fullScreenCover(item: $selected) { channel in
            PlayerScreen(model: model, channel: channel)
        }
    }
}

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
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(.secondary)
                    }
                    Text(channel.name)
                        .font(.body.weight(.medium))
                        .lineLimit(1)
                }

                if let now = nowNext?.now {
                    Text(now.title)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)

                    // Only drawn while the programme is actually running.
                    // `progress` is nil outside it, which is the difference
                    // between "not on" and "just started".
                    if let fraction = now.progress(at: .now) {
                        ProgressView(value: fraction)
                            .progressViewStyle(.linear)
                            .tint(.secondary)
                    }
                } else {
                    Text("No programme information")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                }
            }

            Spacer(minLength: 0)
            Image(systemName: "play.circle.fill")
                .foregroundStyle(.tint)
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
    }
}

/// A logo when there is one, initials when there is not.
///
/// A missing logo is the normal case for a lot of bouquets, so it gets a real
/// design rather than an empty box.
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
                    .font(.caption2.bold())
                    .foregroundStyle(.secondary)
            }
        }
        .frame(width: 44, height: 34)
        .background(.quaternary, in: RoundedRectangle(cornerRadius: 6))
    }

    private var initials: String {
        name.split(separator: " ").prefix(2).compactMap { $0.first.map(String.init) }.joined()
    }
}
