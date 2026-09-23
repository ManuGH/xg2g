// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Combine

/// Adapter bridging the Greenfield `PlaybackControlling` protocol to canonical `PlaybackManager`.
@MainActor
final class LegacyBridgePlaybackController: PlaybackControlling {
    private weak var playbackManager: PlaybackManager?
    private weak var appModel: AppModel?

    init(playbackManager: PlaybackManager, appModel: AppModel? = nil) {
        self.playbackManager = playbackManager
        self.appModel = appModel
    }

    convenience init(appModel: AppModel) {
        self.init(playbackManager: appModel.playbackManager, appModel: appModel)
    }

    var currentChannel: Channel? {
        playbackManager?.currentChannel
    }

    var isPlaying: Bool {
        playbackManager?.isPlaying ?? false
    }

    func play(channel: Channel) {
        appModel?.recordChannelPlayback(channel)
        playbackManager?.play(channel: channel, mode: .fullscreen)
    }

    func stop() {
        playbackManager?.stop()
    }

    func togglePlayPause() {
        if isPlaying {
            stop()
        } else if let channel = currentChannel {
            play(channel: channel)
        }
    }

    func observeState(_ handler: @escaping @MainActor (_ channel: Channel?, _ isPlaying: Bool) -> Void) -> AnyCancellable {
        guard let playbackManager else {
            handler(nil, false)
            return AnyCancellable {}
        }
        return playbackManager.observeState(handler)
    }
}
