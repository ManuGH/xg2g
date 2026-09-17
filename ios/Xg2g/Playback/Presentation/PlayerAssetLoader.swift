// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import CoreMedia
import Foundation
import UIKit

/// Unified asset and options builder for AVPlayer instances in Xg2g.
///
/// Ensures all video streams (Live, DVR recordings, offline) consistently
/// carry required authentication cookies, User-Agent, and origin headers.
enum PlayerAssetLoader {

    static func makeAsset(url: URL, baseURL: URL? = nil, extraHeaders: [String: String] = [:]) -> AVURLAsset {
        let cookieTarget = baseURL ?? url
        var options: [String: Any] = [:]
        var headers = extraHeaders
        if headers["User-Agent"] == nil {
            headers["User-Agent"] = "xg2g-ios/3.0"
        }

        var allCookies = HTTPCookieStorage.shared.cookies(for: cookieTarget) ?? []
        if let streamCookies = HTTPCookieStorage.shared.cookies(for: url), !streamCookies.isEmpty {
            for c in streamCookies where !allCookies.contains(where: { $0.name == c.name }) {
                allCookies.append(c)
            }
        }

        if !allCookies.isEmpty {
            options[AVURLAssetHTTPCookiesKey] = allCookies
            let cookieHeader = allCookies.map { "\($0.name)=\($0.value)" }.joined(separator: "; ")
            headers["Cookie"] = cookieHeader
        }

        options["AVURLAssetHTTPHeaderFieldsKey"] = headers
        return AVURLAsset(url: url, options: options)
    }

    static func makePlayerItem(url: URL, baseURL: URL? = nil, extraHeaders: [String: String] = [:]) -> AVPlayerItem {
        let asset = makeAsset(url: url, baseURL: baseURL, extraHeaders: extraHeaders)
        return AVPlayerItem(asset: asset)
    }

    /// Builds a configured AVPlayer for a live HLS stream with authentication cookies and DVR settings.
    @MainActor
    static func makeLivePlayer(for stream: LiveStream, channel: Channel? = nil, nowNext: NowNext? = nil) -> AVPlayer {
        var cookies: [HTTPCookie] = []
        if let cookie = stream.ticket.httpCookie(for: stream.playlistURL) {
            HTTPCookieStorage.shared.setCookie(cookie)
            cookies.append(cookie)
        }
        if let rootCookie = stream.ticket.rootCookie(for: stream.playlistURL) {
            HTTPCookieStorage.shared.setCookie(rootCookie)
            cookies.append(rootCookie)
        }

        var options: [String: Any] = [:]
        var headers: [String: String] = [
            "User-Agent": "xg2g-ios/3.0"
        ]

        if !cookies.isEmpty {
            options[AVURLAssetHTTPCookiesKey] = cookies
            let cookieHeader = cookies.map { "\($0.name)=\($0.value)" }.joined(separator: "; ")
            headers["Cookie"] = cookieHeader
        }
        options["AVURLAssetHTTPHeaderFieldsKey"] = headers

        let asset = AVURLAsset(url: stream.playlistURL, options: options)
        let item = AVPlayerItem(asset: asset)
        item.automaticallyPreservesTimeOffsetFromLive = true
        item.preferredForwardBufferDuration = 4.0

        if let channel {
            updatePlayerMetadata(for: item, channel: channel, nowNext: nowNext)
        }

        let player = AVPlayer(playerItem: item)
        player.automaticallyWaitsToMinimizeStalling = true
        player.allowsExternalPlayback = true
        player.usesExternalPlaybackWhileExternalScreenIsActive = true
        return player
    }

    /// Updates native iOS OSD metadata (Title, Artist, Description) for Apple's Transport Bar on the fly.
    @MainActor
    static func updatePlayerMetadata(for item: AVPlayerItem?, channel: Channel, nowNext: NowNext?) {
        guard let item else { return }
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

        if let logoURL = channel.logoURL,
           let image = LogoImageCache.shared.anyImage(for: logoURL),
           let data = image.pngData() {
            let artItem = AVMutableMetadataItem()
            artItem.identifier = .commonIdentifierArtwork
            artItem.value = data as NSData
            artItem.dataType = kCMMetadataBaseDataType_PNG as String
            metadata.append(artItem)
        }

        item.externalMetadata = metadata
    }
}
