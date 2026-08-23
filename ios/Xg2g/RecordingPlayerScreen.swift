// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import AVKit
import SwiftUI
import UIKit

/// Fullscreen Plex/Infuse-style VOD player for remote DVR recordings with
/// dedicated start & end timeline, interactive scrubber, quick-skip controls,
/// cinematic loading stage, and automatic resume tracking.
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

    // Playback & HUD State
    @State private var isPlaying = true
    @State private var currentTime: Double = 0
    @State private var durationOverride: Double = 0
    @State private var isScrubbing = false
    @State private var isSeeking = false
    @State private var scrubTime: Double = 0
    @State private var showControls = true
    @State private var showRemainingTime = true
    @State private var autoHideTask: Task<Void, Never>?
    @State private var skipFeedback: (text: String, isForward: Bool)? = nil
    @State private var hideSkipFeedbackTask: Task<Void, Never>?
    @State private var dragOffsetY: CGFloat = 0
    @State private var isDraggingToDismiss = false

    // Video Customization & Stats State
    @State private var videoGravity: AVLayerVideoGravity = .resizeAspect
    @State private var playbackRate: Float = 1.0
    @State private var showStatsOverlay = false

    // Live Telemetry & Measured Metrics (AVPlayer & Hardware Engine)
    @State private var measuredResolution: String = "1920 × 1080"
    @State private var measuredBitrate: String = "—"
    @State private var droppedFramesCount: Int = 0
    @State private var transferredBytes: String = "—"

    private var totalDuration: Double {
        let recDur = Double(recording.durationSeconds)
        if durationOverride > 0 {
            return max(recDur, durationOverride)
        }
        return max(recDur, 1)
    }

    private var displayCurrentTime: Double {
        isScrubbing ? scrubTime : currentTime
    }

    var body: some View {
        ZStack {
            // Background Dimming Backdrop that fades as you drag down
            Color.black
                .opacity(1.0 - Double(max(0, dragOffsetY) / 600.0))
                .ignoresSafeArea()

            let base = serverAddress.starts(with: "http") ? serverAddress : "https://\(serverAddress)"
            if errorMessage == nil && URL(string: base) != nil {
                if let player {
                    // Video Layer (Native AVPlayer without system controls)
                    NativeVideoPlayerView(
                        player: player,
                        videoGravity: videoGravity,
                        showsPlaybackControls: false,
                        onDismiss: {
                            cleanup()
                            dismiss()
                        }
                    )
                    .ignoresSafeArea()
                    .transition(.opacity)

                    // Touch Surface for Gestures & Controls Toggle
                    gestureSurface

                    // Plex/Infuse-Style VOD HUD Overlay
                    if showControls && !isPreparing {
                        vodHUDOverlay
                            .transition(.opacity.combined(with: .scale(scale: 0.98)))
                    }

                    // Floating Stream & Farbraum Stats Overlay
                    if showStatsOverlay {
                        VStack {
                            HStack {
                                Spacer()
                                statsOverlayView
                            }
                            Spacer()
                        }
                        .transition(.opacity.combined(with: .scale(scale: 0.95)))
                    }

                    // Double-Tap Skip Feedback Ripple
                    if let feedback = skipFeedback {
                        skipFeedbackBadge(feedback.text, isForward: feedback.isForward)
                            .transition(.scale.combined(with: .opacity))
                    }
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
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .ignoresSafeArea(.all)
        .offset(y: max(0, dragOffsetY))
        .scaleEffect(dragOffsetY > 0 ? max(0.82, 1.0 - (dragOffsetY / 1100.0)) : 1.0)
        .clipShape(RoundedRectangle(cornerRadius: dragOffsetY > 0 ? 28 : 0, style: .continuous))
        .shadow(color: Color.black.opacity(dragOffsetY > 0 ? 0.6 : 0), radius: 24, y: 12)
        .gesture(
            DragGesture(minimumDistance: 20)
                .onChanged { value in
                    if value.translation.height > 0 && abs(value.translation.height) > abs(value.translation.width) && !isScrubbing {
                        isDraggingToDismiss = true
                        dragOffsetY = value.translation.height
                    }
                }
                .onEnded { value in
                    if isDraggingToDismiss {
                        if value.translation.height > 120 || value.predictedEndTranslation.height > 250 {
                            triggerHaptic(.light)
                            withAnimation(.easeOut(duration: 0.22)) {
                                dragOffsetY = 900
                            }
                            Task { @MainActor in
                                try? await Task.sleep(nanoseconds: 180_000_000)
                                cleanup()
                                dismiss()
                            }
                        } else {
                            withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) {
                                dragOffsetY = 0
                            }
                        }
                        isDraggingToDismiss = false
                    }
                }
        )
        .animation(.easeInOut(duration: 0.25), value: showControls)
        .animation(.easeInOut(duration: 0.35), value: player != nil)
        .animation(.easeInOut(duration: 0.35), value: isPreparing)
        .onAppear {
            setupPlayer()
        }
        .onDisappear {
            cleanup()
        }
    }

    // MARK: - Gesture Surface

    @ViewBuilder
    private var gestureSurface: some View {
        GeometryReader { geo in
            Color.clear
                .contentShape(Rectangle())
                .onTapGesture {
                    triggerHaptic(.light)
                    withAnimation(.easeInOut(duration: 0.2)) {
                        showControls.toggle()
                    }
                    if showControls {
                        resetAutoHideTimer()
                    }
                }
                .simultaneousGesture(
                    SpatialTapGesture(count: 2)
                        .onEnded { location in
                            let isRight = location.location.x > (geo.size.width / 2)
                            if isRight {
                                skip(seconds: 30)
                            } else {
                                skip(seconds: -15)
                            }
                        }
                )
                .simultaneousGesture(
                    MagnificationGesture()
                        .onEnded { scale in
                            if scale > 1.15 && videoGravity != .resizeAspectFill {
                                triggerHaptic(.medium)
                                videoGravity = .resizeAspectFill
                                showZoomFeedback("Ans iPhone angepasst")
                            } else if scale < 0.85 && videoGravity != .resizeAspect {
                                triggerHaptic(.medium)
                                videoGravity = .resizeAspect
                                showZoomFeedback("Standard (16:9)")
                            }
                        }
                )
        }
        .ignoresSafeArea()
    }

    // MARK: - Plex/Infuse Style VOD HUD Overlay

    @ViewBuilder
    private var vodHUDOverlay: some View {
        let palette = RecordingArtworkTheme.palette(for: recording)

        ZStack {
            // Subtle top and bottom vignette gradients for high contrast readability
            VStack(spacing: 0) {
                LinearGradient(
                    colors: [Color.black.opacity(0.85), Color.black.opacity(0.4), Color.clear],
                    startPoint: .top,
                    endPoint: .bottom
                )
                .frame(height: 140)
                .allowsHitTesting(false)

                Spacer()

                LinearGradient(
                    colors: [Color.clear, Color.black.opacity(0.5), Color.black.opacity(0.92)],
                    startPoint: .top,
                    endPoint: .bottom
                )
                .frame(height: 180)
                .allowsHitTesting(false)
            }
            .ignoresSafeArea()

            VStack(spacing: 0) {
                // Top Header Bar
                topHeaderBar(palette: palette)
                    .padding(.horizontal, 20)
                    .padding(.top, 16)

                Spacer()

                // Center Quick Playback Controls
                centerPlaybackControls(palette: palette)

                Spacer()

                // Bottom VOD Scrubber & Timeline Bar
                bottomTimelineBar(palette: palette)
                    .padding(.horizontal, 24)
                    .padding(.bottom, 28)
            }
        }
    }

    // MARK: - Top Header Bar

    @ViewBuilder
    private func topHeaderBar(palette: RecordingArtworkTheme.Palette) -> some View {
        VStack(spacing: 12) {
            // Visual Grabber Pill for Swipe-Down
            Capsule()
                .fill(Color.white.opacity(0.35))
                .frame(width: 38, height: 5)

            HStack(alignment: .center, spacing: 14) {
                // Dismiss Button
                Button {
                    triggerHaptic(.light)
                    withAnimation(.easeOut(duration: 0.22)) {
                        dragOffsetY = 900
                    }
                    Task { @MainActor in
                        try? await Task.sleep(nanoseconds: 180_000_000)
                        cleanup()
                        dismiss()
                    }
                } label: {
                    Image(systemName: "chevron.down")
                        .font(.system(size: 16, weight: .bold))
                        .foregroundStyle(Theme.Colors.textPrimary)
                        .frame(width: 42, height: 42)
                        .background(.ultraThinMaterial, in: Circle())
                        .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1))
                }
                .buttonStyle(.plain)

                // Title & Channel Subtitle Stack
                VStack(alignment: .leading, spacing: 3) {
                    Text(recording.title)
                        .font(.system(size: 17, weight: .bold))
                        .foregroundStyle(Theme.Colors.textPrimary)
                        .lineLimit(1)

                    HStack(spacing: 6) {
                        Text(recording.formattedDate)
                            .font(.system(size: 12, weight: .medium, design: .monospaced))
                            .foregroundStyle(Theme.Colors.textTertiary)

                        Text("•")
                            .foregroundStyle(Theme.Colors.textDisabled)

                        Text(recording.formattedDuration)
                            .font(.system(size: 12, weight: .semibold, design: .monospaced))
                            .foregroundStyle(palette.accent)
                    }
                }

                Spacer()

                // 1-Tap Aspect Ratio Toggle Button (Identisch zum Live-Player: Standard vs Füllen)
                Button {
                    cycleAspectPreset()
                } label: {
                    HStack(spacing: 4) {
                        Image(systemName: videoGravity == .resizeAspectFill ? "arrow.up.left.and.arrow.down.right" : "aspectratio")
                            .font(.system(size: 11, weight: .bold))
                        Text(videoGravity == .resizeAspectFill ? "Füllen" : "Standard")
                            .font(.system(size: 11, weight: .bold, design: .monospaced))
                    }
                    .foregroundStyle(.white)
                    .padding(.horizontal, 9)
                    .padding(.vertical, 6)
                    .background(.ultraThinMaterial, in: Capsule())
                    .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                }
                .buttonStyle(.plain)

                // AirPlay Button
                AirPlayButton()
                    .frame(width: 36, height: 36)
                    .background(.ultraThinMaterial, in: Circle())
                    .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))

                // Quick Settings Menu (Video-Größe, Farbraum, Geschwindigkeit)
                Menu {
                    // Video-Größe / Aspect Ratio
                    Section("Video-Größe") {
                        Button {
                            videoGravity = .resizeAspect
                            triggerHaptic(.light)
                        } label: {
                            Label("Standard (16:9 • Nicht abgeschnitten)", systemImage: videoGravity == .resizeAspect ? "checkmark" : "")
                        }

                        Button {
                            videoGravity = .resizeAspectFill
                            triggerHaptic(.light)
                        } label: {
                            Label("Bildschirm füllen (Zoom • Randlos)", systemImage: videoGravity == .resizeAspectFill ? "checkmark" : "")
                        }

                        Button {
                            videoGravity = .resize
                            triggerHaptic(.light)
                        } label: {
                            Label("Strecken (Vollbild ohne Abschneiden)", systemImage: videoGravity == .resize ? "checkmark" : "")
                        }
                    }

                    // Wiedergabegeschwindigkeit
                    Section("Geschwindigkeit") {
                        ForEach([0.5, 0.75, 1.0, 1.25, 1.5, 2.0], id: \.self) { rate in
                            Button {
                                setPlaybackRate(Float(rate))
                            } label: {
                                Label(
                                    rate == 1.0 ? "1.0x (Normal)" : "\(String(format: "%.2gx", rate))",
                                    systemImage: playbackRate == Float(rate) ? "checkmark" : ""
                                )
                            }
                        }
                    }

                    // Farbraum & Stream-Informationen
                    Section("Stream & Farbraum") {
                        Button {
                            withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) {
                                showStatsOverlay.toggle()
                            }
                        } label: {
                            Label(
                                showStatsOverlay ? "Details ausblenden" : "Farbraum & Details anzeigen",
                                systemImage: "sparkles.tv"
                            )
                        }
                    }
                } label: {
                    Image(systemName: "slider.horizontal.3")
                        .font(.system(size: 15, weight: .bold))
                        .foregroundStyle(Theme.Colors.textPrimary)
                        .frame(width: 42, height: 42)
                        .background(.ultraThinMaterial, in: Circle())
                        .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1))
                }
                .buttonStyle(.plain)

                // DVR / VOD Badge
                HStack(spacing: 6) {
                    Circle()
                        .fill(palette.accent)
                        .frame(width: 6, height: 6)

                    Text("DVR")
                        .font(.system(size: 11, weight: .bold, design: .rounded))
                        .foregroundStyle(Theme.Colors.textPrimary)
                }
                .padding(.horizontal, 10)
                .padding(.vertical, 5)
                .background(.ultraThinMaterial, in: Capsule())
                .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
            }
        }
    }

    // MARK: - Center Playback Controls

    @ViewBuilder
    private func centerPlaybackControls(palette: RecordingArtworkTheme.Palette) -> some View {
        HStack(spacing: 44) {
            // Skip Back -15s
            Button {
                skip(seconds: -15)
            } label: {
                ZStack {
                    Circle()
                        .fill(.ultraThinMaterial)
                        .frame(width: 54, height: 54)
                        .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1))

                    VStack(spacing: 1) {
                        Image(systemName: "gobackward.15")
                            .font(.system(size: 22, weight: .semibold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                    }
                }
            }
            .buttonStyle(.plain)

            // Play / Pause Toggle Orb
            Button {
                togglePlayPause()
            } label: {
                ZStack {
                    Circle()
                        .fill(palette.gradient)
                        .frame(width: 74, height: 74)
                        .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1.5))
                        .shadow(color: palette.accent.opacity(0.4), radius: 18, y: 6)

                    Image(systemName: isPlaying ? "pause.fill" : "play.fill")
                        .font(.system(size: 28, weight: .bold))
                        .foregroundStyle(.white)
                        .offset(x: isPlaying ? 0 : 2)
                }
            }
            .buttonStyle(.plain)

            // Skip Forward +30s
            Button {
                skip(seconds: 30)
            } label: {
                ZStack {
                    Circle()
                        .fill(.ultraThinMaterial)
                        .frame(width: 54, height: 54)
                        .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1))

                    VStack(spacing: 1) {
                        Image(systemName: "goforward.30")
                            .font(.system(size: 22, weight: .semibold))
                            .foregroundStyle(Theme.Colors.textPrimary)
                    }
                }
            }
            .buttonStyle(.plain)
        }
    }

    // MARK: - Bottom VOD Timeline & Transport Bar (Plex Style)

    @ViewBuilder
    private func bottomTimelineBar(palette: RecordingArtworkTheme.Palette) -> some View {
        VStack(spacing: 10) {
            // Interactive Scrubber Track
            GeometryReader { geo in
                let width = geo.size.width
                let progress = totalDuration > 0 ? min(max(displayCurrentTime / totalDuration, 0), 1) : 0

                ZStack(alignment: .leading) {
                    // Track Rail Background
                    Capsule()
                        .fill(Color.white.opacity(0.2))
                        .frame(height: isScrubbing ? 8 : 5)

                    // Active Progress Fill
                    Capsule()
                        .fill(
                            LinearGradient(
                                colors: [palette.accent, palette.accent.opacity(0.85)],
                                startPoint: .leading,
                                endPoint: .trailing
                            )
                        )
                        .frame(width: max(0, width * CGFloat(progress)), height: isScrubbing ? 8 : 5)

                    // Scrubber Knob Thumb
                    Circle()
                        .fill(Color.white)
                        .frame(width: isScrubbing ? 20 : 14, height: isScrubbing ? 20 : 14)
                        .shadow(color: Color.black.opacity(0.5), radius: 4, y: 2)
                        .offset(x: max(0, min(width * CGFloat(progress) - (isScrubbing ? 10 : 7), width - (isScrubbing ? 20 : 14))))
                }
                .contentShape(Rectangle())
                .gesture(
                    DragGesture(minimumDistance: 0)
                        .onChanged { value in
                            if !isScrubbing {
                                triggerHaptic(.light)
                                isScrubbing = true
                            }
                            let fraction = min(max(0, Double(value.location.x / width)), 1.0)
                            scrubTime = fraction * totalDuration
                            resetAutoHideTimer()
                        }
                        .onEnded { value in
                            let fraction = min(max(0, Double(value.location.x / width)), 1.0)
                            let targetSec = fraction * totalDuration
                            seek(to: targetSec)
                            isScrubbing = false
                            resetAutoHideTimer()
                        }
                )
            }
            .frame(height: 22)

            // Start & End Timestamps (Left: Elapsed, Right: Total / Remaining)
            HStack {
                // Elapsed Time (Start)
                Text(formatDuration(displayCurrentTime))
                    .font(.system(size: 13, weight: .medium, design: .monospaced))
                    .foregroundStyle(Theme.Colors.textPrimary)

                Spacer()

                // End Time / Remaining Duration (Tap to Toggle like Plex & Apple TV+)
                Button {
                    triggerSelectionHaptic()
                    showRemainingTime.toggle()
                    resetAutoHideTimer()
                } label: {
                    Text(showRemainingTime ? "-\(formatDuration(max(0, totalDuration - displayCurrentTime)))" : formatDuration(totalDuration))
                        .font(.system(size: 13, weight: .semibold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textSecondary)
                }
                .buttonStyle(.plain)
            }
        }
    }

    // MARK: - Double-Tap Skip Feedback Badge

    @ViewBuilder
    private func skipFeedbackBadge(_ text: String, isForward: Bool) -> some View {
        HStack(spacing: 8) {
            Image(systemName: isForward ? "goforward.30" : "gobackward.15")
                .font(.system(size: 24, weight: .bold))
            Text(text)
                .font(.system(size: 18, weight: .bold, design: .rounded))
        }
        .foregroundStyle(.white)
        .padding(.horizontal, 24)
        .padding(.vertical, 14)
        .background(.ultraThinMaterial, in: Capsule())
        .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1.2))
        .shadow(color: Color.black.opacity(0.4), radius: 16, y: 4)
    }

    // MARK: - Cinematic Loading Stage

    @ViewBuilder
    private var cinematicLoadingStage: some View {
        let palette = RecordingArtworkTheme.palette(for: recording)

        ZStack {
            Theme.Colors.bgVideoStage.ignoresSafeArea()

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

    // MARK: - Stream & Farbraum Stats Overlay (Infuse / Apple Developer HUD Style)

    @ViewBuilder
    private var statsOverlayView: some View {
        let palette = RecordingArtworkTheme.palette(for: recording)
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Image(systemName: "sparkles.tv")
                    .font(.system(size: 13, weight: .bold))
                    .foregroundStyle(palette.accent)

                Text("STREAM & FARBRAUM DETAILS")
                    .font(.system(size: 10, weight: .bold, design: .monospaced))
                    .foregroundStyle(palette.accent)

                Spacer()

                Button {
                    withAnimation(.easeOut(duration: 0.2)) {
                        showStatsOverlay = false
                    }
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 16))
                        .foregroundStyle(Color.white.opacity(0.6))
                }
                .buttonStyle(.plain)
            }

            Divider().background(Color.white.opacity(0.2))

            statsRow(label: "Auflösung (Live)", value: measuredResolution, icon: "tv")
            statsRow(label: "Bandbreite", value: measuredBitrate, icon: "network")
            statsRow(label: "Dropped Frames", value: "\(droppedFramesCount)", icon: "square.stack.3d.up.slash")
            statsRow(label: "Geladen", value: transferredBytes, icon: "arrow.down.circle")
            statsRow(label: "Farbraum", value: "BT.709 (Rec. 709 / SDR)", icon: "circle.grid.2x1.left.filled")
            statsRow(label: "Video Codec", value: "H.264 / AVC (fMP4)", icon: "video.fill")
            statsRow(label: "Audio Codec", value: "AAC Stereo • 320 kbps", icon: "waveform")
            statsRow(label: "Server", value: serverAddress.replacingOccurrences(of: "http://", with: "").replacingOccurrences(of: "https://", with: ""), icon: "server.rack")
        }
        .padding(14)
        .frame(maxWidth: 320)
        .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        .overlay(
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1)
        )
        .shadow(color: Color.black.opacity(0.6), radius: 20, y: 8)
        .padding(.top, 70)
        .padding(.trailing, 16)
    }

    @ViewBuilder
    private func statsRow(label: String, value: String, icon: String) -> some View {
        HStack(spacing: 6) {
            Image(systemName: icon)
                .font(.system(size: 11))
                .foregroundStyle(Color.white.opacity(0.6))
                .frame(width: 14)

            Text(label + ":")
                .font(.system(size: 11, weight: .medium))
                .foregroundStyle(Color.white.opacity(0.7))

            Spacer()

            Text(value)
                .font(.system(size: 11, weight: .bold, design: .monospaced))
                .foregroundStyle(Color.white)
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

    // MARK: - Player Actions & Lifecycle

    private func togglePlayPause() {
        guard let player else { return }
        triggerHaptic(.medium)
        if isPlaying {
            player.pause()
            isPlaying = false
        } else {
            player.play()
            player.rate = playbackRate
            isPlaying = true
        }
        resetAutoHideTimer()
    }

    private func setPlaybackRate(_ rate: Float) {
        playbackRate = rate
        if isPlaying {
            player?.rate = rate
        }
        triggerHaptic(.light)

        // Show Visual Feedback
        hideSkipFeedbackTask?.cancel()
        withAnimation(.spring(response: 0.25, dampingFraction: 0.7)) {
            skipFeedback = (rate == 1.0 ? "1.0x" : "\(String(format: "%.2gx", rate))", true)
        }
        hideSkipFeedbackTask = Task {
            try? await Task.sleep(nanoseconds: 800_000_000)
            if !Task.isCancelled {
                withAnimation(.easeOut(duration: 0.2)) {
                    skipFeedback = nil
                }
            }
        }
    }

    private func showZoomFeedback(_ text: String) {
        hideSkipFeedbackTask?.cancel()
        withAnimation(.spring(response: 0.25, dampingFraction: 0.7)) {
            skipFeedback = (text, true)
        }
        hideSkipFeedbackTask = Task {
            try? await Task.sleep(nanoseconds: 1_000_000_000)
            if !Task.isCancelled {
                withAnimation(.easeOut(duration: 0.2)) {
                    skipFeedback = nil
                }
            }
        }
    }

    private func cycleAspectPreset() {
        triggerHaptic(.medium)
        if videoGravity == .resizeAspect {
            videoGravity = .resizeAspectFill
            showZoomFeedback("Bildschirm füllen")
        } else {
            videoGravity = .resizeAspect
            showZoomFeedback("Standard (16:9)")
        }
        resetAutoHideTimer()
    }

    private func skip(seconds: Double) {
        guard player != nil else { return }
        triggerHaptic(.light)
        let baseTime = isScrubbing ? scrubTime : currentTime
        let target = max(0, min(baseTime + seconds, totalDuration))
        seek(to: target)

        // Show Visual Feedback
        hideSkipFeedbackTask?.cancel()
        withAnimation(.spring(response: 0.25, dampingFraction: 0.7)) {
            skipFeedback = (seconds > 0 ? "+\(Int(seconds))s" : "\(Int(seconds))s", seconds > 0)
        }
        hideSkipFeedbackTask = Task {
            try? await Task.sleep(nanoseconds: 800_000_000)
            if !Task.isCancelled {
                withAnimation(.easeOut(duration: 0.2)) {
                    skipFeedback = nil
                }
            }
        }

        resetAutoHideTimer()
    }

    private func seek(to seconds: Double) {
        guard let player else { return }
        let clampedSec = max(0, min(seconds, totalDuration))
        TelemetryServer.shared.log("[RecordingPlayer] ⏩ Seek requested to \(String(format: "%.1f", clampedSec))s")
        isSeeking = true
        currentTime = clampedSec
        scrubTime = clampedSec

        let shouldResume = isPlaying
        let targetTime = CMTime(seconds: clampedSec, preferredTimescale: 600)
        let tolerance = CMTime(seconds: 2, preferredTimescale: 600)

        player.seek(to: targetTime, toleranceBefore: tolerance, toleranceAfter: tolerance) { [weak player] finished in
            Task { @MainActor in
                self.isSeeking = false
                TelemetryServer.shared.log("[RecordingPlayer] ⏩ Seek completed (finished=\(finished)) to \(String(format: "%.1f", clampedSec))s")
                if shouldResume {
                    player?.play()
                    self.isPlaying = true
                }
            }
        }
    }

    private func resetAutoHideTimer() {
        autoHideTask?.cancel()
        guard isPlaying else { return }
        autoHideTask = Task {
            try? await Task.sleep(nanoseconds: 4_500_000_000)
            if !Task.isCancelled {
                withAnimation(.easeInOut(duration: 0.25)) {
                    showControls = false
                }
            }
        }
    }

    private func triggerHaptic(_ type: UIImpactFeedbackGenerator.FeedbackStyle) {
        UIImpactFeedbackGenerator(style: type).impactOccurred()
    }

    private func triggerSelectionHaptic() {
        UISelectionFeedbackGenerator().selectionChanged()
    }

    private func formatDuration(_ seconds: Double) -> String {
        guard seconds.isFinite && !seconds.isNaN && seconds >= 0 else { return "00:00" }
        let total = Int(seconds)
        let hours = total / 3600
        let minutes = (total % 3600) / 60
        let secs = total % 60
        if hours > 0 {
            return String(format: "%d:%02d:%02d", hours, minutes, secs)
        } else {
            return String(format: "%02d:%02d", minutes, secs)
        }
    }

    private func updateLiveMetrics(player: AVPlayer?) {
        guard let item = player?.currentItem else { return }
        let size = item.presentationSize
        if size.width > 0 && size.height > 0 {
            measuredResolution = "\(Int(size.width)) × \(Int(size.height))"
        }
        if let lastEvent = item.accessLog()?.events.last {
            if lastEvent.observedBitrate > 0 {
                let mbps = lastEvent.observedBitrate / 1_000_000.0
                measuredBitrate = String(format: "%.1f Mbps", mbps)
            } else if lastEvent.indicatedBitrate > 0 {
                let mbps = lastEvent.indicatedBitrate / 1_000_000.0
                measuredBitrate = String(format: "%.1f Mbps", mbps)
            }
            if lastEvent.numberOfDroppedVideoFrames >= 0 {
                droppedFramesCount = lastEvent.numberOfDroppedVideoFrames
            }
            if lastEvent.numberOfBytesTransferred > 0 {
                let mb = Double(lastEvent.numberOfBytesTransferred) / (1024.0 * 1024.0)
                transferredBytes = String(format: "%.1f MB", mb)
            }
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

            let streamURL: URL
            if let path = negotiatedPath, !path.isEmpty {
                if let directURL = URL(string: path), directURL.scheme != nil {
                    streamURL = directURL
                } else if let relURL = URL(string: path, relativeTo: baseURL)?.absoluteURL {
                    streamURL = relURL
                } else {
                    let cleanPath = path.hasPrefix("/") ? String(path.dropFirst()) : path
                    let rawBase = base.hasSuffix("/") ? base : base + "/"
                    streamURL = URL(string: rawBase + cleanPath) ?? baseURL.appendingPathComponent(cleanPath)
                }
            } else {
                streamURL = baseURL.appendingPathComponent("api/v3/recordings/\(recording.id)/playlist.m3u8")
            }

            if let cookieValue = sessionCookie, let host = baseURL.host {
                var props: [HTTPCookiePropertyKey: Any] = [
                    .name: "xg2g_session",
                    .value: cookieValue,
                    .domain: host,
                    .path: "/",
                    .expires: Date().addingTimeInterval(86400)
                ]
                if baseURL.scheme?.lowercased() == "https" {
                    props[.secure] = "TRUE"
                }
                if let cookie = HTTPCookie(properties: props) {
                    HTTPCookieStorage.shared.setCookie(cookie)
                    HTTPCookieStorage.shared.setCookies([cookie], for: baseURL, mainDocumentURL: nil)
                    HTTPCookieStorage.shared.setCookies([cookie], for: streamURL, mainDocumentURL: nil)
                }
            }

            var extraHeaders: [String: String] = [:]
            if let cookieValue = sessionCookie {
                extraHeaders["Cookie"] = "xg2g_session=\(cookieValue)"
            }

            // Ensure HLS playlist is ready on backend before handing to AVPlayer
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

            TelemetryServer.shared.log("[RecordingPlayer] ▶️ Loading '\(recording.title)' (\(recording.id)) URL: \(streamURL.absoluteString)")

            let item = PlayerAssetLoader.makePlayerItem(url: streamURL, baseURL: baseURL, extraHeaders: extraHeaders)
            let p = AVPlayer(playerItem: item)

            await MainActor.run {
                let progressCallback = self.onProgressUpdate

                // High-resolution time observer (every 250ms) for smooth VOD scrubber & time updates
                self.timeObserver = p.addPeriodicTimeObserver(
                    forInterval: CMTime(seconds: 0.25, preferredTimescale: 600),
                    queue: .main
                ) { [weak p] time in
                    Task { @MainActor [weak p] in
                        guard let p else { return }
                        let sec = time.seconds
                        if sec.isFinite && !sec.isNaN && !self.isScrubbing && !self.isSeeking {
                            self.currentTime = max(0, sec)
                        }
                        if let itemDur = p.currentItem?.duration.seconds, itemDur.isFinite && itemDur > 0 {
                            self.durationOverride = itemDur
                        }

                        self.updateLiveMetrics(player: p)

                        // Periodic resume progress update to server
                        if let itemDur = p.currentItem?.duration.seconds, itemDur > 0 {
                            progressCallback(sec, itemDur)
                        } else if self.recording.durationSeconds > 0 {
                            progressCallback(sec, Double(self.recording.durationSeconds))
                        }
                    }
                }

                self.statusObserver = item.observe(\.status, options: [.new]) { [weak p] observedItem, _ in
                    Task { @MainActor [weak p] in
                        guard let p else { return }
                        if observedItem.status == .readyToPlay {
                            TelemetryServer.shared.log("[RecordingPlayer] ✅ readyToPlay '\(self.recording.title)' (dur=\(self.totalDuration)s)")
                            let startPos = self.initialPosition ?? self.recording.serverResumePos
                            if let pos = startPos, pos > 5 {
                                let targetTime = CMTime(seconds: pos, preferredTimescale: 600)
                                p.seek(to: targetTime, toleranceBefore: .zero, toleranceAfter: .zero) { [weak p] finished in
                                    Task { @MainActor [weak p] in
                                        if finished {
                                            p?.play()
                                            self.isPlaying = true
                                        }
                                    }
                                }
                            } else {
                                p.play()
                                self.isPlaying = true
                            }

                            withAnimation(.easeInOut(duration: 0.35)) {
                                self.isPreparing = false
                            }
                            self.resetAutoHideTimer()
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
        autoHideTask?.cancel()
        autoHideTask = nil
        hideSkipFeedbackTask?.cancel()
        hideSkipFeedbackTask = nil

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
