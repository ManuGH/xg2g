// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import AVKit
import SwiftUI
import UIKit

/// 100% Native Apple iOS Video Player for remote DVR recordings.
/// Uses Apple's system `AVPlayerViewController` for pixel-perfect edge-to-edge
/// scaling, native pinch-to-zoom, AirPlay, Picture-in-Picture, scrubbing,
/// and automatic server resume progress tracking.
struct RecordingPlayerScreen: View {

    let recording: Recording
    let serverAddress: ServerAddress
    var initialPosition: Double? = nil
    var model: AppModel? = nil
    var onProgressUpdate: @Sendable @MainActor (Double, Double) -> Void = { _, _ in }

    @Environment(\.dismiss) private var dismiss
    @State private var player: AVPlayer?
    @State private var timeObserver: Any?
    @State private var statusObserver: NSKeyValueObservation?
    @State private var isPreparing = true
    @State private var errorMessage: String? = nil

    private var totalDuration: Double {
        let recDur = Double(recording.durationSeconds)
        return max(recDur, 1)
    }

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()

            if errorMessage == nil {
                if let player {
                    // 100% Native Apple System Player with built-in controls,
                    // PiP, AirPlay, native pinch-to-zoom, and scrubbing.
                    NativeVideoPlayerView(
                        player: player,
                        videoGravity: .resizeAspect,
                        showsPlaybackControls: true,
                        onDismiss: {
                            cleanup()
                            model?.playbackManager.stop()
                            dismiss()
                        }
                    )
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                    .ignoresSafeArea(.all)
                    .transition(.opacity)
                }

                // Cinematic Loading Stage while stream initializes
                if player == nil || isPreparing {
                    cinematicLoadingStage
                        .transition(.opacity)
                }
            } else {
                errorStateView
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .ignoresSafeArea(.all)
        .onAppear {
            model?.playbackManager.registerRecordingCleanup {
                self.cleanup()
            }
            setupPlayer()
        }
        .onDisappear {
            model?.playbackManager.unregisterRecordingCleanup()
            cleanup()
        }
    }

    // MARK: - Cinematic Loading Stage

    @ViewBuilder
    private var cinematicLoadingStage: some View {
        let palette = RecordingArtworkTheme.palette(for: recording)

        ZStack {
            Theme.Colors.bgVideoStage.ignoresSafeArea()

            RadialGradient(
                colors: [palette.accent.opacity(0.25), Color.clear],
                center: .center,
                startRadius: 20,
                endRadius: 380
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

                // Center Focus Card
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
                .font(.system(size: 40))
                .foregroundStyle(Color.red)

            Text(errorMessage ?? "Aufnahme konnte nicht geladen werden")
                .font(.system(size: 15, weight: .medium))
                .foregroundStyle(Theme.Colors.textPrimary)
                .multilineTextAlignment(.center)
                .padding(.horizontal, 32)

            Button("Schließen") {
                cleanup()
                model?.playbackManager.stop()
                dismiss()
            }
            .padding(.horizontal, 20)
            .padding(.vertical, 10)
            .background(.ultraThinMaterial, in: Capsule())
            .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1))
        }
    }

    // MARK: - Player Setup

    private func setupPlayer() {
        AudioSessionManager.shared.configureForPlayback()
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

            // One resolution, owned by the transport: what the backend named,
            // scope-checked against this deployment, with the canonical
            // playlist path as the fallback.
            guard let media = RecordingPlayback.resolve(
                address: serverAddress,
                recordingID: recording.id,
                negotiatedPath: negotiatedPath,
                sessionCookie: sessionCookie
            ) else {
                await MainActor.run {
                    errorMessage = "Ungültige Server-Adresse"
                    isPreparing = false
                }
                return
            }
            let streamURL = media.url

            // The credential travels as cookies the transport built; this layer
            // never spells the cookie's name or decides its scope.
            for cookie in media.cookies {
                HTTPCookieStorage.shared.setCookie(cookie)
                HTTPCookieStorage.shared.setCookies([cookie], for: serverAddress.rootURL, mainDocumentURL: nil)
                HTTPCookieStorage.shared.setCookies([cookie], for: streamURL, mainDocumentURL: nil)
            }

            let extraHeaders = HTTPCookie.requestHeaderFields(with: media.cookies)

            // Ensure HLS playlist is ready on backend before handing to AVPlayer.
            // The wait itself belongs to the transport: it owns the method, the
            // cookie and the retry budget.
            if streamURL.path.hasSuffix(".m3u8") {
                _ = await MediaFetcher.waitUntilServable(url: streamURL, sessionCookie: sessionCookie)
            }

            TelemetryServer.shared.log("[RecordingPlayer] ▶️ Loading '\(recording.title)' (\(recording.id)) URL: \(streamURL.absoluteString)")

            let item = PlayerAssetLoader.makePlayerItem(url: streamURL, baseURL: serverAddress.rootURL, extraHeaders: extraHeaders)
            let p = AVPlayer(playerItem: item)

            await MainActor.run {
                let progressCallback = self.onProgressUpdate

                // Periodic progress tracking (every 1.0s) for server resume state updates
                self.timeObserver = p.addPeriodicTimeObserver(
                    forInterval: CMTime(seconds: 1.0, preferredTimescale: 600),
                    queue: .main
                ) { [weak p] time in
                    Task { @MainActor [weak p] in
                        guard let p else { return }
                        let sec = time.seconds
                        if sec.isFinite && !sec.isNaN {
                            if let itemDur = p.currentItem?.duration.seconds, itemDur > 0 {
                                progressCallback(sec, itemDur)
                            } else if self.recording.durationSeconds > 0 {
                                progressCallback(sec, Double(self.recording.durationSeconds))
                            }
                        }
                    }
                }

                self.statusObserver = item.observe(\.status, options: [.new]) { [weak p] observedItem, _ in
                    Task { @MainActor [weak p] in
                        guard let p else { return }
                        if observedItem.status == .readyToPlay {
                            TelemetryServer.shared.log("[RecordingPlayer] ✅ readyToPlay '\(self.recording.title)'")
                            let startPos = self.initialPosition ?? self.recording.serverResumePos
                            if let pos = startPos, pos > 5 {
                                let targetTime = CMTime(seconds: pos, preferredTimescale: 600)
                                p.seek(to: targetTime, toleranceBefore: .zero, toleranceAfter: .zero) { [weak p] finished in
                                    Task { @MainActor [weak p] in
                                        if finished {
                                            p?.play()
                                        }
                                    }
                                }
                            } else {
                                p.play()
                            }

                            withAnimation(.easeInOut(duration: 0.35)) {
                                self.isPreparing = false
                            }
                        } else if observedItem.status == .failed {
                            let errStr = observedItem.error?.localizedDescription ?? "Wiedergabefehler"
                            TelemetryServer.shared.log("[RecordingPlayer] ❌ AVPlayerItem failed: \(errStr)")
                            print("[RecordingPlayer] ❌ AVPlayerItem failed: \(String(describing: observedItem.error))")
                            withAnimation(.easeInOut(duration: 0.35)) {
                                self.errorMessage = errStr
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
