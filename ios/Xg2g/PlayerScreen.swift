// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import SwiftUI
import UIKit

/// Live playback for a channel with native AVPlayerViewController stage,
/// edge-to-edge aspect ratio fill toggle, conflict-free gestural zapping,
/// swipe-up Mini-EPG, broadcast telemetry inspector, and adaptive landscape HUD.
struct PlayerScreen: View {

    let model: AppModel
    let channel: Channel

    @Environment(\.dismiss) private var dismiss
    @State private var currentChannel: Channel
    @State private var player: AVPlayer?
    @State private var failure: String?
    @State private var showControls = true
    @State private var isAspectFill = false
    @State private var showMiniEPG = false
    @State private var showInspector = false
    @State private var zapNotice: String?
    @State private var hideControlsTask: Task<Void, Never>?
    @State private var hideZapNoticeTask: Task<Void, Never>?

    init(model: AppModel, channel: Channel) {
        self.model = model
        self.channel = channel
        self._currentChannel = State(initialValue: channel)
    }

    var nowNext: NowNext? {
        model.schedule[currentChannel.serviceRef]
    }

    private var isDirectStream: Bool {
        model.qualityPreference != .dataSaver
    }

    var body: some View {
        GeometryReader { geometry in
            let isLandscape = geometry.size.width > geometry.size.height

            ZStack {
                Theme.Colors.bgVideoStage.ignoresSafeArea()

                // MARK: - Native Video Player Stage
                if let player {
                    NativeVideoPlayerView(
                        player: player,
                        videoGravity: isAspectFill ? .resizeAspectFill : .resizeAspect
                    )
                    .ignoresSafeArea()

                    // Transparent Gesture Catch Layer (Reliable Taps & Gestures)
                    Color.black.opacity(0.001)
                        .ignoresSafeArea()
                        .onTapGesture(count: 2) {
                            triggerHaptic(.medium)
                            withAnimation(.spring(response: 0.3, dampingFraction: 0.7)) {
                                isAspectFill.toggle()
                            }
                            displayZapToast(isAspectFill ? "Vollbild (Ausgefüllt)" : "Standard (16:9)")
                        }
                        .onTapGesture(count: 1) {
                            withAnimation(.easeInOut(duration: 0.25)) {
                                showControls.toggle()
                            }
                            if showControls {
                                scheduleControlsHiding()
                            }
                        }
                        .gesture(
                            DragGesture(minimumDistance: 25)
                                .onEnded { value in
                                    let horiz = value.translation.width
                                    let vert = value.translation.height

                                    // Swipe Down -> Close Player
                                    if vert > 50 && vert > abs(horiz) * 1.3 {
                                        closePlayer()
                                    }
                                    // Horizontal Swipe -> Channel Zapping
                                    else if model.playerGesturesEnabled && abs(horiz) > 50 && abs(horiz) > abs(vert) * 1.3 {
                                        if horiz < 0 {
                                            zapNext()
                                        } else {
                                            zapPrevious()
                                        }
                                    }
                                }
                        )
                } else if let failure {
                    VStack(spacing: 20) {
                        ContentUnavailableView(
                            "Wiedergabefehler",
                            systemImage: "exclamationmark.triangle",
                            description: Text(failure)
                        )
                        .foregroundStyle(Theme.Colors.textSecondary)

                        HStack(spacing: 12) {
                            Button {
                                closePlayer()
                            } label: {
                                HStack(spacing: 6) {
                                    Image(systemName: "xmark")
                                    Text("Zurück")
                                }
                                .font(.subheadline.weight(.medium))
                                .padding(.horizontal, 16)
                                .padding(.vertical, 10)
                                .background(Theme.Colors.surfaceGlass, in: Capsule())
                                .foregroundStyle(Theme.Colors.textSecondary)
                            }
                            .buttonStyle(.plain)

                            Button {
                                Task { await startStreaming(channel: currentChannel) }
                            } label: {
                                HStack(spacing: 6) {
                                    Image(systemName: "arrow.clockwise")
                                    Text("Erneut versuchen")
                                }
                                .font(.headline)
                                .padding(.horizontal, 20)
                                .padding(.vertical, 10)
                                .background(Theme.Colors.accentAction, in: Capsule())
                                .foregroundStyle(Theme.Colors.textPrimary)
                            }
                            .buttonStyle(.plain)
                        }
                    }
                } else {
                    VStack(spacing: 16) {
                        ProgressView()
                            .tint(Theme.Colors.accentLive)
                            .scaleEffect(1.3)

                        Text("\(currentChannel.name) wird geladen…")
                            .font(.headline)
                            .foregroundStyle(Theme.Colors.textPrimary)

                        Button {
                            closePlayer()
                        } label: {
                            Text("Abbrechen")
                                .font(.caption.weight(.medium))
                                .foregroundStyle(Theme.Colors.textTertiary)
                                .padding(.horizontal, 14)
                                .padding(.vertical, 6)
                                .background(Theme.Colors.surfaceGlass, in: Capsule())
                        }
                        .buttonStyle(.plain)
                        .padding(.top, 10)
                    }
                }

                // MARK: - Zap HUD Banner (Translucent Toast)
                if let zapNotice {
                    VStack {
                        HStack(spacing: 8) {
                            Image(systemName: "bolt.fill")
                                .foregroundStyle(Theme.Colors.accentLive)
                            Text(zapNotice)
                                .font(.subheadline.weight(.semibold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                        }
                        .padding(.horizontal, 16)
                        .padding(.vertical, 8)
                        .glassCard(cornerRadius: 20)
                        .shadow(color: Color.black.opacity(0.4), radius: 10, y: 5)
                        .transition(.move(edge: .top).combined(with: .opacity))
                        .padding(.top, isLandscape ? 20 : 60)

                        Spacer()
                    }
                }

                // MARK: - Broadcast Console OSD Overlay
                if showControls {
                    VStack(spacing: 0) {
                        // Top Bar
                        HStack(spacing: 10) {
                            Button {
                                closePlayer()
                            } label: {
                                HStack(spacing: 4) {
                                    Image(systemName: "chevron.down")
                                        .font(.system(size: 13, weight: .bold))
                                    Text("Zurück")
                                        .font(.subheadline.weight(.semibold))
                                        .lineLimit(1)
                                }
                                .fixedSize()
                                .foregroundStyle(Theme.Colors.textPrimary)
                                .padding(.horizontal, 12)
                                .padding(.vertical, 7)
                                .background(Color.black.opacity(0.65), in: Capsule())
                                .overlay(Capsule().strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1))
                            }
                            .buttonStyle(.plain)

                            HStack(spacing: 6) {
                                if let number = currentChannel.number {
                                    Text(number)
                                        .font(.caption.monospacedDigit().bold())
                                        .foregroundStyle(Theme.Colors.accentAction)
                                        .padding(.horizontal, 6)
                                        .padding(.vertical, 2)
                                        .background(Theme.Colors.accentAction.opacity(0.15), in: RoundedRectangle(cornerRadius: 4))
                                }

                                Text(currentChannel.name)
                                    .font(.headline)
                                    .foregroundStyle(Theme.Colors.textPrimary)
                                    .lineLimit(1)
                            }

                            Spacer(minLength: 8)

                            // Aspect Ratio Zoom Button
                            Button {
                                triggerHaptic(.light)
                                withAnimation(.easeInOut(duration: 0.2)) {
                                    isAspectFill.toggle()
                                }
                                displayZapToast(isAspectFill ? "Vollbild (Ausgefüllt)" : "Standard (16:9)")
                            } label: {
                                Image(systemName: isAspectFill ? "arrow.down.right.and.arrow.up.left" : "arrow.up.left.and.arrow.down.right")
                                    .font(.system(size: 14, weight: .medium))
                                    .foregroundStyle(isAspectFill ? Theme.Colors.accentLive : Theme.Colors.textPrimary)
                                    .padding(8)
                                    .background(Color.black.opacity(0.65), in: Circle())
                                    .overlay(Circle().strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1))
                            }
                            .buttonStyle(.plain)

                            // Stream Telemetry Inspector Toggle
                            Button {
                                triggerHaptic(.light)
                                withAnimation(.spring(response: 0.3, dampingFraction: 0.7)) {
                                    showInspector.toggle()
                                }
                            } label: {
                                HStack(spacing: 5) {
                                    PulsingLiveDot(size: 6)
                                    Text("LIVE")
                                        .font(.caption.bold().monospaced())
                                        .foregroundStyle(Theme.Colors.accentLive)
                                        .lineLimit(1)

                                    Image(systemName: "info.circle")
                                        .font(.caption)
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                }
                                .fixedSize()
                                .padding(.horizontal, 8)
                                .padding(.vertical, 5)
                                .background(Color.black.opacity(0.65), in: Capsule())
                                .overlay(Capsule().strokeBorder(Theme.Colors.accentLive.opacity(0.4), lineWidth: 1))
                            }
                            .buttonStyle(.plain)

                            // Live Record Button
                            Button {
                                triggerHaptic(.medium)
                                Task {
                                    let ok = await model.recordLiveNow(channel: currentChannel)
                                    if ok {
                                        LiveActivityManager.shared.updateActivity(
                                            nowNext: nowNext,
                                            isRecording: true,
                                            isDirectStream: isDirectStream,
                                            channelNumber: currentChannel.number
                                        )
                                    }
                                    displayZapToast(ok ? "🔴 Aufnahme gestartet: \(nowNext?.now?.title ?? currentChannel.name)" : "Fehler beim Starten der Aufnahme")
                                }
                            } label: {
                                Image(systemName: "record.circle")
                                    .font(.title3)
                                    .foregroundStyle(Theme.Colors.statusError)
                                    .padding(6)
                                    .background(Color.black.opacity(0.65), in: Circle())
                            }
                            .buttonStyle(.plain)

                            // Quick EPG Button
                            Button {
                                triggerHaptic(.light)
                                showMiniEPG = true
                            } label: {
                                Image(systemName: "list.bullet.rectangle")
                                    .font(.title3)
                                    .foregroundStyle(Theme.Colors.textPrimary.opacity(0.9))
                                    .padding(6)
                                    .background(Color.black.opacity(0.65), in: Circle())
                            }
                            .buttonStyle(.plain)
                        }
                        .padding(.horizontal, isLandscape ? max(20, geometry.safeAreaInsets.leading) : 16)
                        .padding(.top, isLandscape ? max(10, geometry.safeAreaInsets.top) : 16)
                        .padding(.bottom, 12)
                        .background(
                            LinearGradient(
                                colors: [Color.black.opacity(0.85), Color.clear],
                                startPoint: .top,
                                endPoint: .bottom
                            )
                        )

                        // Stream Telemetry Inspector Panel
                        if showInspector {
                            StreamInspectorOverlay(
                                channel: currentChannel,
                                liveStream: model.liveStream
                            )
                            .padding(.horizontal, isLandscape ? max(24, geometry.safeAreaInsets.leading) : 16)
                            .transition(.scale(scale: 0.95).combined(with: .opacity))
                        }

                        Spacer()

                        // MARK: - Adaptive Bottom Bar (Portrait Card vs Landscape Slim HUD)
                        if isLandscape {
                            // Sleek, unobtrusive Landscape HUD
                            VStack(spacing: 6) {
                                HStack(spacing: 12) {
                                    if let now = nowNext?.now {
                                        PulsingLiveDot(size: 5)
                                        Text(now.title)
                                            .font(.subheadline.weight(.semibold))
                                            .foregroundStyle(Theme.Colors.textPrimary)
                                            .lineLimit(1)

                                        if let remaining = now.remainingMinutes(at: .now) {
                                            Text("• noch \(remaining)m")
                                                .font(.caption.monospacedDigit().weight(.medium))
                                                .foregroundStyle(Theme.Colors.accentLive)
                                        }
                                    } else {
                                        Text(currentChannel.name)
                                            .font(.subheadline.weight(.semibold))
                                            .foregroundStyle(Theme.Colors.textPrimary)
                                    }

                                    Spacer()

                                    if let next = nowNext?.next {
                                        HStack(spacing: 4) {
                                            Text("Danach:")
                                                .font(.caption2.bold())
                                                .foregroundStyle(Theme.Colors.textTertiary)
                                            Text(next.title)
                                                .font(.caption2)
                                                .foregroundStyle(Theme.Colors.textSecondary)
                                                .lineLimit(1)
                                        }
                                    }
                                }

                                if let fraction = nowNext?.now?.progress(at: .now) {
                                    ProgressView(value: fraction)
                                        .progressViewStyle(.linear)
                                        .tint(Theme.Colors.accentLive)
                                }

                                HStack {
                                    Button { zapPrevious() } label: {
                                        HStack(spacing: 4) {
                                            Image(systemName: "chevron.left")
                                            Text("Vorheriger")
                                        }
                                        .font(.caption.weight(.medium))
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                        .padding(.horizontal, 10)
                                        .padding(.vertical, 4)
                                        .background(Theme.Colors.surfaceGlass, in: Capsule())
                                    }
                                    .buttonStyle(.plain)

                                    Spacer()

                                    Text("Wischen zum Zappen • Doppeltippen für Vollbild")
                                        .font(.system(size: 10, design: .monospaced))
                                        .foregroundStyle(Theme.Colors.textTertiary)

                                    Spacer()

                                    Button { zapNext() } label: {
                                        HStack(spacing: 4) {
                                            Text("Nächster")
                                            Image(systemName: "chevron.right")
                                        }
                                        .font(.caption.weight(.medium))
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                        .padding(.horizontal, 10)
                                        .padding(.vertical, 4)
                                        .background(Theme.Colors.surfaceGlass, in: Capsule())
                                    }
                                    .buttonStyle(.plain)
                                }
                            }
                            .padding(.horizontal, 16)
                            .padding(.vertical, 8)
                            .glassCard(cornerRadius: 14)
                            .padding(.horizontal, max(24, geometry.safeAreaInsets.leading))
                            .padding(.bottom, max(8, geometry.safeAreaInsets.bottom))
                        } else {
                            // Portrait Rich Info Card
                            VStack(spacing: 8) {
                                if let now = nowNext?.now {
                                    VStack(alignment: .leading, spacing: 6) {
                                        HStack {
                                            PulsingLiveDot(size: 6)
                                            Text(now.title)
                                                .font(.headline)
                                                .foregroundStyle(Theme.Colors.textPrimary)
                                                .lineLimit(1)

                                            Spacer()

                                            if let remaining = now.remainingMinutes(at: .now) {
                                                Text("noch \(remaining) Min.")
                                                    .font(.caption.monospacedDigit().weight(.medium))
                                                    .foregroundStyle(Theme.Colors.accentLive)
                                            }
                                        }

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

                                        if let next = nowNext?.next {
                                            HStack(spacing: 6) {
                                                Text("DANACH:")
                                                    .font(.system(size: 9, weight: .bold, design: .monospaced))
                                                    .foregroundStyle(Theme.Colors.textTertiary)

                                                Text(next.title)
                                                    .font(.caption2)
                                                    .foregroundStyle(Theme.Colors.textSecondary)
                                                    .lineLimit(1)

                                                Spacer()

                                                Text(next.formattedTimeRange)
                                                    .font(.system(size: 9, design: .monospaced))
                                                    .foregroundStyle(Theme.Colors.textTertiary)
                                            }
                                            .padding(.top, 2)
                                        }
                                    }
                                    .padding(14)
                                    .glassCard(cornerRadius: 12)
                                }

                                // Bottom Quick Controls
                                HStack {
                                    Button { zapPrevious() } label: {
                                        HStack(spacing: 4) {
                                            Image(systemName: "chevron.left")
                                            Text("Vorheriger")
                                        }
                                        .font(.caption.weight(.medium))
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                        .padding(.horizontal, 12)
                                        .padding(.vertical, 6)
                                        .background(Theme.Colors.surfaceGlass, in: Capsule())
                                    }
                                    .buttonStyle(.plain)

                                    Spacer()

                                    Text("Wischen zum Zappen")
                                        .font(.system(size: 10, design: .monospaced))
                                        .foregroundStyle(Theme.Colors.textTertiary)

                                    Spacer()

                                    Button { zapNext() } label: {
                                        HStack(spacing: 4) {
                                            Text("Nächster")
                                            Image(systemName: "chevron.right")
                                        }
                                        .font(.caption.weight(.medium))
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                        .padding(.horizontal, 12)
                                        .padding(.vertical, 6)
                                        .background(Theme.Colors.surfaceGlass, in: Capsule())
                                    }
                                    .buttonStyle(.plain)
                                }
                            }
                            .padding(.horizontal, 16)
                            .padding(.bottom, 24)
                        }
                    }
                    .transition(.opacity)
                }
            }
        }
        .task(id: currentChannel.id) {
            await startStreaming(channel: currentChannel)
        }
        .onDisappear {
            teardownPlayer()
        }
        .sheet(isPresented: $showMiniEPG) {
            MiniEPGDrawer(model: model, currentChannel: currentChannel) { selectedChannel in
                showMiniEPG = false
                switchChannel(to: selectedChannel)
            }
            .presentationDetents([.medium, .fraction(0.75)])
            .presentationDragIndicator(.visible)
        }
    }

