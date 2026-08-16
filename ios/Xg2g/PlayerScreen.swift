// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import CoreMedia
import SwiftUI
import UIKit

/// 100% Native Apple AVPlayerViewController playback inspired by
/// MagentaTV 2.0, Zattoo & Channels DVR:
/// - Single persistent Video Stage: Seamless orientation transitions with ZERO reload or seek resets
/// - In-Playback Live EPG Guide in portrait with 1-tap instant zapping
/// - Landscape Quick-Zap Channel Carousel
/// - Swipe-to-Zap gestures on the video stage
/// - „Von Beginn ansehen“ (Restart) & „Zur Live-Kante“ Timeshift controls
/// - Automatic Picture-in-Picture (PiP) on Home swipe
/// - Instant 4K/HD streaming with native Apple transport bar & DVR Timeshift timeline
struct PlayerScreen: View {

    let model: AppModel
    let channel: Channel

    @Environment(\.dismiss) private var dismiss
    @State private var currentChannel: Channel
    @State private var player: AVPlayer?
    @State private var failure: String?
    @State private var isTimeshifted = false
    @State private var showLandscapeGuide = false
    @State private var zapNotice: String?
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

            ZStack(alignment: .top) {
                Color.black.ignoresSafeArea()

                VStack(spacing: 0) {
                    // MARK: - Single Persistent Video Stage (NEVER recreated across rotations)
                    ZStack(alignment: .topLeading) {
                        if let player {
                            NativeVideoPlayerView(
                                player: player,
                                onDismiss: { closePlayer() }
                            )
                        } else if let failure {
                            ZStack {
                                Color.black
                                VStack(spacing: 16) {
                                    Image(systemName: "exclamationmark.triangle")
                                        .font(.system(size: 36))
                                        .foregroundStyle(Theme.Colors.statusError)

                                    Text(failure)
                                        .font(.subheadline)
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                        .multilineTextAlignment(.center)

                                    Button("Erneut versuchen") {
                                        Task { await startStreaming(channel: currentChannel) }
                                    }
                                    .buttonStyle(.borderedProminent)
                                    .tint(Theme.Colors.accentAction)
                                }
                                .padding()
                            }
                        } else {
                            ZStack {
                                Color.black
                                Circle()
                                    .fill(Theme.Colors.accentLive.opacity(0.18))
                                    .frame(width: 140, height: 140)
                                    .blur(radius: 45)

                                VStack(spacing: 12) {
                                    ProgressView()
                                        .tint(Theme.Colors.accentLive)
                                        .scaleEffect(1.2)

                                    Text("\(currentChannel.name) wird geladen…")
                                        .font(.subheadline.weight(.medium))
                                        .foregroundStyle(Theme.Colors.textSecondary)
                                }
                            }
                        }

                        // Minimal Portrait Dismiss Chevron
                        if !isLandscape {
                            Button {
                                closePlayer()
                            } label: {
                                Image(systemName: "chevron.down")
                                    .font(.system(size: 13, weight: .bold))
                                    .foregroundStyle(.white)
                                    .padding(9)
                                    .background(.ultraThinMaterial, in: Circle())
                                    .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                                    .shadow(color: Color.black.opacity(0.4), radius: 6)
                            }
                            .buttonStyle(.plain)
                            .padding(.top, max(6, geometry.safeAreaInsets.top))
                            .padding(.leading, 12)
                        }
                    }
                    .frame(
                        maxWidth: .infinity,
                        maxHeight: isLandscape ? .infinity : (geometry.size.width * 9 / 16)
                    )
                    .background(Color.black)
                    .simultaneousGesture(
                        DragGesture(minimumDistance: 30)
                            .onEnded { value in
                                guard model.playerGesturesEnabled else { return }
                                let horizontal = value.translation.width
                                let vertical = value.translation.height
                                guard abs(horizontal) > abs(vertical) * 1.5, abs(horizontal) > 45 else { return }
                                if horizontal < 0 {
                                    zapNext()
                                } else {
                                    zapPrevious()
                                }
                            }
                    )

                    // MARK: - Portrait Dual-Stage Bottom Interactive EPG Guide (Modern Sleek UI)
                    if !isLandscape {
                        ScrollView {
                            VStack(spacing: 14) {
                                PortraitBroadcastHeroCard(
                                    currentChannel: currentChannel,
                                    nowNext: nowNext,
                                    isTimeshifted: isTimeshifted,
                                    isFavorite: model.isFavorite(currentChannel),
                                    onToggleFavorite: { model.toggleFavorite(currentChannel) },
                                    onRecord: {
                                        triggerHaptic(.medium)
                                        Task {
                                            let ok = await model.recordLiveNow(channel: currentChannel)
                                            displayZapToast(ok ? "🔴 Aufnahme gestartet" : "Aufnahmefehler")
                                        }
                                    },
                                    onSeekToBeginning: { seekToBeginning() },
                                    onJumpToLive: { jumpToLive() },
                                    onZapPrevious: { zapPrevious() },
                                    onZapNext: { zapNext() }
                                )

                                PortraitBouquetFilterBar(
                                    channelsCount: model.channels.count,
                                    favoriteCount: model.favoriteChannelIDs.count,
                                    bouquets: model.bouquets,
                                    selectedBouquet: model.selectedBouquet,
                                    onSelectBouquet: { b in
                                        triggerHaptic(.light)
                                        model.selectedBouquet = b
                                    }
                                )

                                HStack {
                                    HStack(spacing: 6) {
                                        Image(systemName: "tv")
                                            .font(.system(size: 11, weight: .bold))
                                            .foregroundStyle(Theme.Colors.accentAction)
                                        Text("SENDER & LIVE-PROGRAMM")
                                            .font(.system(size: 11, weight: .bold, design: .monospaced))
                                            .foregroundStyle(Theme.Colors.textSecondary)
                                    }

                                    Spacer()

                                    Text("\(model.filteredChannels.count) Sender")
                                        .font(.system(size: 10, weight: .semibold, design: .monospaced))
                                        .foregroundStyle(Theme.Colors.textTertiary)
                                }
                                .padding(.horizontal, 2)

                                PortraitChannelList(
                                    channels: model.filteredChannels,
                                    currentChannelID: currentChannel.id,
                                    schedule: model.schedule,
                                    onSelectChannel: { ch in
                                        switchChannel(to: ch)
                                    }
                                )
                            }
                            .padding(12)
                        }
                    }
                }

                // MARK: - Landscape Quick-Zap Channel Carousel
                if isLandscape && showLandscapeGuide {
                    VStack {
                        Spacer()
                        LandscapeQuickZapBar(
                            channels: model.filteredChannels,
                            currentChannel: currentChannel,
                            schedule: model.schedule,
                            onSelect: { ch in
                                switchChannel(to: ch)
                            },
                            onClose: {
                                withAnimation(.easeInOut(duration: 0.25)) {
                                    showLandscapeGuide = false
                                }
                            }
                        )
                        .padding(.horizontal, 16)
                        .padding(.bottom, max(12, geometry.safeAreaInsets.bottom))
                    }
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                }

                // MARK: - Zap HUD Banner (Translucent Toast)
                if let zapNotice {
                    VStack {
                        HStack(spacing: 8) {
                            Image(systemName: "bolt.fill")
                                .font(.system(size: 13, weight: .bold))
                                .foregroundStyle(Theme.Colors.accentLive)
                            Text(zapNotice)
                                .font(.subheadline.weight(.semibold))
                                .foregroundStyle(Theme.Colors.textPrimary)
                        }
                        .padding(.horizontal, 16)
                        .padding(.vertical, 8)
                        .background(.ultraThinMaterial, in: Capsule())
                        .overlay(Capsule().strokeBorder(Color.white.opacity(0.15), lineWidth: 0.5))
                        .shadow(color: Color.black.opacity(0.4), radius: 10, y: 5)
                        .transition(.move(edge: .top).combined(with: .opacity))
                        .padding(.top, isLandscape ? 16 : 56)

                        Spacer()
                    }
                }
            }
            .contentShape(Rectangle())
            .onTapGesture {
                if isLandscape {
                    withAnimation(.spring(response: 0.35, dampingFraction: 0.8)) {
                        showLandscapeGuide.toggle()
                    }
                }
            }
        }
        .task(id: currentChannel.id) {
            await startStreaming(channel: currentChannel)
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(5))
                if Task.isCancelled { break }
                if let stream = model.liveStream {
                    try? await model.heartbeat(sessionID: stream.sessionID)
                }
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: .AVPlayerItemPlaybackStalled)) { notif in
            if let currentItem = player?.currentItem, notif.object as? AVPlayerItem == currentItem {
                player?.play()
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: .AVPlayerItemFailedToPlayToEndTime)) { notif in
            if let currentItem = player?.currentItem, notif.object as? AVPlayerItem == currentItem {
                if let err = notif.userInfo?[AVPlayerItemFailedToPlayToEndTimeErrorKey] as? Error {
                    self.failure = "Wiedergabefehler: \(err.localizedDescription)"
                }
            }
        }
        .onDisappear {
            teardownPlayer()
        }
    }

    // MARK: - Streaming & Channel Switching

    private func startStreaming(channel: Channel) async {
        failure = nil
        isTimeshifted = false
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
        let p = Self.makePlayer(for: stream, channel: channel, nowNext: nowNext)
        player = p
        p?.play()
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
        // Setting currentChannel automatically triggers .task(id: currentChannel.id) without race condition
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

    private func seekToBeginning() {
        guard let player, let item = player.currentItem else { return }
        triggerHaptic(.medium)
        if let firstRange = item.seekableTimeRanges.first?.timeRangeValue {
            player.seek(to: firstRange.start, toleranceBefore: .zero, toleranceAfter: .zero)
            isTimeshifted = true
            displayZapToast("⏪ Beginn der Sendung / Puffer")
        }
    }

    private func jumpToLive() {
        guard let player, let item = player.currentItem else { return }
        triggerHaptic(.medium)
        if let lastRange = item.seekableTimeRanges.last?.timeRangeValue {
            player.seek(to: lastRange.end, toleranceBefore: .zero, toleranceAfter: .zero)
            isTimeshifted = false
            displayZapToast("🔴 Live-Kante")
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

    private func closePlayer() {
        triggerHaptic(.light)
        teardownPlayer()
        model.playingChannel = nil
        dismiss()
    }

    private func teardownPlayer() {
        hideZapNoticeTask?.cancel()
        LiveActivityManager.shared.endActivity()
        HandoffCoordinator.shared.clearPlaybackActivity()
        NowPlayingManager.shared.clear()
        AudioSessionManager.shared.deactivate()
        player?.pause()
        player = nil
        Task { await model.stopPlayback() }
    }

    /// Builds the player with native metadata attached for the Apple system player and DVR Timeshift.
    private static func makePlayer(for stream: LiveStream, channel: Channel, nowNext: NowNext?) -> AVPlayer? {
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
        item.preferredForwardBufferDuration = 6.0

        // Inject Native iOS OSD Metadata for Apple's Transport Bar
        var metadata: [AVMetadataItem] = []

        let titleItem = AVMutableMetadataItem()
        titleItem.identifier = .commonIdentifierTitle
        titleItem.value = (nowNext?.now?.title ?? channel.name) as NSString
        metadata.append(titleItem)

        let artistItem = AVMutableMetadataItem()
        artistItem.identifier = .commonIdentifierArtist
        artistItem.value = channel.name as NSString
        metadata.append(artistItem)

        if let description = nowNext?.now?.description {
            let descItem = AVMutableMetadataItem()
            descItem.identifier = .commonIdentifierDescription
            descItem.value = description as NSString
            metadata.append(descItem)
        }

        item.externalMetadata = metadata

        let player = AVPlayer(playerItem: item)
        player.automaticallyWaitsToMinimizeStalling = true
        player.allowsExternalPlayback = true
        player.usesExternalPlaybackWhileExternalScreenIsActive = true
        return player
    }
}

// MARK: - Landscape Quick-Zap Channel Carousel (MagentaTV 2.0 / Zattoo Pattern)

struct LandscapeQuickZapBar: View {

    let channels: [Channel]
    let currentChannel: Channel
    let schedule: [String: NowNext]
    let onSelect: (Channel) -> Void
    let onClose: () -> Void

    var body: some View {
        VStack(spacing: 8) {
            HStack {
                Text("SCHNELL-ZAPPING")
                    .font(.system(size: 10, weight: .bold, design: .monospaced))
                    .foregroundStyle(Theme.Colors.textTertiary)

                Spacer()

                Button(action: onClose) {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 18))
                        .foregroundStyle(Theme.Colors.textTertiary)
                }
                .buttonStyle(.plain)
            }
            .padding(.horizontal, 8)

            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 10) {
                    ForEach(channels) { ch in
                        let isCurrent = ch.id == currentChannel.id
                        Button {
                            onSelect(ch)
                        } label: {
                            HStack(spacing: 8) {
                                ChannelLogo(url: ch.logoURL, name: ch.name, size: 36)

                                VStack(alignment: .leading, spacing: 2) {
                                    HStack(spacing: 4) {
                                        if let num = ch.number {
                                            Text(num)
                                                .font(.system(size: 10, weight: .bold, design: .monospaced))
                                                .foregroundStyle(Theme.Colors.accentAction)
                                        }
                                        Text(ch.name)
                                            .font(.caption.weight(.bold))
                                            .foregroundStyle(isCurrent ? Theme.Colors.accentLive : Theme.Colors.textPrimary)
                                            .lineLimit(1)
                                    }

                                    if let title = schedule[ch.serviceRef]?.now?.title {
                                        Text(title)
                                            .font(.system(size: 10))
                                            .foregroundStyle(Theme.Colors.textSecondary)
                                            .lineLimit(1)
                                    }
                                }
                            }
                            .padding(.horizontal, 10)
                            .padding(.vertical, 6)
                            .background(
                                isCurrent ? Theme.Colors.accentAction.opacity(0.25) : Theme.Colors.surfaceElevated,
                                in: RoundedRectangle(cornerRadius: 10)
                            )
                            .overlay(
                                RoundedRectangle(cornerRadius: 10)
                                    .strokeBorder(isCurrent ? Theme.Colors.accentLive : Theme.Colors.borderSubtle, lineWidth: 1)
                            )
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(.horizontal, 4)
            }
            .fadingHorizontalEdges(fadeWidth: 16)
        }
        .padding(10)
        .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 16, style: .continuous).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
        .shadow(color: Color.black.opacity(0.4), radius: 12, y: 6)
    }
}

