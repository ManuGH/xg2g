// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import Testing
@testable import Xg2g

/// Measurements for the reported fault: some channels play a picture and stay
/// silent after a zap. These reproduce the audio chain's behaviour rather than
/// change it - what each one records is what the pipeline does today.
struct ZapAudioContinuityTests {

    // MARK: - Where the sound is lost

    /// MPEG Layer II is announced, understood, and reported as undecodable.
    ///
    /// This channel is silent on iOS, but it is silent *legibly*: the track
    /// reaches the delegate, so the pipeline logs "no decodable audio track".
    @Test func mpegLayer2ChannelIsAnnouncedAndFlaggedUndecodable() {
        let parser = TSPacketParser()
        let sink = ZapAudioSink()
        parser.delegate = sink

        parser.feed(data: makePAT(programNumber: 1, pmtPID: 98))
        var streams = Data()
        streams.append(contentsOf: [0x1B, 0xE1, 0x00, 0xF0, 0x00])             // H.264, PID 256
        streams.append(contentsOf: [0x04, 0xE1, 0x01, 0xF0, 0x00])             // MPEG audio, PID 257
        parser.feed(data: makePMT(pmtPID: 98, programNumber: 1, pcrPID: 256, streamData: streams))

        #expect(parser.audioTracks.count == 1)
        #expect(sink.callCount == 1, "an announced track must reach the delegate")
        #expect(parser.audioTracks.first?.codec == .mpegAudio)
        #expect(parser.audioTracks.first?.codec.isDecodableOnDevice == false)

        // A track is still selected, so the PID is not lost and the rest of the
        // pipeline keeps reporting.
        #expect(NativeTSVideoPipeline.preferredAudioTrack(from: parser.audioTracks) != nil)
    }

    /// A stream the PMT names but that this build cannot classify must still be
    /// reported.
    ///
    /// It is not turned into a track - selecting a stream whose codec is unknown
    /// would be guessing - but a channel whose only audio arrives this way is
    /// silent, and silence with no record of why is the failure mode this whole
    /// observation phase exists to remove. The AAC descriptor (0x7C) on stream
    /// type 0x06 is the case measured here; the backend's own PMT reader does
    /// count it as audio, so the two sides disagree and the disagreement has to
    /// be visible on the client.
    @Test func audioBehindAnUnknownDescriptorIsReportedRatherThanDropped() {
        let parser = TSPacketParser()
        let sink = ZapAudioSink()
        parser.delegate = sink

        parser.feed(data: makePAT(programNumber: 1, pmtPID: 98))
        var streams = Data()
        streams.append(contentsOf: [0x1B, 0xE1, 0x00, 0xF0, 0x00])             // H.264, PID 256
        streams.append(contentsOf: [0x06, 0xE1, 0x01, 0xF0, 0x02, 0x7C, 0x00]) // AAC descriptor, PID 257
        parser.feed(data: makePMT(pmtPID: 98, programNumber: 1, pcrPID: 256, streamData: streams))

        #expect(parser.videoPID == 256)
        #expect(parser.audioTracks.isEmpty, "an unidentified codec must not become a track")
        #expect(sink.unclassified.count == 1, "but it must be reported")
        #expect(sink.unclassified.first?.pid == 257)
        #expect(sink.unclassified.first?.streamType == 0x06)
    }

    /// Teletext and subtitles are named in the same way and are positively
    /// explained by their descriptors. Reporting those would bury the case that
    /// matters, so they stay silent in the log as well as on the wire.
    @Test func streamsExplainedAsNonAudioAreNotReported() {
        let parser = TSPacketParser()
        let sink = ZapAudioSink()
        parser.delegate = sink

        parser.feed(data: makePAT(programNumber: 1, pmtPID: 98))
        var streams = Data()
        streams.append(contentsOf: [0x1B, 0xE1, 0x00, 0xF0, 0x00])                   // H.264
        streams.append(contentsOf: [0x06, 0xE1, 0x01, 0xF0, 0x02, 0x6A, 0x00])       // AC-3
        streams.append(contentsOf: [0x06, 0xE0, 0x21, 0xF0, 0x05, 0x56, 0x03, 0xDE, 0x01, 0x00]) // teletext
        streams.append(contentsOf: [0x06, 0xE0, 0x22, 0xF0, 0x02, 0x59, 0x00])       // subtitling
        parser.feed(data: makePMT(pmtPID: 98, programNumber: 1, pcrPID: 256, streamData: streams))

        #expect(parser.audioTracks.count == 1)
        #expect(sink.unclassified.isEmpty, "explained streams must not be reported as unidentified")
    }

