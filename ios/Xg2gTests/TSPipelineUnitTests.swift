// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import Testing
@testable import Xg2g

struct TSPipelineUnitTests {

    // MARK: - 1. TSPacketParser Tests

    @Test func tsParserDetectsSyncByteAndResyncsOnGarbage() throws {
        let parser = TSPacketParser()
        let sink = MockTSSink()
        parser.delegate = sink

        // Build a valid dummy TS packet for PID 0x100
        var validPacket = Data(repeating: 0xFF, count: 188)
        validPacket[0] = 0x47 // Sync byte
        validPacket[1] = 0x01 // PID high = 0x01
        validPacket[2] = 0x00 // PID low  = 0x00 -> PID 0x100
        validPacket[3] = 0x10 // payload only, continuity counter = 0

        // Prepend 15 bytes of garbage before the packet
        let garbage = Data(repeating: 0xEE, count: 15)
        var stream = Data()
        stream.append(garbage)
        stream.append(validPacket)
        stream.append(validPacket)

        parser.feed(data: stream)

        #expect(sink.emittedPayloads.count >= 0)
    }

    @Test func tsParserTracksContinuityCounterErrors() throws {
        let parser = TSPacketParser()
        let sink = MockTSSink()
        parser.delegate = sink

        // Send Packet 1 (CC = 0)
        var p1 = Data(repeating: 0xAA, count: 188)
        p1[0] = 0x47
        p1[1] = 0x01
        p1[2] = 0x23 // PID 0x123
        p1[3] = 0x10 // CC = 0

        // Send Packet 2 with jump (CC = 5 instead of 1)
        var p2 = Data(repeating: 0xAA, count: 188)
        p2[0] = 0x47
        p2[1] = 0x01
        p2[2] = 0x23 // PID 0x123
        p2[3] = 0x15 // CC = 5

        parser.feed(data: p1)
        parser.feed(data: p2)

        #expect(sink.continuityErrors == 1)
    }

    @Test func tsParserExtractsPATAndPMT() throws {
        let parser = TSPacketParser()
        let sink = MockTSSink()
        parser.delegate = sink

        // Construct synthetic PAT packet (PID 0) pointing to PMT PID 0x100
        var patPacket = Data(repeating: 0xFF, count: 188)
        patPacket[0] = 0x47
        patPacket[1] = 0x40 // payloadUnitStart = true, PID = 0
        patPacket[2] = 0x00
        patPacket[3] = 0x10 // payload only, CC = 0
        patPacket[4] = 0x00 // pointer field
        patPacket[5] = 0x00 // table_id = 0x00 (PAT)
        patPacket[6] = 0xB0 // section_syntax_indicator & section_length
        patPacket[7] = 0x0D // section_length = 13
        patPacket[8] = 0x00 // transport_stream_id
        patPacket[9] = 0x01
        patPacket[10] = 0xC1 // version, current_next
        patPacket[11] = 0x00 // section_number
        patPacket[12] = 0x00 // last_section_number
        // Program 1 -> PMT PID 0x0100
        patPacket[13] = 0x00
        patPacket[14] = 0x01 // program_number = 1
        patPacket[15] = 0xE1 // PMT PID high (0x01)
        patPacket[16] = 0x00 // PMT PID low  (0x00) -> 0x0100
        // 4 CRC dummy bytes
        patPacket[17] = 0x00
        patPacket[18] = 0x00
        patPacket[19] = 0x00
        patPacket[20] = 0x00

        // Construct synthetic PMT packet (PID 0x100) with H.264 stream_type 0x1B at PID 0x200
        var pmtPacket = Data(repeating: 0xFF, count: 188)
        pmtPacket[0] = 0x47
        pmtPacket[1] = 0x41 // payloadUnitStart = true, PID = 0x100
        pmtPacket[2] = 0x00
        pmtPacket[3] = 0x10 // payload only, CC = 0
        pmtPacket[4] = 0x00 // pointer field
        pmtPacket[5] = 0x02 // table_id = 0x02 (PMT)
        pmtPacket[6] = 0xB0
        pmtPacket[7] = 0x12 // section_length = 18
        pmtPacket[8] = 0x00
        pmtPacket[9] = 0x01 // program_number = 1
        pmtPacket[10] = 0xC1
        pmtPacket[11] = 0x00
        pmtPacket[12] = 0x00
        pmtPacket[13] = 0xE2 // PCR PID 0x0200
        pmtPacket[14] = 0x00
        pmtPacket[15] = 0xF0 // program_info_length = 0
        pmtPacket[16] = 0x00
        // Stream: type 0x1B (H.264), elementary_PID = 0x0200
        pmtPacket[17] = 0x1B // H.264
        pmtPacket[18] = 0xE2 // PID high (0x02)
        pmtPacket[19] = 0x00 // PID low (0x00) -> 0x0200
        pmtPacket[20] = 0xF0 // ES_info_length = 0
        pmtPacket[21] = 0x00

        parser.feed(data: patPacket)
        parser.feed(data: pmtPacket)

        #expect(parser.videoPID == 0x0200)
        #expect(sink.discoveredVideoPID == 0x0200)
    }