    // MARK: - Streaming & Channel Switching

    private func startStreaming(channel: Channel) async {
        failure = nil
        player?.pause()
        player = nil

        AudioSessionManager.shared.configureForPlayback()
        NowPlayingManager.shared.setupRemoteCommands()
        NowPlayingManager.shared.update(channel: channel, nowEntry: nowNext?.now)

        LiveActivityManager.shared.startActivity(
            channel: channel,
            nowNext: nowNext,
            isDirectStream: isDirectStream
        )
        HandoffCoordinator.shared.updatePlaybackActivity(
            channel: channel,
            nowNext: nowNext,
            serverAddress: model.serverURLString
        )

        await model.play(channel)

        guard let stream = model.liveStream else {
            failure = model.lastError ?? "Der Stream konnte nicht gestartet werden."
            return
        }
        player = Self.makePlayer(for: stream)
        player?.play()
        scheduleControlsHiding()
    }

    private func switchChannel(to newChannel: Channel) {
        guard newChannel.id != currentChannel.id else { return }
        triggerHaptic(.medium)
        currentChannel = newChannel
        let newSchedule = model.schedule[newChannel.serviceRef]
        NowPlayingManager.shared.update(channel: newChannel, nowEntry: newSchedule?.now)

        LiveActivityManager.shared.startActivity(
            channel: newChannel,
            nowNext: newSchedule,
            isDirectStream: isDirectStream
        )
        HandoffCoordinator.shared.updatePlaybackActivity(
            channel: newChannel,
            nowNext: newSchedule,
            serverAddress: model.serverURLString
        )

        displayZapToast("Kanal: \(newChannel.name)")
        Task {
            await startStreaming(channel: newChannel)
        }
    }

