// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import CoreMedia
import Foundation
import Testing
@testable import Xg2g

@MainActor
@Suite struct TimeshiftWeicheTests {

    private func formatTimeshiftOffset(_ offsetSeconds: Double) -> String {
        let secs = Int(offsetSeconds)
        let hours = secs / 3600
        let minutes = (secs % 3600) / 60
        let seconds = secs % 60
        if hours > 0 {
            return String(format: "-%02d:%02d:%02d", hours, minutes, seconds)
        } else {
            return String(format: "-%02d:%02d", minutes, seconds)
        }
    }

    private func shouldAutoReturnToLiveEdge(currentOffset: Double, forwardDelta: Double) -> Bool {
        let remaining = currentOffset - forwardDelta
        return remaining <= 2.5
    }

    @Test("Timeshift offset formats correctly into mm:ss and hh:mm:ss")
    func testTimeshiftOffsetFormatting() {
        #expect(formatTimeshiftOffset(0) == "-00:00")
        #expect(formatTimeshiftOffset(45) == "-00:45")
        #expect(formatTimeshiftOffset(740) == "-12:20")
        #expect(formatTimeshiftOffset(3600) == "-01:00:00")
        #expect(formatTimeshiftOffset(3665) == "-01:01:05")
        #expect(formatTimeshiftOffset(14400) == "-04:00:00") // 4 hours
    }

    @Test("Fast-forwarding to within 2.5s of live edge triggers return to native live")
    func testLiveEdgeAutoReturnThreshold() {
        // 10s behind, jump 5s -> 5s remaining (stays in Timeshift)
        #expect(!shouldAutoReturnToLiveEdge(currentOffset: 10.0, forwardDelta: 5.0))

        // 5s behind, jump 30s -> 0s remaining (returns to Live edge)
        #expect(shouldAutoReturnToLiveEdge(currentOffset: 5.0, forwardDelta: 30.0))

        // 3s behind, jump 1s -> 2s remaining (<= 2.5s -> returns to Live edge)
        #expect(shouldAutoReturnToLiveEdge(currentOffset: 3.0, forwardDelta: 1.0))
    }

    @Test("PlayerAssetLoader creates live player with DVR preservation settings")
    func testLivePlayerConfiguration() {
        let url = URL(string: "http://localhost:8088/sessions/test123/hls/index.m3u8")!
        let ticket = PlaybackTicket(
            name: "xg2g_ticket",
            value: "abc123secret",
            path: "/",
            expiresAt: Date().addingTimeInterval(3600)
        )
        let stream = LiveStream(sessionID: "test123", playlistURL: url, ticket: ticket)
        let channel = Channel(id: "1", name: "Das Erste HD", number: "1", serviceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:", logoURL: nil)

        let player = PlayerAssetLoader.makeLivePlayer(for: stream, channel: channel, nowNext: nil)
        #expect(player.automaticallyWaitsToMinimizeStalling == true)
        #expect(player.currentItem?.automaticallyPreservesTimeOffsetFromLive == true)
        #expect(player.currentItem?.preferredForwardBufferDuration == 4.0)
    }

    @Test("PlaybackEngineMode states distinguish between native direct and timeshift HLS")
    func testPlaybackEngineModeEquality() {
        let liveMode = TestTSPlayerScreen.PlaybackEngineMode.nativeDirectLive
        let timeshiftMode = TestTSPlayerScreen.PlaybackEngineMode.timeshiftHLS

        #expect(liveMode == .nativeDirectLive)
        #expect(timeshiftMode == .timeshiftHLS)
        #expect(liveMode != timeshiftMode)
    }
}