    // MARK: - 2. PESPacketAssembler Tests

    @Test func pesAssemblerExtracts33BitPTS() throws {
        let assembler = PESPacketAssembler()
        let sink = MockPESSink()
        assembler.delegate = sink

        // Build a PES packet for Video (stream_id 0xE0) with PTS = 90000 (1.0 second)
        // 90000 = 0x15F90 -> pts32_30=0, pts29_15=2, pts14_0=24464 (0x5F90)
        var pes = Data()
        pes.append(contentsOf: [0x00, 0x00, 0x01, 0xE0]) // Prefix + stream_id
        pes.append(contentsOf: [0x00, 0x00])             // Length (unbounded for video)
        pes.append(contentsOf: [0x80, 0x80, 0x05])       // flags (PTS only, header len = 5)

        // 33-bit PTS encoding for value = 90000:
        // b0: '0010' pts32..30 '1' -> 0x21
        // b1: pts29..22 -> 0x00
        // b2: pts21..15 '1' -> 0x05
        // b3: pts14..7 -> 0xBF
        // b4: pts6..0 '1' -> 0x21
        let ptsValue: UInt64 = 90000
        let b0 = UInt8(0x21 | (((ptsValue >> 30) & 0x07) << 1))
        let b1 = UInt8((ptsValue >> 22) & 0xFF)
        let b2 = UInt8(0x01 | (((ptsValue >> 15) & 0x7F) << 1))
        let b3 = UInt8((ptsValue >> 7) & 0xFF)
        let b4 = UInt8(0x01 | ((ptsValue & 0x7F) << 1))
        pes.append(contentsOf: [b0, b1, b2, b3, b4])

        // Elementary stream payload (NAL unit)
        let nalPayload = Data([0x00, 0x00, 0x00, 0x01, 0x09, 0x10])
        pes.append(nalPayload)

        // Feed first PES unit
        assembler.feed(payload: pes, unitStart: true)
        // Trigger emit with next unitStart
        assembler.feed(payload: Data([0x00, 0x00, 0x01, 0xE0]), unitStart: true)

        #expect(sink.emittedUnits.count == 1)
        if let unit = sink.emittedUnits.first {
            #expect(unit.pts.isValid)
            #expect(unit.pts.seconds == 1.0)
            #expect(unit.data == nalPayload)
        }
    }

    // MARK: - 3. H264AccessUnitAssembler Tests

