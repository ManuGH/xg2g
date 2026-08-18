// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing

@testable import Xg2g

/// When the picture may drive the clock by itself.
///
/// The clock only ever started on audio, so a channel with nothing this device
/// can decode did not play silently — it froze on the one field that had been
/// forced onto the screen while the decoder kept running at full rate. The rule
/// that ends that has to stay narrow in the other direction: starting the clock
/// on a stream that does have sound puts every later audio buffer in the past,
/// where the renderer discards it, and a permanently silent channel is worse
/// than the fault being fixed.
@Suite struct VideoOnlyClockTests {

    private func decide(
        known: Bool,
        decodable: Bool,
        since: Double = 10,
        buffered: Double = 2
    ) -> Bool {
        NativeTSVideoPipeline.shouldStartClockOnPictureAlone(
            audioTracksKnown: known,
            hasDecodableAudio: decodable,
            secondsSinceFirstField: since,
            pictureBufferedSeconds: buffered
        )
    }

    @Test func aChannelWhoseAudioCannotBeDecodedPlaysWithoutIt() {
        // MPEG Layer II only, which most German public broadcasters still carry
        // and iOS has no decoder for.
        #expect(decide(known: true, decodable: false))
    }

    @Test func aChannelWithPlayableAudioIsLeftAlone() {
        #expect(!decide(known: true, decodable: true))
    }

    @Test func audioThatIsMerelyLateIsWaitedFor() {
        // No table yet is the ordinary state at tune-in, not a silent service.
        #expect(!decide(known: false, decodable: false, since: 0.5))
    }

    @Test func aChannelThatNamesNoAudioAtAllEventuallyPlaysAnyway() {
        #expect(decide(known: false, decodable: false, since: 5))
    }

    @Test func theClockWaitsForEnoughPictureToPlayFrom() {
        // Same reasoning as the audio cushion: this source arrives in bursts, and
        // a clock started level with the newest frame runs dry at the first gap.
        #expect(!decide(known: true, decodable: false, buffered: 0.2))
        #expect(decide(known: true, decodable: false, buffered: 1.5))
    }

    @Test func aDecodableTrackIsNeverOverriddenByTheTimeout() {
        // The timeout only answers "nothing has been named yet"; once something
        // has, no amount of waiting makes the picture take over.
        #expect(!decide(known: true, decodable: true, since: 600))
    }
}