// MARK: - Native iOS AVPlayerViewController (Plex / Apple TV System Player)

struct NativeVideoPlayerView: UIViewControllerRepresentable {

    let player: AVPlayer
    var videoGravity: AVLayerVideoGravity = .resizeAspect
    var onDismiss: (@MainActor @Sendable () -> Void)? = nil

    func makeUIViewController(context: Context) -> AVPlayerViewController {
        let controller = AVPlayerViewController()
        controller.player = player
        controller.showsPlaybackControls = true
        controller.videoGravity = videoGravity
        controller.allowsPictureInPicturePlayback = true
        controller.canStartPictureInPictureAutomaticallyFromInline = true
        controller.updatesNowPlayingInfoCenter = true
        controller.delegate = context.coordinator
        return controller
    }

    func updateUIViewController(_ controller: AVPlayerViewController, context: Context) {
        if controller.player !== player {
            controller.player = player
        }
        if controller.videoGravity != videoGravity {
            controller.videoGravity = videoGravity
        }
    }

    func makeCoordinator() -> Coordinator {
        Coordinator(self)
    }

    @MainActor
    final class Coordinator: NSObject, @preconcurrency AVPlayerViewControllerDelegate {
        let parent: NativeVideoPlayerView

        init(_ parent: NativeVideoPlayerView) {
            self.parent = parent
        }

