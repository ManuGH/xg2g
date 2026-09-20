// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

// MARK: - Recent Channel Card Component (Infuse / Apple TV+ Aesthetics)

struct RecentChannelCard: View {
    let channel: Channel
    let nowNext: NowNext?
    var onPlay: () -> Void
    var onShowInfo: (NowNext.Entry) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            // Top Header: Logo + Channel Name & Number + Live Pulse
            HStack(spacing: 10) {
                ChannelLogo(url: channel.logoURL, name: channel.name, size: 38)

                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 5) {
                        if let number = channel.number {
                            Text(number)
                                .font(.app(size: 10, weight: .bold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentAction)
                                .padding(.horizontal, 5)
                                .padding(.vertical, 1.5)
                                .background(Theme.Colors.accentAction.opacity(0.15), in: RoundedRectangle(cornerRadius: 4))
                        }

                        Text(channel.name)
                            .font(.app(size: 14, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(1)
                    }

                    if let now = nowNext?.now {
                        HStack(spacing: 5) {
                            PulsingLiveDot(size: 5)
                            Text(now.formattedTimeRange)
                                .font(.app(size: 10, weight: .medium, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentLive)
                        }
                    }
                }

                Spacer(minLength: 4)

                if let now = nowNext?.now, let remaining = now.remainingMinutes(at: .now) {
                    Text("noch \(remaining)m")
                        .font(.app(size: 10, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.accentLive)
                        .padding(.horizontal, 7)
                        .padding(.vertical, 3)
                        .background(Theme.Colors.accentLive.opacity(0.14), in: Capsule())
                }
            }

            // Middle: Show Title & Progress Scrubber
            if let now = nowNext?.now {
                VStack(alignment: .leading, spacing: 5) {
                    HStack(alignment: .top) {
                        Text(now.title)
                            .font(.app(size: 13, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(1)

                        Spacer(minLength: 4)

                        Button {
                            Haptics.shared.impact(.light)
                            onShowInfo(now)
                        } label: {
                            Image(systemName: "info.circle")
                                .font(.app(size: 13))
                                .foregroundStyle(Theme.Colors.textTertiary)
                                .padding(2)
                        }
                        .buttonStyle(.plain)
                    }

                    // Gradient Live Progress Bar
                    if let fraction = now.progress(at: .now) {
                        GeometryReader { geo in
                            ZStack(alignment: .leading) {
                                Capsule()
                                    .fill(Color.white.opacity(0.12))
                                    .frame(height: 3)

                                Capsule()
                                    .fill(
                                        LinearGradient(
                                            colors: [Theme.Colors.accentAction, Theme.Colors.accentLive],
                                            startPoint: .leading,
                                            endPoint: .trailing
                                        )
                                    )
                                    .frame(width: max(0, min(geo.size.width, geo.size.width * CGFloat(fraction))), height: 3)
                            }
                        }
                        .frame(height: 3)
                    }
                }
            } else {
                Text("Keine Programminformationen")
                    .font(.caption)
                    .foregroundStyle(Theme.Colors.textTertiary)
                    .padding(.vertical, 2)
            }

            // Bottom: Action Pill
            HStack {
                HStack(spacing: 5) {
                    Image(systemName: "play.fill")
                        .font(.app(size: 10, weight: .bold))
                    Text("Fortsetzen")
                        .font(.app(size: 11, weight: .bold))
                        .lineLimit(1)
                }
                .fixedSize(horizontal: true, vertical: false)
                .padding(.horizontal, 10)
                .padding(.vertical, 4.5)
                .background(Theme.Colors.accentAction.opacity(0.18), in: Capsule())
                .foregroundStyle(Theme.Colors.accentAction)
                .overlay(Capsule().strokeBorder(Theme.Colors.accentAction.opacity(0.35), lineWidth: 0.8))

                Spacer()

                if let next = nowNext?.next {
                    Text("Danach: \(next.title)")
                        .font(.app(size: 10))
                        .foregroundStyle(Theme.Colors.textTertiary)
                        .lineLimit(1)
                        .frame(maxWidth: 110, alignment: .trailing)
                }
            }
        }
        .padding(12)
        .frame(width: 260)
        .background(
            LinearGradient(
                colors: [
                    Theme.Colors.surfaceElevated.opacity(0.95),
                    Color(red: 0.08, green: 0.11, blue: 0.16)
                ],
                startPoint: .topLeading,
                endPoint: .bottomTrailing
            )
        )
        .clipShape(RoundedRectangle(cornerRadius: 15, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 15, style: .continuous)
                .strokeBorder(Theme.Gradients.liveAuraBorder, lineWidth: 0.8)
        )
        .shadow(color: Theme.Colors.accentLive.opacity(0.08), radius: 8, y: 3)
        .contentShape(RoundedRectangle(cornerRadius: 15, style: .continuous))
        .appHoverEffect(.lift)
        .onTapGesture {
            Haptics.shared.impact(.light)
            onPlay()
        }
    }
}
