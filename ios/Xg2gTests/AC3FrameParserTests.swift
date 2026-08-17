// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AudioToolbox
import CoreMedia
import Foundation
import Testing
@testable import Xg2g

struct AC3FrameParserTests {

    // MARK: - 1. Standard AC-3 Stereo Parsing (48 kHz / 192 kbps)

    @Test func parsesStandardAC3StereoFrame() throws {
        let parser = AC3FrameParser()
        let sink = MockAC3FrameSink()
        parser.delegate = sink

        // Build synthetic 48 kHz / 192 kbps AC-3 syncframe (768 bytes, acmod = 2 stereo)
        let frameData = makeAC3Frame(
            fscod: 0,        // 48 kHz
            frmsizecod: 20,  // 192 kbps -> 768 bytes
            bsid: 8,         // standard AC-3
            acmod: 2,        // 2/0 stereo
            lfeon: false
        )

        let initialPTS = CMTime(value: 90000, timescale: 90000) // 1.0s
        parser.feed(data: frameData, pts: initialPTS, pts90k: 90000)

        #expect(sink.emittedFrames.count == 1)
        if let frame = sink.emittedFrames.first {
            #expect(frame.info.isEnhanced == false)
            #expect(frame.info.sampleRate == 48000)
            #expect(frame.info.channelCount == 2)
            #expect(frame.info.isLFEOn == false)
            #expect(frame.info.bitrateKbps == 192)
            #expect(frame.info.frameSizeBytes == 768)
            #expect(frame.info.samplesPerFrame == 1536)
            #expect(frame.info.duration.seconds == 0.032)
            #expect(frame.pts?.seconds == 1.0)
            #expect(frame.data.count == 768)
        }
    }

    // MARK: - 2. Standard AC-3 5.1 Surround Parsing (48 kHz / 448 kbps)

    @Test func parsesStandardAC3Surround51Frame() throws {
        let parser = AC3FrameParser()
        let sink = MockAC3FrameSink()
        parser.delegate = sink

        // Build synthetic 48 kHz / 448 kbps AC-3 5.1 syncframe (1792 bytes, acmod = 7, lfeon = true)
        let frameData = makeAC3Frame(
            fscod: 0,        // 48 kHz
            frmsizecod: 30,  // 448 kbps -> 1792 bytes
            bsid: 8,         // standard AC-3
            acmod: 7,        // 3/2 = 5 main channels
            lfeon: true      // + 1 LFE channel = 6 channels
        )

        parser.feed(data: frameData)

        #expect(sink.emittedFrames.count == 1)
        if let frame = sink.emittedFrames.first {
            #expect(frame.info.sampleRate == 48000)
            #expect(frame.info.channelCount == 6)
            #expect(frame.info.isLFEOn == true)
            #expect(frame.info.bitrateKbps == 448)
            #expect(frame.info.frameSizeBytes == 1792)
            #expect(frame.info.samplesPerFrame == 1536)
        }
    }

    // MARK: - 3. E-AC-3 Dynamic Duration & Block Structure Parsing

    @Test func parsesEAC3FrameWithDynamicDuration() throws {
        let parser = AC3FrameParser()
        let sink = MockAC3FrameSink()
        parser.delegate = sink

        // Build synthetic E-AC-3 frame with:
        // - frmsiz = 383 -> (383 + 1) * 2 = 768 bytes
        // - fscod = 0 (48 kHz)
        // - numblkscod = 1 (2 blocks = 512 samples)
        // - acmod = 2 (stereo)
        let eac3Frame = makeEAC3Frame(
            frmsizWords: 383,
            fscod: 0,
            numblkscod: 1, // 2 blocks = 512 samples
            acmod: 2,
            lfeon: false
        )

        parser.feed(data: eac3Frame)

        #expect(sink.emittedFrames.count == 1)
        if let frame = sink.emittedFrames.first {
            #expect(frame.info.isEnhanced == true)
            #expect(frame.info.sampleRate == 48000)
            #expect(frame.info.channelCount == 2)
            #expect(frame.info.frameSizeBytes == 768)
            #expect(frame.info.samplesPerFrame == 512) // Dynamically calculated from numblkscod!
            #expect(frame.info.duration.seconds == Double(512) / 48000.0)
        }
    }

