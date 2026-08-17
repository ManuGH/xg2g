// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation

/// Configures and manages the iOS audio session for video/live-stream playback,
/// background audio, and seamless Picture-in-Picture.
final class AudioSessionManager: @unchecked Sendable {

    static let shared = AudioSessionManager()

    private init() {}

    func configureForPlayback() {
        do {
            let session = AVAudioSession.sharedInstance()
            try session.setCategory(.playback, mode: .moviePlayback, options: [.allowAirPlay, .allowBluetooth, .allowBluetoothA2DP])
            if #available(iOS 15.0, *) {
                try session.setSupportsMultichannelContent(true)
            }
            try session.setActive(true)
        } catch {
            // Audio session setup is best-effort on simulators
        }
    }

    func deactivate() {
        do {
            try AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        } catch {
            // Best effort
        }
    }
}
