// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Landscape Quick-Zap Channel Carousel (MagentaTV 2.0 / Zattoo Pattern)
struct LandscapeQuickZapBar: View {

    let channels: [Channel]
    let currentChannel: Channel
    let schedule: [String: NowNext]
    let onSelect: (Channel) -> Void
    let onClose: () -> Void

    var body: some View {
        VStack(spacing: 8) {
            // Header Bar
            HStack(spacing: 8) {
                HStack(spacing: 6) {
                    Circle()
                        .fill(Theme.Colors.accentLive)
                        .frame(width: 6, height: 6)
                    Text("SCHNELL-ZAPPING")
                        .font(.app(size: 11, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textPrimary)
                }

                Spacer()

                Button(action: onClose) {
                    Image(systemName: "xmark.circle.fill")
                        .font(.app(size: 18))
                        .foregroundStyle(Theme.Colors.textSecondary)
                }
                .buttonStyle(.plain)
            }
            .padding(.horizontal, 8)

            // Horizontal Channel Cards Carousel
            ScrollViewReader { proxy in
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: 12) {
                        ForEach(channels) { ch in
                            let isCurrent = ch.id == currentChannel.id
                            let nowEntry = schedule[ch.serviceRef]?.now
                            let progress = nowEntry?.progress(at: Date())

                            Button {
                                onSelect(ch)
                            } label: {
                                HStack(spacing: 10) {
                                    ChannelLogo(url: ch.logoURL, name: ch.name, size: 38)
                                        .background(Color.black.opacity(0.3), in: RoundedRectangle(cornerRadius: 6))

                                    VStack(alignment: .leading, spacing: 3) {
                                        HStack(spacing: 5) {
                                            if let num = ch.number {
                                                Text(num)
                                                    .font(.app(size: 11, weight: .bold, design: .monospaced))
                                                    .foregroundStyle(isCurrent ? Theme.Colors.accentLive : Theme.Colors.accentAction)
                                            }
                                            Text(ch.name)
                                                .font(.app(size: 13, weight: .bold))
                                                .foregroundStyle(isCurrent ? .white : Theme.Colors.textPrimary)
                                                .lineLimit(1)

                                            if isCurrent {
                                                Text("LIVE")
                                                    .font(.app(size: 8, weight: .black, design: .monospaced))
                                                    .foregroundStyle(.white)
                                                    .padding(.horizontal, 4)
                                                    .padding(.vertical, 1)
                                                    .background(Theme.Colors.accentLive, in: Capsule())
                                            }
                                        }

                                        if let title = nowEntry?.title {
                                            Text(title)
                                                .font(.app(size: 11, weight: .medium))
                                                .foregroundStyle(isCurrent ? Color.white.opacity(0.9) : Theme.Colors.textSecondary)
                                                .lineLimit(1)
                                        }

                                        if let p = progress {
                                            Capsule()
                                                .fill(Color.white.opacity(0.15))
                                                .frame(height: 2.5)
                                                .overlay(alignment: .leading) {
                                                    GeometryReader { barGeo in
                                                        Capsule()
                                                            .fill(isCurrent ? Theme.Colors.accentLive : Theme.Colors.accentAction)
                                                            .frame(width: max(0, barGeo.size.width * CGFloat(p)), height: 2.5)
                                                    }
                                                }
                                                .frame(height: 2.5)
                                        }
                                    }
                                }
                                .padding(.horizontal, 12)
                                .padding(.vertical, 8)
                                .frame(width: 220, height: 62, alignment: .leading)
                                .background(
                                    isCurrent ? Theme.Colors.accentAction.opacity(0.3) : Color.white.opacity(0.06),
                                    in: RoundedRectangle(cornerRadius: 12, style: .continuous)
                                )
                                .overlay(
                                    RoundedRectangle(cornerRadius: 12, style: .continuous)
                                        .strokeBorder(isCurrent ? Theme.Colors.accentLive : Theme.Colors.borderSubtle, lineWidth: isCurrent ? 1.5 : 0.8)
                                )
                                .shadow(color: isCurrent ? Theme.Colors.accentLive.opacity(0.3) : Color.clear, radius: 8)
                            }
                            .buttonStyle(.plain)
                            .id(ch.id)
                        }
                    }
                    .padding(.horizontal, 4)
                    .padding(.vertical, 2)
                }
                .frame(height: 68)
                .fadingHorizontalEdges(fadeWidth: 16)
                .onAppear {
                    proxy.scrollTo(currentChannel.id, anchor: .center)
                }
            }
        }
        .padding(12)
        .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 18, style: .continuous).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1))
        .shadow(color: Color.black.opacity(0.5), radius: 16, y: 6)
    }
}
