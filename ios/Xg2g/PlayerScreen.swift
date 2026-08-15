// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import SwiftUI
import UIKit

/// Live playback for a channel with gestural channel zapping, swipe-up Mini-EPG,
/// broadcast telemetry inspector, and native haptic feedback.
struct PlayerScreen: View {

    let model: AppModel
    let channel: Channel

    @Environment(\.dismiss) private var dismiss
    @State private var currentChannel: Channel
    @State private var player: AVPlayer?
    @State private var failure: String?
    @State private var showControls = true
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
        ZStack {
            Theme.Colors.bgVideoStage.ignoresSafeArea()

            // Video Player Layer
            if let player {
                VideoPlayer(player: player)
                    .ignoresSafeArea()

                // Transparent Gesture Catch Layer (Reliable Taps & Gestures)
                Color.black.opacity(0.001)
                    .ignoresSafeArea()
                    .onTapGesture {
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
                                // Swipe Down -> Close Player & return to Channel List
                                if value.translation.height > 60 && abs(value.translation.width) < value.translation.height {
                                    closePlayer()
                                }
                                // Swipe Left -> Next Channel
                                else if value.translation.width < -60 && abs(value.translation.height) < abs(value.translation.width) {
                                    zapNext()
                                }
                                // Swipe Right -> Previous Channel
                                else if value.translation.width > 60 && abs(value.translation.height) < abs(value.translation.width) {
                                    zapPrevious()
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
                    .padding(.top, 60)

                    Spacer()
                }
            }

            // MARK: - Broadcast Console OSD Overlay
            if showControls {
                VStack {
                    // Top Bar
                    HStack(spacing: 12) {
                        Button {
                            closePlayer()
                        } label: {
                            HStack(spacing: 4) {
                                Image(systemName: "chevron.down")
                                    .font(.system(size: 14, weight: .bold))
                                Text("Zurück")
                                    .font(.subheadline.weight(.semibold))
                            }
                            .foregroundStyle(Theme.Colors.textPrimary)
                            .padding(.horizontal, 12)
                            .padding(.vertical, 7)
                            .background(Color.black.opacity(0.6), in: Capsule())
                            .overlay(Capsule().strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1))
                        }
                        .buttonStyle(.plain)

                        HStack(spacing: 8) {
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
                        }

                        Spacer()

                        // Stream Telemetry Inspector Toggle
                        Button {
                            triggerHaptic(.light)
                            withAnimation(.spring(response: 0.3, dampingFraction: 0.7)) {
                                showInspector.toggle()
                            }
                        } label: {
                            HStack(spacing: 6) {
                                PulsingLiveDot(size: 6)
                                Text("LIVE")
                                    .font(.caption.bold().monospaced())
                                    .foregroundStyle(Theme.Colors.accentLive)

                                Image(systemName: "info.circle")
                                    .font(.caption)
                                    .foregroundStyle(Theme.Colors.textSecondary)
                            }
                            .padding(.horizontal, 8)
                            .padding(.vertical, 4)
                            .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())
                            .overlay(Capsule().strokeBorder(Theme.Colors.accentLive.opacity(0.3), lineWidth: 1))
                        }

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
                        }

                        // Quick EPG Button
                        Button {
                            triggerHaptic(.light)
                            showMiniEPG = true
                        } label: {
                            Image(systemName: "list.bullet.rectangle")
                                .font(.title3)
                                .foregroundStyle(Theme.Colors.textPrimary.opacity(0.85))
                        }
                    }
                    .padding()
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
                        .padding(.horizontal, 16)
                        .transition(.scale(scale: 0.95).combined(with: .opacity))
                    }

                    Spacer()

                    // Bottom Bar (Now/Next Info & Zapping Bar)
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
                            Button {
                                zapPrevious()
                            } label: {
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

                            Spacer()

                            Text("Wischen zum Zappen")
                                .font(.system(size: 10, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textTertiary)

                            Spacer()

                            Button {
                                zapNext()
                            } label: {
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
                        }
                    }
                    .padding(.horizontal, 16)
                    .padding(.bottom, 24)
                }
                .transition(.opacity)
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
                        Button {
                            onSelect(channel)
                        } label: {
                            HStack {
                                ChannelRow(
                                    channel: channel,
                                    nowNext: model.schedule[channel.serviceRef]
                                )

                                if channel.id == currentChannel.id {
                                    Image(systemName: "waveform.circle.fill")
                                        .font(.title3)
                                        .foregroundStyle(Theme.Colors.accentLive)
                                }
                            }
                        }
                        .buttonStyle(.plain)
                        .listRowBackground(
                            channel.id == currentChannel.id
                                ? Theme.Colors.accentAction.opacity(0.12)
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

            Grid(alignment: .leading, horizontalSpacing: 16, verticalSpacing: 4) {
                GridRow {
                    Text("Kanal:")
                        .foregroundStyle(Theme.Colors.textTertiary)
                    Text("\(channel.name) (#\(channel.number ?? "–"))")
                        .foregroundStyle(Theme.Colors.textPrimary)
                }
                GridRow {
                    Text("Streaming-Modus:")
                        .foregroundStyle(Theme.Colors.textTertiary)
                    Text(NetworkMonitor.shared.currentType == .wifi ? "1:1 Direct Stream (Passthrough)" : "Adaptive Transcode (AV1/HEVC)")
                        .font(.system(size: 10, weight: .medium, design: .monospaced))
                        .foregroundStyle(Theme.Colors.statusSuccess)
                }
                GridRow {
                    Text("Netzwerk:")
                        .foregroundStyle(Theme.Colors.textTertiary)
                    Text("\(NetworkMonitor.shared.currentType.rawValue.uppercased()) • \(NetworkMonitor.shared.isExpensive ? "Metered" : "Unmetered")")
                        .font(.system(size: 10, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textSecondary)
                }
                GridRow {
                    Text("ServiceRef:")
                        .foregroundStyle(Theme.Colors.textTertiary)
                    Text(channel.serviceRef)
                        .font(.system(size: 10, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textSecondary)
                }
                GridRow {
                    Text("Session ID:")
                        .foregroundStyle(Theme.Colors.textTertiary)
                    Text(liveStream?.sessionID ?? "–")
                        .font(.system(size: 10, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textSecondary)
                }
                GridRow {
                    Text("Auth Ticket:")
                        .foregroundStyle(Theme.Colors.textTertiary)
                    Text("xg2g_playback (4h Session-Bound)")
                        .font(.system(size: 10, design: .monospaced))
                        .foregroundStyle(Theme.Colors.accentAction)
                }
            }
            .font(.caption)
        }
        .padding(12)
        .glassCard(cornerRadius: 10)
    }
}
