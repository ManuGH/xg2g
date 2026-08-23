// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import OSLog

private let logger = Logger(subsystem: "io.github.manugh.xg2g.ios", category: "audio-session")

/// Configures and manages the iOS audio session for video/live-stream playback,
/// background audio, and seamless Picture-in-Picture.
final class AudioSessionManager: @unchecked Sendable {

    static let shared = AudioSessionManager()
    private let queue = DispatchQueue(label: "io.github.manugh.xg2g.audiosession", qos: .userInitiated)

    private init() {}

    /// Below this many output channels the route cannot carry multichannel
    /// content — every Bluetooth headphone, AirPods included, sits here.
    private static let stereoChannelCount = 2

    func configureForPlayback() {
        queue.async {
            do {
                let session = AVAudioSession.sharedInstance()
                try session.setCategory(.playback, mode: .moviePlayback, options: [])

                if #available(iOS 15.0, *) {
                    // Claiming multichannel support on a two-channel route asks the
                    // system to keep a 5.1 configuration alive across a link that
                    // cannot carry it, and the fold-down that follows is exactly
                    // where AirPods playback loses its dialogue. Claim it only where
                    // the route can actually take it (HDMI, AirPlay, multichannel USB).
                    //
                    // Set while the session is still inactive, as the category is:
                    // this is session configuration, and a route change re-runs the
                    // whole function anyway, so a route that appears later is picked
                    // up there rather than by reordering the setup.
                    let outputChannels = session.currentRoute.outputs
                        .map { $0.channels?.count ?? 0 }
                        .max() ?? session.outputNumberOfChannels
                    let supportsMultichannel = outputChannels > Self.stereoChannelCount
                    try session.setSupportsMultichannelContent(supportsMultichannel)
                }

                try session.setActive(true)
                self.logCurrentRoute(session)
            } catch {
                logger.error("[AudioSessionManager] ⚠️ Audio session config warning: \(String(describing: error), privacy: .public)")
                print("[AudioSessionManager] ⚠️ Audio session config warning: \(error)")
            }
        }
    }

    /// Whether the active output route is a Bluetooth link.
    ///
    /// Bluetooth is the route where sample rate, channel count, and output
    /// latency all differ sharply from the built-in speaker, so it is the case
    /// worth naming in diagnostics.
    var isBluetoothOutput: Bool {
        AVAudioSession.sharedInstance().currentRoute.outputs.contains { output in
            output.portType == .bluetoothA2DP
                || output.portType == .bluetoothLE
                || output.portType == .bluetoothHFP
        }
    }

    /// Records what the audio actually comes out of.
    ///
    /// Route-dependent playback problems are unfalsifiable without this: the
    /// port type separates A2DP from the narrow-band HFP fallback, and the
    /// sample rate and channel count show what the renderer is being folded into.
    func logCurrentRoute(_ session: AVAudioSession = .sharedInstance()) {
        let outputs = session.currentRoute.outputs
            .map { "\($0.portType.rawValue)/\($0.portName) ch=\($0.channels?.count ?? 0)" }
            .joined(separator: ", ")
        let msg = "[AudioSessionManager] 🎧 Route: [\(outputs)] | session \(Int(session.sampleRate))Hz \(session.outputNumberOfChannels)ch | outputLatency \(String(format: "%.1f", session.outputLatency * 1000))ms | ioBuffer \(String(format: "%.1f", session.ioBufferDuration * 1000))ms"
        logger.notice("\(msg, privacy: .public)")
        print(msg)
    }

    func deactivate() {
        queue.async {
            do {
                try AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
            } catch {
                // Best effort
            }
        }
    }
}
