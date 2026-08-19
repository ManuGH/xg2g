// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

/// Choosing between the two playback routes, and the receiver address the
/// direct one needs.
@MainActor
struct PlaybackEngineTests {

    private func makeModel(receiver: String) -> AppModel {
        let model = AppModel()
        model.receiverStreamBaseURL = receiver
        return model
    }

    private let channel = Channel(
        id: "1",
        name: "ZDF HD",
        number: "2",
        serviceRef: "1:0:19:2B66:3F3:1:C00000:0:0:0:",
        logoURL: nil
    )

    @Test("A receiver address produces the service URL")
    func buildsDirectURL() throws {
        let model = makeModel(receiver: "http://10.0.0.5:8001")
        let url = try #require(model.directStreamURL(for: channel))
        #expect(url.absoluteString == "http://10.0.0.5:8001/1:0:19:2B66:3F3:1:C00000:0:0:0:")
    }

    /// Someone typing an address ends it with a slash as often as not, and a
    /// doubled separator is a tune that fails for a reason nobody can see.
    @Test("A trailing slash does not produce a doubled separator")
    func toleratesTrailingSlash() throws {
        let model = makeModel(receiver: "http://10.0.0.5:8001/")
        let url = try #require(model.directStreamURL(for: channel))
        #expect(!url.absoluteString.contains("8001//"))
        #expect(url.absoluteString == "http://10.0.0.5:8001/1:0:19:2B66:3F3:1:C00000:0:0:0:")
    }

    /// Nothing tells the app where the receiver is, so until someone says, the
    /// direct route cannot run and must report that rather than build a URL
    /// pointing nowhere.
    @Test("Without an address there is no direct URL and no direct playback", arguments: ["", "   "])
    func requiresAnAddress(receiver: String) {
        let model = makeModel(receiver: receiver)
        #expect(model.directStreamURL(for: channel) == nil)
        #expect(model.isDirectPlaybackAvailable == false)
    }

    @Test("An entered address enables direct playback")
    func addressEnablesDirectPlayback() {
        #expect(makeModel(receiver: "http://10.0.0.5:8001").isDirectPlaybackAvailable)
    }

    /// The preference has to outlive the screen that set it.
    @Test("The chosen route is remembered")
    func preferencePersists() {
        let model = AppModel()
        let original = model.playbackEngine
        defer { model.playbackEngine = original }

        model.playbackEngine = .native
        #expect(UserDefaults.standard.string(forKey: "xg2g.playback_engine") == "native")
        #expect(AppModel().playbackEngine == .native)
    }

    /// Both routes have to state a real cost. A description listing only
    /// upsides is what leaves a viewer puzzled when the pause button stops
    /// working, which is the confusion this screen exists to prevent.
    @Test("Both routes name what they give up", arguments: AppModel.PlaybackEngine.allCases)
    func everyRouteNamesItsCosts(engine: AppModel.PlaybackEngine) {
        let tradeoff = engine.tradeoff
        #expect(!tradeoff.gains.isEmpty)
        #expect(!tradeoff.costs.isEmpty)
        #expect(!engine.displayName.isEmpty)
        #expect(!engine.summary.isEmpty)
    }
}