    // MARK: - 4. Frame Slicing Across Arbitrary PES Chunk Boundaries

    @Test func slicesFramesAcrossArbitraryPESChunkBoundaries() throws {
        let parser = AC3FrameParser()
        let sink = MockAC3FrameSink()
        parser.delegate = sink

        // Create 3 consecutive 192 kbps AC-3 frames (768 bytes each -> total 2304 bytes)
        let frame1 = makeAC3Frame(fscod: 0, frmsizecod: 20, bsid: 8, acmod: 2, lfeon: false)
        let frame2 = makeAC3Frame(fscod: 0, frmsizecod: 20, bsid: 8, acmod: 2, lfeon: false)
        let frame3 = makeAC3Frame(fscod: 0, frmsizecod: 20, bsid: 8, acmod: 2, lfeon: false)

        var stream = Data()
        stream.append(frame1)
        stream.append(frame2)
        stream.append(frame3)

        // Split across arbitrary PES chunk sizes (500, 1000, 804 bytes)
        let chunk1 = stream.prefix(500)
        let chunk2 = stream.dropFirst(500).prefix(1000)
        let chunk3 = stream.dropFirst(1500)

        let initialPTS = CMTime(value: 90000, timescale: 90000) // 1.0s
        parser.feed(data: chunk1, pts: initialPTS, pts90k: 90000)
        #expect(sink.emittedFrames.count == 0) // Incomplete frame

        parser.feed(data: chunk2)
        #expect(sink.emittedFrames.count == 1) // Frame 1 completed, Frame 2 partially buffered

        parser.feed(data: chunk3)
        #expect(sink.emittedFrames.count == 3) // All 3 frames completed

        // Verify accurate PTS timestamp progression (32 ms per frame)
        if sink.emittedFrames.count == 3 {
            let f1 = sink.emittedFrames[0]
            let f2 = sink.emittedFrames[1]
            let f3 = sink.emittedFrames[2]

            #expect(f1.pts?.seconds == 1.0)
            #expect(abs((f2.pts?.seconds ?? 0) - 1.032) < 0.0001)
            #expect(abs((f3.pts?.seconds ?? 0) - 1.064) < 0.0001)

            #expect(f1.data.count == 768)
            #expect(f2.data.count == 768)
            #expect(f3.data.count == 768)
        }
    }

    // MARK: - 5. Resynchronization on Garbage Data

    @Test func resynchronizesAfterGarbageBytes() throws {
        let parser = AC3FrameParser()
        let sink = MockAC3FrameSink()
        parser.delegate = sink

        let validFrame = makeAC3Frame(fscod: 0, frmsizecod: 20, bsid: 8, acmod: 2, lfeon: false)
        let garbage = Data([0xFF, 0xEE, 0xDD, 0x0B, 0x00, 0xAA, 0xBB]) // 0x0B followed by non-0x77

        var stream = Data()
        stream.append(garbage)
        stream.append(validFrame)

        parser.feed(data: stream)

        #expect(sink.emittedFrames.count == 1)
        #expect(sink.emittedFrames.first?.data == validFrame)
    }

    // MARK: - 6. End-to-End CMSampleBuffer Assembler Validation

    @Test func audioSampleBufferAssemblerEmitsValidCMSampleBuffers() throws {
        let frameParser = AC3FrameParser()
        let sbAssembler = AudioSampleBufferAssembler()
        let sink = MockSampleBufferSink()

        frameParser.delegate = sbAssembler
        sbAssembler.delegate = sink

        let frame1 = makeAC3Frame(fscod: 0, frmsizecod: 20, bsid: 8, acmod: 2, lfeon: false)
        let frame2 = makeAC3Frame(fscod: 0, frmsizecod: 20, bsid: 8, acmod: 2, lfeon: false)

        let initialPTS = CMTime(value: 90000, timescale: 90000)
        frameParser.feed(data: frame1, pts: initialPTS, pts90k: 90000)
        frameParser.feed(data: frame2)

        #expect(sink.emittedSampleBuffers.count == 2)
        #expect(sink.formatDescription != nil)

        if let sb1 = sink.emittedSampleBuffers.first {
            let pts = CMSampleBufferGetPresentationTimeStamp(sb1)
            let dur = CMSampleBufferGetDuration(sb1)
            let format = CMSampleBufferGetFormatDescription(sb1)

            #expect(pts.seconds == 1.0)
            #expect(abs(dur.seconds - 0.032) < 0.0001)
            #expect(format != nil)

            if let asbd = format.flatMap({ CMAudioFormatDescriptionGetStreamBasicDescription($0)?.pointee }) {
                #expect(asbd.mFormatID == kAudioFormatAC3)
                #expect(asbd.mSampleRate == 48000)
                #expect(asbd.mChannelsPerFrame == 2)
                #expect(asbd.mFramesPerPacket == 1536)
            }
        }
    }

