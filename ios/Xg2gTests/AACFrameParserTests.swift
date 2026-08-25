// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AudioToolbox
import CoreMedia
import Foundation
import Testing
@testable import Xg2g

struct AACFrameParserTests {

    // MARK: - 1. Standard ADTS AAC Frame Parsing

    @Test func parsesStandardAACStereoFrame() throws {
        let parser = AACADTSFrameParser()
        let sink = MockAACFrameSink()
        parser.delegate = sink

        // Construct 48 kHz Stereo AAC-LC frame (300 bytes)
        let frame = makeADTSFrame(profile: 1, sampleRateIdx: 3, channelConfig: 2, frameLength: 300)
        let pts = CMTime(value: 90000, timescale: 90000)

        parser.feed(data: frame, pts: pts, pts90k: 90000)

        #expect(sink.emittedFrames.count == 1)
        if let emitted = sink.emittedFrames.first {
            #expect(emitted.info.sampleRate == 48000)
            #expect(emitted.info.channelCount == 2)
            #expect(emitted.info.samplesPerFrame == 1024)
            #expect(emitted.info.frameSizeBytes == 300)
            #expect(emitted.info.headerSizeBytes == 7)
            #expect(emitted.rawPayload.count == 293)
            #expect(emitted.pts?.seconds == 1.0)
            #expect(emitted.pts90k == 90000)
        }
    }

    @Test func parsesStandardAAC51SurroundFrame() throws {
        let parser = AACADTSFrameParser()
        let sink = MockAACFrameSink()
        parser.delegate = sink

        // Construct 44.1 kHz 5.1 Surround AAC-LC frame (500 bytes)
        let frame = makeADTSFrame(profile: 1, sampleRateIdx: 4, channelConfig: 6, frameLength: 500)
        let pts = CMTime(value: 180000, timescale: 90000)

        parser.feed(data: frame, pts: pts, pts90k: 180000)

        #expect(sink.emittedFrames.count == 1)
        if let emitted = sink.emittedFrames.first {
            #expect(emitted.info.sampleRate == 44100)
            #expect(emitted.info.channelCount == 6)
            #expect(emitted.info.samplesPerFrame == 1024)
            #expect(emitted.info.frameSizeBytes == 500)
            #expect(emitted.rawPayload.count == 493)
        }
    }

    // MARK: - 2. Continuous Chunk Slicing Across Boundaries

    @Test func slicesAACFramesAcrossArbitraryPESChunkBoundaries() throws {
        let parser = AACADTSFrameParser()
        let sink = MockAACFrameSink()
        parser.delegate = sink

        let frame1 = makeADTSFrame(profile: 1, sampleRateIdx: 3, channelConfig: 2, frameLength: 200)
        let frame2 = makeADTSFrame(profile: 1, sampleRateIdx: 3, channelConfig: 2, frameLength: 250)
        var stream = Data()
        stream.append(frame1)
        stream.append(frame2)

        // Chop stream into 37-byte arbitrary chunks
        let chunkSize = 37
        var offset = 0
        let startPTS = CMTime(value: 90000, timescale: 90000)
        var isFirst = true

        while offset < stream.count {
            let end = min(offset + chunkSize, stream.count)
            let chunk = stream.subdata(in: offset..<end)
            if isFirst {
                parser.feed(data: chunk, pts: startPTS, pts90k: 90000)
                isFirst = false
            } else {
                parser.feed(data: chunk)
            }
            offset = end
        }

        #expect(sink.emittedFrames.count == 2)
        if sink.emittedFrames.count == 2 {
            let f1 = sink.emittedFrames[0]
            let f2 = sink.emittedFrames[1]

            #expect(f1.info.frameSizeBytes == 200)
            #expect(f2.info.frameSizeBytes == 250)

            #expect(f1.pts?.seconds == 1.0)
            // Duration of 1024 samples @ 48 kHz = ~0.02133s (~1920 90kHz ticks)
            #expect(f2.pts90k == 90000 + 1920)
        }
    }

    // MARK: - 3. CoreMedia CMSampleBuffer Creation for AAC

    @Test func aacSampleBufferAssemblerEmitsValidCMSampleBuffers() throws {
        let parser = AACADTSFrameParser()
        let sbAssembler = AudioSampleBufferAssembler()
        let sink = MockAACSampleBufferSink()

        parser.delegate = sbAssembler
        sbAssembler.delegate = sink

        let frame = makeADTSFrame(profile: 1, sampleRateIdx: 3, channelConfig: 2, frameLength: 300)
        let pts = CMTime(value: 90000, timescale: 90000)

        parser.feed(data: frame, pts: pts, pts90k: 90000)

        #expect(sink.emittedSampleBuffers.count == 1)
        #expect(sink.formatDescription != nil)

        if let sb = sink.emittedSampleBuffers.first {
            let format = CMSampleBufferGetFormatDescription(sb)
            #expect(format != nil)
            if let f = format {
                let asbd = CMAudioFormatDescriptionGetStreamBasicDescription(f)?.pointee
                #expect(asbd?.mFormatID == kAudioFormatMPEG4AAC)
                #expect(asbd?.mSampleRate == 48000)
                #expect(asbd?.mChannelsPerFrame == 2)
                #expect(asbd?.mFramesPerPacket == 1024)
            }
        }
    }
}

// MARK: - Test Helpers

private func makeADTSFrame(profile: UInt8, sampleRateIdx: UInt8, channelConfig: UInt8, frameLength: Int) -> Data {
    var frame = Data(repeating: 0x55, count: frameLength)

    // Byte 0: 0xFF
    frame[0] = 0xFF
    // Byte 1: 0xF1 (Layer=00, ID=1 MPEG-2 or ID=0 MPEG-4, protection_absent=1)
    frame[1] = 0xF1
    // Byte 2: profile (2 bits) | sampleRateIdx (4 bits) | private (1 bit) | channelConfig high (1 bit)
    frame[2] = ((profile & 0x03) << 6) | ((sampleRateIdx & 0x0F) << 2) | ((channelConfig >> 2) & 0x01)
    // Byte 3: channelConfig low (2 bits) | orig/home/copy | frameLength high (2 bits)
    frame[3] = ((channelConfig & 0x03) << 6) | UInt8((frameLength >> 11) & 0x03)
    // Byte 4: frameLength mid (8 bits)
    frame[4] = UInt8((frameLength >> 3) & 0xFF)
    // Byte 5: frameLength low (3 bits) | buffer fullness high (5 bits)
    frame[5] = UInt8((frameLength & 0x07) << 5) | 0x1F
    // Byte 6: buffer fullness low (6 bits) | num_raw_data_blocks (2 bits = 0 -> 1 block)
    frame[6] = 0xFC

    return frame
}

private final class MockAACFrameSink: AACFrameParserDelegate, @unchecked Sendable {
    var emittedFrames: [ParsedAACFrame] = []
    var errors: [String] = []

    func aacFrameParser(_ parser: AACADTSFrameParser, didEmitFrame frame: ParsedAACFrame) {
        emittedFrames.append(frame)
    }

    func aacFrameParser(_ parser: AACADTSFrameParser, didEncounterError reason: String) {
        errors.append(reason)
    }
}

private final class MockAACSampleBufferSink: AudioSampleBufferAssemblerDelegate, @unchecked Sendable {
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
