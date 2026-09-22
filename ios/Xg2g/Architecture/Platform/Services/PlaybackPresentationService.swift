// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
#if canImport(AVKit)
import AVKit
#endif

/// Presentation coordination for system media features like Picture-in-Picture and external routing.
@MainActor
protocol PlaybackPresentationServicing: AnyObject {
    var isPictureInPictureSupported: Bool { get }
    var isPictureInPictureActive: Bool { get }
    func startPictureInPicture()
    func stopPictureInPicture()
}

/// Default presentation service implementation.
@MainActor
final class DefaultPlaybackPresentationService: PlaybackPresentationServicing {
    var isPictureInPictureSupported: Bool {
        #if canImport(AVKit) && !os(tvOS)
        return AVPictureInPictureController.isPictureInPictureSupported()
        #else
        return false
        #endif
    }

    private(set) var isPictureInPictureActive: Bool = false

    init() {}

    func startPictureInPicture() {
        guard isPictureInPictureSupported else { return }
        isPictureInPictureActive = true
    }

    func stopPictureInPicture() {
        isPictureInPictureActive = false
    }
}