    // MARK: - 7. Real DVB TS Stream End-to-End Audio Ingest

    @Test func parsesRealTSAudioIntoCMSampleBuffers() throws {
        let data = PULS24CaptureFixture.data
        #expect(!data.isEmpty, "Embedded PULS24 capture fixture must not be empty")

        let tsParser = TSPacketParser()
        let pesAssembler = AudioPESAssembler()
        let frameParser = AC3FrameParser()
        let sbAssembler = AudioSampleBufferAssembler()
        let sink = MockSampleBufferSink()

        let bridge = FullAudioPipelineBridge(
            tsParser: tsParser,
            pesAssembler: pesAssembler,
            frameParser: frameParser
        )

        tsParser.delegate = bridge
        pesAssembler.delegate = bridge
        frameParser.delegate = sbAssembler
        sbAssembler.delegate = sink

        tsParser.feed(data: data)
        pesAssembler.flush()

        #expect(sink.emittedSampleBuffers.count > 0)
        #expect(sink.formatDescription != nil)

        if let firstSB = sink.emittedSampleBuffers.first {
            let format = CMSampleBufferGetFormatDescription(firstSB)
            let asbd = format.flatMap { CMAudioFormatDescriptionGetStreamBasicDescription($0)?.pointee }

            #expect(asbd?.mFormatID == kAudioFormatAC3)
            #expect(asbd?.mSampleRate == 48000)
            #expect(asbd?.mChannelsPerFrame == 2 || asbd?.mChannelsPerFrame == 6)
        }

        // Verify monotonically increasing PTS
        if sink.emittedSampleBuffers.count >= 2 {
            let sb1 = sink.emittedSampleBuffers[0]
            let sb2 = sink.emittedSampleBuffers[1]
            let pts1 = CMSampleBufferGetPresentationTimeStamp(sb1)
            let pts2 = CMSampleBufferGetPresentationTimeStamp(sb2)
            #expect(pts2 > pts1)
        }
    }
}

// MARK: - Test Helpers & Generators

private func makeAC3Frame(fscod: Int, frmsizecod: Int, bsid: Int, acmod: Int, lfeon: Bool) -> Data {
    let sizeTable = [
        // 48 kHz
        [
            128, 128, 160, 160, 192, 192, 224, 224, 256, 256,
            320, 320, 384, 384, 448, 448, 512, 512, 640, 640,
            768, 768, 896, 896, 1024, 1024, 1280, 1280, 1536, 1536,
            1792, 1792, 2048, 2048, 2304, 2304, 2560, 2560
        ]
    ]

    let frameSize = sizeTable[0][frmsizecod]
    var frame = Data(repeating: 0x00, count: frameSize)

    // Byte 0, 1: Syncword 0x0B77
    frame[0] = 0x0B
    frame[1] = 0x77

    // Byte 2, 3: CRC1 (dummy)
    frame[2] = 0x12
    frame[3] = 0x34

    // Byte 4: fscod (2 bits) | frmsizecod (6 bits)
    frame[4] = UInt8(((fscod & 0x03) << 6) | (frmsizecod & 0x3F))

    // Byte 5: bsid (5 bits) | bsmod (3 bits)
    frame[5] = UInt8(((bsid & 0x1F) << 3) | 0x00)

    // Byte 6: acmod (3 bits) | cmixlev/surmixlev/dsurmod/lfeon
    var b6: UInt8 = UInt8((acmod & 0x07) << 5)
    if acmod == 7 && lfeon {
        // For acmod = 7 (3/2): bit 4-3 cmixlev, bit 2-1 surmixlev, bit 0 lfeon
        b6 |= 0x01 // LFE on
    } else if acmod == 2 && lfeon {
        b6 |= 0x01
    }
    frame[6] = b6

    return frame
}