    private func zapNext() {
        if let nextChannel = model.channelAfter(currentChannel) {
            switchChannel(to: nextChannel)
        }
    }

    private func zapPrevious() {
        if let prevChannel = model.channelBefore(currentChannel) {
            switchChannel(to: prevChannel)
        }
    }

    private func displayZapToast(_ message: String) {
        hideZapNoticeTask?.cancel()
        withAnimation(.easeOut(duration: 0.2)) {
            zapNotice = message
        }
        hideZapNoticeTask = Task {
            try? await Task.sleep(for: .seconds(2))
            if !Task.isCancelled {
                withAnimation(.easeInOut(duration: 0.3)) {
                    zapNotice = nil
                }
            }
        }
    }

    private func triggerHaptic(_ style: UIImpactFeedbackGenerator.FeedbackStyle) {
        let generator = UIImpactFeedbackGenerator(style: style)
        generator.impactOccurred()
    }

    private func scheduleControlsHiding() {
        hideControlsTask?.cancel()
        hideControlsTask = Task {
            try? await Task.sleep(for: .seconds(4))
            if !Task.isCancelled && !showInspector {
                withAnimation(.easeInOut(duration: 0.25)) {
                    showControls = false
                }
            }
        }
    }

    private func closePlayer() {
        triggerHaptic(.light)
        teardownPlayer()
        model.playingChannel = nil
        dismiss()
    }

