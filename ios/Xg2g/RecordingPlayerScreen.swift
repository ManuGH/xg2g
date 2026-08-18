// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import SwiftUI

/// Fullscreen player screen for remote DVR recordings with resume progress tracking.
struct RecordingPlayerScreen: View {

    let recording: Recording
    let serverAddress: String
    var initialPosition: Double? = nil
    var onProgressUpdate: @Sendable @MainActor (Double, Double) -> Void = { _, _ in }

    @Environment(\.dismiss) private var dismiss
    @State private var player: AVPlayer?
    @State private var timeObserver: Any?

    var body: some View {
        ZStack {
            Theme.Colors.bgVideoStage.ignoresSafeArea()

            let base = serverAddress.starts(with: "http") ? serverAddress : "https://\(serverAddress)"
            if URL(string: base) != nil {
                if let player {
                    NativeVideoPlayerView(
                        player: player,
                        onDismiss: {
                            cleanup()
                            dismiss()
                        }
                    )
                    .ignoresSafeArea()
                } else {
                    ProgressView("Lade Aufnahme…")
                        .tint(Theme.Colors.accentAction)
                        .foregroundStyle(.white)
                }
            } else {
                VStack(spacing: 16) {
                    Image(systemName: "exclamationmark.triangle")
                        .font(.system(size: 48))
                        .foregroundStyle(Theme.Colors.statusWarning)

                    Text("Keine Streaming-URL für diese Aufnahme verfügbar.")
                        .font(.headline)
                        .foregroundStyle(Theme.Colors.textPrimary)

                    Button("Schließen") {
                        dismiss()
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(Theme.Colors.accentAction)
                }
            }
        }
        .onAppear {
            setupPlayer()
        }
        .onDisappear {
            cleanup()
        }
    }

    private func setupPlayer() {
        AudioSessionManager.shared.configureForPlayback()
        let base = serverAddress.starts(with: "http") ? serverAddress : "https://\(serverAddress)"
        guard let baseURL = URL(string: base) else { return }
        let streamURL = baseURL.appendingPathComponent("api/v3/recordings/\(recording.id)/stream.mp4")

        let item = PlayerAssetLoader.makePlayerItem(url: streamURL, baseURL: baseURL)
        let p = AVPlayer(playerItem: item)

        // Add periodic time observer to track resume progress
        let progressCallback = self.onProgressUpdate
        timeObserver = p.addPeriodicTimeObserver(
            forInterval: CMTime(seconds: 5, preferredTimescale: 1),
            queue: .main
        ) { [weak p] time in
            guard let p, let duration = p.currentItem?.duration.seconds, duration > 0 else { return }
            Task { @MainActor in
                progressCallback(time.seconds, duration)
            }
        }

        if let initPos = initialPosition, initPos > 5 {
            let targetTime = CMTime(seconds: initPos, preferredTimescale: 600)
            p.seek(to: targetTime, toleranceBefore: .zero, toleranceAfter: .zero) { [weak p] finished in
                if finished {
                    p?.play()
                }
            }
        } else {
            p.play()
        }

        self.player = p
    }

    private func cleanup() {
        if let player, let observer = timeObserver {
            player.removeTimeObserver(observer)
            timeObserver = nil
        }
        player?.pause()
        AudioSessionManager.shared.deactivate()
    }
}
