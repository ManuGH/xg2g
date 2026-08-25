// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import SwiftUI

/// Full-screen offline video player for downloaded recordings (Plex/Netflix style).
/// Plays directly from local flash storage without internet connection or server auth.
struct OfflinePlayerScreen: View {

    let offlineRecording: OfflineRecording

    @Environment(\.dismiss) private var dismiss
    @State private var player: AVPlayer?
    @State private var showControls = true
    @State private var hideControlsTask: Task<Void, Never>?

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
            } else {
                ProgressView()
                    .tint(Theme.Colors.accentLive)
            }

            // OSD Controls
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

                        VStack(alignment: .leading, spacing: 2) {
                            Text(offlineRecording.title)
                                .font(.headline)
                                .foregroundStyle(Theme.Colors.textPrimary)
                                .lineLimit(1)

                            if let channel = offlineRecording.channelName {
                                Text(channel)
                                    .font(.caption)
                                    .foregroundStyle(Theme.Colors.textSecondary)
                            }
                        }

                        Spacer()

                        // Offline Badge
                        HStack(spacing: 4) {
                            Image(systemName: "arrow.down.circle.fill")
                                .font(.caption)
                            Text("OFFLINE")
                                .font(.system(size: 10, weight: .bold, design: .monospaced))
                        }
                        .padding(.horizontal, 8)
                        .padding(.vertical, 4)
                        .background(Theme.Colors.accentAction.opacity(0.2), in: Capsule())
                        .foregroundStyle(Theme.Colors.accentAction)
                        .overlay(Capsule().strokeBorder(Theme.Colors.accentAction.opacity(0.4), lineWidth: 1))
                    }
                    .padding()
                    .background(
                        LinearGradient(
                            colors: [Color.black.opacity(0.85), Color.clear],
                            startPoint: .top,
                            endPoint: .bottom
                        )
                    )

                    Spacer()

                    // Bottom Details
                    HStack(spacing: 8) {
                        Text("\(offlineRecording.formattedDuration) • \(offlineRecording.formattedSize)")
                            .font(.caption.monospacedDigit())
                            .foregroundStyle(Theme.Colors.textSecondary)

                        if let q = offlineRecording.quality {
                            Text("•")
                                .font(.caption2)
                                .foregroundStyle(Theme.Colors.textTertiary)

                            Label(q.title, systemImage: q.icon)
                                .font(.caption.bold())
                                .foregroundStyle(Theme.Colors.accentLive)
                        }

                        Spacer()
                    }
                    .padding(16)
                    .glassCard(cornerRadius: 12)
                    .padding(.horizontal, 16)
                    .padding(.bottom, 24)
                }
                .transition(.opacity)
            }
        }
        .task {
            AudioSessionManager.shared.configureForPlayback()
            let localURL = offlineRecording.localFileURL()
            let player = AVPlayer(url: localURL)
            self.player = player
            player.play()
            scheduleControlsHiding()
        }
        .onDisappear {
            hideControlsTask?.cancel()
            player?.pause()
            player = nil
            AudioSessionManager.shared.deactivate()
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
}
