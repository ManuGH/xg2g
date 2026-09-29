// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import AVKit
import SwiftUI
import UIKit

/// Pure decision policy for recording player item status and seek completion transitions.
/// Extracted to enable deterministic unit testing without requiring live AVPlayerItem KVO observer execution.
enum RecordingPlaybackAction: Equatable {
    case pauseAndDiscard
    case seek(Double)
    case play
    case handleFailure(String)
    case ignore
}

struct RecordingPlaybackDecision {
    static func decideStatusAction(
        status: AVPlayerItem.Status,
        error: Error?,
        sessionToken: UUID,
        activeToken: UUID?,
        startPosition: Double?
    ) -> RecordingPlaybackAction {
        guard activeToken == sessionToken else {
            return .pauseAndDiscard
        }
        switch status {
        case .readyToPlay:
            if let pos = startPosition, pos > 5 {
                return .seek(pos)
            } else {
                return .play
            }
        case .failed:
            let msg = error?.localizedDescription ?? "Wiedergabefehler"
            return .handleFailure(msg)
        default:
            return .ignore
        }
    }

    static func decideSeekCompletionAction(
        sessionToken: UUID,
        activeToken: UUID?,
        finished: Bool
    ) -> RecordingPlaybackAction {
        guard activeToken == sessionToken else {
            return .pauseAndDiscard
        }
        return finished ? .play : .ignore
    }
}

/// 100% Native Apple iOS Video Player for remote DVR recordings.
/// Uses Apple's system `AVPlayerViewController` for pixel-perfect edge-to-edge
/// scaling, native pinch-to-zoom, AirPlay, Picture-in-Picture, scrubbing,
/// and automatic server resume progress tracking.
struct RecordingPlayerScreen: View {

    let recording: Recording
    let serverAddress: ServerAddress
    var initialPosition: Double? = nil
    let sessionToken: UUID
    var model: AppModel? = nil
    var playbackManager: PlaybackManager? = nil
    var onProgressUpdate: @Sendable @MainActor (Double, Double) -> Void = { _, _ in }

    private var activeManager: PlaybackManager? {
        playbackManager ?? model?.playbackManager
    }

    @Environment(\.dismiss) private var dismiss
    @State private var player: AVPlayer?
    @State private var timeObserver: Any?
    @State private var statusObserver: NSKeyValueObservation?
    @State private var isPreparing = true
    @State private var errorMessage: String? = nil
    @State private var verticalDragOffset: CGFloat = 0
    @State private var isDraggingDown: Bool = false

    private var totalDuration: Double {
        let recDur = Double(recording.durationSeconds)
        return max(recDur, 1)
    }

    private func minimizePlayer() {
        verticalDragOffset = 0
        isDraggingDown = false
        Haptics.shared.impact(.medium)
        model?.playbackManager.minimize()
        dismiss()
    }

