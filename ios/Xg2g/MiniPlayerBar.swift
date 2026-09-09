// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Docked, floating Glass Mini-Player Bar shown above the TabBar.
///
/// Displays the active channel, live EPG title, runtime plan badge, and basic controls.
/// Tapping the bar expands the player back to full-screen.
struct MiniPlayerBar: View {

    @ObservedObject var playbackManager: PlaybackManager
    var model: AppModel?

    init(playbackManager: PlaybackManager, model: AppModel? = nil) {
        self.playbackManager = playbackManager
        self.model = model
    }

    private var channel: Channel? {
        playbackManager.currentChannel
    }

    private var recordingItem: PlayingRecordingItem? {
        playbackManager.activeRecordingItem
    }

    private var logoURL: URL? {
        channel?.logoURL
    }

    private var currentProgramTitle: String {
        guard let channel else { return "Live TV" }
        return model?.schedule[channel.serviceRef]?.now?.title ?? channel.name
    }

    public var body: some View {
        if let channel {
            renderContent(
                icon: AnyView(
                    ChannelLogo(url: logoURL, name: channel.name, size: 36)
                        .clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
                ),
                eyebrow: AnyView(
                    HStack(spacing: 6) {
                        PulsingLiveDot(size: 6)
                        Text(channel.name)
                            .font(.system(size: 13, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(1)

                        if let plan = playbackManager.displayedPlan {
                            Text(plan.userSummary)
                                .font(.system(size: 9, weight: .semibold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentLive)
                                .padding(.horizontal, 5)
                                .padding(.vertical, 1)
                                .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())
                        }
                    }
                ),
                subtitle: currentProgramTitle
            )
        } else if let rec = recordingItem {
            renderContent(
                icon: AnyView(
                    ZStack {
                        RoundedRectangle(cornerRadius: 8, style: .continuous)
                            .fill(Theme.Colors.accentAction.opacity(0.2))
                            .frame(width: 36, height: 36)
                        Image(systemName: "film.stack.fill")
                            .font(.system(size: 16))
                            .foregroundStyle(Theme.Colors.accentAction)
                    }
                ),
                eyebrow: AnyView(
                    HStack(spacing: 6) {
                        Text("AUFNAHME")
                            .font(.system(size: 9, weight: .bold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.accentAction)
                            .padding(.horizontal, 5)
                            .padding(.vertical, 1)
                            .background(Theme.Colors.accentAction.opacity(0.15), in: Capsule())

                        Text(rec.recording.title)
                            .font(.system(size: 13, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(1)
                    }
                ),
                subtitle: rec.recording.formattedDuration
            )
        } else {
            EmptyView()
        }
    }

    @ViewBuilder
    private func renderContent(icon: AnyView, eyebrow: AnyView, subtitle: String) -> some View {
        Button {
            Haptics.shared.impact(.medium)
            playbackManager.expand()
        } label: {
            HStack(spacing: 12) {
                icon

                VStack(alignment: .leading, spacing: 2) {
                    eyebrow

                    Text(subtitle)
                        .font(.system(size: 12))
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .lineLimit(1)
                }

                Spacer(minLength: 4)

                HStack(spacing: 8) {
                    Image(systemName: "arrow.up.left.and.arrow.down.right")
                        .font(.system(size: 13, weight: .bold))
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .frame(width: 32, height: 32)
                        .background(Color.white.opacity(0.06), in: Circle())

                    Button {
                        Haptics.shared.impact(.light)
                        playbackManager.stop()
                    } label: {
                        Image(systemName: "xmark")
                            .font(.system(size: 12, weight: .bold))
                            .foregroundStyle(.white)
                            .frame(width: 32, height: 32)
                            .background(Theme.Colors.statusError.opacity(0.8), in: Circle())
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 10)
            .background(.ultraThinMaterial)
            .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
            .overlay(
                RoundedRectangle(cornerRadius: 16, style: .continuous)
                    .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8)
            )
            .shadow(color: Color.black.opacity(0.35), radius: 12, x: 0, y: 6)
            .padding(.horizontal, 12)
            .padding(.bottom, 6)
        }
        .buttonStyle(.plain)
    }
}
