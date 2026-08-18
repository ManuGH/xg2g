// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import Foundation
import MediaPlayer
import UIKit

/// Publishes live stream metadata and high-res channel artwork to the Lock Screen, Dynamic Island, and Control Center,
/// and handles remote hardware/earbud controls (Next Channel, Previous Channel, Stop).
@MainActor
final class NowPlayingManager {

    static let shared = NowPlayingManager()

    /// Lock screen artwork is drawn much larger than an in-app logo, so it gets
    /// the top `LogoImageCache` bucket.
    private static let artworkBucket = 256

    var onNextChannel: (() -> Void)?
    var onPreviousChannel: (() -> Void)?
    var onStop: (() -> Void)?
    var onPlay: (() -> Void)?
    var onPause: (() -> Void)?
    var onSeekRelative: ((Double) -> Void)?

    /// Separate from `onPlay`, because the toggle is not a play.
    var onTogglePlayPause: (() -> Void)?

    private var isConfigured = false
    private var currentArtworkTask: Task<Void, Never>?

    private init() {}

    /// Everything the command centre can invoke, handed over as one set.
    ///
    /// The command centre is process-wide and these handlers are a singleton's,
    /// so two screens wiring themselves up piecemeal is a cross-connection
    /// waiting to happen: the native player set play, pause and the two zap
    /// commands, left stop and seek pointing at whatever the HLS player had
    /// installed, and nothing was cleared when either went away. Taking the set
    /// over as a whole makes that impossible to express — an omitted handler
    /// disables its command rather than inheriting the last screen's.
    struct Handlers {
        var play: (() -> Void)?
        var pause: (() -> Void)?
        var togglePlayPause: (() -> Void)?
        var stop: (() -> Void)?
        var nextChannel: (() -> Void)?
        var previousChannel: (() -> Void)?
        var seekRelative: ((Double) -> Void)?

        init(
            play: (() -> Void)? = nil,
            pause: (() -> Void)? = nil,
            togglePlayPause: (() -> Void)? = nil,
            stop: (() -> Void)? = nil,
            nextChannel: (() -> Void)? = nil,
            previousChannel: (() -> Void)? = nil,
            seekRelative: ((Double) -> Void)? = nil
        ) {
            self.play = play
            self.pause = pause
            self.togglePlayPause = togglePlayPause
            self.stop = stop
            self.nextChannel = nextChannel
            self.previousChannel = previousChannel
            self.seekRelative = seekRelative
        }
    }

    /// Claims the remote controls for one screen, replacing whatever the last
    /// one left behind.
    func takeOver(_ handlers: Handlers) {
        registerCommands()

        onPlay = handlers.play
        onPause = handlers.pause
        onTogglePlayPause = handlers.togglePlayPause
        onStop = handlers.stop
        onNextChannel = handlers.nextChannel
        onPreviousChannel = handlers.previousChannel
        onSeekRelative = handlers.seekRelative

        // A command with no handler is switched off rather than left on screen
        // doing nothing. Live has nothing to skip to, so the skip commands stay
        // dark until something can answer them.
        let centre = MPRemoteCommandCenter.shared()
        centre.playCommand.isEnabled = handlers.play != nil
        centre.pauseCommand.isEnabled = handlers.pause != nil
        centre.togglePlayPauseCommand.isEnabled = handlers.togglePlayPause != nil || handlers.play != nil
        centre.stopCommand.isEnabled = handlers.stop != nil
        centre.nextTrackCommand.isEnabled = handlers.nextChannel != nil
        centre.previousTrackCommand.isEnabled = handlers.previousChannel != nil
        centre.skipForwardCommand.isEnabled = handlers.seekRelative != nil
        centre.skipBackwardCommand.isEnabled = handlers.seekRelative != nil
    }

    /// Gives the controls up. Called when a screen goes away so its handlers do
    /// not answer for the next one.
    func resignRemoteControls() {
        takeOver(Handlers())
    }

