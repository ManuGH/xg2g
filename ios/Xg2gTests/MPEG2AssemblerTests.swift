// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import Testing
@testable import Xg2g

/// Cutting an MPEG-2 elementary stream into access units.
///
/// The rules differ from H.264 in ways that are easy to get subtly wrong and
/// impossible to see afterwards — a picture assembled with the wrong headers
/// decodes into something, just not the right thing. The boundary rule is the
/// one worth pinning: a sequence header introduces the picture that follows it,
/// so it closes the access unit before it rather than joining it.
struct MPEG2AssemblerTests {

    private final class Recorder: VideoAccessUnitAssemblerDelegate, @unchecked Sendable {
        var formats: [CMVideoFormatDescription] = []
        var infos: [H264DecodedInfo] = []
        var samples: [(buffer: CMSampleBuffer, isSync: Bool, structure: H264PictureStructure)] = []

        func videoAssembler(didUpdateFormat formatDescription: CMVideoFormatDescription, info: H264DecodedInfo) {
            formats.append(formatDescription)
            infos.append(info)
        }

        func videoAssembler(didEmitSampleBuffer sampleBuffer: CMSampleBuffer, isSyncSample: Bool, structure: H264PictureStructure) {
            samples.append((sampleBuffer, isSyncSample, structure))
        }
    }

    private func makeAssembler() -> (MPEG2AccessUnitAssembler, Recorder) {
        let assembler = MPEG2AccessUnitAssembler()
        let recorder = Recorder()
        assembler.assemblerDelegate = recorder
        return (assembler, recorder)
    }

    // MARK: - Stream fixtures

    private static func startCode(_ code: UInt8) -> Data { Data([0x00, 0x00, 0x01, code]) }

    /// `sequence_header` carrying 12-bit width and height.
    private static func sequenceHeader(width: Int, height: Int) -> Data {
        var out = startCode(0xB3)
        out.append(UInt8((width >> 4) & 0xFF))
        out.append(UInt8(((width & 0x0F) << 4) | ((height >> 8) & 0x0F)))
        out.append(UInt8(height & 0xFF))
        out.append(contentsOf: [0x13, 0xFF, 0xFF, 0xE0, 0x18])   // aspect, rate, bitrate, vbv
        return out
    }

    /// `picture_header`: 10 bits temporal reference then 3 bits coding type.
    private static func pictureHeader(codingType: UInt8) -> Data {
        var out = startCode(0x00)
        out.append(0x00)
        out.append(UInt8((codingType & 0x07) << 3))
        out.append(contentsOf: [0xFF, 0xF8])
        return out
    }

    /// `picture_coding_extension`, identifier 8.
    private static func pictureCodingExtension(structure: UInt8, topFieldFirst: Bool) -> Data {
        var out = startCode(0xB5)
        out.append(0x8F)                                          // ext id 8 + f_code
        out.append(0xFF)
        out.append(UInt8(0x00 | (structure & 0x03)))              // intra_dc_precision + picture_structure
        out.append(topFieldFirst ? 0x80 : 0x00)
        return out
    }

    /// A `sequence_end_code`, so the picture before it is bounded. A live
    /// stream is bounded by whatever arrives next; a fixture has to say so.
    private static func terminator() -> Data { startCode(0xB7) }

    private static func slice(_ payload: [UInt8] = [0xAA, 0xBB, 0xCC]) -> Data {
        var out = startCode(0x01)
        out.append(contentsOf: payload)
        return out
    }

    // MARK: - Tests

    @Test("A complete picture becomes one access unit")
    func assemblesOnePicture() {
        let (assembler, recorder) = makeAssembler()

        var stream = Self.sequenceHeader(width: 720, height: 576)
        stream.append(Self.pictureHeader(codingType: 1))
        stream.append(Self.slice())
        assembler.feed(data: stream, pts: CMTime(value: 9000, timescale: 90000))

        // Nothing is emitted yet: the picture is only complete once the next
        // start code proves no more slices are coming.
        #expect(recorder.samples.isEmpty)

        var next = Self.pictureHeader(codingType: 2)
        next.append(Self.terminator())
        assembler.feed(data: next)
        #expect(recorder.samples.count == 1)
    }

    @Test("Dimensions come from the sequence header")
    func readsDimensions() {
        let (assembler, recorder) = makeAssembler()

        var stream = Self.sequenceHeader(width: 720, height: 576)
        stream.append(Self.pictureHeader(codingType: 1))
        stream.append(Self.slice())
        stream.append(Self.pictureHeader(codingType: 2))
        stream.append(Self.terminator())
        assembler.feed(data: stream)

        let info = try? #require(recorder.infos.first)
        #expect(info?.width == 720)
        #expect(info?.height == 576)
    }