    var body: some View {
        ZStack(alignment: .topLeading) {
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
                            minimizePlayer()
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

                // Top Left Quick Minimize Button Overlay
                Button {
                    minimizePlayer()
                } label: {
                    Image(systemName: "chevron.down")
                        .font(.system(size: 14, weight: .bold))
                        .foregroundStyle(.white)
                        .frame(width: 36, height: 36)
                        .background(.ultraThinMaterial, in: Circle())
                        .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                }
                .buttonStyle(.plain)
                .frame(width: 44, height: 44)
                .padding(.leading, 16)
                .padding(.top, 12)
                .zIndex(20)
            } else {
                errorStateView
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .ignoresSafeArea(.all)
        .offset(y: max(0, verticalDragOffset))
        .scaleEffect(isDraggingDown ? max(0.85, 1.0 - (verticalDragOffset / 1200)) : 1.0)
        .clipShape(RoundedRectangle(cornerRadius: isDraggingDown ? min(32, verticalDragOffset / 4) : 0, style: .continuous))
        .animation(.interactiveSpring(response: 0.25, dampingFraction: 0.85), value: verticalDragOffset)
        #if !os(tvOS)
        .gesture(
            DragGesture(minimumDistance: 20)
                .onChanged { value in
                    if value.translation.height > 0 && abs(value.translation.height) > abs(value.translation.width) {
                        isDraggingDown = true
                        verticalDragOffset = value.translation.height
                    }
                }
                .onEnded { value in
                    if isDraggingDown {
                        if value.translation.height > 80 || value.predictedEndTranslation.height > 160 {
                            minimizePlayer()
                        } else {
                            withAnimation(.spring(response: 0.3, dampingFraction: 0.82)) {
                                verticalDragOffset = 0
                                isDraggingDown = false
                            }
                        }
                    }
                }
        )
        #endif
        .onAppear {
            let token = self.sessionToken
            guard let manager = activeManager else { return }
            let registered = manager.registerRecordingCleanup(for: token) {
                self.cleanup()
            }
            guard registered else {
                // Token rejected: late mount from superseded session.
                // Do NOT call setupPlayer() and do NOT activate audio session.
                return
            }
            setupPlayer()
        }
        .onDisappear {
            if activeManager?.presentationMode != .miniplayer {
                activeManager?.unregisterRecordingCleanup(for: sessionToken)
                cleanup()
            }
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
        let token = self.sessionToken
        guard let manager = activeManager,
              manager.activateRecordingAudioSession(for: token) else {
            return
        }

        if manager.activeRecordingSessionToken == token,
           let existing = manager.recordingPlayer {
            self.player = existing
            self.isPreparing = false
            return
        }
        Task {
            guard self.activeManager?.activeRecordingSessionToken == token else { return }

            var sessionCookie: String? = nil
            var negotiatedPath: String? = nil
            if let model {
                do {
                    sessionCookie = try await model.mediaSessionCookie()
                } catch {
                    print("[RecordingPlayer] ⚠️ Could not acquire media session cookie: \(error)")
                }
                guard self.activeManager?.activeRecordingSessionToken == token else { return }

                do {
                    negotiatedPath = try await model.recordingPlaybackUrl(for: recording.id)
                } catch {
                    print("[RecordingPlayer] ⚠️ Could not negotiate stream-info: \(error)")
                }
                guard self.activeManager?.activeRecordingSessionToken == token else { return }
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
                    guard self.activeManager?.activeRecordingSessionToken == token else { return }
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
                guard self.activeManager?.activeRecordingSessionToken == token else { return }
            }

            guard self.activeManager?.activeRecordingSessionToken == token else { return }

            TelemetryServer.shared.log("[RecordingPlayer] ▶️ Loading '\(recording.title)' (\(recording.id)) URL: \(streamURL.absoluteString)")

            let item = PlayerAssetLoader.makePlayerItem(url: streamURL, baseURL: serverAddress.rootURL, extraHeaders: extraHeaders)
            let p = AVPlayer(playerItem: item)

            await MainActor.run {
                guard self.activeManager?.activeRecordingSessionToken == token else {
                    p.pause()
                    return
                }

                let progressCallback = self.onProgressUpdate

                // Periodic progress tracking (every 1.0s) for server resume state updates
                self.timeObserver = p.addPeriodicTimeObserver(
                    forInterval: CMTime(seconds: 1.0, preferredTimescale: 600),
                    queue: .main
                ) { [weak p, weak manager = self.activeManager] time in
                    Task { @MainActor [weak p, weak manager] in
                        guard let p, manager?.activeRecordingSessionToken == token else { return }
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

                self.statusObserver = item.observe(\.status, options: [.new]) { [weak p, weak manager = self.activeManager] observedItem, _ in
                    Task { @MainActor [weak p, weak manager] in
                        guard let p else { return }
                        let startPos = self.initialPosition ?? self.recording.serverResumePos
                        let action = RecordingPlaybackDecision.decideStatusAction(
                            status: observedItem.status,
                            error: observedItem.error,
                            sessionToken: token,
                            activeToken: manager?.activeRecordingSessionToken,
                            startPosition: startPos
                        )

                        switch action {
                        case .pauseAndDiscard:
                            p.pause()
                        case .seek(let pos):
                            TelemetryServer.shared.log("[RecordingPlayer] ✅ readyToPlay '\(self.recording.title)'")
                            let targetTime = CMTime(seconds: pos, preferredTimescale: 600)
                            p.seek(to: targetTime, toleranceBefore: .zero, toleranceAfter: .zero) { [weak p, weak manager] finished in
                                Task { @MainActor [weak p, weak manager] in
                                    let completionAction = RecordingPlaybackDecision.decideSeekCompletionAction(
                                        sessionToken: token,
                                        activeToken: manager?.activeRecordingSessionToken,
                                        finished: finished
                                    )
                                    switch completionAction {
                                    case .pauseAndDiscard:
                                        p?.pause()
                                    case .play:
                                        p?.play()
                                    default:
                                        break
                                    }
                                }
                            }
                            withAnimation(.easeInOut(duration: 0.35)) {
                                self.isPreparing = false
                            }
                        case .play:
                            TelemetryServer.shared.log("[RecordingPlayer] ✅ readyToPlay '\(self.recording.title)'")
                            p.play()
                            withAnimation(.easeInOut(duration: 0.35)) {
                                self.isPreparing = false
                            }
                        case .handleFailure(let errStr):
                            TelemetryServer.shared.log("[RecordingPlayer] ❌ AVPlayerItem failed: \(errStr)")
                            print("[RecordingPlayer] ❌ AVPlayerItem failed: \(String(describing: observedItem.error))")
                            withAnimation(.easeInOut(duration: 0.35)) {
                                self.errorMessage = errStr
                                self.isPreparing = false
                            }
                        case .ignore:
                            break
                        }
                    }
                }

                let attached = self.activeManager?.setRecordingPlayer(p, for: token) ?? false
                if attached {
                    self.player = p
                } else {
                    // Attachment rejected: remove observers, pause, and discard player
                    p.pause()
                    if let timeObserver {
                        p.removeTimeObserver(timeObserver)
                        self.timeObserver = nil
                    }
                    self.statusObserver?.invalidate()
                    self.statusObserver = nil
                }
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
        activeManager?.clearRecordingPlayer(for: sessionToken, ownedBy: player)
        activeManager?.deactivateRecordingAudioSession(for: sessionToken)
    }
}
