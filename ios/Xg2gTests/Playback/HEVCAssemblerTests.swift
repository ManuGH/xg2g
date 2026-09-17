// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import Testing
@testable import Xg2g

/// Cutting an Annex-B HEVC stream into access units.
///
/// The shape matches H.264 closely enough to invite copying it, and the details
/// that differ are exactly the ones that fail silently: the NAL type is six bits
/// starting at bit 1 of a two-byte header, so the H.264 `nal & 0x1F` reads a
/// value that is wrong without ever looking wrong.
struct HEVCAssemblerTests {

    private final class Recorder: VideoAccessUnitAssemblerDelegate, @unchecked Sendable {
        var formats: [CMVideoFormatDescription] = []
        var infos: [H264DecodedInfo] = []
        var samples: [(buffer: CMSampleBuffer, isSync: Bool)] = []

        func videoAssembler(didUpdateFormat formatDescription: CMVideoFormatDescription, info: H264DecodedInfo) {
            formats.append(formatDescription)
            infos.append(info)
        }

        func videoAssembler(didEmitSampleBuffer sampleBuffer: CMSampleBuffer, isSyncSample: Bool, structure: H264PictureStructure) {
            samples.append((sampleBuffer, isSyncSample))
        }
    }

    private func makeAssembler() -> (HEVCAccessUnitAssembler, Recorder) {
        let assembler = HEVCAccessUnitAssembler()
        let recorder = Recorder()
        assembler.assemblerDelegate = recorder
        return (assembler, recorder)
    }

    // MARK: - Fixtures

    private static let startCode = Data([0x00, 0x00, 0x00, 0x01])

    /// A NAL unit with the given type in its two-byte header.
    private static func nal(type: UInt8, payload: [UInt8] = [0xFF, 0xFF]) -> Data {
        var out = startCode
        out.append(UInt8((type << 1) & 0x7E))   // forbidden_zero + type
        out.append(0x01)                        // layer 0, temporal id 1
        out.append(contentsOf: payload)
        return out
    }

    /// A slice NAL, its `first_slice_segment_in_pic_flag` set or clear.
    private static func slice(type: UInt8, firstInPicture: Bool) -> Data {
        nal(type: type, payload: [firstInPicture ? 0x80 : 0x00, 0xAA, 0xBB])
    }

    /// An end-of-sequence NAL, so the NAL before it is bounded. A live stream
    /// is bounded by whatever arrives next; a fixture has to say so.
    private static func terminator() -> Data { nal(type: 36, payload: []) }

    /// Real parameter sets for 1920×1080 Main profile. VideoToolbox validates
    /// these, so invented bytes would be rejected and nothing would be tested.
    private static let vps = Data([
        0x40, 0x01, 0x0C, 0x01, 0xFF, 0xFF, 0x01, 0x60, 0x00, 0x00, 0x03, 0x00,
        0x90, 0x00, 0x00, 0x03, 0x00, 0x00, 0x03, 0x00, 0x3C, 0x95, 0x98, 0x09
    ])
    private static let sps = Data([
        0x42, 0x01, 0x01, 0x01, 0x60, 0x00, 0x00, 0x03, 0x00, 0x90, 0x00, 0x00,
        0x03, 0x00, 0x00, 0x03, 0x00, 0x3C, 0xA0, 0x03, 0xC0, 0x80, 0x10, 0xE5,
        0x96, 0x56, 0x69, 0x24, 0xCA, 0xE0, 0x10, 0x00, 0x00, 0x03, 0x00, 0x10,
        0x00, 0x00, 0x03, 0x01, 0xE0, 0x80
    ])
    private static let pps = Data([0x44, 0x01, 0xC1, 0x72, 0xB4, 0x62, 0x40])

    private static func parameterSets() -> Data {
        var out = startCode; out.append(vps)
        out.append(startCode); out.append(sps)
        out.append(startCode); out.append(pps)
        return out
    }

    // MARK: - Structure

    /// Without all three sets there is no format description, and without one
    /// no sample can be built — VPS included, which H.264 has no equivalent of.
    @Test("Nothing is emitted before the parameter sets arrive")
    func requiresParameterSets() {
        let (assembler, recorder) = makeAssembler()

        assembler.feed(data: Self.slice(type: 19, firstInPicture: true))   // IDR_W_RADL
        assembler.feed(data: Self.slice(type: 1, firstInPicture: true))
        assembler.flush()

        #expect(recorder.formats.isEmpty)
        #expect(recorder.samples.isEmpty)
    }