    /// An I-picture can start a decoder; a P-picture cannot. The decode gate
    /// relies on that distinction to know when it may open.
    @Test("Only an intra picture is a sync sample")
    func marksIntraPicturesAsSync() {
        let (assembler, recorder) = makeAssembler()

        var stream = Self.sequenceHeader(width: 720, height: 576)
        stream.append(Self.pictureHeader(codingType: 1))          // I
        stream.append(Self.slice())
        stream.append(Self.pictureHeader(codingType: 2))          // P
        stream.append(Self.slice())
        stream.append(Self.pictureHeader(codingType: 3))          // closes the P
        stream.append(Self.terminator())
        assembler.feed(data: stream)

        #expect(recorder.samples.count == 2)
        #expect(recorder.samples[0].isSync == true)
        #expect(recorder.samples[1].isSync == false)
    }

    /// The rule that separates this from H.264: headers precede their picture.
    /// Appending a sequence header to the picture it follows would describe a
    /// picture the decoder has already been handed.
    @Test("A sequence header closes the previous access unit")
    func sequenceHeaderClosesPreviousPicture() {
        let (assembler, recorder) = makeAssembler()

        var stream = Self.sequenceHeader(width: 720, height: 576)
        stream.append(Self.pictureHeader(codingType: 1))
        stream.append(Self.slice())
        stream.append(Self.sequenceHeader(width: 720, height: 576))   // starts the next
        stream.append(Self.terminator())
        assembler.feed(data: stream)

        #expect(recorder.samples.count == 1)
    }

    @Test("Field pictures report their parity")
    func readsPictureStructure() {
        let (assembler, recorder) = makeAssembler()

        var stream = Self.sequenceHeader(width: 720, height: 576)
        stream.append(Self.pictureHeader(codingType: 1))
        stream.append(Self.pictureCodingExtension(structure: 2, topFieldFirst: false))  // bottom field
        stream.append(Self.slice())
        stream.append(Self.pictureHeader(codingType: 2))
        stream.append(Self.terminator())
        assembler.feed(data: stream)

        let structure = recorder.samples.first?.structure
        #expect(structure?.isFieldPicture == true)
        #expect(structure?.isBottomField == true)
    }

    @Test("A frame picture carries its field order")
    func readsFrameFieldOrder() {
        let (assembler, recorder) = makeAssembler()

        var stream = Self.sequenceHeader(width: 1920, height: 1080)
        stream.append(Self.pictureHeader(codingType: 1))
        stream.append(Self.pictureCodingExtension(structure: 3, topFieldFirst: true))   // frame, TFF
        stream.append(Self.slice())
        stream.append(Self.pictureHeader(codingType: 2))
        stream.append(Self.terminator())
        assembler.feed(data: stream)

        let structure = recorder.samples.first?.structure
        #expect(structure?.isFieldPicture == false)
        #expect(structure?.isTopFieldFirst == true)
    }

    /// The stream arrives in PES payloads that cut wherever they please,
    /// including through a start code prefix.
    @Test("A picture split across feeds is still assembled")
    func survivesSplitAcrossFeeds() {
        let (assembler, recorder) = makeAssembler()

        var stream = Self.sequenceHeader(width: 720, height: 576)
        stream.append(Self.pictureHeader(codingType: 1))
        stream.append(Self.slice())
        stream.append(Self.pictureHeader(codingType: 2))
        stream.append(Self.terminator())

        // Split mid-way through, deliberately inside a start code prefix.
        let cut = stream.count - 2
        assembler.feed(data: stream.prefix(cut))
        assembler.feed(data: stream.suffix(from: cut))

        #expect(recorder.samples.count == 1)
    }

    @Test("A picture with no sequence header yet is not emitted")
    func withoutDimensionsNothingIsEmitted() {
        let (assembler, recorder) = makeAssembler()

        var stream = Self.pictureHeader(codingType: 1)
        stream.append(Self.slice())
        stream.append(Self.pictureHeader(codingType: 2))
        stream.append(Self.terminator())
        assembler.feed(data: stream)

        // No sequence header means no dimensions, and a format description
        // cannot be invented — emitting would mean guessing the picture size.
        #expect(recorder.samples.isEmpty)
        #expect(recorder.formats.isEmpty)
    }

    /// At the end of a stream nothing further bounds the last picture, so it
    /// would be dropped unless the flush treats the buffer as complete.
    @Test("Flush emits the picture the stream ended on")
    func flushEmitsTrailingPicture() {
        let (assembler, recorder) = makeAssembler()

        var stream = Self.sequenceHeader(width: 720, height: 576)
        stream.append(Self.pictureHeader(codingType: 1))
        stream.append(Self.slice())
        assembler.feed(data: stream)
        #expect(recorder.samples.isEmpty)

        assembler.flush()
        #expect(recorder.samples.count == 1)
    }

    @Test("Reset clears everything a channel zap must not carry over")
    func resetClearsState() {
        let (assembler, recorder) = makeAssembler()

        var stream = Self.sequenceHeader(width: 720, height: 576)
        stream.append(Self.pictureHeader(codingType: 1))
        stream.append(Self.slice())
        assembler.feed(data: stream)

        assembler.reset()
        assembler.feed(data: Self.pictureHeader(codingType: 2))

        #expect(recorder.samples.isEmpty)
    }
}
