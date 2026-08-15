// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import SwiftUI

/// Live playback for one channel.
///
/// The screen owns the session's lifetime: it starts a stream when it appears
/// and stops it when it goes away. A session that outlived its screen would
/// hold a tuner for a stream nobody is watching, which is exactly how a lease
/// ends up being reclaimed minutes later instead of immediately.
struct PlayerScreen: View {

    let model: AppModel
    let channel: Channel

    @Environment(\.dismiss) private var dismiss
    @State private var player: AVPlayer?
    @State private var failure: String?

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()

            if let player {
                VideoPlayer(player: player)
                    .ignoresSafeArea()
            } else if let failure {
                ContentUnavailableView("Cannot play \(channel.name)", systemImage: "exclamationmark.triangle", description: Text(failure))
                    .foregroundStyle(.white)
            } else {
                ProgressView("Tuning \(channel.name)…")
                    .tint(.white)
                    .foregroundStyle(.white)
            }

            VStack {
                HStack {
                    Button { dismiss() } label: {
                        Image(systemName: "xmark.circle.fill")
                            .font(.title)
                            .foregroundStyle(.white.opacity(0.85))
                    }
                    .padding()
                    Spacer()
                }
                Spacer()
            }
        }
        .task {
            await model.play(channel)

            guard let stream = model.liveStream else {
                failure = model.lastError ?? "The stream could not be started."
                return
            }
            player = Self.makePlayer(for: stream)
            player?.play()
        }
        .onDisappear {
            player?.pause()
            player = nil
            Task { await model.stopPlayback() }
        }
    }

    /// Builds the player with the playback ticket attached.
    ///
    /// `AVURLAssetHTTPCookiesKey` is what makes a native player possible here:
    /// AVFoundation fetches the playlist and every segment itself, and this is
    /// the supported way to give those internal requests a credential. The
    /// alternative would be a token in the URL — which would then appear in
    /// access logs, in referrers and in the player's own cache — or a resource
    /// loader intercepting every segment, which breaks across ABR, AirPlay and
    /// PiP.
    ///
    /// The cookie is passed explicitly rather than written into
    /// `HTTPCookieStorage.shared`, so it exists only for this asset and does not
    /// become ambient state that some other request might pick up.
    private static func makePlayer(for stream: LiveStream) -> AVPlayer? {
        guard let cookie = stream.ticket.httpCookie(for: stream.playlistURL) else { return nil }

        let asset = AVURLAsset(
            url: stream.playlistURL,
            options: [AVURLAssetHTTPCookiesKey: [cookie]]
        )
        let item = AVPlayerItem(asset: asset)
        let player = AVPlayer(playerItem: item)

        // Live: catch up rather than drift further behind after a stall.
        player.automaticallyWaitsToMinimizeStalling = true
        return player
    }
}
