// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVFoundation
import CoreMedia
import Foundation
import Testing
@testable import Xg2g

/// The channel layout handed to CoreMedia for AC-3 / E-AC-3 broadcast audio.
///
/// Without a layout the renderer knows how many channels arrive and nothing
/// about what is in them, so folding 5.1 down to a two-channel route — which is
/// every Bluetooth headphone — has no defined place for dialogue or LFE. These
/// tests pin the mapping to the ATSC A/52 `acmod` table so a wrong tag cannot
/// pass silently.
struct AC3ChannelLayoutTests {

    /// `acmod` → channel count, exactly as ATSC A/52 §5.4.2.3 defines it and as
    /// `AC3FrameParser` computes it. LFE adds one on top.
    private static let channelCountByAcmod: [Int: Int] = [
        0: 2, // 1+1 dual mono
        1: 1, // 1/0
        2: 2, // 2/0
        3: 3, // 3/0
        4: 3, // 2/1
        5: 4, // 3/1
        6: 4, // 2/2
        7: 5  // 3/2
    ]

    /// A layout tag encodes its channel count in its low 16 bits. If a tag ever
    /// disagrees with the count the parser derived from the same `acmod`, the
    /// renderer is handed a layout describing a different stream than the one
    /// arriving — which is the failure this mapping exists to prevent.
    @Test("Every layout tag matches the channel count its acmod implies")
    func tagChannelCountMatchesAcmod() throws {
        for acmod in 0...7 {
            for lfeOn in [false, true] {
                let expected = try #require(Self.channelCountByAcmod[acmod]) + (lfeOn ? 1 : 0)

                guard let tag = AudioSampleBufferAssembler.channelLayoutTag(
                    acmod: acmod, lfeOn: lfeOn
                ) else {
                    continue // untagged combination, covered separately below
                }

                #expect(
                    Int(tag & 0xFFFF) == expected,
                    "acmod \(acmod) lfe \(lfeOn): tag carries \(tag & 0xFFFF) channels, stream has \(expected)"
                )
            }
        }
    }

    /// The broadcast case that matters: 3/2 plus LFE, in AC-3 bitstream order
    /// (L, C, R, Ls, Rs, LFE) — which is what `MPEG_5_1_C` describes, and not
    /// what the more common `_A` variant (L, R, C, LFE, Ls, Rs) does.
    @Test("5.1 broadcast maps to the AudioUnit 5.1 layout")
    func fiveOneUsesBitstreamOrder() {
        #expect(
            AudioSampleBufferAssembler.channelLayoutTag(acmod: 7, lfeOn: true)
                == kAudioChannelLayoutTag_AudioUnit_5_1
        )
        #expect(
            AudioSampleBufferAssembler.channelLayoutTag(acmod: 7, lfeOn: false)
                == kAudioChannelLayoutTag_AudioUnit_5_0
        )
    }

    /// Plain stereo and mono broadcasts, the overwhelming majority of the feed.
    @Test("Stereo and mono map to their plain layouts")
    func stereoAndMono() {
        #expect(AudioSampleBufferAssembler.channelLayoutTag(acmod: 2, lfeOn: false) == kAudioChannelLayoutTag_Stereo)
        #expect(AudioSampleBufferAssembler.channelLayoutTag(acmod: 1, lfeOn: false) == kAudioChannelLayoutTag_Mono)
    }

    /// CoreAudio has no tag for these three, and inventing one would describe
    /// the stream wrongly. They must report `nil` so the caller falls back to
    /// discrete channels rather than to a plausible-looking mismatch.
    @Test("Combinations CoreAudio has no tag for report nil", arguments: [(0, true), (2, true), (6, true)])
    func untaggedCombinationsReturnNil(acmod: Int, lfeOn: Bool) {
        #expect(AudioSampleBufferAssembler.channelLayoutTag(acmod: acmod, lfeOn: lfeOn) == nil)
    }

    /// End to end: a parsed frame has to arrive at CoreMedia carrying its layout.
    /// The renderer reads the format description, not the parser, so a layout
    /// that never reaches it would leave the original bug fully intact.
    @Test("Format description carries the layout through to CoreMedia")
    func formatDescriptionCarriesLayout() throws {
        let assembler = AudioSampleBufferAssembler()
        let recorder = FormatRecorder()
        assembler.delegate = recorder

        // 3/2 + LFE at 48 kHz — the configuration a 5.1 broadcast produces.
        let info = AC3FrameInfo(
            isEnhanced: false,
            sampleRate: 48000,
            channelCount: 6,
            isLFEOn: true,
            bitrateKbps: 448,
            frameSizeBytes: 1792,
            samplesPerFrame: 1536,
            acmod: 7
        )
        assembler.ac3FrameParser(
            AC3FrameParser(),
            didEmitFrame: ParsedAudioFrame(
                info: info,
                data: Data(repeating: 0, count: 1792),
                pts: CMTime(seconds: 1.0, preferredTimescale: 90000),
                pts90k: 90000
            )
        )

        let format = try #require(recorder.lastFormat, "assembler emitted no format description")
        var layoutSize = 0
        let layout = try #require(
            CMAudioFormatDescriptionGetChannelLayout(format, sizeOut: &layoutSize),
            "format description carries no channel layout"
        )
        #expect(layout.pointee.mChannelLayoutTag == kAudioChannelLayoutTag_AudioUnit_5_1)
    }

    /// Captures the format description the assembler publishes.
    private final class FormatRecorder: AudioSampleBufferAssemblerDelegate, @unchecked Sendable {
        var lastFormat: CMAudioFormatDescription?

        func audioSampleBufferAssembler(
            _ assembler: AudioSampleBufferAssembler,
            didUpdateFormat formatDescription: CMAudioFormatDescription,
            codec: AudioStreamCodec,
            sampleRate: Int,
            channels: Int,
            bitrateKbps: Int
        ) {
            lastFormat = formatDescription
        }

        func audioSampleBufferAssembler(
            _ assembler: AudioSampleBufferAssembler,
            didEmitSampleBuffer sampleBuffer: CMSampleBuffer,
            codec: AudioStreamCodec,
            duration: CMTime
        ) {}

        func audioSampleBufferAssembler(
            _ assembler: AudioSampleBufferAssembler,
            didEncounterError reason: String
        ) {}
    }
}
