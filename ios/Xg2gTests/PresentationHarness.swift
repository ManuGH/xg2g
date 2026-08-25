// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import Foundation

@testable import Xg2g

/// Gives a pipeline a surface of its own, the way a screen showing a single channel
/// would.
///
/// A playback session reaches the screen only through a presentation context, and only
/// once that context has handed the surface over. That is what makes preparing a second
/// channel beside a playing one safe, and it means a test that expects pictures to
/// arrive has to say who owns the surface - there is no longer an implicit answer.
///
/// The returned context has to be kept alive for as long as the test expects output:
/// the pipeline holds it weakly, exactly as the screen's does.
@MainActor
@discardableResult
func giveOwnSurface(to pipeline: NativeTSVideoPipeline,
                    presenter: SystemVideoPresenter = SystemVideoPresenter(),
                    renderView: MetalVideoView? = nil) -> PresentationContext {
    let context = PresentationContext(presenter: presenter, renderView: renderView)
    pipeline.presentationContext = context
    _ = context.issueGeneration(to: pipeline)
    context.bindWithoutPreparation(pipeline)
    return context
}
