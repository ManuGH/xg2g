// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import SwiftUI
import UIKit

// MARK: - Native iOS AVPlayerViewController (Plex / Apple TV System Player)

struct NativeVideoPlayerView: UIViewControllerRepresentable {

    let player: AVPlayer
    var videoGravity: AVLayerVideoGravity = .resizeAspect
    var showsPlaybackControls: Bool = true
    var onDismiss: (@MainActor @Sendable () -> Void)? = nil

    func makeUIViewController(context: Context) -> AVPlayerViewController {
        let controller = AVPlayerViewController()
        controller.player = player
        controller.showsPlaybackControls = showsPlaybackControls
        controller.videoGravity = videoGravity
        controller.allowsPictureInPicturePlayback = true
#if os(iOS)
        controller.canStartPictureInPictureAutomaticallyFromInline = true
        controller.updatesNowPlayingInfoCenter = false
        controller.allowsVideoFrameAnalysis = false
        controller.exitsFullScreenWhenPlaybackEnds = false
#endif
        controller.view.insetsLayoutMarginsFromSafeArea = false
        controller.additionalSafeAreaInsets = .zero
        controller.view.backgroundColor = .black
        controller.delegate = context.coordinator
        return controller
    }

    func updateUIViewController(_ controller: AVPlayerViewController, context: Context) {
        if controller.player !== player {
            controller.player = player
        }
        if controller.showsPlaybackControls != showsPlaybackControls {
            controller.showsPlaybackControls = showsPlaybackControls
        }
        if controller.videoGravity != videoGravity {
            controller.videoGravity = videoGravity
            UIView.animate(withDuration: 0.25) {
                controller.view.setNeedsLayout()
                controller.view.layoutIfNeeded()
            }
        }
    }

    func makeCoordinator() -> Coordinator {
        Coordinator(self)
    }

    @MainActor
    final class Coordinator: NSObject, @preconcurrency AVPlayerViewControllerDelegate {
        let parent: NativeVideoPlayerView

        init(_ parent: NativeVideoPlayerView) {
            self.parent = parent
        }

#if os(iOS)
        func playerViewController(
            _ playerViewController: AVPlayerViewController,
            willBeginFullScreenPresentationWithAnimationCoordinator coordinator: any UIViewControllerTransitionCoordinator
        ) {
            coordinator.animate(alongsideTransition: nil) { [weak self] _ in
                guard let self else { return }
                if self.parent.player.timeControlStatus != .playing && self.parent.player.error == nil {
                    self.parent.player.play()
                }
            }
        }

        func playerViewController(
            _ playerViewController: AVPlayerViewController,
            willEndFullScreenPresentationWithAnimationCoordinator coordinator: any UIViewControllerTransitionCoordinator
        ) {
            coordinator.animate(alongsideTransition: nil) { [weak self] context in
                guard let self else { return }
                if !context.isCancelled {
                    self.parent.onDismiss?()
                } else if self.parent.player.timeControlStatus != .playing && self.parent.player.error == nil {
                    self.parent.player.play()
                }
            }
        }
#endif

        func playerViewController(
            _ playerViewController: AVPlayerViewController,
            restoreUserInterfaceForPictureInPictureStopWithCompletionHandler completionHandler: @escaping (Bool) -> Void
        ) {
            completionHandler(true)
        }
    }
}