        func playerViewController(
            _ playerViewController: AVPlayerViewController,
            willEndFullScreenPresentationWithAnimationCoordinator coordinator: any UIViewControllerTransitionCoordinator
        ) {
            // Inline stage: exiting fullscreen transitions back to inline video stage without tearing down playback
        }
    }
}

// MARK: - Infuse Custom Precision Scrubber

struct InfuseScrubber: View {
    let progress: Double // 0.0 to 1.0
    let startTime: String
    let endTime: String
    let remainingText: String?

    var body: some View {
        VStack(spacing: 5) {
            GeometryReader { geo in
                ZStack(alignment: .leading) {
                    Capsule()
                        .fill(Color.white.opacity(0.18))
                        .frame(height: 3)

                    Capsule()
                        .fill(
                            LinearGradient(
                                colors: [Theme.Colors.accentAction, Theme.Colors.accentLive],
                                startPoint: .leading,
                                endPoint: .trailing
                            )
                        )
                        .frame(width: max(0, min(geo.size.width, geo.size.width * CGFloat(progress))), height: 3)

                    Circle()
                        .fill(Color.white)
                        .frame(width: 8, height: 8)
                        .shadow(color: Theme.Colors.accentLive.opacity(0.8), radius: 3)
                        .offset(x: max(0, min(geo.size.width - 8, geo.size.width * CGFloat(progress) - 4)))
                }
            }
            .frame(height: 8)

            HStack {
                Text(startTime)
                    .font(.system(size: 10, weight: .medium, design: .monospaced))
                    .foregroundStyle(Theme.Colors.textTertiary)

                Spacer()

                if let remainingText {
                    Text(remainingText)
                        .font(.system(size: 10, weight: .semibold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.accentLive)
                }

                Spacer()

                Text(endTime)
                    .font(.system(size: 10, weight: .medium, design: .monospaced))
                    .foregroundStyle(Theme.Colors.textTertiary)
            }
        }
    }
}

// MARK: - Portrait Modular Subviews

struct PortraitBroadcastHeroCard: View {
    let currentChannel: Channel
    let nowNext: NowNext?
    let isTimeshifted: Bool
    let isFavorite: Bool
    let onToggleFavorite: () -> Void
    let onRecord: () -> Void
    let onSeekToBeginning: () -> Void
    let onJumpToLive: () -> Void
    let onZapPrevious: () -> Void
    let onZapNext: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            // Channel Header Bar
            HStack(spacing: 10) {
                ChannelLogo(url: currentChannel.logoURL, name: currentChannel.name, size: 40)

                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 5) {
                        if let number = currentChannel.number {
                            Text(number)
                                .font(.system(size: 10, weight: .bold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentAction)
                                .padding(.horizontal, 5)
                                .padding(.vertical, 1.5)
                                .background(Theme.Colors.accentAction.opacity(0.15), in: RoundedRectangle(cornerRadius: 4))
                        }

                        Text(currentChannel.name)
                            .font(.system(size: 15, weight: .bold))
                            .foregroundStyle(Theme.Colors.textPrimary)

                        Button(action: onToggleFavorite) {
                            Image(systemName: isFavorite ? "star.fill" : "star")
                                .font(.system(size: 12))
                                .foregroundStyle(isFavorite ? .yellow : Theme.Colors.textTertiary)
                        }
                        .buttonStyle(.plain)
                    }

                    HStack(spacing: 5) {
                        PulsingLiveDot(size: 5)
                        Text(isTimeshifted ? "TIMESHIFT" : "LIVE")
                            .font(.system(size: 10, weight: .bold, design: .monospaced))
                            .foregroundStyle(isTimeshifted ? Theme.Colors.accentAction : Theme.Colors.accentLive)
                    }
                }

