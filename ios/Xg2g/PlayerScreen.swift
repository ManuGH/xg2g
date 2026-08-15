// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import CoreMedia
import SwiftUI
import UIKit

/// 100% Native Apple AVPlayerViewController playback with YouTube TV / Zattoo-grade
/// in-playback live EPG channel guide, 1-tap instant zapping, bouquet filtering,
/// AirPlay 2, Picture-in-Picture, and fullscreen OLED rotation.
struct PlayerScreen: View {

    let model: AppModel
    let channel: Channel

    @Environment(\.dismiss) private var dismiss
    @State private var currentChannel: Channel
    @State private var player: AVPlayer?
    @State private var failure: String?
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

            ZStack {
                Theme.Colors.bgBase.ignoresSafeArea()

                if let player {
                    if isLandscape {
                        // MARK: - Landscape 100% Fullscreen OLED Cinema Mode
                        NativeVideoPlayerView(
                            player: player,
                            onDismiss: { closePlayer() }
                        )
                        .ignoresSafeArea()
                    } else {
                        // MARK: - Portrait Dual-Stage (16:9 Top Player + In-Playback EPG Guide)
                        VStack(spacing: 0) {
                            // Top 16:9 Native Video Player Stage
                            ZStack(alignment: .topLeading) {
                                NativeVideoPlayerView(
                                    player: player,
                                    onDismiss: { closePlayer() }
                                )
                                .frame(width: geometry.size.width, height: geometry.size.width * 9 / 16)
                                .background(Color.black)

                                // Minimal Dismiss Button
                                Button {
                                    closePlayer()
                                } label: {
                                    Image(systemName: "chevron.down")
                                        .font(.system(size: 13, weight: .bold))
                                        .foregroundStyle(.white)
                                        .padding(8)
                                        .background(Color.black.opacity(0.65), in: Circle())
                                }
                                .buttonStyle(.plain)
                                .padding(.top, max(6, geometry.safeAreaInsets.top))
                                .padding(.leading, 12)
                            }
                            .frame(width: geometry.size.width, height: geometry.size.width * 9 / 16)

                            // Bottom Interactive Live TV Channel & EPG Guide
                            ScrollView {
                                VStack(spacing: 14) {
                                    // Active Channel & Program Card
                                    if let now = nowNext?.now {
                                        VStack(alignment: .leading, spacing: 8) {
                                            HStack {
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

                                                Spacer()

                                                HStack(spacing: 4) {
                                                    PulsingLiveDot(size: 6)
                                                    Text("LIVE")
                                                        .font(.caption2.bold().monospaced())
                                                        .foregroundStyle(Theme.Colors.accentLive)
                                                }
                                                .padding(.horizontal, 8)
                                                .padding(.vertical, 3)
                                                .background(Theme.Colors.accentLive.opacity(0.15), in: Capsule())

                                                // Quick Record Button
                                                Button {
                                                    triggerHaptic(.medium)
                                                    Task {
                                                        let ok = await model.recordLiveNow(channel: currentChannel)
                                                        displayZapToast(ok ? "🔴 Aufnahme gestartet" : "Aufnahmefehler")
                                                    }
                                                } label: {
                                                    Image(systemName: "record.circle")
                                                        .font(.title3)
                                                        .foregroundStyle(Theme.Colors.statusError)
                                                        .padding(4)
                                                }
                                                .buttonStyle(.plain)
                                            }

                                            Text(now.title)
                                                .font(.subheadline.weight(.semibold))
                                                .foregroundStyle(Theme.Colors.textPrimary)

                                            if let description = now.description {
                                                Text(description)
                                                    .font(.caption)
                                                    .foregroundStyle(Theme.Colors.textSecondary)
                                                    .lineLimit(3)
                                            }

                                            if let fraction = now.progress(at: .now) {
                                                InfuseScrubber(
                                                    progress: fraction,
                                                    startTime: now.formattedStartTime,
                                                    endTime: now.formattedEndTime,
                                                    remainingText: now.remainingMinutes(at: .now).map { "noch \($0)m" }
                                                )
                                                .padding(.top, 2)
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
                                        .background(Theme.Colors.surfaceElevated, in: RoundedRectangle(cornerRadius: 14))
                                        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(Theme.Colors.borderSubtle, lineWidth: 1))
                                    }

                                    // Bouquet Selector Pills (Filter while watching!)
                                    if !model.bouquets.isEmpty {
                                        ScrollView(.horizontal, showsIndicators: false) {
                                            HStack(spacing: 8) {
                                                ForEach(model.bouquets) { bouquet in
                                                    let isSelected = model.selectedBouquet?.id == bouquet.id
                                                    Button {
                                                        triggerHaptic(.light)
                                                        model.selectedBouquet = isSelected ? nil : bouquet
                                                    } label: {
                                                        Text(bouquet.name)
                                                            .font(.caption.weight(isSelected ? .semibold : .regular))
                                                            .foregroundStyle(isSelected ? Theme.Colors.textPrimary : Theme.Colors.textSecondary)
                                                            .padding(.horizontal, 12)
                                                            .padding(.vertical, 6)
                                                            .background(
                                                                isSelected ? Theme.Colors.accentAction : Theme.Colors.surfaceElevated,
                                                                in: Capsule()
                                                            )
                                                            .overlay(
                                                                Capsule().strokeBorder(isSelected ? Color.clear : Theme.Colors.borderSubtle, lineWidth: 1)
                                                            )
                                                    }
                                                    .buttonStyle(.plain)
                                                }
                                            }
                                            .padding(.horizontal, 2)
                                        }
                                    }

                                    // Section Header
                                    HStack {
                                        Text("SENDER & LIVE-PROGRAMM")
                                            .font(.system(size: 11, weight: .bold, design: .monospaced))
                                            .foregroundStyle(Theme.Colors.textTertiary)

                                        Spacer()

                                        Text("\(model.filteredChannels.count) Sender")
                                            .font(.caption2.monospacedDigit())
                                            .foregroundStyle(Theme.Colors.textTertiary)
                                    }
                                    .padding(.horizontal, 4)

                                    // Interactive Channel List (1-Tap Zapping!)
                                    LazyVStack(spacing: 8) {
                                        ForEach(model.filteredChannels) { ch in
                                            let isCurrent = ch.id == currentChannel.id
                                            Button {
                                                switchChannel(to: ch)
                                            } label: {
                                                HStack(spacing: 12) {
                                                    ChannelLogo(url: ch.logoURL, name: ch.name)

                                                    VStack(alignment: .leading, spacing: 4) {
                                                        HStack(spacing: 6) {
                                                            if let number = ch.number {
                                                                Text(number)
                                                                    .font(.caption.monospacedDigit().bold())
                                                                    .foregroundStyle(Theme.Colors.accentAction)
                                                                    .frame(minWidth: 20, alignment: .leading)
                                                            }

                                                            Text(ch.name)
                                                                .font(.body.weight(.semibold))
                                                                .foregroundStyle(isCurrent ? Theme.Colors.accentLive : Theme.Colors.textPrimary)
                                                                .lineLimit(1)

                                                            Spacer()

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
                                                            }
                                                        }

                                                        if let nowEntry = model.schedule[ch.serviceRef]?.now {
                                                            Text(nowEntry.title)
                                                                .font(.caption)
                                                                .foregroundStyle(Theme.Colors.textSecondary)
                                                                .lineLimit(1)

                                                            if let frac = nowEntry.progress(at: .now) {
                                                                ProgressView(value: frac)
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
                                                .padding(12)
                                                .background(
                                                    isCurrent
                                                        ? Theme.Colors.accentAction.opacity(0.15)
                                                        : Theme.Colors.surfaceElevated,
                                                    in: RoundedRectangle(cornerRadius: 12)
                                                )
                                                .overlay(
                                                    RoundedRectangle(cornerRadius: 12)
                                                        .strokeBorder(isCurrent ? Theme.Colors.accentLive.opacity(0.6) : Theme.Colors.borderSubtle, lineWidth: 1)
                                                )
                                            }
                                            .buttonStyle(.plain)
                                        }
                                    }
                                }
                                .padding(14)
                            }
                        }
                    }
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
                            .scaleEffect(1.2)

                        Text("\(currentChannel.name) wird geladen…")
                            .font(.subheadline.weight(.medium))
                            .foregroundStyle(Theme.Colors.textSecondary)

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
                        .padding(.top, 6)
                    }
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
        }
        .task(id: currentChannel.id) {
            await startStreaming(channel: currentChannel)
        }
        .onDisappear {
            teardownPlayer()
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
        player = Self.makePlayer(for: stream, channel: channel, nowNext: nowNext)
        player?.play()
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

    /// Builds the player with native metadata attached for the Apple system player.
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
        item.automaticallyPreservesTimeOffsetFromLive = false

        // Inject Native iOS OSD Metadata
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
        return player
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
        controller.updatesNowPlayingInfoCenter = true
        controller.delegate = context.coordinator
        return controller
    }

    func updateUIViewController(_ controller: AVPlayerViewController, context: Context) {
        if controller.player !== player {
            controller.player = player
        }
        controller.videoGravity = videoGravity
        controller.showsPlaybackControls = true
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
            coordinator.animate(alongsideTransition: nil) { [weak self] _ in
                self?.parent.onDismiss?()
            }
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