    private func registerCommands() {
        guard !isConfigured else { return }
        isConfigured = true

        let commandCenter = MPRemoteCommandCenter.shared()

        // Each target reports failure when nothing is wired, rather than
        // reporting success and doing nothing: a control that silently swallows
        // a press is indistinguishable from a broken player.
        commandCenter.playCommand.addTarget { [weak self] _ in
            self?.invoke(\.onPlay) ?? .commandFailed
        }
        commandCenter.pauseCommand.addTarget { [weak self] _ in
            self?.invoke(\.onPause) ?? .commandFailed
        }
        // Toggle is what a lock screen button and a headphone pinch actually
        // send. It used to call `onPlay` unconditionally, which on the native
        // player re-tuned the channel — the one thing a pause button must not do.
        commandCenter.togglePlayPauseCommand.addTarget { [weak self] _ in
            guard let self = self else { return .commandFailed }
            if self.onTogglePlayPause != nil { return self.invoke(\.onTogglePlayPause) }
            return self.invoke(\.onPlay)
        }
        commandCenter.stopCommand.addTarget { [weak self] _ in
            self?.invoke(\.onStop) ?? .commandFailed
        }
        commandCenter.nextTrackCommand.addTarget { [weak self] _ in
            self?.invoke(\.onNextChannel) ?? .commandFailed
        }
        commandCenter.previousTrackCommand.addTarget { [weak self] _ in
            self?.invoke(\.onPreviousChannel) ?? .commandFailed
        }

        commandCenter.skipForwardCommand.preferredIntervals = [30]
        commandCenter.skipForwardCommand.addTarget { [weak self] _ in
            self?.invokeSeek(30) ?? .commandFailed
        }
        commandCenter.skipBackwardCommand.preferredIntervals = [10]
        commandCenter.skipBackwardCommand.addTarget { [weak self] _ in
            self?.invokeSeek(-10) ?? .commandFailed
        }
    }

    private func invoke(_ key: KeyPath<NowPlayingManager, (() -> Void)?>) -> MPRemoteCommandHandlerStatus {
        guard let handler = self[keyPath: key] else { return .commandFailed }
        DispatchQueue.main.async { handler() }
        return .success
    }

    private func invokeSeek(_ seconds: Double) -> MPRemoteCommandHandlerStatus {
        guard let handler = onSeekRelative else { return .commandFailed }
        DispatchQueue.main.async { handler(seconds) }
        return .success
    }

    /// Publishes the minimum a lock screen needs for a source with no `Channel`
    /// behind it.
    ///
    /// Without this there is no now-playing entry at all, and `updatePlaybackState`
    /// returns at its first line because there is nothing to update — which is why
    /// the native player's controls did nothing while locked: not a broken
    /// command, an empty lock screen.
    func updateLive(title: String, subtitle: String? = nil) {
        currentArtworkTask?.cancel()
        var info: [String: Any] = [
            MPMediaItemPropertyTitle: title,
            MPMediaItemPropertyArtist: subtitle ?? "xg2g Live TV",
            MPMediaItemPropertyAlbumTitle: "xg2g Live TV",
            MPNowPlayingInfoPropertyIsLiveStream: true,
            MPNowPlayingInfoPropertyPlaybackRate: 1.0,
        ]
        info[MPMediaItemPropertyArtwork] = Self.makeArtwork(from: Self.createFallbackArtwork(for: title))
        MPNowPlayingInfoCenter.default().nowPlayingInfo = info
    }

    func update(channel: Channel, nowEntry: NowNext.Entry?) {
        currentArtworkTask?.cancel()

        var info: [String: Any] = [
            MPMediaItemPropertyTitle: nowEntry?.title ?? channel.name,
            MPMediaItemPropertyArtist: channel.name,
            MPMediaItemPropertyAlbumTitle: "xg2g Live TV",
            MPNowPlayingInfoPropertyIsLiveStream: true,
            MPNowPlayingInfoPropertyPlaybackRate: 1.0,
        ]

        if let desc = nowEntry?.description {
            info[MPMediaItemPropertyComments] = desc
        }

        // 1. If logo is already cached in memory, attach immediately
        if let logoURL = channel.logoURL, let cached = LogoImageCache.shared.anyImage(for: logoURL) {
            info[MPMediaItemPropertyArtwork] = Self.makeArtwork(from: cached)
            MPNowPlayingInfoCenter.default().nowPlayingInfo = info
            return
        }

        // 2. Set default badge artwork immediately
        let fallback = Self.createFallbackArtwork(for: channel.name)
        info[MPMediaItemPropertyArtwork] = Self.makeArtwork(from: fallback)
        MPNowPlayingInfoCenter.default().nowPlayingInfo = info

        // 3. Asynchronously fetch the channel logo and update Lock Screen artwork
        if let logoURL = channel.logoURL {
            currentArtworkTask = Task {
                guard let (data, _) = try? await URLSession.shared.data(from: logoURL) else { return }

                // Lock screen artwork is the largest rendition the app asks for,
                // and decoding it here keeps a full-resolution bitmap out of both
                // the cache and the main thread.
                let bucket = Self.artworkBucket
                let image = await Task.detached(priority: .utility) {
                    LogoImageCache.downsampledImage(from: data, bucket: bucket)
                }.value

                guard let image, !Task.isCancelled else { return }
                LogoImageCache.shared.store(image, for: logoURL, bucket: bucket)

                var updated = MPNowPlayingInfoCenter.default().nowPlayingInfo ?? info
                updated[MPMediaItemPropertyArtwork] = Self.makeArtwork(from: image)
                MPNowPlayingInfoCenter.default().nowPlayingInfo = updated
            }
        }
    }

