// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// Adapter bridging the Greenfield `PlaybackControlling` protocol to the existing `AppModel`.
@MainActor
final class LegacyBridgePlaybackController: PlaybackControlling {
    private weak var appModel: AppModel?

    init(appModel: AppModel) {
        self.appModel = appModel
    }

    var currentChannel: Channel? {
        appModel?.playingChannel
    }

    var isPlaying: Bool {
        appModel?.playingChannel != nil
    }

    func play(channel: Channel) {
        appModel?.playingChannel = channel
    }

    func stop() {
        appModel?.playingChannel = nil
        Task { [weak appModel] in
            await appModel?.stopPlayback()
        }
    }

    func togglePlayPause() {
        // AppModel delegates pause/play to active PlayerScreen or PlaybackManager
        if isPlaying {
            stop()
        }
    }
}