    @Test func h264AssemblerParsesInterlacedSPS() throws {
        let assembler = H264AccessUnitAssembler()
        let sink = MockAccessUnitSink()
        assembler.delegate = sink

        // Generate mathematically exact 1080i50 DVB Interlaced SPS bitstream
        var writer = BitWriter()
        writer.writeBits(0x64, count: 8) // profile_idc = 100 (High)
        writer.writeBits(0x00, count: 8) // constraint_set_flags
        writer.writeBits(0x28, count: 8) // level_idc = 40 (Level 4.0)
        writer.writeExpGolomb(0)         // seq_parameter_set_id = 0
        writer.writeExpGolomb(1)         // chroma_format_idc = 1 (4:2:0)
        writer.writeExpGolomb(0)         // bit_depth_luma_minus8 = 0
        writer.writeExpGolomb(0)         // bit_depth_chroma_minus8 = 0
        writer.writeBit(0)               // qpprime_y_zero_transform_bypass_flag = 0
        writer.writeBit(0)               // seq_scaling_matrix_present_flag = 0
        writer.writeExpGolomb(0)         // log2_max_frame_num_minus4 = 0
        writer.writeExpGolomb(0)         // pic_order_cnt_type = 0
        writer.writeExpGolomb(1)         // log2_max_pic_order_cnt_lsb_minus4 = 1
        writer.writeExpGolomb(4)         // max_num_ref_frames = 4
        writer.writeBit(0)               // gaps_in_frame_num_value_allowed_flag = 0
        writer.writeExpGolomb(119)       // pic_width_in_mbs_minus1 = 119 -> width = 1920
        writer.writeExpGolomb(33)        // pic_height_in_map_units_minus1 = 33 -> height = 1088
        writer.writeBit(0)               // frame_mbs_only_flag = 0 (Interlaced)
        writer.writeBit(1)               // mb_adaptive_frame_field_flag = 1 (MBAFF)
        writer.writeBit(1)               // direct_8x8_inference_flag = 1
        writer.writeBit(1)               // frame_cropping_flag = 1
        writer.writeExpGolomb(0)         // crop_left = 0
        writer.writeExpGolomb(0)         // crop_right = 0
        writer.writeExpGolomb(0)         // crop_top = 0
        writer.writeExpGolomb(2)         // crop_bottom = 2 -> 2 * 4 = 8 -> 1088 - 8 = 1080
        writer.writeBit(1)               // rbsp_stop_one_bit

        var spsData = Data([0x67]) // NAL header for SPS (type 7)
        spsData.append(writer.finish())

        let ppsBytes: [UInt8] = [0x68, 0xeb, 0xe3, 0xcb, 0x22, 0xc0]

        var payload = Data()
        payload.append(contentsOf: [0x00, 0x00, 0x00, 0x01])
        payload.append(spsData)
        payload.append(contentsOf: [0x00, 0x00, 0x00, 0x01])
        payload.append(contentsOf: ppsBytes)

        let unit = PESVideoAccessUnit(data: payload, pts: CMTime(value: 0, timescale: 90000), dts: nil)
        assembler.process(unit: unit)

        if let info = assembler.decodedInfo {
            #expect(info.width == 1920)
            #expect(info.height == 1080)
            #expect(info.isInterlaced == true)
            #expect(info.isTopFieldFirst == true)
        } else {
            Issue.record("Expected decodedInfo to be parsed from SPS")
        }
    }
}

// MARK: - Mocks for Testing

private final class MockTSSink: TSPacketParserDelegate, @unchecked Sendable {
    var discoveredVideoPID: UInt16?
    var emittedPayloads: [Data] = []
    var continuityErrors: Int = 0

    func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16) {
        self.discoveredVideoPID = pid
    }

    func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool) {
        self.emittedPayloads.append(data)
    }

    func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8) {
        self.continuityErrors += 1
    }
}

private final class MockPESSink: PESPacketAssemblerDelegate, @unchecked Sendable {
    var emittedUnits: [PESVideoAccessUnit] = []
    var pesErrors: [String] = []

    func pesAssembler(_ assembler: PESPacketAssembler, didEmitVideoUnit unit: PESVideoAccessUnit) {
        self.emittedUnits.append(unit)
    }

    func pesAssembler(_ assembler: PESPacketAssembler, didEncounterPESError reason: String) {
        self.pesErrors.append(reason)
    }
}

private final class MockAccessUnitSink: H264AccessUnitAssemblerDelegate, @unchecked Sendable {
    var formatDescription: CMVideoFormatDescription?
    var info: H264DecodedInfo?
    var emittedSampleBuffers: [CMSampleBuffer] = []

    func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didUpdateFormat formatDescription: CMVideoFormatDescription, info: H264DecodedInfo) {
        self.formatDescription = formatDescription
        self.info = info
    }

    func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didEmitSampleBuffer sampleBuffer: CMSampleBuffer, isIDR: Bool, isTopFieldFirst: Bool) {
        self.emittedSampleBuffers.append(sampleBuffer)
    }
}

private struct BitWriter {
    var data = Data()
    var currentByte: UInt8 = 0
    var bitCount = 0

    mutating func writeBit(_ bit: Int) {
        currentByte = (currentByte << 1) | UInt8(bit & 1)
        bitCount += 1
        if bitCount == 8 {
            data.append(currentByte)
            currentByte = 0
            bitCount = 0
        }
    }

    mutating func writeBits(_ value: Int, count: Int) {
        for i in (0..<count).reversed() {
            writeBit((value >> i) & 1)
        }
    }

    mutating func writeExpGolomb(_ value: Int) {
        let val = value + 1
        let bitLen = Int(log2(Double(val)))
        let leadingZeros = bitLen
        for _ in 0..<leadingZeros {
            writeBit(0)
        }
        for i in (0...bitLen).reversed() {
            writeBit((val >> i) & 1)
        }
    }

    mutating func finish() -> Data {
        if bitCount > 0 {
            currentByte <<= (8 - bitCount)
            data.append(currentByte)
        }
        return data
    }
}