    func updatePlaybackState(isPlaying: Bool) {
        guard var info = MPNowPlayingInfoCenter.default().nowPlayingInfo else { return }
        info[MPNowPlayingInfoPropertyPlaybackRate] = isPlaying ? 1.0 : 0.0
        MPNowPlayingInfoCenter.default().nowPlayingInfo = info
    }

    func clear() {
        currentArtworkTask?.cancel()
        MPNowPlayingInfoCenter.default().nowPlayingInfo = nil
        resignRemoteControls()
    }

    private nonisolated static func makeArtwork(from image: UIImage) -> MPMediaItemArtwork {
        let targetSize = CGSize(width: 512, height: 512)
        let rendered = UIGraphicsImageRenderer(size: targetSize).image { ctx in
            // Studio Dark Gradient Background
            let rect = CGRect(origin: .zero, size: targetSize)
            let colors = [
                UIColor(red: 0.12, green: 0.14, blue: 0.20, alpha: 1.0).cgColor,
                UIColor(red: 0.05, green: 0.06, blue: 0.09, alpha: 1.0).cgColor
            ]
            let colorSpace = CGColorSpaceCreateDeviceRGB()
            if let gradient = CGGradient(colorsSpace: colorSpace, colors: colors as CFArray, locations: [0.0, 1.0]) {
                ctx.cgContext.drawLinearGradient(gradient, start: CGPoint(x: 0, y: 0), end: CGPoint(x: 0, y: targetSize.height), options: [])
            }

            // Draw image aspect-fit with clean margins
            let margin: CGFloat = 56
            let fitRect = AVMakeRect(aspectRatio: image.size, insideRect: rect.insetBy(dx: margin, dy: margin))
            image.draw(in: fitRect)
        }

        return MPMediaItemArtwork(boundsSize: targetSize) { _ in
            rendered
        }
    }

    private nonisolated static func createFallbackArtwork(for channelName: String) -> UIImage {
        let size = CGSize(width: 512, height: 512)
        return UIGraphicsImageRenderer(size: size).image { ctx in
            let colors = [
                UIColor(red: 0.15, green: 0.18, blue: 0.26, alpha: 1.0).cgColor,
                UIColor(red: 0.05, green: 0.07, blue: 0.12, alpha: 1.0).cgColor
            ]
            let colorSpace = CGColorSpaceCreateDeviceRGB()
            if let gradient = CGGradient(colorsSpace: colorSpace, colors: colors as CFArray, locations: [0.0, 1.0]) {
                ctx.cgContext.drawLinearGradient(gradient, start: CGPoint(x: 0, y: 0), end: CGPoint(x: 0, y: size.height), options: [])
            }

            let text = channelName
            let paragraphStyle = NSMutableParagraphStyle()
            paragraphStyle.alignment = .center
            let attrs: [NSAttributedString.Key: Any] = [
                .font: UIFont.systemFont(ofSize: 44, weight: .bold),
                .foregroundColor: UIColor.white,
                .paragraphStyle: paragraphStyle
            ]
            let textRect = CGRect(x: 32, y: (size.height - 60) / 2, width: size.width - 64, height: 80)
            (text as NSString).draw(in: textRect, withAttributes: attrs)
        }
    }
}
