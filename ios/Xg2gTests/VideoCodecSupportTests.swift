// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
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

    /// The parser reports what the PMT named even when nothing can play it, so
    /// the viewer-facing message can name the format instead of shrugging.
    @Test("An unplayable codec still reaches the delegate")
    func unplayableCodecStillReported() {
        let recorder = CodecRecorder()
        let parser = TSPacketParser()
        parser.delegate = recorder

        parser.feed(data: Self.transportStream(videoStreamType: 0x01, videoPID: 0x0100))

        #expect(recorder.codec == .mpeg1)
        #expect(recorder.codec?.isDecodableOnDevice == false)
    }

    /// The codecs an assembler exists for map to a VideoToolbox type; the rest
    /// map to nothing, which is what makes them unplayable regardless of what
    /// the platform could decode.
    @Test("Only the assembled codecs have a VideoToolbox type")
    func codecTypeMapping() {
        #expect(VideoStreamCodec.h264.videoToolboxCodecType == kCMVideoCodecType_H264)
        #expect(VideoStreamCodec.mpeg2.videoToolboxCodecType == kCMVideoCodecType_MPEG2Video)
        #expect(VideoStreamCodec.hevc.videoToolboxCodecType == kCMVideoCodecType_HEVC)
        #expect(VideoStreamCodec.mpeg1.videoToolboxCodecType == nil)
        #expect(VideoStreamCodec.unknown(0x42).videoToolboxCodecType == nil)
    }

    /// Playability is measured, not tabulated, so only the invariants can be
    /// asserted here. H.264 must work — the native path is built on it. MPEG-1
    /// and unknown types must not, because no assembler cuts them.
    ///
    /// MPEG-2 and HEVC are deliberately left unasserted: both depend on what
    /// the running platform offers, and the Simulator answers differently from
    /// a device. `VideoDecoderAvailabilityTests` records what this platform
    /// actually said.
    @Test("Playability holds for the cases that cannot vary")
    func playabilityInvariants() {
        #expect(VideoStreamCodec.h264.isDecodableOnDevice)
        #expect(VideoStreamCodec.mpeg1.isDecodableOnDevice == false)
        #expect(VideoStreamCodec.unknown(0x42).isDecodableOnDevice == false)
    }

    /// The answer has to be stable within a run: it decides whether a channel
    /// shows a picture or an explanation, and a value that flickered would show
    /// both on the same stream.
    @Test("Repeated queries agree", arguments: [VideoStreamCodec.h264, .mpeg2, .hevc])
    func answerIsStable(codec: VideoStreamCodec) {
        #expect(codec.isDecodableOnDevice == codec.isDecodableOnDevice)
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