    private func teardownPlayer() {
        hideControlsTask?.cancel()
        hideZapNoticeTask?.cancel()
        LiveActivityManager.shared.endActivity()
        HandoffCoordinator.shared.clearPlaybackActivity()
        NowPlayingManager.shared.clear()
        AudioSessionManager.shared.deactivate()
        player?.pause()
        player = nil
        Task { await model.stopPlayback() }
    }

    /// Builds the player with the playback ticket attached.
    private static func makePlayer(for stream: LiveStream) -> AVPlayer? {
        if let cookie = stream.ticket.httpCookie(for: stream.playlistURL) {
            HTTPCookieStorage.shared.setCookie(cookie)
        }
        if let rootCookie = stream.ticket.rootCookie(for: stream.playlistURL) {
            HTTPCookieStorage.shared.setCookie(rootCookie)
        }

        var options: [String: Any] = [:]
        if let cookie = stream.ticket.httpCookie(for: stream.playlistURL) {
            options[AVURLAssetHTTPCookiesKey] = [cookie]
            options["AVURLAssetHTTPHeaderFieldsKey"] = [
                "Cookie": "\(cookie.name)=\(cookie.value)"
            ]
        }

        let asset = AVURLAsset(url: stream.playlistURL, options: options)
        let item = AVPlayerItem(asset: asset)
        item.automaticallyPreservesTimeOffsetFromLive = true
        item.preferredForwardBufferDuration = 2.0
        let player = AVPlayer(playerItem: item)

        player.automaticallyWaitsToMinimizeStalling = true
        return player
    }
}

