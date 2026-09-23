// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI
import UIKit

/// Composite container that coordinates the processing sink (`MetalVideoView`)
/// and the visual presentation host (`NativeVideoSurfaceView`).
///
/// In the Apple PlayerCore architecture:
/// 1. `MetalVideoView` acts as the processing, deinterlacing, and field-scheduling sink
///    (feeding sample buffers into `SystemVideoPresenter`).
/// 2. `NativeVideoSurfaceView` hosts `SystemVideoPresenter.displayLayer` as its sublayer.
/// 3. In `.systemLayer` mode, `surfaceView` is visible and `metalView` runs in the background.
/// 4. In `.metalDrawable` mode, `surfaceView` is hidden and `metalView` renders directly
///    into its CAMetalLayer for testing and A/B verification.
@MainActor
public final class SystemVideoStage: UIView {

    public let metalView: MetalVideoView
    public let surfaceView: NativeVideoSurfaceView

    public weak var presenter: SystemVideoPresenter? {
        didSet {
            metalView.systemPresenter = presenter
            surfaceView.presenter = presenter
            updateVisibility()
        }
    }

    public var presentationPath: MetalVideoView.PresentationPath {
        get { metalView.presentationPath }
        set {
            metalView.presentationPath = newValue
            updateVisibility()
        }
    }

    public var scalingMode: VideoScalingMode {
        get { metalView.scalingMode }
        set {
            metalView.scalingMode = newValue
            presenter?.scalingMode = newValue
        }
    }

    public override init(frame: CGRect) {
        self.metalView = MetalVideoView(frame: frame)
        self.surfaceView = NativeVideoSurfaceView(frame: frame)
        super.init(frame: frame)

        backgroundColor = .black
        addSubview(metalView)
        addSubview(surfaceView)
        updateVisibility()
    }

    public required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    public override func layoutSubviews() {
        super.layoutSubviews()
        metalView.frame = bounds
        surfaceView.frame = bounds
    }

    public func updateVisibility() {
        let isSystem = (metalView.presentationPath == .systemLayer) && (presenter != nil)
        surfaceView.isHidden = !isSystem
        surfaceView.updateDisplayLayerFrame()
    }
}

/// SwiftUI wrapper for `SystemVideoStage`.
public struct SystemVideoStageView: UIViewRepresentable {
    public let telemetry: StreamTelemetry?
    public let presenter: SystemVideoPresenter
    public let presentationContext: PresentationContext
    public let presentationPath: MetalVideoView.PresentationPath
    public let scalingMode: VideoScalingMode
    public let aspectRatioOverride: VideoAspectRatio

    public init(
        telemetry: StreamTelemetry?,
        presenter: SystemVideoPresenter,
        presentationContext: PresentationContext,
        presentationPath: MetalVideoView.PresentationPath,
        scalingMode: VideoScalingMode,
        aspectRatioOverride: VideoAspectRatio
    ) {
        self.telemetry = telemetry
        self.presenter = presenter
        self.presentationContext = presentationContext
        self.presentationPath = presentationPath
        self.scalingMode = scalingMode
        self.aspectRatioOverride = aspectRatioOverride
    }

    public func makeUIView(context: Context) -> SystemVideoStage {
        let stage = SystemVideoStage(frame: .zero)
        stage.metalView.telemetry = telemetry
        stage.metalView.scalingMode = scalingMode
        stage.metalView.aspectRatioOverride = aspectRatioOverride

        // PresentationContext owns the processing sink:
        presentationContext.setRenderView(stage.metalView)

        stage.presenter = presenter
        stage.presentationPath = presentationPath
        presenter.scalingMode = scalingMode
        presenter.enablePictureInPicture()

        return stage
    }

    public func updateUIView(_ stage: SystemVideoStage, context: Context) {
        stage.metalView.telemetry = telemetry
        stage.presentationPath = presentationPath
        stage.metalView.scalingMode = scalingMode
        stage.metalView.aspectRatioOverride = aspectRatioOverride
        presenter.scalingMode = scalingMode
    }
}