    /// The DVB extension descriptor carries AC-4, so a stream using it is a
    /// candidate rather than an exclusion and has to surface.
    @Test func extensionDescriptorStreamsSurfaceAsCandidates() {
        let parser = TSPacketParser()
        let sink = ZapAudioSink()
        parser.delegate = sink

        parser.feed(data: makePAT(programNumber: 1, pmtPID: 98))
        var streams = Data()
        streams.append(contentsOf: [0x24, 0xE1, 0xFF, 0xF0, 0x00])             // HEVC
        streams.append(contentsOf: [0x06, 0xE2, 0x00, 0xF0, 0x02, 0x7F, 0x00]) // extension descriptor
        parser.feed(data: makePMT(pmtPID: 98, programNumber: 1, pcrPID: 511, streamData: streams))

        #expect(parser.audioTracks.isEmpty)
        #expect(sink.unclassified.count == 1)
    }

    // MARK: - A -> B -> A across codecs

    /// The format state must not survive a codec change and come back.
    ///
    /// The assembler keeps one format description and the frame info it was built
    /// from. Returning to a codec with the same parameters it had two channels ago
    /// must still rebuild the description, or AC-3 frames would be emitted carrying
    /// the AAC format left behind by the channel in between.
    @Test func returningToTheFirstCodecRebuildsItsFormat() {
        let assembler = AudioSampleBufferAssembler()
        let sink = ZapFormatSink()
        assembler.delegate = sink

        let ac3Parser = AC3FrameParser()
        let aacParser = AACADTSFrameParser()

        assembler.ac3FrameParser(ac3Parser, didEmitFrame: makeAC3Frame())
        let afterA = assembler.currentFormatDescription
        #expect(afterA != nil)

        assembler.aacFrameParser(aacParser, didEmitFrame: makeAACFrame())
        let afterB = assembler.currentFormatDescription
        #expect(afterB != nil)
        #expect(afterB !== afterA, "the AAC channel must not inherit the AC-3 format")

        assembler.ac3FrameParser(ac3Parser, didEmitFrame: makeAC3Frame())
        let afterAAgain = assembler.currentFormatDescription
        #expect(afterAAgain != nil)
        #expect(afterAAgain !== afterB, "returning to AC-3 must not keep the AAC format")

        let media = CMFormatDescriptionGetMediaSubType(afterAAgain!)
        #expect(media == kAudioFormatAC3, "the rebuilt format must describe AC-3")
        #expect(sink.formats.count == 3, "each change of codec must be announced")
        #expect(sink.errors.isEmpty)
    }

    /// The same guarantee across the AC-3 / E-AC-3 boundary, which shares one
    /// parser path and is told apart only by a field inside the frame info.
    @Test func enhancedAC3IsNotServedWithThePlainAC3Format() {
        let assembler = AudioSampleBufferAssembler()
        let sink = ZapFormatSink()
        assembler.delegate = sink
        let parser = AC3FrameParser()

        assembler.ac3FrameParser(parser, didEmitFrame: makeAC3Frame())
        let plain = assembler.currentFormatDescription
        assembler.ac3FrameParser(parser, didEmitFrame: makeAC3Frame(enhanced: true))
        let enhanced = assembler.currentFormatDescription

        #expect(plain != nil && enhanced != nil)
        #expect(enhanced !== plain)
        #expect(CMFormatDescriptionGetMediaSubType(enhanced!) == kAudioFormatEnhancedAC3)
    }

    /// `reset()` is what a zap runs. Nothing may survive it.
    @Test func resetDropsTheFormatSoTheNextChannelStartsCold() {
        let assembler = AudioSampleBufferAssembler()
        assembler.delegate = ZapFormatSink()
        assembler.ac3FrameParser(AC3FrameParser(), didEmitFrame: makeAC3Frame())
        #expect(assembler.currentFormatDescription != nil)

        assembler.reset()
        #expect(assembler.currentFormatDescription == nil)
        #expect(assembler.currentAC3FrameInfo == nil)
        #expect(assembler.currentAACFrameInfo == nil)
    }
}

// MARK: - Fixtures

private func makeAC3Frame(enhanced: Bool = false) -> ParsedAudioFrame {
    let info = AC3FrameInfo(
        isEnhanced: enhanced,
        sampleRate: 48000,
        channelCount: 2,
        isLFEOn: false,
        bitrateKbps: 448,
        frameSizeBytes: 1792,
        samplesPerFrame: 1536
    )
    return ParsedAudioFrame(
        info: info,
        data: Data(repeating: 0x0B, count: info.frameSizeBytes),
        pts: CMTime(value: 0, timescale: 90000),
        pts90k: 0
    )
}

