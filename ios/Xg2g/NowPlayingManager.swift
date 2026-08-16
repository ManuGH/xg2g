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

    var onNextChannel: (() -> Void)?
    var onPreviousChannel: (() -> Void)?
    var onStop: (() -> Void)?

    private var isConfigured = false
    private var currentArtworkTask: Task<Void, Never>?

    private init() {}

    func setupRemoteCommands() {
        guard !isConfigured else { return }
        isConfigured = true

        let commandCenter = MPRemoteCommandCenter.shared()

        commandCenter.playCommand.isEnabled = true
        commandCenter.playCommand.addTarget { _ in .success }

        commandCenter.pauseCommand.isEnabled = true
        commandCenter.pauseCommand.addTarget { [weak self] _ in
            self?.onStop?()
            return .success
        }

        commandCenter.stopCommand.isEnabled = true
        commandCenter.stopCommand.addTarget { [weak self] _ in
            self?.onStop?()
            return .success
        }

        commandCenter.nextTrackCommand.isEnabled = true
        commandCenter.nextTrackCommand.addTarget { [weak self] _ in
            self?.onNextChannel?()
            return .success
        }

        commandCenter.previousTrackCommand.isEnabled = true
        commandCenter.previousTrackCommand.addTarget { [weak self] _ in
            self?.onPreviousChannel?()
            return .success
        }
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
        if let logoURL = channel.logoURL, let cached = LogoImageCache.shared.image(for: logoURL) {
            info[MPMediaItemPropertyArtwork] = makeArtwork(from: cached)
            MPNowPlayingInfoCenter.default().nowPlayingInfo = info
            return
        }

        // 2. Set default badge artwork immediately
        let fallback = createFallbackArtwork(for: channel)
        info[MPMediaItemPropertyArtwork] = makeArtwork(from: fallback)
        MPNowPlayingInfoCenter.default().nowPlayingInfo = info

        // 3. Asynchronously fetch the channel logo and update Lock Screen artwork
        if let logoURL = channel.logoURL {
            currentArtworkTask = Task { [weak self] in
                guard let self else { return }
                if let (data, _) = try? await URLSession.shared.data(from: logoURL),
                   let image = UIImage(data: data) {
                    LogoImageCache.shared.store(image, for: logoURL)
                    if !Task.isCancelled {
                        var updated = MPNowPlayingInfoCenter.default().nowPlayingInfo ?? info
                        updated[MPMediaItemPropertyArtwork] = self.makeArtwork(from: image)
                        MPNowPlayingInfoCenter.default().nowPlayingInfo = updated
                    }
                }
            }
        }
    }

    func clear() {
        currentArtworkTask?.cancel()
        MPNowPlayingInfoCenter.default().nowPlayingInfo = nil
    }

    private func makeArtwork(from image: UIImage) -> MPMediaItemArtwork {
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

    private func createFallbackArtwork(for channel: Channel) -> UIImage {
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

            let text = channel.name
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