private func makeEAC3Frame(frmsizWords: Int, fscod: Int, numblkscod: Int, acmod: Int, lfeon: Bool) -> Data {
    let frameSize = (frmsizWords + 1) * 2
    var frame = Data(repeating: 0x00, count: frameSize)

    frame[0] = 0x0B
    frame[1] = 0x77

    // Byte 2: strmtyp (2 bits) | substreamid (3 bits) | frmsiz high (3 bits)
    let frmsizHigh = (frmsizWords >> 8) & 0x07
    frame[2] = UInt8(frmsizHigh)

    // Byte 3: frmsiz low (8 bits)
    frame[3] = UInt8(frmsizWords & 0xFF)

    // Byte 4: fscod (2 bits) | numblkscod (2 bits) | acmod (3 bits) | lfeon (1 bit)
    let b4 = ((fscod & 0x03) << 6) | ((numblkscod & 0x03) << 4) | ((acmod & 0x07) << 1) | (lfeon ? 1 : 0)
    frame[4] = UInt8(b4)

    // Byte 5: bsid = 16 (0x10 << 3 = 0x80)
    frame[5] = 0x80

    return frame
}

private final class MockAC3FrameSink: AC3FrameParserDelegate, @unchecked Sendable {
    var emittedFrames: [ParsedAudioFrame] = []
    var errors: [String] = []

    func ac3FrameParser(_ parser: AC3FrameParser, didEmitFrame frame: ParsedAudioFrame) {
        emittedFrames.append(frame)
    }

    func ac3FrameParser(_ parser: AC3FrameParser, didEncounterError reason: String) {
        errors.append(reason)
    }
}

private final class MockSampleBufferSink: AudioSampleBufferAssemblerDelegate, @unchecked Sendable {
    var formatDescription: CMAudioFormatDescription?
    var emittedSampleBuffers: [CMSampleBuffer] = []
    var errors: [String] = []

    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didUpdateFormat formatDescription: CMAudioFormatDescription, codec: AudioStreamCodec, sampleRate: Int, channels: Int, bitrateKbps: Int) {
        self.formatDescription = formatDescription
    }

    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didEmitSampleBuffer sampleBuffer: CMSampleBuffer, codec: AudioStreamCodec, duration: CMTime) {
        emittedSampleBuffers.append(sampleBuffer)
    }

    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didEncounterError reason: String) {
        errors.append(reason)
    }
}

private final class FullAudioPipelineBridge: TSPacketParserDelegate, AudioPESAssemblerDelegate, @unchecked Sendable {
    let tsParser: TSPacketParser
    let pesAssembler: AudioPESAssembler
    let frameParser: AC3FrameParser
    var selectedAudioPID: UInt16?

    init(tsParser: TSPacketParser, pesAssembler: AudioPESAssembler, frameParser: AC3FrameParser) {
        self.tsParser = tsParser
        self.pesAssembler = pesAssembler
        self.frameParser = frameParser
    }

    func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16) {}

    func tsParser(_ parser: TSPacketParser, didDiscoverAudioTracks tracks: [AudioTrackInfo]) {
        if selectedAudioPID == nil, let ac3Track = tracks.first(where: { $0.codec == .ac3 || $0.codec == .eac3 }) {
            selectedAudioPID = ac3Track.pid
        }
    }

    func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool) {
        if let audioPID = selectedAudioPID, pid == audioPID {
            pesAssembler.feed(payload: data, pid: pid, unitStart: unitStart)
        }
    }

    func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8) {}

    func audioPESAssembler(_ assembler: AudioPESAssembler, didEmitAudioPES payload: AudioPESData) {
        frameParser.feed(data: payload.payload, pts: payload.pts, pts90k: payload.pts90k)
    }

    func audioPESAssembler(_ assembler: AudioPESAssembler, didEncounterPESError reason: String, onPID pid: UInt16) {}
}