// MARK: - Native Video Player (AVPlayerViewController wrapper)

struct NativeVideoPlayerView: UIViewControllerRepresentable {

    let player: AVPlayer
    let videoGravity: AVLayerVideoGravity
    var showsNativeControls: Bool = false

    func makeUIViewController(context: Context) -> AVPlayerViewController {
        let controller = AVPlayerViewController()
        controller.player = player
        controller.showsPlaybackControls = showsNativeControls
        controller.videoGravity = videoGravity
        controller.allowsPictureInPicturePlayback = true
        controller.updatesNowPlayingInfoCenter = false
        return controller
    }

    func updateUIViewController(_ controller: AVPlayerViewController, context: Context) {
        if controller.player !== player {
            controller.player = player
        }
        controller.videoGravity = videoGravity
        controller.showsPlaybackControls = showsNativeControls
    }
}

// MARK: - Mini-EPG Drawer Sheet

struct MiniEPGDrawer: View {

    let model: AppModel
    let currentChannel: Channel
    let onSelect: (Channel) -> Void

    var body: some View {
        NavigationStack {
            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                VStack(spacing: 0) {
                    if !model.bouquets.isEmpty {
                        BouquetPicker(model: model)
                            .padding(.vertical, 8)
                    }

                    List(model.filteredChannels) { channel in
                        MiniEPGRow(
                            channel: channel,
                            nowNext: model.schedule[channel.serviceRef],
                            isCurrent: channel.id == currentChannel.id
                        ) {
                            onSelect(channel)
                        }
                        .listRowBackground(
                            channel.id == currentChannel.id
                                ? Theme.Colors.accentAction.opacity(0.15)
                                : Theme.Colors.surfaceElevated
                        )
                        .listRowSeparatorTint(Theme.Colors.borderSubtle)
                    }
                    .listStyle(.plain)
                    .scrollContentBackground(.hidden)
                }
            }
            .navigationTitle("Programm & Sender")
            .navigationBarTitleDisplayMode(.inline)
        }
    }
}

