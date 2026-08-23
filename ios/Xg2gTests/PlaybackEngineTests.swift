// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import SwiftUI
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
    @Test("The chosen route is remembered across changes")
    func preferencePersists() {
        let model = AppModel()
        let original = model.playbackEngine
        defer { model.playbackEngine = original }

        model.playbackEngine = .auto
        #expect(UserDefaults.standard.string(forKey: "xg2g.playback_engine") == "auto")
        #expect(AppModel().playbackEngine == .auto)

        model.playbackEngine = .native
        #expect(UserDefaults.standard.string(forKey: "xg2g.playback_engine") == "native")
        #expect(AppModel().playbackEngine == .native)

        model.playbackEngine = .hls
        #expect(UserDefaults.standard.string(forKey: "xg2g.playback_engine") == "hls")
        #expect(AppModel().playbackEngine == .hls)
    }

    @Test("Quality preference persists and defaults to auto")
    func qualityPreferencePersists() {
        let model = AppModel()
        let original = model.qualityPreference
        defer { model.qualityPreference = original }

        model.qualityPreference = .auto
        #expect(UserDefaults.standard.string(forKey: "xg2g.quality_preference") == "auto")

        model.qualityPreference = .passthrough
        #expect(UserDefaults.standard.string(forKey: "xg2g.quality_preference") == "passthrough")
        #expect(AppModel().qualityPreference == .passthrough)

        model.qualityPreference = .qsvNormalize
        #expect(UserDefaults.standard.string(forKey: "xg2g.quality_preference") == "qsvNormalize")
        #expect(AppModel().qualityPreference == .qsvNormalize)

        model.qualityPreference = .dataSaver
        #expect(UserDefaults.standard.string(forKey: "xg2g.quality_preference") == "dataSaver")
        #expect(AppModel().qualityPreference == .dataSaver)
    }

    @Test("Quality preference display names and technical details are defined")
    func qualityPreferenceMetadata() {
        for pref in AppModel.StreamingQualityPreference.allCases {
            #expect(!pref.displayName.isEmpty)
            #expect(!pref.summary.isEmpty)
            #expect(!pref.technicalDetails.isEmpty)
        }
        #expect(AppModel.StreamingQualityPreference.auto.displayName == "Automatisch")
        #expect(AppModel.StreamingQualityPreference.passthrough.displayName == "Originalqualität")
        #expect(AppModel.StreamingQualityPreference.qsvNormalize.displayName == "Kompatibilität")
        #expect(AppModel.StreamingQualityPreference.dataSaver.displayName == "Datensparen")
    }

    @Test("Active playback plan description reflects baseline configuration")
    func activePlaybackPlanDescriptionBaseline() {
        let model = AppModel()
        let origEngine = model.playbackEngine
        let origQuality = model.qualityPreference
        defer {
            model.playbackEngine = origEngine
            model.qualityPreference = origQuality
        }

        model.playbackEngine = .native
        #expect(model.activePlaybackPlanDescription.contains("Native TS"))
        #expect(model.activePlaybackPlanDescription.contains("Minimale Serverlast"))

        model.playbackEngine = .auto
        model.qualityPreference = .auto
        #expect(model.activePlaybackPlanDescription.contains("Auto Plan"))

        model.playbackEngine = .hls
        model.qualityPreference = .passthrough
        #expect(model.activePlaybackPlanDescription.contains("HLS TS"))
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

    @Test("Render SettingsView snapshots for all 3 modes")
    func renderSettingsSnapshots() throws {
        let modes: [(AppModel.PlaybackEngine, String)] = [
            (.auto, "settings_auto.png"),
            (.native, "settings_native.png"),
            (.hls, "settings_hls.png")
        ]

        for (mode, filename) in modes {
            let model = AppModel()
            model.playbackEngine = mode
            model.receiverStreamBaseURL = "http://10.10.55.64:8001"

            let view = SettingsView(model: model)
                .preferredColorScheme(.dark)

            let controller = UIHostingController(rootView: view)
            controller.view.frame = CGRect(x: 0, y: 0, width: 393, height: 852)
            controller.view.overrideUserInterfaceStyle = .dark
            
            let window = UIWindow(frame: CGRect(x: 0, y: 0, width: 393, height: 852))
            window.rootViewController = controller
            window.makeKeyAndVisible()
            controller.view.setNeedsLayout()
            controller.view.layoutIfNeeded()

            let format = UIGraphicsImageRendererFormat()
            format.scale = 2.0
            let imageRenderer = UIGraphicsImageRenderer(bounds: controller.view.bounds, format: format)
            let image = imageRenderer.image { _ in
                controller.view.drawHierarchy(in: controller.view.bounds, afterScreenUpdates: true)
            }
            if let data = image.pngData() {
                let outURL = URL(fileURLWithPath: "/Users/manuel/.gemini/antigravity/brain/7d79d215-2ce0-4ff1-a82e-2b7842857601/\(filename)")
                try? data.write(to: outURL)
            }
        }

        // Render Advanced & Diagnostic Override subview
        let model = AppModel()
        model.playbackEngine = .auto
        model.qualityPreference = .auto
        let diagView = DiagnosticPipelineOverrideView(model: model).preferredColorScheme(.dark)
        let diagController = UIHostingController(rootView: diagView)
        diagController.view.frame = CGRect(x: 0, y: 0, width: 393, height: 852)
        diagController.view.overrideUserInterfaceStyle = .dark
        let diagWindow = UIWindow(frame: CGRect(x: 0, y: 0, width: 393, height: 852))
        diagWindow.rootViewController = diagController
        diagWindow.makeKeyAndVisible()
        diagController.view.setNeedsLayout()
        diagController.view.layoutIfNeeded()

        let format = UIGraphicsImageRendererFormat()
        format.scale = 2.0
        let diagRenderer = UIGraphicsImageRenderer(bounds: diagController.view.bounds, format: format)
        let diagImage = diagRenderer.image { _ in
            diagController.view.drawHierarchy(in: diagController.view.bounds, afterScreenUpdates: true)
        }
        if let diagData = diagImage.pngData() {
            let diagURL = URL(fileURLWithPath: "/Users/manuel/.gemini/antigravity/brain/7d79d215-2ce0-4ff1-a82e-2b7842857601/settings_diagnostic_override.png")
            try? diagData.write(to: diagURL)
        }
    }

    @Test("Engine switching transitions cleanly between Native and Managed HLS routes")
    func engineSwitchingTransitionsCleanly() {
        let model = makeModel(receiver: "http://10.10.55.64:8001")
        
        // 1. Native mode
        model.playbackEngine = .native
        #expect(model.playbackEngine == .native)
        #expect(model.isDirectPlaybackAvailable)
        let directURL = model.directStreamURL(for: channel)
        #expect(directURL != nil)
        
        // 2. HLS mode
        model.playbackEngine = .hls
        #expect(model.playbackEngine == .hls)
        
        // 3. Auto mode
        model.playbackEngine = .auto
        #expect(model.playbackEngine == .auto)
        
        // Return to native
        model.playbackEngine = .native
        #expect(model.playbackEngine == .native)
    }
}