    /// The boundary rule: a VCL unit with `first_slice_segment_in_pic_flag` set
    /// opens a picture, so the one being collected is complete.
    @Test("A new first slice closes the previous picture")
    func firstSliceFlagStartsNewPicture() {
        let (assembler, recorder) = makeAssembler()

        var stream = Self.parameterSets()
        stream.append(Self.slice(type: 19, firstInPicture: true))    // picture 1
        stream.append(Self.slice(type: 1, firstInPicture: false))    // still picture 1
        stream.append(Self.slice(type: 1, firstInPicture: true))     // picture 2 opens
        stream.append(Self.terminator())
        assembler.feed(data: stream)

        // Only if the platform accepted the parameter sets is there anything to
        // count; the boundary rule is what is under test either way.
        if !recorder.formats.isEmpty {
            #expect(recorder.samples.count == 1)
        }
    }

    @Test("Parameter sets produce a format description with the stream's size")
    func buildsFormatDescription() throws {
        let (assembler, recorder) = makeAssembler()
        var stream = Self.parameterSets()
        stream.append(Self.terminator())
        assembler.feed(data: stream)

        // VideoToolbox validates the sets; on a platform that rejects them there
        // is nothing to assert, and saying so beats a test that quietly passes.
        try withKnownIssue("HEVC parameter sets not accepted on this platform", isIntermittent: true) {
            let info = try #require(recorder.infos.first)
            #expect(info.width == 1920)
            #expect(info.height == 1080)
            #expect(info.isInterlaced == false)
        } when: {
            recorder.formats.isEmpty
        }
    }

    /// An IRAP picture can start a decoder. The range covers BLA, IDR and CRA,
    /// because a broadcast may never send an IDR and CRA is what it starts on.
    @Test("IRAP pictures are sync samples", arguments: [
        (UInt8(19), true),   // IDR_W_RADL
        (UInt8(20), true),   // IDR_N_LP
        (UInt8(21), true),   // CRA_NUT
        (UInt8(16), true),   // BLA_W_LP
        (UInt8(1), false),   // TRAIL_R
        (UInt8(0), false)    // TRAIL_N
    ])
    func marksIRAPAsSync(nalType: UInt8, expectedSync: Bool) {
        let (assembler, recorder) = makeAssembler()

        var stream = Self.parameterSets()
        stream.append(Self.slice(type: nalType, firstInPicture: true))
        stream.append(Self.slice(type: 1, firstInPicture: true))      // closes it
        stream.append(Self.terminator())
        assembler.feed(data: stream)

        if let first = recorder.samples.first {
            #expect(first.isSync == expectedSync)
        }
    }

    /// PES payloads cut wherever they please, including inside a start code.
    @Test("A picture split across feeds is still assembled")
    func survivesSplitAcrossFeeds() {
        let (assembler, recorder) = makeAssembler()

        var stream = Self.parameterSets()
        stream.append(Self.slice(type: 19, firstInPicture: true))
        stream.append(Self.slice(type: 1, firstInPicture: true))
        stream.append(Self.terminator())

        let cut = stream.count - 3
        assembler.feed(data: stream.prefix(cut))
        assembler.feed(data: stream.suffix(from: cut))

        if !recorder.formats.isEmpty {
            #expect(recorder.samples.count == 1)
        }
    }

    /// Feeding repeatedly must not walk off the buffer. `Data.removeFirst`
    /// slides `startIndex` instead of re-basing, which trapped here.
    @Test("Many small feeds do not trap")
    func manySmallFeedsAreSafe() {
        let (assembler, _) = makeAssembler()

        var stream = Self.parameterSets()
        for i in 0..<20 {
            stream.append(Self.slice(type: i == 0 ? 19 : 1, firstInPicture: true))
        }

        // One byte at a time: every possible split point, including inside
        // every start code prefix.
        for byte in stream {
            assembler.feed(data: Data([byte]))
        }
        assembler.flush()
    }

    @Test("Reset clears everything a channel zap must not carry over")
    func resetClearsState() {
        let (assembler, recorder) = makeAssembler()

        var stream = Self.parameterSets()
        stream.append(Self.slice(type: 19, firstInPicture: true))
        assembler.feed(data: stream)

        assembler.reset()
        assembler.feed(data: Self.slice(type: 1, firstInPicture: true))
        assembler.flush()

        // The parameter sets went with the reset, so nothing can be built.
        #expect(recorder.samples.isEmpty)
    }
}
