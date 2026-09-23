// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import QuartzCore
import SwiftUI
import UIKit

/// Lightweight UIView host for `SystemVideoPresenter.displayLayer`.
///
/// In the Apple PlayerCore architecture, `NativeVideoSurfaceView` is the authoritative
/// visual presentation host for native Live TS playback (`.systemLayer`).
/// It manages the lifecycle, layer attachment, reparenting safety, and bounds
/// geometry of `AVSampleBufferDisplayLayer`.
///
/// Scaling authority remains exclusively with `SystemVideoPresenter` (`scalingMode`),
/// while compute, deinterlacing, and frame scheduling remain with `MetalVideoView`
/// until Phase B6.
@MainActor
public final class NativeVideoSurfaceView: UIView {

    public weak var presenter: SystemVideoPresenter? {
        didSet {
            guard oldValue !== presenter else { return }
            handlePresenterChange(from: oldValue, to: presenter)
        }
    }

    public override init(frame: CGRect) {
        super.init(frame: frame)
        backgroundColor = .black
    }

    public required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    public override func layoutSubviews() {
        super.layoutSubviews()
        updateDisplayLayerFrame()
    }

    public override func willMove(toSuperview newSuperview: UIView?) {
        super.willMove(toSuperview: newSuperview)
        if newSuperview == nil {
            // Only detach if the display layer is still hosted on this view's layer.
            // If another view already reparented the layer (e.g. during a SwiftUI view
            // rebuild), do NOT detach it from the new host!
            if let displayLayer = presenter?.displayLayer, displayLayer.superlayer === layer {
                displayLayer.removeFromSuperlayer()
            }
        }
    }

    private func handlePresenterChange(from old: SystemVideoPresenter?, to new: SystemVideoPresenter?) {
        // Safe detach of old presenter's layer if still on this view:
        if let oldLayer = old?.displayLayer, oldLayer.superlayer === layer {
            oldLayer.removeFromSuperlayer()
        }

        // Attach new presenter's layer:
        if let newPresenter = new {
            let newLayer = newPresenter.displayLayer
            if newLayer.superlayer !== layer {
                layer.addSublayer(newLayer)
            }
            updateDisplayLayerFrame()
        }
    }

    /// Synchronizes the display layer's frame with this view's bounds without
    /// triggering implicit CoreAnimation transactions.
    public func updateDisplayLayerFrame() {
        guard let displayLayer = presenter?.displayLayer else { return }
        guard displayLayer.superlayer === layer else { return }
        if displayLayer.frame != bounds {
            CATransaction.begin()
            CATransaction.setDisableActions(true)
            displayLayer.frame = bounds
            CATransaction.commit()
        }
    }
}

/// SwiftUI wrapper for `NativeVideoSurfaceView`.
public struct NativeVideoSurfaceRepresentable: UIViewRepresentable {
    public let presenter: SystemVideoPresenter

    public init(presenter: SystemVideoPresenter) {
        self.presenter = presenter
    }

    public func makeUIView(context: Context) -> NativeVideoSurfaceView {
        let view = NativeVideoSurfaceView(frame: .zero)
        view.presenter = presenter
        return view
    }

    public func updateUIView(_ uiView: NativeVideoSurfaceView, context: Context) {
        if uiView.presenter !== presenter {
            uiView.presenter = presenter
        }
    }
}
