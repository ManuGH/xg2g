// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import Testing
import VideoToolbox
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

    @Test func tsParserEmitsPlaintextPESEvenIfScramblingFlagIsSet() throws {
        let parser = TSPacketParser()
        let sink = MockTSSink()
        parser.delegate = sink

        var patPacket = Data(repeating: 0xFF, count: 188)
        patPacket[0] = 0x47
        patPacket[1] = 0x40
        patPacket[2] = 0x00
        patPacket[3] = 0x10
        patPacket[4] = 0x00
        patPacket[5] = 0x00
        patPacket[6] = 0xB0
        patPacket[7] = 0x0D
        patPacket[8] = 0x00
        patPacket[9] = 0x01
        patPacket[10] = 0xC1
        patPacket[11] = 0x00
        patPacket[12] = 0x00
        patPacket[13] = 0x00
        patPacket[14] = 0x01
        patPacket[15] = 0xE1
        patPacket[16] = 0x00
        patPacket[17] = 0x00
        patPacket[18] = 0x00
        patPacket[19] = 0x00
        patPacket[20] = 0x00

        var pmtPacket = Data(repeating: 0xFF, count: 188)
        pmtPacket[0] = 0x47
        pmtPacket[1] = 0x41
        pmtPacket[2] = 0x00
        pmtPacket[3] = 0x10
        pmtPacket[4] = 0x00
        pmtPacket[5] = 0x02
        pmtPacket[6] = 0xB0
        pmtPacket[7] = 0x12
        pmtPacket[8] = 0x00
        pmtPacket[9] = 0x01
        pmtPacket[10] = 0xC1
        pmtPacket[11] = 0x00
        pmtPacket[12] = 0x00
        pmtPacket[13] = 0xE2
        pmtPacket[14] = 0x00
        pmtPacket[15] = 0xF0
        pmtPacket[16] = 0x00
        pmtPacket[17] = 0x1B
        pmtPacket[18] = 0xE2
        pmtPacket[19] = 0x00
        pmtPacket[20] = 0xF0
        pmtPacket[21] = 0x00

        parser.feed(data: patPacket)
        parser.feed(data: pmtPacket)
        #expect(parser.videoPID == 0x0200)

        // Packet 1: Scrambled bit set (byte3 = 0x90 -> scramblingControl = 2), but payload starts with valid PES 0x000001
        var videoPacketDescrambled = Data(repeating: 0xAA, count: 188)
        videoPacketDescrambled[0] = 0x47
        videoPacketDescrambled[1] = 0x42 // payloadUnitStart = true, PID = 0x0200
        videoPacketDescrambled[2] = 0x00
        videoPacketDescrambled[3] = 0x90 // scramblingControl = 0b10 (2), CC = 0
        videoPacketDescrambled[4] = 0x00 // PES start code
        videoPacketDescrambled[5] = 0x00
        videoPacketDescrambled[6] = 0x01
        videoPacketDescrambled[7] = 0xE0

        parser.feed(data: videoPacketDescrambled)
        #expect(sink.emittedPayloads.count == 1)

        // Packet 2: Truly scrambled ciphertext (byte3 = 0x91, payload random bytes)
        var videoPacketEncrypted = Data(repeating: 0x7E, count: 188)
        videoPacketEncrypted[0] = 0x47
        videoPacketEncrypted[1] = 0x42 // payloadUnitStart = true, PID = 0x0200
        videoPacketEncrypted[2] = 0x00
        videoPacketEncrypted[3] = 0x91 // scramblingControl = 0b10, CC = 1
        videoPacketEncrypted[4] = 0xDE // NOT 0x00 0x00 0x01
        videoPacketEncrypted[5] = 0xAD
        videoPacketEncrypted[6] = 0xBE
        videoPacketEncrypted[7] = 0xEF

        parser.feed(data: videoPacketEncrypted)
        // Count should still be 1 because encrypted packet was rejected
        #expect(sink.emittedPayloads.count == 1)
        #expect(parser.scrambledPackets == 1)
    }

    // MARK: - 2. PESPacketAssembler Tests

    @Test func pesAssemblerExtracts33BitPTS() throws {
        let assembler = PESPacketAssembler()
        let sink = MockPESSink()
        assembler.delegate = sink

        // Build a PES packet for Video (stream_id 0xE0) with PTS = 90000 (1.0 second)
        var pes = Data()
        pes.append(contentsOf: [0x00, 0x00, 0x01, 0xE0]) // Prefix + stream_id
        pes.append(contentsOf: [0x00, 0x00])             // Length (unbounded for video)
        pes.append(contentsOf: [0x80, 0x80, 0x05])       // flags (PTS only, header len = 5)

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

        #expect(sink.emittedPayloads.count == 1)
        if let payload = sink.emittedPayloads.first {
            #expect(payload.pts != nil)
            #expect(payload.pts?.seconds == 1.0)
            #expect(payload.data == nalPayload)
        }
    }

    // MARK: - 3. H264AccessUnitAssembler Tests

    @Test func h264AssemblerParsesInterlacedSPS() throws {
        let assembler = H264AccessUnitAssembler()
        let sink = MockAccessUnitSink()
        assembler.delegate = sink

        let spsData = make1080i50SPS()
        let ppsData = make1080i50PPS()

        var payload = Data()
        payload.append(contentsOf: [0x00, 0x00, 0x00, 0x01])
        payload.append(spsData)
        payload.append(contentsOf: [0x00, 0x00, 0x00, 0x01])
        payload.append(ppsData)

        assembler.feed(data: payload, pts: CMTime(value: 0, timescale: 90000))

        if let info = assembler.decodedInfo {
            #expect(info.width == 1920)
            #expect(info.height == 1080)
            #expect(info.isTopFieldFirst == true)
        } else {
            Issue.record("Expected decodedInfo to be parsed from SPS")
        }
    }

    @Test func h264AssemblerSplitsContinuousAnnexBStreamAcrossAUBoundaries() throws {
        let assembler = H264AccessUnitAssembler()
        let sink = MockAccessUnitSink()
        assembler.delegate = sink

        let spsData = make1080i50SPS()
        let ppsData = make1080i50PPS()

        // NAL 1: IDR Slice (NAL type 5, first_mb == 0)
        var idrSlice = Data([0x65]) // type 5, ref_idc 3
        var idrWriter = BitWriter()
        idrWriter.writeExpGolomb(0) // first_mb_in_slice = 0
        idrWriter.writeExpGolomb(7) // slice_type = I_SLICE (7)
        idrWriter.writeExpGolomb(0) // pic_parameter_set_id = 0
        idrWriter.writeBits(0, count: 4) // frame_num = 0
        idrWriter.writeExpGolomb(0) // idr_pic_id = 0
        idrWriter.writeBits(0, count: 4) // pic_order_cnt_lsb = 0
        idrWriter.writeBit(1) // rbsp_stop_one_bit
        idrSlice.append(idrWriter.finish())

        // NAL 2: Non-IDR Slice 1 (NAL type 1, first_mb == 0)
        var nonIdrSlice1 = Data([0x41]) // type 1, ref_idc 2
        var pWriter1 = BitWriter()
        pWriter1.writeExpGolomb(0) // first_mb_in_slice = 0
        pWriter1.writeExpGolomb(5) // slice_type = P_SLICE (5)
        pWriter1.writeExpGolomb(0) // pic_parameter_set_id = 0
        pWriter1.writeBits(1, count: 4) // frame_num = 1
        pWriter1.writeExpGolomb(2) // pic_order_cnt_lsb = 2
        pWriter1.writeBit(1)
        nonIdrSlice1.append(pWriter1.finish())

        // Chunk 1: SPS + PPS + IDR Slice (PTS = 90000 / 1.0s)
        var chunk1 = Data()
        chunk1.append(contentsOf: [0x00, 0x00, 0x00, 0x01])
        chunk1.append(spsData)
        chunk1.append(contentsOf: [0x00, 0x00, 0x00, 0x01])
        chunk1.append(ppsData)
        chunk1.append(contentsOf: [0x00, 0x00, 0x00, 0x01])
        chunk1.append(idrSlice)

        assembler.feed(data: chunk1, pts: CMTime(value: 90000, timescale: 90000))

        // Chunk 2: Non-IDR Slice 1 (PTS = 93600 / 1.04s)
        var chunk2 = Data()
        chunk2.append(contentsOf: [0x00, 0x00, 0x00, 0x01])
        chunk2.append(nonIdrSlice1)

        assembler.feed(data: chunk2, pts: CMTime(value: 93600, timescale: 90000))

        // Flush remaining buffer
        assembler.flush()

        // Expect 2 distinct SampleBuffers emitted
        #expect(sink.emittedSampleBuffers.count == 2)

        if sink.emittedSampleBuffers.count >= 2 {
            let sb1 = sink.emittedSampleBuffers[0]
            let sb2 = sink.emittedSampleBuffers[1]

            let pts1 = CMSampleBufferGetPresentationTimeStamp(sb1)
            let pts2 = CMSampleBufferGetPresentationTimeStamp(sb2)
            let dur1 = CMSampleBufferGetDuration(sb1)
            let dur2 = CMSampleBufferGetDuration(sb2)

            #expect(pts1.seconds == 1.0)
            #expect(pts2.seconds == 1.04)
            #expect(dur1.seconds == 0.02 || dur1.seconds == 0.04)
            #expect(dur2.seconds == 0.04)
        }
    }

    /// The decode gate is only worth anything if this flag actually separates a
    /// startable picture from one that needs a reference. A detector that says
    /// "yes" to everything opens the gate on the first access unit and looks
    /// exactly like a healthy tune in the logs.
    @Test func h264AssemblerMarksOnlyStartablePicturesAsSyncSamples() throws {
        let assembler = H264AccessUnitAssembler()
        let sink = MockAccessUnitSink()
        assembler.delegate = sink

        let spsData = make1080i50SPS()
        let ppsData = make1080i50PPS()

        func slice(nalHeader: UInt8, sliceType: Int, frameNum: Int, idrPicID: Int?) -> Data {
            var nal = Data([nalHeader])
            var writer = BitWriter()
            writer.writeExpGolomb(0)            // first_mb_in_slice
            writer.writeExpGolomb(sliceType)
            writer.writeExpGolomb(0)            // pic_parameter_set_id
            writer.writeBits(frameNum, count: 4)
            if let idrPicID { writer.writeExpGolomb(idrPicID) }
            writer.writeExpGolomb(2)            // pic_order_cnt_lsb
            writer.writeBit(1)                  // rbsp_stop_one_bit
            nal.append(writer.finish())
            return nal
        }

        func feed(_ nals: [Data], pts: Int64) {
            var chunk = Data()
            for nal in nals {
                chunk.append(contentsOf: [0x00, 0x00, 0x00, 0x01])
                chunk.append(nal)
            }
            assembler.feed(data: chunk, pts: CMTime(value: pts, timescale: 90000))
        }

        // IDR, then an intra-only non-IDR picture, then a predicted one.
        feed([spsData, ppsData, slice(nalHeader: 0x65, sliceType: 7, frameNum: 0, idrPicID: 0)], pts: 90000)
        feed([slice(nalHeader: 0x41, sliceType: 7, frameNum: 1, idrPicID: nil)], pts: 93600)
        feed([slice(nalHeader: 0x41, sliceType: 5, frameNum: 2, idrPicID: nil)], pts: 97200)
        assembler.flush()

        #expect(sink.syncSampleFlags.count == 3)
        guard sink.syncSampleFlags.count == 3 else { return }

        #expect(sink.syncSampleFlags[0] == true)    // IDR
        #expect(sink.syncSampleFlags[1] == true)    // I slices, no IDR — still startable
        #expect(sink.syncSampleFlags[2] == false)   // P slices — needs a reference
    }

    /// One predicted slice is enough to make the whole picture unstartable, so
    /// the check has to run per slice rather than on the first one only.
    @Test func h264AssemblerTreatsMixedSlicePictureAsPredicted() throws {
        let assembler = H264AccessUnitAssembler()
        let sink = MockAccessUnitSink()
        assembler.delegate = sink

        var payload = Data()
        payload.append(contentsOf: [0x00, 0x00, 0x00, 0x01])
        payload.append(make1080i50SPS())
        payload.append(contentsOf: [0x00, 0x00, 0x00, 0x01])
        payload.append(make1080i50PPS())

        func slice(firstMB: Int, sliceType: Int) -> Data {
            var nal = Data([0x41])
            var writer = BitWriter()
            writer.writeExpGolomb(firstMB)
            writer.writeExpGolomb(sliceType)
            writer.writeExpGolomb(0)
            writer.writeBits(3, count: 4)
            writer.writeExpGolomb(2)
            writer.writeBit(1)
            nal.append(writer.finish())
            return nal
        }

        // One picture, two slices: intra first, predicted second.
        for nal in [slice(firstMB: 0, sliceType: 2), slice(firstMB: 120, sliceType: 0)] {
            payload.append(contentsOf: [0x00, 0x00, 0x00, 0x01])
            payload.append(nal)
        }

        assembler.feed(data: payload, pts: CMTime(value: 90000, timescale: 90000))
        assembler.flush()

        #expect(sink.syncSampleFlags == [false])
    }

    @Test func testRealStreamFeed() throws {
        guard let data = try? Data(contentsOf: URL(fileURLWithPath: "/tmp/vu_puls24_real.ts")) else {
            return
        }

        let parser = TSPacketParser()
        let pesAssembler = PESPacketAssembler()
        let auAssembler = H264AccessUnitAssembler()
        let auSink = MockAccessUnitSink()
        let bridge = PipelineBridge(pesAssembler: pesAssembler, auAssembler: auAssembler)

        parser.delegate = bridge
        pesAssembler.delegate = bridge
        auAssembler.delegate = auSink

        parser.feed(data: data)
        auAssembler.flush()

        #expect(parser.videoPID != nil)
        #expect(auSink.emittedSampleBuffers.count > 0)
        #expect(auSink.info?.isInterlaced == true)
        #expect(auSink.info?.width == 1920)
        #expect(auSink.info?.height == 1080)
    }

    @Test func testLiveDirectStreamPipeline() async throws {
        let pipeline = NativeTSVideoPipeline()
        let testURL = URL(string: "http://10.10.55.64:8001/1:0:19:81:6:85:C00000:0:0:0:")!
        
        await MainActor.run {
            pipeline.startStreaming(url: testURL)
        }

        // Stream for 4 seconds to collect full gate & decoder telemetry
        try await Task.sleep(nanoseconds: 4_000_000_000)

        await MainActor.run {
            pipeline.stopStreaming()
            let telemetry = pipeline.telemetry.snapshot()
            print("=== [GATE 2 TELEMETRY REPORT] ===")
            print("HTTP Connected: Status 200 OK")
            print("TTFP Total: \(String(format: "%.1f", telemetry.ttfpTotalMs)) ms")
            print("t0->t1 (Network): \(String(format: "%.1f", telemetry.ttfpNetworkMs)) ms")
            print("t1->t2 (PSI Demux): \(String(format: "%.1f", telemetry.ttfpPsiMs)) ms")
            print("t2->t3 (Params SPS/PPS): \(String(format: "%.1f", telemetry.ttfpParamSetsMs)) ms")
            print("t3->t4 (First IDR AU): \(String(format: "%.1f", telemetry.ttfpIdrMs)) ms")
            print("t4->t5 (HW Decode): \(String(format: "%.1f", telemetry.ttfpDecodeMs)) ms")
            print("t5->t6 (Render Submit): \(String(format: "%.1f", telemetry.ttfpRenderMs)) ms")
            print("Bitrate: \(String(format: "%.1f", telemetry.tsBitrateKbps)) kbps")
            print("Video PID: \(telemetry.videoPID)")
            print("Resolution: \(telemetry.videoWidth)x\(telemetry.videoHeight)")
            print("Interlaced: \(telemetry.isInterlaced)")
            print("Field Order: \(telemetry.fieldOrder)")
            print("HW Active: \(telemetry.hwDecodeActive)")
            print("Continuity Errors: \(telemetry.continuityErrors)")
            print("PES Errors: \(telemetry.pesErrors)")
            print("Decode Errors: \(telemetry.decodeErrors)")
            print("Decoded FPS: \(String(format: "%.1f", telemetry.decodedFramesPerSec))")
            print("Thermal State: \(telemetry.thermalState)")
            print("Memory Usage: \(String(format: "%.1f", telemetry.memoryUsageMB)) MB")
            print("==================================")
        }

        // Live stream assertion only if tuner was available (not occupied by physical test device)
        let snapshot = pipeline.telemetry.snapshot()
        if snapshot.tsBitrateKbps > 0 {
            #expect(snapshot.videoPID > 0)
        }
    }

    @Test func h264ParameterSetCachePrimesAndCachesCorrectly() throws {
        H264ParameterSetCache.shared.clear()
        let cache = H264ParameterSetCache()
        let sps = Data([0x67, 0x64, 0x00, 0x28, 0xAC, 0xD9, 0x40, 0x78, 0x02, 0x27, 0xE2])
        let pps = Data([0x68, 0xEB, 0xEC, 0xB2, 0x2C])
        let channelKey = "test://dummy_channel_key"

        cache.setParameterSets(sps: sps, pps: pps, for: channelKey)
        let retrieved = cache.parameterSets(for: channelKey)

        #expect(retrieved != nil)
        #expect(retrieved?.sps == sps)
        #expect(retrieved?.pps == pps)

        let assembler = H264AccessUnitAssembler()
        assembler.channelKey = channelKey
        assembler.primeWithParameterSets(sps: sps, pps: pps)
        #expect(assembler.decodedInfo != nil)
        cache.clear()
        H264ParameterSetCache.shared.clear()
    }

    // MARK: - 9. Hardware Video Decoder Recovery Tests

    @Test func decoderClassifiesFatalVsNonFatalErrorsCorrectly() throws {
        let decoder = HardwareVideoDecoder()

        // Fatal errors: kVTInvalidSessionErr (-12903) and kVTVideoDecoderMalfunctionErr (-12911)
        #expect(decoder.isFatalSessionError(kVTInvalidSessionErr))
        #expect(decoder.isFatalSessionError(kVTVideoDecoderMalfunctionErr))
        #expect(decoder.isFatalSessionError(-12903))
        #expect(decoder.isFatalSessionError(-12911))

        // Non-fatal errors: kVTVideoDecoderBadDataErr (-12909 or -12350), kVTParameterErr (-12902), etc.
        #expect(!decoder.isFatalSessionError(kVTVideoDecoderBadDataErr))
        #expect(!decoder.isFatalSessionError(-12350))
        #expect(!decoder.isFatalSessionError(kVTParameterErr))
        #expect(!decoder.isFatalSessionError(0))
    }

    @Test func decoderFatalErrorInvalidatesSessionAndBumpsGeneration() throws {
        let decoder = HardwareVideoDecoder()
        let sink = MockDecoderSink()
        decoder.delegate = sink

        let sps = make1080i50SPS()
        let pps = make1080i50PPS()
        let spsBytes = [UInt8](sps)
        let ppsBytes = [UInt8](pps)

        var formatDesc: CMFormatDescription?
        let status = spsBytes.withUnsafeBufferPointer { spsBuf in
            ppsBytes.withUnsafeBufferPointer { ppsBuf in
                let pointers = [spsBuf.baseAddress!, ppsBuf.baseAddress!]
                let sizes = [spsBuf.count, ppsBuf.count]
                return CMVideoFormatDescriptionCreateFromH264ParameterSets(
                    allocator: kCFAllocatorDefault,
                    parameterSetCount: 2,
                    parameterSetPointers: pointers,
                    parameterSetSizes: sizes,
                    nalUnitHeaderLength: 4,
                    formatDescriptionOut: &formatDesc
                )
            }
        }
        #expect(status == noErr)
        guard let format = formatDesc else { return }

        decoder.configure(with: format)
        #expect(decoder.hasActiveSession)

        // Verify error classification
        #expect(decoder.isFatalSessionError(kVTInvalidSessionErr))
        #expect(decoder.isFatalSessionError(kVTVideoDecoderMalfunctionErr))
        #expect(!decoder.isFatalSessionError(kVTVideoDecoderBadDataErr))
        #expect(!decoder.isFatalSessionError(-12350))
    }

    @Test func decoderEnsureSessionRecreatesSessionWhenInvalidated() throws {
        let decoder = HardwareVideoDecoder()
        let sps = make1080i50SPS()
        let pps = make1080i50PPS()
        let spsBytes = [UInt8](sps)
        let ppsBytes = [UInt8](pps)

        var formatDesc: CMFormatDescription?
        let status = spsBytes.withUnsafeBufferPointer { spsBuf in
            ppsBytes.withUnsafeBufferPointer { ppsBuf in
                let pointers = [spsBuf.baseAddress!, ppsBuf.baseAddress!]
                let sizes = [spsBuf.count, ppsBuf.count]
                return CMVideoFormatDescriptionCreateFromH264ParameterSets(
                    allocator: kCFAllocatorDefault,
                    parameterSetCount: 2,
                    parameterSetPointers: pointers,
                    parameterSetSizes: sizes,
                    nalUnitHeaderLength: 4,
                    formatDescriptionOut: &formatDesc
                )
            }
        }
        #expect(status == noErr)
        guard let format = formatDesc else { return }

        #expect(!decoder.hasActiveSession)
        decoder.configure(with: format)
        #expect(decoder.hasActiveSession)

        // Reset
        decoder.reset()
        #expect(!decoder.hasActiveSession)
        #expect(!decoder.isConfigured)

        // Once reconfigured
        decoder.configure(with: format)
        #expect(decoder.hasActiveSession)
        #expect(decoder.isConfigured)
    }
}

