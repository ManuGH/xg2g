// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Docked, floating Glass Mini-Player Bar shown above the TabBar.
///
/// Displays the active channel, live EPG title, runtime plan badge, and basic controls.
/// Tapping the bar expands the player back to full-screen.
struct MiniPlayerBar: View {

    var playbackManager: PlaybackManager
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
                            .font(.app(size: 13, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .lineLimit(1)

                        if let plan = playbackManager.displayedPlan {
                            Text(plan.userSummary)
                                .font(.app(size: 9, weight: .semibold, design: .monospaced))
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
                            .font(.app(size: 16))
                            .foregroundStyle(Theme.Colors.accentAction)
                    }
                ),
                eyebrow: AnyView(
                    HStack(spacing: 6) {
                        Text("AUFNAHME")
                            .font(.app(size: 9, weight: .bold, design: .monospaced))
                            .foregroundStyle(Theme.Colors.accentAction)
                            .padding(.horizontal, 5)
                            .padding(.vertical, 1)
                            .background(Theme.Colors.accentAction.opacity(0.15), in: Capsule())

                        Text(rec.recording.title)
                            .font(.app(size: 13, weight: .bold))
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
        HStack(spacing: 14) {
            // Main clickable / focusable area: expands to fullscreen
            Button {
                Haptics.shared.impact(.medium)
                playbackManager.expand()
            } label: {
                HStack(spacing: 12) {
                    icon

                    VStack(alignment: .leading, spacing: 2) {
                        eyebrow

                        Text(subtitle)
                            .font(.app(size: 12))
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .lineLimit(1)
                    }

                    Spacer(minLength: 4)

#if os(tvOS)
                    Image(systemName: "arrow.up.left.and.arrow.down.right")
                        .font(.system(size: 14, weight: .semibold))
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .padding(.trailing, 6)
#endif
                }
                .contentShape(Rectangle())
            }
#if os(tvOS)
            .buttonStyle(TVMiniPlayerCardButtonStyle())
#else
            .buttonStyle(.plain)
#endif

            // Play / Pause Toggle Button
            Button {
                Haptics.shared.impact(.medium)
                playbackManager.togglePlayPause()
            } label: {
                Image(systemName: playbackManager.isPlaying ? "pause.fill" : "play.fill")
                    .font(.app(size: 13, weight: .bold))
                    .foregroundStyle(.white)
#if !os(tvOS)
                    .frame(width: 32, height: 32)
                    .background(Color.white.opacity(0.12), in: Circle())
#endif
            }
#if os(tvOS)
            .buttonStyle(TVMiniPlayerControlButtonStyle(isDestructive: false))
#else
            .buttonStyle(.plain)
#endif

            // Close Button
            Button {
                Haptics.shared.impact(.light)
                playbackManager.stop()
            } label: {
                Image(systemName: "xmark")
                    .font(.app(size: 12, weight: .bold))
                    .foregroundStyle(.white)
#if !os(tvOS)
                    .frame(width: 32, height: 32)
                    .background(Theme.Colors.statusError.opacity(0.85), in: Circle())
#endif
            }
#if os(tvOS)
            .buttonStyle(TVMiniPlayerControlButtonStyle(isDestructive: true))
#else
            .buttonStyle(.plain)
#endif
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
}

#if os(tvOS)
struct TVMiniPlayerCardButtonStyle: ButtonStyle {
    @Environment(\.isFocused) private var isFocused

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .padding(.horizontal, 14)
            .padding(.vertical, 8)
            .background(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(isFocused ? Theme.Colors.surfaceElevated : Color.white.opacity(0.05))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .strokeBorder(isFocused ? Theme.Colors.accentAction : Color.clear, lineWidth: 2)
            )
            .scaleEffect(isFocused ? 1.02 : 1.0)
            .animation(.easeOut(duration: 0.16), value: isFocused)
    }
}

struct TVMiniPlayerControlButtonStyle: ButtonStyle {
    let isDestructive: Bool
    @Environment(\.isFocused) private var isFocused

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .frame(width: 40, height: 40)
            .background(
                Circle()
                    .fill(isDestructive ? (isFocused ? Theme.Colors.statusError : Theme.Colors.statusError.opacity(0.7)) : (isFocused ? Theme.Colors.accentAction : Color.white.opacity(0.15)))
            )
            .overlay(
                Circle()
                    .strokeBorder(isFocused ? Color.white : Color.clear, lineWidth: 2)
            )
            .scaleEffect(isFocused ? 1.15 : 1.0)
            .animation(.easeOut(duration: 0.16), value: isFocused)
    }
}
#endif

#if DEBUG
@available(iOS 26.0, *)
#Preview("MiniPlayer States", arguments: [
    GuidePreviewData.channel("1", "Das Erste HD"),
    GuidePreviewData.channel("2", "ZDF HD"),
    GuidePreviewData.channel("201", "Sky Sport Top Event"),
    GuidePreviewData.channel("99", "FM4 Radio")
]) { channel in
    let playbackManager: PlaybackManager = {
        let pm = PlaybackManager(streamURL: { _ in nil })
        pm.state = .live(channel, mode: .miniplayer)
        return pm
    }()

    VStack {
        Spacer()
        MiniPlayerBar(playbackManager: playbackManager)
    }
    .background(Theme.Colors.bgBase)
}
#endif
