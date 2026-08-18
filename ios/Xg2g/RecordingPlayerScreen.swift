// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import SwiftUI

/// Fullscreen player screen for remote DVR recordings with cinematic loading stage and resume progress tracking.
struct RecordingPlayerScreen: View {

    let recording: Recording
    let serverAddress: String
    var initialPosition: Double? = nil
    var onProgressUpdate: @Sendable @MainActor (Double, Double) -> Void = { _, _ in }

    @Environment(\.dismiss) private var dismiss
    @State private var player: AVPlayer?
    @State private var timeObserver: Any?
    @State private var isPreparing = true

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
                    .transition(.opacity)
                }

                // Cinematic Loading Backdrop (Infuse / Apple TV+ Style)
                if player == nil || isPreparing {
                    cinematicLoadingStage
                        .transition(.opacity)
                }
            } else {
                errorStateView
            }
        }
        .animation(.easeInOut(duration: 0.35), value: player != nil)
        .animation(.easeInOut(duration: 0.35), value: isPreparing)
        .onAppear {
            setupPlayer()
        }
        .onDisappear {
            cleanup()
        }
    }

    // MARK: - Cinematic Loading Stage

    @ViewBuilder
    private var cinematicLoadingStage: some View {
        let palette = RecordingArtworkTheme.palette(for: recording)

        ZStack {
            Theme.Colors.bgVideoStage.ignoresSafeArea()

            // Ambient background glow
            RadialGradient(
                colors: [palette.accent.opacity(0.22), Color.clear],
                center: .center,
                startRadius: 20,
                endRadius: 360
            )
            .ignoresSafeArea()
            .allowsHitTesting(false)

            VStack(spacing: 24) {
                // Top Dismiss Bar
                HStack {
                    Button {
                        cleanup()
                        dismiss()
                    } label: {
                        Image(systemName: "xmark")
                            .font(.system(size: 15, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .padding(12)
                            .background(.ultraThinMaterial, in: Circle())
                            .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1))
                    }
                    .buttonStyle(.plain)

                    Spacer()
                }
                .padding(.horizontal, 20)
                .padding(.top, 16)

                Spacer()

                // Center Poster / Title Focus Card
                VStack(spacing: 16) {
                    ZStack {
                        RoundedRectangle(cornerRadius: 18, style: .continuous)
                            .fill(palette.gradient)
                            .frame(width: 84, height: 84)
                            .overlay(
                                RoundedRectangle(cornerRadius: 18, style: .continuous)
                                    .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1.2)
                            )
                            .shadow(color: palette.accent.opacity(0.35), radius: 16, y: 6)

                        Image(systemName: palette.icon)
                            .font(.system(size: 36, weight: .semibold))
                            .foregroundStyle(.white)
                    }

                    VStack(spacing: 6) {
                        Text(recording.title)
                            .font(.system(size: 20, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .multilineTextAlignment(.center)
                            .lineLimit(2)
                            .padding(.horizontal, 32)

                        HStack(spacing: 8) {
                            Text(recording.formattedDate)
                                .font(.system(size: 13, weight: .medium, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textTertiary)

                            Text("•")
                                .foregroundStyle(Theme.Colors.textDisabled)

                            Text(recording.formattedDuration)
                                .font(.system(size: 13, weight: .semibold, design: .monospaced))
                                .foregroundStyle(palette.accent)
                        }
                    }

                    // Progress Indicator Pill
                    HStack(spacing: 10) {
                        ProgressView()
                            .tint(palette.accent)
                            .scaleEffect(0.9)

                        Text(initialPosition != nil && initialPosition! > 5 ? "Fortsetzen wird geladen…" : "Wiedergabe wird gestartet…")
                            .font(.system(size: 13, weight: .semibold))
                            .foregroundStyle(Theme.Colors.textSecondary)
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 8)
                    .background(.ultraThinMaterial, in: Capsule())
                    .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                }

                Spacer()
            }
        }
    }

    // MARK: - Error View

    @ViewBuilder
    private var errorStateView: some View {
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

    // MARK: - Player Setup

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
                Task { @MainActor in
                    if finished {
                        p?.play()
                    }
                    withAnimation(.easeInOut(duration: 0.3)) {
                        self.isPreparing = false
                    }
                }
            }
        } else {
            p.play()
            withAnimation(.easeInOut(duration: 0.3)) {
                self.isPreparing = false
            }
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