struct MiniEPGRow: View {

    let channel: Channel
    let nowNext: NowNext?
    let isCurrent: Bool
    let onSelect: () -> Void

    var body: some View {
        Button(action: onSelect) {
            HStack(spacing: 12) {
                ChannelLogo(url: channel.logoURL, name: channel.name)

                VStack(alignment: .leading, spacing: 3) {
                    HStack(spacing: 6) {
                        if let number = channel.number {
                            Text(number)
                                .font(.caption.monospacedDigit().weight(.semibold))
                                .foregroundStyle(Theme.Colors.accentAction)
                                .frame(minWidth: 22, alignment: .leading)
                        }
                        Text(channel.name)
                            .font(.body.weight(.semibold))
                            .foregroundStyle(isCurrent ? Theme.Colors.accentLive : Theme.Colors.textPrimary)
                            .lineLimit(1)

                        Spacer()

                        if isCurrent {
                            HStack(spacing: 4) {
                                PulsingLiveDot(size: 5)
                                Text("LIVE")
                                    .font(.system(size: 9, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentLive)
                            }
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())
                        }
                    }

                    if let now = nowNext?.now {
                        Text(now.title)
                            .font(.caption.weight(.medium))
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .lineLimit(1)

                        if let fraction = now.progress(at: .now) {
                            ProgressView(value: fraction)
                                .progressViewStyle(.linear)
                                .tint(isCurrent ? Theme.Colors.accentLive : Theme.Colors.accentAction)
                        }
                    } else {
                        Text("Keine Programminformationen")
                            .font(.caption2)
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                }

                Image(systemName: isCurrent ? "waveform.circle.fill" : "play.circle")
                    .font(.title3)
                    .foregroundStyle(isCurrent ? Theme.Colors.accentLive : Theme.Colors.textTertiary)
            }
            .padding(.vertical, 4)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

// MARK: - Stream Telemetry Inspector

struct StreamInspectorOverlay: View {

    let channel: Channel
    let liveStream: LiveStream?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack {
                Text("BROADCAST TELEMETRY")
                    .font(.system(size: 10, weight: .bold, design: .monospaced))
                    .foregroundStyle(Theme.Colors.accentLive)

                Spacer()

                Text("HW DECODER AKTIV")
                    .font(.system(size: 9, weight: .semibold, design: .monospaced))
                    .foregroundStyle(Theme.Colors.statusSuccess)
            }

            Divider().overlay(Theme.Colors.borderSubtle)

            Grid(alignment: .leading, horizontalSpacing: 12, verticalSpacing: 6) {
                GridRow {
                    Text("Sender")
                        .font(.caption2)
                        .foregroundStyle(Theme.Colors.textTertiary)
                    Text(channel.name)
                        .font(.caption2.bold())
                        .foregroundStyle(Theme.Colors.textPrimary)
                }

                GridRow {
                    Text("ServiceRef")
                        .font(.caption2)
                        .foregroundStyle(Theme.Colors.textTertiary)
                    Text(channel.serviceRef)
                        .font(.system(size: 9, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .lineLimit(1)
                }

                if let sessionID = liveStream?.sessionID {
                    GridRow {
                        Text("Session ID")
                            .font(.caption2)
                            .foregroundStyle(Theme.Colors.textTertiary)
                        Text(sessionID)
                            .font(.system(size: 9, design: .monospaced))
                            .foregroundStyle(Theme.Colors.accentAction)
                            .lineLimit(1)
                    }
                }

                GridRow {
                    Text("Decoder Engine")
                        .font(.caption2)
                        .foregroundStyle(Theme.Colors.textTertiary)
                    Text("AVSampleBufferDisplayLayer (Hardware)")
                        .font(.system(size: 9, design: .monospaced))
                        .foregroundStyle(Theme.Colors.statusSuccess)
                }
            }
        }
        .padding(12)
        .glassCard(cornerRadius: 12)
    }
}
