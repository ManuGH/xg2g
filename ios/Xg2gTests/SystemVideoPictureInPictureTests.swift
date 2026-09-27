// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import MediaPlayer
import Testing
@testable import Xg2g

/// The PiP window's controls as the presenter hands them on.
///
/// AVKit calls these delegate methods itself; here they are called directly in the
/// order AVKit uses, which is the part of the contract this app controls. Whether a
/// real device delivers that order — in particular across a device lock — is for the
/// device matrix, not for these tests.
@Suite("SystemVideoPresenter: Picture in Picture controls")
@MainActor
struct SystemVideoPictureInPictureTests {

    private final class Recorder {
        var playing = true
        var setPlayingCalls: [Bool] = []
        var dismissals = 0
    }

    private func makeFixture(inBackground: Bool) -> (SystemVideoPresenter, AVPictureInPictureController, SystemVideoPlaybackControls, Recorder) {
        let presenter = SystemVideoPresenter()
        presenter.isApplicationInBackground = { inBackground }
        let recorder = Recorder()
        let controls = SystemVideoPlaybackControls()
        controls.isPlaying = { recorder.playing }
        controls.setPlaying = { recorder.setPlayingCalls.append($0) }
        controls.dismissed = { recorder.dismissals += 1 }
        presenter.playbackDelegate = controls
        let controller = AVPictureInPictureController(
            contentSource: .init(sampleBufferDisplayLayer: presenter.displayLayer, playbackDelegate: presenter)
        )
        return (presenter, controller, controls, recorder)
    }

    @Test("Closing the window while the app is in the background reports a dismissal")
    func closeInBackgroundDismisses() {
        let (presenter, controller, controls, recorder) = makeFixture(inBackground: true)
        withExtendedLifetime(controls) {
            presenter.pictureInPictureControllerWillStartPictureInPicture(controller)
            presenter.pictureInPictureControllerDidStopPictureInPicture(controller)
        }
        #expect(recorder.dismissals == 1)
    }

    @Test("Returning to the app through the window's restore button is not a dismissal")
    func restoreIsNotDismissal() {
        let (presenter, controller, controls, recorder) = makeFixture(inBackground: true)
        var restoreAnswer: Bool?
        withExtendedLifetime(controls) {
            presenter.pictureInPictureControllerWillStartPictureInPicture(controller)
            presenter.pictureInPictureController(
                controller,
                restoreUserInterfaceForPictureInPictureStopWithCompletionHandler: { restoreAnswer = $0 }
            )
            presenter.pictureInPictureControllerDidStopPictureInPicture(controller)
        }
        #expect(restoreAnswer == true)
        #expect(recorder.dismissals == 0)
    }

    @Test("A stop while the app is in front is not a dismissal")
    func stopInForegroundIsNotDismissal() {
        let (presenter, controller, controls, recorder) = makeFixture(inBackground: false)
        withExtendedLifetime(controls) {
            presenter.pictureInPictureControllerWillStartPictureInPicture(controller)
            presenter.pictureInPictureControllerDidStopPictureInPicture(controller)
        }
        #expect(recorder.dismissals == 0)
    }

    @Test("A restore from one session does not excuse a close in the next")
    func restoreDoesNotLeakAcrossSessions() {
        let (presenter, controller, controls, recorder) = makeFixture(inBackground: true)
        withExtendedLifetime(controls) {
            // Restore requested, but the stop that should follow it never arrives.
            presenter.pictureInPictureControllerWillStartPictureInPicture(controller)
            presenter.pictureInPictureController(
                controller,
                restoreUserInterfaceForPictureInPictureStopWithCompletionHandler: { _ in }
            )

            presenter.pictureInPictureControllerWillStartPictureInPicture(controller)
            presenter.pictureInPictureControllerDidStopPictureInPicture(controller)
        }
        #expect(recorder.dismissals == 1)
    }

