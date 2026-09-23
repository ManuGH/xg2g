// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import CoreGraphics
import QuartzCore
import Testing
import UIKit
@testable import Xg2g

@Suite("SystemVideoPresenter & NativeVideoSurfaceView Tests")
@MainActor
struct SystemVideoPresenterTests {

    @Test("NativeVideoSurfaceView: Layer attachment and bounds geometry synchronization")
    func testLayerAttachmentAndGeometry() {
        let presenter = SystemVideoPresenter()
        let surfaceView = NativeVideoSurfaceView(frame: CGRect(x: 0, y: 0, width: 320, height: 180))

        #expect(presenter.displayLayer.superlayer == nil)

        surfaceView.presenter = presenter

        #expect(presenter.displayLayer.superlayer === surfaceView.layer)
        #expect(presenter.displayLayer.frame == CGRect(x: 0, y: 0, width: 320, height: 180))
    }

    @Test("NativeVideoSurfaceView: Bounds resizing updates displayLayer without implicit CATransactions")
    func testBoundsResizing() {
        let presenter = SystemVideoPresenter()
        let surfaceView = NativeVideoSurfaceView(frame: CGRect(x: 0, y: 0, width: 100, height: 100))
        surfaceView.presenter = presenter

        #expect(presenter.displayLayer.frame == CGRect(x: 0, y: 0, width: 100, height: 100))

        surfaceView.bounds = CGRect(x: 0, y: 0, width: 1920, height: 1080)
        surfaceView.layoutSubviews()

        #expect(presenter.displayLayer.frame == CGRect(x: 0, y: 0, width: 1920, height: 1080))
    }

    @Test("NativeVideoSurfaceView: Layer reparenting safety prevents old view teardown from detaching new host")
    func testLayerReparentingSafetyOnSwiftUIRebuild() {
        let presenter = SystemVideoPresenter()

        // 1. Initial SwiftUI view (stage1)
        let stage1 = NativeVideoSurfaceView(frame: CGRect(x: 0, y: 0, width: 400, height: 300))
        stage1.presenter = presenter
        #expect(presenter.displayLayer.superlayer === stage1.layer)

        // 2. SwiftUI view rebuild creates new view (stage2) and binds the same presenter
        let stage2 = NativeVideoSurfaceView(frame: CGRect(x: 0, y: 0, width: 800, height: 600))
        stage2.presenter = presenter
        #expect(presenter.displayLayer.superlayer === stage2.layer)
        #expect(presenter.displayLayer.frame == CGRect(x: 0, y: 0, width: 800, height: 600))

        // 3. SwiftUI tears down old view (stage1)
        stage1.willMove(toSuperview: nil)
        stage1.presenter = nil

        // CRITICAL GUARD: stage1 must NOT have detached presenter.displayLayer from stage2!
        #expect(presenter.displayLayer.superlayer === stage2.layer)
        #expect(presenter.displayLayer.frame == CGRect(x: 0, y: 0, width: 800, height: 600))

        // 4. Teardown of the active host (stage2) cleanly detaches the display layer
        stage2.willMove(toSuperview: nil)
        stage2.presenter = nil
        #expect(presenter.displayLayer.superlayer == nil)
    }

    @Test("Single Scaling Authority: SystemVideoPresenter controls displayLayer.videoGravity")
    func testSingleScalingAuthority() {
        let presenter = SystemVideoPresenter()
        let surfaceView = NativeVideoSurfaceView(frame: CGRect(x: 0, y: 0, width: 200, height: 150))
        surfaceView.presenter = presenter

        #expect(presenter.scalingMode == .fit)
        #expect(presenter.displayLayer.videoGravity == .resizeAspect)

        presenter.scalingMode = .fill
        #expect(presenter.displayLayer.videoGravity == .resizeAspectFill)

        presenter.scalingMode = .fit
        #expect(presenter.displayLayer.videoGravity == .resizeAspect)
    }

    @Test("SystemVideoStage: Composite container coordinates MetalVideoView and NativeVideoSurfaceView")
    func testSystemVideoStageCompositeHosting() {
        let presenter = SystemVideoPresenter()
        let stage = SystemVideoStage(frame: CGRect(x: 0, y: 0, width: 1280, height: 720))
        stage.presenter = presenter
        stage.layoutSubviews()

        #expect(stage.metalView.frame == CGRect(x: 0, y: 0, width: 1280, height: 720))
        #expect(stage.surfaceView.frame == CGRect(x: 0, y: 0, width: 1280, height: 720))

        // Default path is .systemLayer
        #expect(stage.presentationPath == .systemLayer)
        #expect(!stage.surfaceView.isHidden)
        #expect(!presenter.displayLayer.isHidden)
        #expect(presenter.displayLayer.superlayer === stage.surfaceView.layer)

        // Switch to .metalDrawable (testing/benchmark comparison mode)
        stage.presentationPath = .metalDrawable
        #expect(stage.surfaceView.isHidden)
        #expect(presenter.displayLayer.isHidden)
        #expect(stage.metalView.presentationPath == .metalDrawable)

        // Switch back to .systemLayer
        stage.presentationPath = .systemLayer
        #expect(!stage.surfaceView.isHidden)
        #expect(!presenter.displayLayer.isHidden)
        #expect(stage.metalView.presentationPath == .systemLayer)
    }
}
