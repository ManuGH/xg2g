// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing

@testable import Xg2g

/// Which track a channel's audio ends up on.
///
/// Every rule here exists because a real broadcaster broke the previous one,
/// and each failure is silent in its own way: the wrong codec plays nothing at
/// all, the wrong audio type plays a narrator over the programme. Neither shows
/// up in any other metric, which is why the choice is pinned rather than left
/// to whatever the PMT happens to list first.
@Suite struct AudioTrackSelectionTests {

    private func track(
        _ pid: UInt16,
        _ codec: AudioStreamCodec,
        lang: String? = nil,
        audioType: UInt8 = 0
    ) -> AudioTrackInfo {
        AudioTrackInfo(pid: pid, streamType: 0, codec: codec, language: lang, audioType: audioType)
    }

    @Test func decodableBeatsLanguage() {
        // ZDF: Layer II first in the PMT, both tagged German. Picking by
        // language alone selects the one iOS cannot decode.
        let picked = NativeTSVideoPipeline.preferredAudioTrack(from: [
            track(6120, .mpegAudio, lang: "deu"),
            track(6122, .ac3, lang: "deu")
        ])
        #expect(picked?.pid == 6122)
    }

    @Test func audioDescriptionIsPassedOverForTheMainProgramme() {
        // Both AC-3, both decodable, neither German — only the DVB audio type
        // separates a narrator from the programme.
        let picked = NativeTSVideoPipeline.preferredAudioTrack(from: [
            track(1794, .ac3, lang: "qae", audioType: 0x03),
            track(1796, .ac3, lang: "qaf")
        ])
        #expect(picked?.pid == 1796)
    }

    @Test func hearingImpairedMixIsPassedOverToo() {
        let picked = NativeTSVideoPipeline.preferredAudioTrack(from: [
            track(770, .ac3, lang: "deu", audioType: 0x02),
            track(772, .ac3, lang: "deu")
        ])
        #expect(picked?.pid == 772)
    }

    @Test func accessibilityTrackIsStillPlayedWhenItIsAllThereIs() {
        // A narrated soundtrack beats silence.
        let picked = NativeTSVideoPipeline.preferredAudioTrack(from: [
            track(1794, .ac3, lang: "deu", audioType: 0x03)
        ])
        #expect(picked?.pid == 1794)
    }

    @Test func germanWinsAmongTracksThatAreOtherwiseEqual() {
        let picked = NativeTSVideoPipeline.preferredAudioTrack(from: [
            track(100, .ac3, lang: "eng"),
            track(101, .ac3, lang: "deu")
        ])
        #expect(picked?.pid == 101)
    }

    @Test func aChannelWithNothingDecodableStillSelectsATrack() {
        // The PID has to stay selected or the rest of the pipeline stops
        // reporting on it; the silence is warned about separately.
        let picked = NativeTSVideoPipeline.preferredAudioTrack(from: [
            track(6120, .mpegAudio, lang: "deu")
        ])
        #expect(picked?.pid == 6120)
    }

    @Test func decodabilityOutranksTheAudioTypeRule() {
        // An AC-3 narrator is still preferable to Layer II silence.
        let picked = NativeTSVideoPipeline.preferredAudioTrack(from: [
            track(6120, .mpegAudio, lang: "deu"),
            track(6122, .ac3, lang: "deu", audioType: 0x03)
        ])
        #expect(picked?.pid == 6122)
    }

    @Test func noTracksSelectsNothing() {
        #expect(NativeTSVideoPipeline.preferredAudioTrack(from: []) == nil)
    }

    @Test func audioTrackDisplayNameFormatting() {
        let t1 = track(1001, .ac3, lang: "deu")
        #expect(t1.displayName == "Deutsch – AC-3")

        let t2 = track(1002, .ac3, lang: "eng")
        #expect(t2.displayName == "Englisch – AC-3")

        let t3 = track(1003, .aac, lang: "deu", audioType: 0x03)
        #expect(t3.displayName == "Deutsch – AAC (Audiodeskription)")

        let t4 = track(1004, .eac3, lang: "qaa")
        #expect(t4.displayName == "Originalton – E-AC-3")
    }
}
