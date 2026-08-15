// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import MediaPlayer

/// Publishes live stream metadata to the Lock Screen, Dynamic Island, and Control Center,
/// and handles remote hardware/earbud controls (Next Channel, Previous Channel, Stop).
@MainActor
final class NowPlayingManager {

    static let shared = NowPlayingManager()

    var onNextChannel: (() -> Void)?
    var onPreviousChannel: (() -> Void)?
    var onStop: (() -> Void)?

    private var isConfigured = false

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
        var info: [String: Any] = [
            MPMediaItemPropertyTitle: channel.name,
            MPMediaItemPropertyArtist: nowEntry?.title ?? "Live TV",
            MPMediaItemPropertyAlbumTitle: "xg2g Broadcast",
            MPNowPlayingInfoPropertyIsLiveStream: true,
            MPNowPlayingInfoPropertyPlaybackRate: 1.0,
        ]

        if let desc = nowEntry?.description {
            info[MPMediaItemPropertyComments] = desc
        }

        MPNowPlayingInfoCenter.default().nowPlayingInfo = info
    }

    func clear() {
        MPNowPlayingInfoCenter.default().nowPlayingInfo = nil
    }
}