    @Test("A restore is consumed by the stop it belongs to")
    func restoreIsConsumedByItsStop() {
        let (presenter, controller, controls, recorder) = makeFixture(inBackground: true)
        withExtendedLifetime(controls) {
            presenter.pictureInPictureControllerWillStartPictureInPicture(controller)
            presenter.pictureInPictureController(
                controller,
                restoreUserInterfaceForPictureInPictureStopWithCompletionHandler: { _ in }
            )
            presenter.pictureInPictureControllerDidStopPictureInPicture(controller)
            // A second stop with no restore of its own is a close.
            presenter.pictureInPictureControllerDidStopPictureInPicture(controller)
        }
        #expect(recorder.dismissals == 1)
    }

    @Test("Play and pause in the window reach the owner of playback state")
    func transportReachesOwner() {
        let (presenter, controller, controls, recorder) = makeFixture(inBackground: true)
        withExtendedLifetime(controls) {
            presenter.pictureInPictureController(controller, setPlaying: false)
            presenter.pictureInPictureController(controller, setPlaying: true)
        }
        #expect(recorder.setPlayingCalls == [false, true])
    }

    @Test("The window's paused state is read from the owner of playback state")
    func pausedStateIsReadFromOwner() {
        let (presenter, controller, controls, recorder) = makeFixture(inBackground: true)
        withExtendedLifetime(controls) {
            recorder.playing = true
            #expect(presenter.pictureInPictureControllerIsPlaybackPaused(controller) == false)
            recorder.playing = false
            #expect(presenter.pictureInPictureControllerIsPlaybackPaused(controller) == true)
        }
    }
}

@Suite("PictureInPictureTransportAction: every engine × request × state")
struct PictureInPictureTransportActionTests {

    @Test(
        "Resolves a PiP transport request",
        arguments: [
            (TestTSPlayerScreen.PlaybackEngineMode.nativeDirectLive, false, true, PictureInPictureTransportAction.stopLive),
            (.nativeDirectLive, false, false, .none),
            (.nativeDirectLive, true, false, .resumeLive),
            (.nativeDirectLive, true, true, .none),
            (.timeshiftHLS, false, true, .pauseTimeshift),
            (.timeshiftHLS, false, false, .none),
            (.timeshiftHLS, true, false, .returnToLiveEdge),
            (.timeshiftHLS, true, true, .none),
        ]
    )
    func resolves(
        engine: TestTSPlayerScreen.PlaybackEngineMode,
        play: Bool,
        isPlaying: Bool,
        expected: PictureInPictureTransportAction
    ) {
        #expect(PictureInPictureTransportAction.resolve(play: play, engine: engine, isPlaying: isPlaying) == expected)
    }

    @Test("Pausing the live pipeline in the window never routes to the timeshift player")
    func livePauseNeverEntersTimeshift() {
        // The timeshift player is not in the PiP window; entering it from there froze
        // the window's picture while the stream went on elsewhere.
        let action = PictureInPictureTransportAction.resolve(play: false, engine: .nativeDirectLive, isPlaying: true)
        #expect(action == .stopLive)
    }
}

@Suite("PlaybackManager: stopping live releases the lock screen", .serialized)
@MainActor
struct PlaybackManagerNowPlayingReleaseTests {

    private let channel = Channel(
        id: "test-channel",
        name: "Test Channel",
        number: "1",
        serviceRef: "1:0:1:0:0:0:0:0:0:0:",
        logoURL: nil
    )

    @Test("Stopping live from the mini player clears the entry and its controls")
    func stopFromMiniPlayerReleasesNowPlaying() async {
        let manager = PlaybackManager(streamURL: { ref in
            URL(string: "http://127.0.0.1:9/api/v3/stream/live/\(ref)")!
        })
        await manager.play(channel: channel, mode: .miniplayer)

        // What the live screen leaves behind when it goes away minimised.
        NowPlayingManager.shared.takeOver(.init(play: {}, pause: {}))
        NowPlayingManager.shared.updateLive(title: "Test Channel")
        #expect(MPNowPlayingInfoCenter.default().nowPlayingInfo != nil)
        #expect(MPRemoteCommandCenter.shared().playCommand.isEnabled)

        await manager.stop()

        #expect(manager.presentationMode == .hidden)
        #expect(MPNowPlayingInfoCenter.default().nowPlayingInfo == nil)
        #expect(!MPRemoteCommandCenter.shared().playCommand.isEnabled)
    }
}