private func makeAACFrame() -> ParsedAACFrame {
    let info = AACFrameInfo(
        profile: 1,
        sampleRate: 48000,
        sampleRateIndex: 3,
        channelCount: 2,
        channelConfig: 2,
        frameSizeBytes: 384,
        headerSizeBytes: 7,
        samplesPerFrame: 1024
    )
    return ParsedAACFrame(
        info: info,
        rawPayload: Data(repeating: 0x21, count: info.frameSizeBytes - info.headerSizeBytes),
        adtsData: Data(repeating: 0xFF, count: info.frameSizeBytes),
        pts: CMTime(value: 0, timescale: 90000),
        pts90k: 0
    )
}

private func makePAT(programNumber: UInt16, pmtPID: UInt16) -> Data {
    var pat = Data(repeating: 0xFF, count: 188)
    pat[0] = 0x47
    pat[1] = 0x40
    pat[2] = 0x00
    pat[3] = 0x10
    pat[4] = 0x00
    pat[5] = 0x00
    pat[6] = 0xB0
    pat[7] = 0x0D
    pat[8] = 0x00
    pat[9] = 0x01
    pat[10] = 0xC1
    pat[11] = 0x00
    pat[12] = 0x00
    pat[13] = UInt8((programNumber >> 8) & 0xFF)
    pat[14] = UInt8(programNumber & 0xFF)
    pat[15] = 0xE0 | UInt8((pmtPID >> 8) & 0x1F)
    pat[16] = UInt8(pmtPID & 0xFF)
    return pat
}

private func makePMT(pmtPID: UInt16, programNumber: UInt16, pcrPID: UInt16, streamData: Data) -> Data {
    var pmt = Data(repeating: 0xFF, count: 188)
    pmt[0] = 0x47
    pmt[1] = 0x40 | UInt8((pmtPID >> 8) & 0x1F)
    pmt[2] = UInt8(pmtPID & 0xFF)
    pmt[3] = 0x10
    pmt[4] = 0x00
    pmt[5] = 0x02

    let sectionLength = 9 + streamData.count + 4
    pmt[6] = 0xB0 | UInt8((sectionLength >> 8) & 0x0F)
    pmt[7] = UInt8(sectionLength & 0xFF)
    pmt[8] = UInt8((programNumber >> 8) & 0xFF)
    pmt[9] = UInt8(programNumber & 0xFF)
    pmt[10] = 0xC1
    pmt[11] = 0x00
    pmt[12] = 0x00
    pmt[13] = 0xE0 | UInt8((pcrPID >> 8) & 0x1F)
    pmt[14] = UInt8(pcrPID & 0xFF)
    pmt[15] = 0xF0
    pmt[16] = 0x00

    pmt.replaceSubrange(17..<(17 + streamData.count), with: streamData)
    return pmt
}

private final class ZapAudioSink: TSPacketParserDelegate, @unchecked Sendable {
    var discoveredTracks: [AudioTrackInfo] = []
    var unclassified: [UnclassifiedStreamInfo] = []
    var callCount = 0
    var videoPID: UInt16?

    func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16) {
        videoPID = pid
    }

    func tsParser(_ parser: TSPacketParser, didDiscoverAudioTracks tracks: [AudioTrackInfo]) {
        discoveredTracks = tracks
        callCount += 1
    }

    func tsParser(_ parser: TSPacketParser, didObserveUnclassifiedStreams streams: [UnclassifiedStreamInfo]) {
        unclassified = streams
    }

    func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool) {}

    func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8) {}
}

private final class ZapFormatSink: AudioSampleBufferAssemblerDelegate, @unchecked Sendable {
    var formats: [(CMAudioFormatDescription, AudioStreamCodec)] = []
    var errors: [String] = []
    var buffers = 0

    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didUpdateFormat formatDescription: CMAudioFormatDescription, codec: AudioStreamCodec, sampleRate: Int, channels: Int, bitrateKbps: Int) {
        formats.append((formatDescription, codec))
    }

    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didEmitSampleBuffer sampleBuffer: CMSampleBuffer, codec: AudioStreamCodec, duration: CMTime) {
        buffers += 1
    }

    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didEncounterError reason: String) {
        errors.append(reason)
    }
}

/// Audio layouts are not a fixed property of a channel. DVB signals a change with
/// a PMT version bump, and broadcasters use it: several German and Austrian
/// services carry MPEG Layer II during the day and add a Dolby track only for a
/// film or a match. A channel measured once is a measurement of that moment.
struct ZapAudioTrackChangeTests {