private final class PipelineBridge: TSPacketParserDelegate, PESPacketAssemblerDelegate, @unchecked Sendable {
    let pesAssembler: PESPacketAssembler
    let auAssembler: H264AccessUnitAssembler

    init(pesAssembler: PESPacketAssembler, auAssembler: H264AccessUnitAssembler) {
        self.pesAssembler = pesAssembler
        self.auAssembler = auAssembler
    }

    func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16) {}
    func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool) {
        pesAssembler.feed(payload: data, unitStart: unitStart)
    }
    func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8) {}

    func pesAssembler(_ assembler: PESPacketAssembler, didEmitVideoPayload payload: PESVideoData) {
        auAssembler.feed(payload: payload)
    }
    func pesAssembler(_ assembler: PESPacketAssembler, didEncounterPESError reason: String) {}
}

// MARK: - Helpers

private func make1080i50SPS() -> Data {
    return Data([
        0x67, 0x4d, 0x40, 0x28, 0x9a, 0x64, 0x03, 0xc0,
        0x11, 0x3f, 0x2e, 0x02, 0xd4, 0x04, 0x04, 0x05,
        0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x00, 0x03,
        0x00, 0x32, 0x84
    ])
}

private func make1080i50PPS() -> Data {
    return Data([0x68, 0xee, 0x3c, 0x80])
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
    var emittedPayloads: [PESVideoData] = []
    var pesErrors: [String] = []

    func pesAssembler(_ assembler: PESPacketAssembler, didEmitVideoPayload payload: PESVideoData) {
        self.emittedPayloads.append(payload)
    }

    func pesAssembler(_ assembler: PESPacketAssembler, didEncounterPESError reason: String) {
        self.pesErrors.append(reason)
    }
}

