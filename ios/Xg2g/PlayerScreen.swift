// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import SwiftUI

/// Live playback for one channel.
///
/// The screen owns the session's lifetime: it starts a stream when it appears
/// and stops it when it goes away. A session that outlived its screen would
/// hold a tuner for a stream nobody is watching, which is exactly how a lease
/// ends up being reclaimed minutes later instead of immediately.
struct PlayerScreen: View {

    let model: AppModel
    let channel: Channel

    @Environment(\.dismiss) private var dismiss
    @State private var player: AVPlayer?
    @State private var failure: String?
    @State private var showControls = true
    @State private var hideControlsTask: Task<Void, Never>?

    var nowNext: NowNext? {
        model.schedule[channel.serviceRef]
    }

    var body: some View {
        ZStack {
            Theme.Colors.bgVideoStage.ignoresSafeArea()

            if let player {
                VideoPlayer(player: player)
                    .ignoresSafeArea()
                    .onTapGesture {
                        withAnimation(.easeInOut(duration: 0.25)) {
                            showControls.toggle()
                        }
                        if showControls {
                            scheduleControlsHiding()
                        }
                    }
            } else if let failure {
                ContentUnavailableView(
                    "Wiedergabefehler",
                    systemImage: "exclamationmark.triangle",
                    description: Text(failure)
                )
                .foregroundStyle(Theme.Colors.textSecondary)
            } else {
                VStack(spacing: 16) {
                    ProgressView()
                        .tint(Theme.Colors.accentLive)
                        .scaleEffect(1.3)

                    Text("\(channel.name) wird gestreamt…")
                        .font(.headline)
                        .foregroundStyle(Theme.Colors.textPrimary)
                }
            }

            // MARK: - Broadcast Console OSD Overlay
            if showControls {
                VStack {
                    // Top Bar
                    HStack(spacing: 12) {
                        Button {
                            dismiss()
                        } label: {
                            Image(systemName: "chevron.down.circle.fill")
                                .font(.title2)
                                .foregroundStyle(Theme.Colors.textPrimary.opacity(0.85))
                        }

                        HStack(spacing: 8) {
                            if let number = channel.number {
                                Text(number)
                                    .font(.caption.monospacedDigit().bold())
                                    .foregroundStyle(Theme.Colors.accentAction)
                                    .padding(.horizontal, 6)
                                    .padding(.vertical, 2)
                                    .background(Theme.Colors.accentAction.opacity(0.15), in: RoundedRectangle(cornerRadius: 4))
                            }

                            Text(channel.name)
                                .font(.headline)
                                .foregroundStyle(Theme.Colors.textPrimary)
                        }

                        Spacer()

                        HStack(spacing: 6) {
                            Circle()
                                .fill(Theme.Colors.accentLive)
                                .frame(width: 8, height: 8)
                            Text("LIVE")
                                .font(.caption.bold().monospaced())
                                .foregroundStyle(Theme.Colors.accentLive)
                        }
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())
                    }
                    .padding()
                    .background(
                        LinearGradient(
                            colors: [Color.black.opacity(0.75), Color.clear],
                            startPoint: .top,
                            endPoint: .bottom
                        )
                    )

                    Spacer()

                    // Bottom Bar (Now/Next Info)
                    if let now = nowNext?.now {
                        VStack(alignment: .leading, spacing: 6) {
                            Text(now.title)
                                .font(.headline)
                                .foregroundStyle(Theme.Colors.textPrimary)
                                .lineLimit(1)

                            if let description = now.description {
                                Text(description)
                                    .font(.caption)
                                    .foregroundStyle(Theme.Colors.textSecondary)
                                    .lineLimit(2)
                            }

                            if let fraction = now.progress(at: .now) {
                                ProgressView(value: fraction)
                                    .progressViewStyle(.linear)
                                    .tint(Theme.Colors.accentLive)
                            }
                        }
                        .padding(16)
                        .glassCard(cornerRadius: 12)
                        .padding(.horizontal, 16)
                        .padding(.bottom, 24)
                    }
                }
                .transition(.opacity)
            }
        }
        .task {
            await model.play(channel)

            guard let stream = model.liveStream else {
                failure = model.lastError ?? "Der Stream konnte nicht gestartet werden."
                return
            }
            player = Self.makePlayer(for: stream)
            player?.play()
            scheduleControlsHiding()
        }
        .onDisappear {
            hideControlsTask?.cancel()
            player?.pause()
            player = nil
            Task { await model.stopPlayback() }
        }
    }

    private func scheduleControlsHiding() {
        hideControlsTask?.cancel()
        hideControlsTask = Task {
            try? await Task.sleep(for: .seconds(4))
            if !Task.isCancelled {
                withAnimation(.easeInOut(duration: 0.25)) {
                    showControls = false
                }
            }
        }
    }

    /// Builds the player with the playback ticket attached.
    private static func makePlayer(for stream: LiveStream) -> AVPlayer? {
        guard let cookie = stream.ticket.httpCookie(for: stream.playlistURL) else { return nil }

        let asset = AVURLAsset(
            url: stream.playlistURL,
            options: [AVURLAssetHTTPCookiesKey: [cookie]]
        )
        let item = AVPlayerItem(asset: asset)
        let player = AVPlayer(playerItem: item)

        player.automaticallyWaitsToMinimizeStalling = true
        return player
    }
}