    /// The case that matters: nothing decodable was on offer, so the pipeline
    /// selected an undecodable track rather than lose the PID. When a decodable
    /// track later appears, it must switch to it - otherwise the channel stays
    /// silent for the whole film despite carrying Dolby the entire time.
    @Test func aDecodableTrackAppearingLaterIsSelected() {
        let pipeline = NativeTSVideoPipeline()
        let parser = TSPacketParser()

        let mp2Only = [
            AudioTrackInfo(pid: 257, streamType: 0x04, codec: .mpegAudio,
                           language: "deu", audioType: 0, descriptorTags: [0x0A])
        ]
        pipeline.tsParser(parser, didDiscoverAudioTracks: mp2Only)
        #expect(pipeline.selectedAudioPID == 257, "with nothing decodable, the PID is still kept")

        // The film starts: the broadcaster adds an AC-3 track. The MPEG track stays.
        let withAC3 = mp2Only + [
            AudioTrackInfo(pid: 258, streamType: 0x06, codec: .ac3,
                           language: "deu", audioType: 0, descriptorTags: [0x6A, 0x0A])
        ]
        pipeline.tsParser(parser, didDiscoverAudioTracks: withAC3)

        #expect(pipeline.selectedAudioPID == 258,
                "a decodable track that appears later must win over the undecodable one held so far")
    }

    /// The converse must not happen: a stable, decodable selection must not be
    /// disturbed because the broadcaster added another track.
    @Test func aWorkingSelectionSurvivesAnUnrelatedTrackAppearing() {
        let pipeline = NativeTSVideoPipeline()
        let parser = TSPacketParser()

        let initial = [
            AudioTrackInfo(pid: 258, streamType: 0x06, codec: .ac3,
                           language: "deu", audioType: 0, descriptorTags: [0x6A, 0x0A])
        ]
        pipeline.tsParser(parser, didDiscoverAudioTracks: initial)
        #expect(pipeline.selectedAudioPID == 258)

        let plusOriginal = initial + [
            AudioTrackInfo(pid: 259, streamType: 0x06, codec: .ac3,
                           language: "eng", audioType: 0, descriptorTags: [0x6A, 0x0A])
        ]
        pipeline.tsParser(parser, didDiscoverAudioTracks: plusOriginal)
        #expect(pipeline.selectedAudioPID == 258, "the German track must keep playing")
    }
}

/// SCTE-35 splice signalling appears on 24 of the 144 services in the measured
/// bouquet. Reporting it as an unidentified audio candidate would put two dozen
/// false lines in the log on every channel change and bury the one case the
/// report exists for.
struct ZapUnclassifiedFilterTests {

    @Test func scte35SpliceStreamsAreNotAudioCandidates() {
        #expect(UnclassifiedStreamInfo.isAudioCandidate(streamType: 0x86, descriptorTags: [0x05]) == false)
        #expect(UnclassifiedStreamInfo.isAudioCandidate(streamType: 0x86, descriptorTags: [0x52, 0x8A]) == false)
    }

    @Test func aCUEIRegistrationIsNotAnAudioCandidate() {
        #expect(UnclassifiedStreamInfo.isAudioCandidate(
            streamType: 0x06, descriptorTags: [0x05], registration: "CUEI") == false)
    }

    @Test func anUnexplainedPrivateStreamRemainsACandidate() {
        #expect(UnclassifiedStreamInfo.isAudioCandidate(streamType: 0x06, descriptorTags: [0x7C]) == true)
        #expect(UnclassifiedStreamInfo.isAudioCandidate(streamType: 0x06, descriptorTags: [0x7F]) == true)
    }
}

/// The clock a session presents on is replaced when the session starts.
///
/// `startStreaming` tears down whatever came before it, and that teardown builds a
/// fresh render synchronizer. Anything that attached to the old one is then waiting
/// on a clock nobody will ever start — the display layer reports itself never ready,
/// the presenter's queue fills, and the picture freezes on its last frame while the
/// audio, which follows the new clock, plays on. Measured on device, and the reason
/// the surface must be bound after the session is started, never before.
@MainActor
struct PresentationSynchronizerIdentityTests {

    @Test func startingASessionReplacesTheSynchronizerItPresentsOn() {
        let pipeline = NativeTSVideoPipeline()
        let before = pipeline.presentationSynchronizer

        // A stop is what a start performs first, so it is the operation under test.
        pipeline.stopStreaming()
        let after = pipeline.presentationSynchronizer

        #expect(before !== after, """
            the synchronizer survived a teardown; if that ever becomes true, the \
            ordering this depends on is no longer load-bearing and the comment in \
            ZapCoordinator.startOutright should be revisited rather than trusted
            """)
    }

    /// And the session keeps reporting the current one, so binding after a start
    /// attaches to the clock that will actually run.
    @Test func theSessionReportsItsCurrentSynchronizer() {
        let pipeline = NativeTSVideoPipeline()
        pipeline.stopStreaming()
        let current = pipeline.presentationSynchronizer
        #expect(pipeline.presentationSynchronizer === current)
    }
}
