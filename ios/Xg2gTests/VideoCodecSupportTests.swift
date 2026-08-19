// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

/// Which broadcast video formats the direct path can actually turn into
/// pictures.
///
/// The PMT classifies four stream types as video and the pipeline has one
/// assembler, so three of them produce a black screen. These tests pin which is
/// which, because the difference is invisible at runtime: nothing errors, the
/// packets keep arriving, and no picture ever appears.
struct VideoCodecSupportTests {

    @Test("Stream types map to the codec the PMT names", arguments: [
        (UInt8(0x1B), VideoStreamCodec.h264),
        (UInt8(0x02), VideoStreamCodec.mpeg2),
        (UInt8(0x01), VideoStreamCodec.mpeg1),
        (UInt8(0x24), VideoStreamCodec.hevc)
    ])
    func mapsStreamTypes(streamType: UInt8, expected: VideoStreamCodec) {
        #expect(VideoStreamCodec(streamType: streamType) == expected)
    }

    @Test("An unrecognised stream type is carried, not guessed")
    func keepsUnknownStreamType() {
        #expect(VideoStreamCodec(streamType: 0x42) == .unknown(0x42))
        #expect(VideoStreamCodec(streamType: 0x42).isDecodableOnDevice == false)
    }

    /// H.264 alone, because `H264AccessUnitAssembler` is the only assembler on
    /// this path. HEVC is the trap: the hardware decodes it, so a check written
    /// against device capability would wave it through and still show nothing.
    @Test("Only H.264 is playable on the direct path")
    func onlyH264IsPlayable() {
        #expect(VideoStreamCodec.h264.isDecodableOnDevice)
        #expect(VideoStreamCodec.mpeg2.isDecodableOnDevice == false)
        #expect(VideoStreamCodec.mpeg1.isDecodableOnDevice == false)
        #expect(VideoStreamCodec.hevc.isDecodableOnDevice == false)
    }

    /// The viewer is told which format, so the message is not a generic
    /// failure. It must never be empty, and never leak the raw stream type.
    @Test("Every codec has something to tell the viewer", arguments: [
        VideoStreamCodec.h264, .mpeg2, .mpeg1, .hevc, .unknown(0x42)
    ])
    func viewerDescriptionIsUsable(codec: VideoStreamCodec) {
        #expect(!codec.viewerDescription.isEmpty)
        #expect(!codec.viewerDescription.contains("0x"))
    }

    /// A PMT naming MPEG-2 video has to reach the delegate as MPEG-2, since
    /// that is the only point where the black screen can still be explained.
    @Test("The parser reports the codec its PMT names")
    func parserReportsCodecFromPMT() throws {
        let recorder = CodecRecorder()
        let parser = TSPacketParser()
        parser.delegate = recorder

        parser.feed(data: Self.transportStream(videoStreamType: 0x02, videoPID: 0x0100))

        #expect(recorder.codec == .mpeg2)
        #expect(recorder.codec?.isDecodableOnDevice == false)
        #expect(parser.videoCodec == .mpeg2)
    }

    @Test("An H.264 PMT reports a playable codec")
    func parserReportsH264() throws {
        let recorder = CodecRecorder()
        let parser = TSPacketParser()
        parser.delegate = recorder

        parser.feed(data: Self.transportStream(videoStreamType: 0x1B, videoPID: 0x0100))

        #expect(recorder.codec == .h264)
        #expect(recorder.codec?.isDecodableOnDevice == true)
    }

    // MARK: - Fixtures

    private final class CodecRecorder: TSPacketParserDelegate, @unchecked Sendable {
        var codec: VideoStreamCodec?

        func tsParser(_ parser: TSPacketParser, didDetermineVideoCodec codec: VideoStreamCodec) {
            self.codec = codec
        }
        func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16) {}
        func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool) {}
        func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8) {}
    }

    /// A PAT naming one programme, and its PMT naming one video stream.
    private static func transportStream(videoStreamType: UInt8, videoPID: UInt16) -> Data {
        let pmtPID: UInt16 = 0x1000
        return packet(pid: 0x0000, payload: patSection(programNumber: 1, pmtPID: pmtPID))
            + packet(pid: pmtPID, payload: pmtSection(pcrPID: videoPID, streamType: videoStreamType, elementaryPID: videoPID))
    }

    private static func patSection(programNumber: UInt16, pmtPID: UInt16) -> Data {
        var body = Data()
        body.append(contentsOf: [0x00, 0x01])                 // transport stream id
        body.append(0xC1)                                     // version, current
        body.append(0x00)                                     // section number
        body.append(0x00)                                     // last section number
        body.append(UInt8(programNumber >> 8))
        body.append(UInt8(programNumber & 0xFF))
        body.append(UInt8(0xE0 | UInt8(pmtPID >> 8)))
        body.append(UInt8(pmtPID & 0xFF))
        return section(tableID: 0x00, body: body)
    }

    private static func pmtSection(pcrPID: UInt16, streamType: UInt8, elementaryPID: UInt16) -> Data {
        var body = Data()
        body.append(contentsOf: [0x00, 0x01])                 // programme number
        body.append(0xC1)
        body.append(0x00)
        body.append(0x00)
        body.append(UInt8(0xE0 | UInt8(pcrPID >> 8)))
        body.append(UInt8(pcrPID & 0xFF))
        body.append(contentsOf: [0xF0, 0x00])                 // programme info length 0
        body.append(streamType)
        body.append(UInt8(0xE0 | UInt8(elementaryPID >> 8)))
        body.append(UInt8(elementaryPID & 0xFF))
        body.append(contentsOf: [0xF0, 0x00])                 // ES info length 0
        return section(tableID: 0x02, body: body)
    }

    /// Wraps a table body in its section header and a CRC placeholder.
    private static func section(tableID: UInt8, body: Data) -> Data {
        var out = Data([tableID])
        let length = body.count + 4                           // body + CRC32
        out.append(UInt8(0xB0 | UInt8((length >> 8) & 0x0F)))
        out.append(UInt8(length & 0xFF))
        out.append(body)
        out.append(contentsOf: [0x00, 0x00, 0x00, 0x00])      // CRC32, unchecked here
        return out
    }

    /// One 188-byte packet carrying a section, pointer field included.
    private static func packet(pid: UInt16, payload: Data) -> Data {
        var out = Data([0x47])
        out.append(UInt8(0x40 | UInt8(pid >> 8)))             // payload unit start
        out.append(UInt8(pid & 0xFF))
        out.append(0x10)                                      // payload only, continuity 0
        out.append(0x00)                                      // pointer field
        out.append(payload)
        if out.count < 188 {
            out.append(Data(repeating: 0xFF, count: 188 - out.count))
        }
        return out.prefix(188)
    }
}