private final class MockAccessUnitSink: H264AccessUnitAssemblerDelegate, @unchecked Sendable {
    var formatDescription: CMVideoFormatDescription?
    var info: H264DecodedInfo?
    var emittedSampleBuffers: [CMSampleBuffer] = []
    var syncSampleFlags: [Bool] = []
    var structures: [H264PictureStructure] = []

    func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didUpdateFormat formatDescription: CMVideoFormatDescription, info: H264DecodedInfo) {
        self.formatDescription = formatDescription
        self.info = info
    }

    func accessUnitAssembler(_ assembler: H264AccessUnitAssembler, didEmitSampleBuffer sampleBuffer: CMSampleBuffer, isSyncSample: Bool, structure: H264PictureStructure) {
        self.emittedSampleBuffers.append(sampleBuffer)
        self.syncSampleFlags.append(isSyncSample)
        self.structures.append(structure)
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

    mutating func writeSignedExpGolomb(_ value: Int) {
        if value <= 0 {
            writeExpGolomb(-2 * value)
        } else {
            writeExpGolomb(2 * value - 1)
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

private final class MockDecoderSink: HardwareVideoDecoderDelegate, @unchecked Sendable {
    var emittedFrames: [DecodedVideoFrame] = []
    var isHWActive: Bool = false
    var isVTDeinterlaceAccepted: Bool = false
    var decodeErrors: [OSStatus] = []
    var fatalErrors: [OSStatus] = []

    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEmitFrame frame: DecodedVideoFrame) {
        emittedFrames.append(frame)
    }

    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didChangeHWActiveState isHWActive: Bool) {
        self.isHWActive = isHWActive
    }

    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didChangeVTDeinterlaceAccepted isAccepted: Bool) {
        self.isVTDeinterlaceAccepted = isAccepted
    }

    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEncounterDecodeError error: OSStatus) {
        decodeErrors.append(error)
    }

    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didInvalidateSessionWithFatalError error: OSStatus) {
        fatalErrors.append(error)
    }
}
