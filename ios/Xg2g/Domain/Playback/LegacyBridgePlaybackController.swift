// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Combine
import CoreMedia

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

    var currentTarget: PlaybackTarget? {
        playbackManager?.currentTarget
    }

    var currentChannel: Channel? {
        playbackManager?.currentChannel
    }

    var currentRecording: Recording? {
        playbackManager?.activeRecordingItem?.recording
    }

    var isPlaying: Bool {
        playbackManager?.isPlaying ?? false
    }

    func play(channel: Channel) {
        appModel?.recordChannelPlayback(channel)
        playbackManager?.play(channel: channel, mode: .fullscreen)
    }

    func play(recording: Recording, startPosition: Double? = nil) {
        playbackManager?.play(recording: recording, startPosition: startPosition ?? 0, mode: .fullscreen)
    }

    func seek(to position: Double) {
        playbackManager?.seek(to: position)
    }

    func stop() {
        playbackManager?.stop()
    }

    /// Toggles play / pause transport state.
    ///
    /// - Note: In C1, calling this while playing stops playback completely. Because `stop()` resets
    ///   the state to `.idle` (clearing `currentChannel`), subsequent calls cannot retune.
    ///   It does NOT provide VOD pause/resume for recordings.
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

    func observeTargetState(_ handler: @escaping @MainActor (_ target: PlaybackTarget?, _ isPlaying: Bool) -> Void) -> AnyCancellable {
        guard let playbackManager else {
            handler(nil, false)
            return AnyCancellable {}
        }
        return playbackManager.observeTargetState(handler)
    }
}