                Spacer(minLength: 4)

                // Quick 1-Tap Record Button
                Button(action: onRecord) {
                    HStack(spacing: 4) {
                        Image(systemName: "record.circle")
                            .font(.system(size: 13, weight: .bold))
                        Text("Aufnehmen")
                            .font(.system(size: 12, weight: .semibold))
                    }
                    .padding(.horizontal, 10)
                    .padding(.vertical, 5)
                    .background(Theme.Colors.statusError.opacity(0.15), in: Capsule())
                    .foregroundStyle(Theme.Colors.statusError)
                    .overlay(Capsule().strokeBorder(Theme.Colors.statusError.opacity(0.3), lineWidth: 0.8))
                }
                .buttonStyle(.plain)
            }

            // Active Show Information
            if let now = nowNext?.now {
                VStack(alignment: .leading, spacing: 4) {
                    Text(now.title)
                        .font(.system(size: 16, weight: .bold))
                        .foregroundStyle(Theme.Colors.textPrimary)
                        .lineLimit(2)

                    HStack(spacing: 6) {
                        Text(now.formattedTimeRange)
                            .font(.system(size: 11, weight: .medium, design: .monospaced))
                            .foregroundStyle(Theme.Colors.textSecondary)

                        if let remaining = now.remainingMinutes(at: .now) {
                            Text("• noch \(remaining) Min")
                                .font(.system(size: 11, weight: .semibold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentLive)
                        }
                    }

                    // Gradient Live Scrubber
                    if let fraction = now.progress(at: .now) {
                        LiveScrubberBar(progress: fraction)
                            .padding(.top, 3)
                    }

                    if let desc = now.description, !desc.isEmpty {
                        Text(desc)
                            .font(.system(size: 12))
                            .foregroundStyle(Theme.Colors.textTertiary)
                            .lineLimit(2)
                            .padding(.top, 2)
                    }
                }
            }

            // Timeshift & Quick Zap Control Bar
            HStack(spacing: 8) {
                Button(action: onSeekToBeginning) {
                    HStack(spacing: 4) {
                        Image(systemName: "arrow.counterclockwise")
                            .font(.system(size: 11, weight: .bold))
                        Text("Von Beginn")
                            .font(.system(size: 11, weight: .semibold))
                    }
                    .padding(.horizontal, 10)
                    .padding(.vertical, 5)
                    .background(Theme.Colors.surfaceElevated, in: Capsule())
                    .foregroundStyle(Theme.Colors.textSecondary)
                    .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                }
                .buttonStyle(.plain)

                if isTimeshifted {
                    Button(action: onJumpToLive) {
                        HStack(spacing: 4) {
                            PulsingLiveDot(size: 5)
                            Text("Zur Live-Kante")
                                .font(.system(size: 11, weight: .bold))
                        }
                        .padding(.horizontal, 10)
                        .padding(.vertical, 5)
                        .background(Theme.Colors.accentLive, in: Capsule())
                        .foregroundStyle(Color.black)
                    }
                    .buttonStyle(.plain)
                }

                Spacer()

                // Zap Prev / Next buttons
                HStack(spacing: 6) {
                    Button(action: onZapPrevious) {
                        Image(systemName: "chevron.backward")
                            .font(.system(size: 12, weight: .bold))
                            .padding(7)
                            .background(Theme.Colors.surfaceElevated, in: Circle())
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                    .buttonStyle(.plain)

                    Button(action: onZapNext) {
                        Image(systemName: "chevron.forward")
                            .font(.system(size: 12, weight: .bold))
                            .padding(7)
                            .background(Theme.Colors.surfaceElevated, in: Circle())
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .overlay(Circle().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.top, 2)

            if let next = nowNext?.next {
                HStack(spacing: 6) {
                    Text("DANACH:")
                        .font(.system(size: 9, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.textTertiary)

                    Text(next.formattedStartTime)
                        .font(.system(size: 10, weight: .bold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.accentAction)

                    Text(next.title)
                        .font(.system(size: 11))
                        .foregroundStyle(Theme.Colors.textSecondary)
                        .lineLimit(1)

                    Spacer()
                }
                .padding(.top, 2)
            }
        }
        .padding(14)
        .background(
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .fill(
                    LinearGradient(
                        colors: [
                            Theme.Colors.surfaceElevated.opacity(0.95),
                            Color(red: 0.08, green: 0.11, blue: 0.16)
                        ],
                        startPoint: .topLeading,
                        endPoint: .bottomTrailing
                    )
                )
        )
        .overlay(
            RoundedRectangle(cornerRadius: 16, style: .continuous)
                .strokeBorder(isTimeshifted ? Theme.Gradients.specularBorder : Theme.Gradients.liveAuraBorder, lineWidth: 0.8)
        )
        .shadow(color: isTimeshifted ? Color.black.opacity(0.2) : Theme.Colors.accentLive.opacity(0.12), radius: 8, y: 3)
    }
}

struct LiveScrubberBar: View {
    let progress: Double

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                Capsule()
                    .fill(Color.white.opacity(0.12))
                    .frame(height: 3.5)

                Capsule()
                    .fill(
                        LinearGradient(
                            colors: [Theme.Colors.accentAction, Theme.Colors.accentLive],
                            startPoint: .leading,
                            endPoint: .trailing
                        )
                    )
                    .frame(width: max(0, min(geo.size.width, geo.size.width * CGFloat(progress))), height: 3.5)
            }
        }
        .frame(height: 3.5)
    }
}

struct PortraitBouquetFilterBar: View {
    let channelsCount: Int
    let favoriteCount: Int
    let bouquets: [Bouquet]
    let selectedBouquet: Bouquet?
    let onSelectBouquet: (Bouquet?) -> Void

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                let isAll = selectedBouquet == nil
                Button {
                    onSelectBouquet(nil)
                } label: {
                    Text("Alle Sender (\(channelsCount))")
                        .font(.system(size: 12, weight: isAll ? .bold : .medium))
                        .padding(.horizontal, 12)
                        .padding(.vertical, 6)
                        .background(isAll ? Theme.Colors.accentAction : Theme.Colors.surfaceElevated, in: Capsule())
                        .foregroundStyle(isAll ? .white : Theme.Colors.textSecondary)
                        .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                }
                .buttonStyle(.plain)

                if favoriteCount > 0 {
                    let isFav = selectedBouquet?.id == AppModel.favoritesBouquetID
                    Button {
                        onSelectBouquet(Bouquet(id: AppModel.favoritesBouquetID, name: "Favoriten"))
                    } label: {
                        HStack(spacing: 4) {
                            Image(systemName: "star.fill")
                                .font(.system(size: 10))
                                .foregroundStyle(.yellow)
                            Text("Favoriten (\(favoriteCount))")
                                .font(.system(size: 12, weight: isFav ? .bold : .medium))
                        }
                        .padding(.horizontal, 12)
                        .padding(.vertical, 6)
                        .background(isFav ? Theme.Colors.accentAction : Theme.Colors.surfaceElevated, in: Capsule())
                        .foregroundStyle(isFav ? .white : Theme.Colors.textSecondary)
                        .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                    .buttonStyle(.plain)
                }

                ForEach(bouquets) { bouquet in
                    let isSelected = selectedBouquet?.id == bouquet.id
                    Button {
                        onSelectBouquet(isSelected ? nil : bouquet)
                    } label: {
                        Text(bouquet.name)
                            .font(.system(size: 12, weight: isSelected ? .bold : .medium))
                            .padding(.horizontal, 12)
                            .padding(.vertical, 6)
                            .background(isSelected ? Theme.Colors.accentAction : Theme.Colors.surfaceElevated, in: Capsule())
                            .foregroundStyle(isSelected ? .white : Theme.Colors.textSecondary)
                            .overlay(Capsule().strokeBorder(Theme.Gradients.specularBorder, lineWidth: 0.8))
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.horizontal, 2)
        }
    }
}

struct PortraitChannelList: View {
    let channels: [Channel]
    let currentChannelID: String
    let schedule: [String: NowNext]
    let onSelectChannel: (Channel) -> Void

    var body: some View {
        LazyVStack(spacing: 8) {
            ForEach(channels) { ch in
                PortraitChannelRow(
                    channel: ch,
                    isCurrent: ch.id == currentChannelID,
                    nowEntry: schedule[ch.serviceRef]?.now,
                    onSelect: { onSelectChannel(ch) }
                )
            }
        }
    }
}

struct PortraitChannelRow: View {
    let channel: Channel
    let isCurrent: Bool
    let nowEntry: NowNext.Entry?
    let onSelect: () -> Void

    var body: some View {
        Button(action: onSelect) {
            HStack(spacing: 10) {
                ChannelLogo(url: channel.logoURL, name: channel.name, size: 36)

                VStack(alignment: .leading, spacing: 2) {
                    HStack(spacing: 5) {
                        if let number = channel.number {
                            Text(number)
                                .font(.system(size: 9, weight: .bold, design: .monospaced))
                                .foregroundStyle(Theme.Colors.accentAction)
                                .padding(.horizontal, 4)
                                .padding(.vertical, 1)
                                .background(Theme.Colors.accentAction.opacity(0.15), in: RoundedRectangle(cornerRadius: 3))
                        }

                        Text(channel.name)
                            .font(.system(size: 14, weight: .bold))
                            .foregroundStyle(isCurrent ? Theme.Colors.accentLive : Theme.Colors.textPrimary)
                            .lineLimit(1)

                        Spacer(minLength: 4)

                        if isCurrent {
                            HStack(spacing: 4) {
                                PulsingLiveDot(size: 5)
                                Text("LÄUFT")
                                    .font(.system(size: 9, weight: .bold, design: .monospaced))
                                    .foregroundStyle(Theme.Colors.accentLive)
                            }
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())
                        } else if let now = nowEntry {
                            Text(now.formattedTimeRange)
                                .font(.system(size: 10, design: .monospaced))
                                .foregroundStyle(Theme.Colors.textTertiary)
                        }
                    }

                    if let now = nowEntry {
                        Text(now.title)
                            .font(.system(size: 12, weight: .medium))
                            .foregroundStyle(Theme.Colors.textSecondary)
                            .lineLimit(1)

                        if let frac = now.progress(at: .now) {
                            MiniProgressBar(progress: frac, isLive: isCurrent)
                                .padding(.top, 1)
                        }
                    } else {
                        Text("Keine Programminformationen")
                            .font(.system(size: 11))
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }
                }
            }
            .padding(10)
            .background(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(isCurrent ? AnyShapeStyle(Theme.Colors.surfaceElevated.opacity(0.95)) : AnyShapeStyle(Theme.Gradients.cardSurface))
            )
            .overlay(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .strokeBorder(isCurrent ? Theme.Gradients.liveAuraBorder : Theme.Gradients.specularBorder, lineWidth: isCurrent ? 1.0 : 0.8)
            )
            .shadow(color: isCurrent ? Theme.Colors.accentLive.opacity(0.12) : Color.black.opacity(0.15), radius: 5, y: 2)
        }
        .buttonStyle(.plain)
    }
}

struct MiniProgressBar: View {
    let progress: Double
    let isLive: Bool

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                Capsule()
                    .fill(Color.white.opacity(0.10))
                    .frame(height: 2.5)

                Capsule()
                    .fill(isLive ? Theme.Colors.accentLive : Theme.Colors.accentAction)
                    .frame(width: max(0, min(geo.size.width, geo.size.width * CGFloat(progress))), height: 2.5)
            }
        }
        .frame(height: 2.5)
    }
}
