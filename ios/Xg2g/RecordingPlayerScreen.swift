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
    var model: AppModel? = nil
    var onProgressUpdate: @Sendable @MainActor (Double, Double) -> Void = { _, _ in }

    @Environment(\.dismiss) private var dismiss
    @State private var player: AVPlayer?
    @State private var timeObserver: Any?
    @State private var statusObserver: NSKeyValueObservation?
    @State private var isPreparing = true
    @State private var errorMessage: String? = nil

    var body: some View {
        ZStack {
            Theme.Colors.bgVideoStage.ignoresSafeArea()

            let base = serverAddress.starts(with: "http") ? serverAddress : "https://\(serverAddress)"
            if errorMessage == nil && URL(string: base) != nil {
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

            Text(errorMessage ?? "Keine Streaming-URL für diese Aufnahme verfügbar.")
                .font(.headline)
                .foregroundStyle(Theme.Colors.textPrimary)
                .multilineTextAlignment(.center)
                .padding(.horizontal, 24)

            Button("Schließen") {
                cleanup()
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
        guard let baseURL = URL(string: base) else {
            errorMessage = "Ungültige Server-Adresse"
            isPreparing = false
            return
        }

        Task {
            var sessionCookie: String? = nil
            var negotiatedPath: String? = nil
            if let model {
                do {
                    sessionCookie = try await model.mediaSessionCookie()
                } catch {
                    print("[RecordingPlayer] ⚠️ Could not acquire media session cookie: \(error)")
                }
                do {
                    negotiatedPath = try await model.recordingPlaybackUrl(for: recording.id)
                } catch {
                    print("[RecordingPlayer] ⚠️ Could not negotiate stream-info: \(error)")
                }
            }

            if let cookieValue = sessionCookie, let host = baseURL.host {
                let cookie = HTTPCookie(properties: [
                    .name: "xg2g_session",
                    .value: cookieValue,
                    .domain: host,
                    .path: "/",
                    .secure: baseURL.scheme?.lowercased() == "https" ? "TRUE" : "FALSE",
                    .expires: Date().addingTimeInterval(86400)
                ])
                if let cookie {
                    HTTPCookieStorage.shared.setCookie(cookie)
                }
            }

            let streamURL: URL
            if let path = negotiatedPath, !path.isEmpty {
                if let directURL = URL(string: path), directURL.scheme != nil {
                    streamURL = directURL
                } else {
                    let cleanPath = path.hasPrefix("/") ? String(path.dropFirst()) : path
                    let rawBase = base.hasSuffix("/") ? base : base + "/"
                    streamURL = URL(string: rawBase + cleanPath) ?? baseURL.appendingPathComponent(cleanPath)
                }
            } else {
                streamURL = baseURL.appendingPathComponent("api/v3/recordings/\(recording.id)/playlist.m3u8")
            }

            var extraHeaders: [String: String] = [:]
            if let cookieValue = sessionCookie {
                extraHeaders["Cookie"] = "xg2g_session=\(cookieValue)"
            }

            // Ensure HLS playlist is ready on backend before handing to AVPlayer (prevents -1008 on first prepare)
            if streamURL.path.hasSuffix(".m3u8") {
                for _ in 0..<10 {
                    var req = URLRequest(url: streamURL)
                    req.httpMethod = "HEAD"
                    if let cookieValue = sessionCookie {
                        req.setValue("xg2g_session=\(cookieValue)", forHTTPHeaderField: "Cookie")
                    }
                    if let (_, response) = try? await URLSession.shared.data(for: req),
                       let http = response as? HTTPURLResponse, http.statusCode == 200 {
                        break
                    }
                    try? await Task.sleep(nanoseconds: 800_000_000)
                }
            }

            let item = PlayerAssetLoader.makePlayerItem(url: streamURL, baseURL: baseURL, extraHeaders: extraHeaders)
            let p = AVPlayer(playerItem: item)

            await MainActor.run {
                // Add periodic time observer to track resume progress
                let progressCallback = self.onProgressUpdate
                self.timeObserver = p.addPeriodicTimeObserver(
                    forInterval: CMTime(seconds: 5, preferredTimescale: 1),
                    queue: .main
                ) { [weak p] time in
                    guard let p, let duration = p.currentItem?.duration.seconds, duration > 0 else { return }
                    Task { @MainActor in
                        progressCallback(time.seconds, duration)
                    }
                }

                self.statusObserver = item.observe(\.status, options: [.new]) { [weak p] observedItem, _ in
                    Task { @MainActor [weak p] in
                        guard let p else { return }
                        if observedItem.status == .readyToPlay {
                            if let initPos = self.initialPosition, initPos > 5 {
                                let targetTime = CMTime(seconds: initPos, preferredTimescale: 600)
                                p.seek(to: targetTime, toleranceBefore: .zero, toleranceAfter: .zero) { [weak p] finished in
                                    if finished {
                                        p?.play()
                                    }
                                }
                            } else {
                                p.play()
                            }
                            withAnimation(.easeInOut(duration: 0.35)) {
                                self.isPreparing = false
                            }
                        } else if observedItem.status == .failed {
                            print("[RecordingPlayer] ❌ AVPlayerItem failed: \(String(describing: observedItem.error))")
                            withAnimation(.easeInOut(duration: 0.35)) {
                                self.errorMessage = observedItem.error?.localizedDescription ?? "Wiedergabefehler"
                                self.isPreparing = false
                            }
                        }
                    }
                }

                self.player = p
            }
        }
    }

    private func cleanup() {
        if let player, let observer = timeObserver {
            player.removeTimeObserver(observer)
            timeObserver = nil
        }
        statusObserver?.invalidate()
        statusObserver = nil
        player?.pause()
        AudioSessionManager.shared.deactivate()
    }
}
