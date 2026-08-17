// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import Testing
@testable import Xg2g

struct AudioDemuxAndCodecTests {

    // MARK: - 1. PMT Parsing with DVB Descriptors & Multi-Track Audio

    @Test func pmtParserExtractsMultiTrackAudioWithLanguageAndDescriptors() throws {
        let parser = TSPacketParser()
        let sink = MockTSAudioSink()
        parser.delegate = sink

        // 1. Send PAT pointing Program 1 to PMT PID 0x62 (98)
        let pat = makePATPacket(programNumber: 1, pmtPID: 98)
        parser.feed(data: pat)

        // 2. Build PMT for Sky Sport Top Event (PMT PID 98):
        //    - Video: PID 767, StreamType 0x1B (H.264)
        //    - Audio 1: PID 770, StreamType 0x06, Descriptor 0x6A (AC-3), Descriptor 0x0A (lang: "qae")
        //    - Audio 2: PID 772, StreamType 0x06, Descriptor 0x6A (AC-3), Descriptor 0x0A (lang: "qaf")
        //    - Teletext: PID 33, StreamType 0x06, Descriptor 0x56 (teletext) -> NOT audio
        var pmtStreams = Data()

        // Video: 0x1B, PID 767 (0x02FF), ES_info_length = 0
        pmtStreams.append(contentsOf: [0x1B, 0xE2, 0xFF, 0xF0, 0x00])

        // Audio 1: 0x06, PID 770 (0x0302), ES_info: [0x6A, 0x00], [0x0A, 0x04, 'q', 'a', 'e', 0x00] -> len = 8
        pmtStreams.append(contentsOf: [
            0x06, 0xE3, 0x02, 0xF0, 0x08,
            0x6A, 0x00,                         // AC-3 descriptor
            0x0A, 0x04, 0x71, 0x61, 0x65, 0x00  // ISO 639 "qae"
        ])

        // Audio 2: 0x06, PID 772 (0x0304), ES_info: [0x6A, 0x00], [0x0A, 0x04, 'q', 'a', 'f', 0x00] -> len = 8
        pmtStreams.append(contentsOf: [
            0x06, 0xE3, 0x04, 0xF0, 0x08,
            0x6A, 0x00,                         // AC-3 descriptor
            0x0A, 0x04, 0x71, 0x61, 0x66, 0x00  // ISO 639 "qaf"
        ])

        // Teletext: 0x06, PID 33 (0x0021), ES_info: [0x56, 0x03, 0xDE, 0x01, 0x00] -> len = 5
        pmtStreams.append(contentsOf: [
            0x06, 0xE0, 0x21, 0xF0, 0x05,
            0x56, 0x03, 0xDE, 0x01, 0x00       // Teletext descriptor
        ])

        let pmt = makePMTPacket(pmtPID: 98, programNumber: 1, pcrPID: 767, streamData: pmtStreams)
        parser.feed(data: pmt)

        #expect(parser.videoPID == 767)
        #expect(sink.discoveredVideoPID == 767)

        #expect(parser.audioTracks.count == 2)
        #expect(sink.discoveredTracks.count == 2)

        let track1 = parser.audioTracks[0]
        #expect(track1.pid == 770)
        #expect(track1.codec == .ac3)
        #expect(track1.language == "qae")
        #expect(track1.descriptorTags.contains(0x6A))
        #expect(track1.descriptorTags.contains(0x0A))

        let track2 = parser.audioTracks[1]
        #expect(track2.pid == 772)
        #expect(track2.codec == .ac3)
        #expect(track2.language == "qaf")
        #expect(track2.descriptorTags.contains(0x6A))

        #expect(parser.audioPIDs.contains(770))
        #expect(parser.audioPIDs.contains(772))
        #expect(!parser.audioPIDs.contains(33)) // Teletext is not an audio PID
    }

    // MARK: - 2. Audio PES Reassembly Across Multiple TS Packets

    @Test func audioPESReassemblesAcrossMultipleTSPackets() throws {
        let parser = TSPacketParser()
        let assembler = AudioPESAssembler()
        let sink = MockAudioPESSink()
        assembler.delegate = sink

        // Register PID 770 in parser via dummy PMT
        let pat = makePATPacket(programNumber: 1, pmtPID: 98)
        var pmtStreams = Data([
            0x06, 0xE3, 0x02, 0xF0, 0x02, 0x6A, 0x00 // PID 770 AC-3
        ])
        let pmt = makePMTPacket(pmtPID: 98, programNumber: 1, pcrPID: 770, streamData: pmtStreams)
        parser.feed(data: pat)
        parser.feed(data: pmt)

        let router = TSRouter(audioAssembler: assembler)
        parser.delegate = router

        // Construct an audio PES packet:
        // - stream_id: 0xBD (private stream 1 / AC-3)
        // - PTS: 183745820
        // - Payload: 400 bytes of dummy AC-3 data
        let dummyPayload = Data((0..<400).map { UInt8($0 & 0xFF) })
        let rawPES = makeAudioPESPacket(streamID: 0xBD, pts90k: 183745820, payload: dummyPayload)

        // Split rawPES across 3 TS packets on PID 770:
        // TS Packet 1: 150 bytes (unitStart = true)
        // TS Packet 2: 150 bytes (unitStart = false)
        // TS Packet 3: remaining bytes (unitStart = false)
        let chunk1 = rawPES.prefix(150)
        let chunk2 = rawPES.dropFirst(150).prefix(150)
        let chunk3 = rawPES.dropFirst(300)

        let ts1 = makeTSPacket(pid: 770, cc: 0, unitStart: true, payload: chunk1)
        let ts2 = makeTSPacket(pid: 770, cc: 1, unitStart: false, payload: chunk2)
        let ts3 = makeTSPacket(pid: 770, cc: 2, unitStart: false, payload: chunk3)

        parser.feed(data: ts1)
        parser.feed(data: ts2)
        parser.feed(data: ts3)

        // Trigger completion by feeding next unitStart packet (or flush)
        assembler.flush(pid: 770)

        #expect(sink.emittedPackets.count == 1)
        if let emitted = sink.emittedPackets.first {
            #expect(emitted.pid == 770)
            #expect(emitted.streamID == 0xBD)
            #expect(emitted.pts90k == 183745820)
            #expect(emitted.payload == dummyPayload)
            #expect(emitted.payload.count == 400)
        }
    }

    // MARK: - 3. Multi-Track Concurrent Demuxing Without Cross-Talk

    @Test func demuxesMultipleAudioTracksConcurrently() throws {
        let assembler = AudioPESAssembler()
        let sink = MockAudioPESSink()
        assembler.delegate = sink

        // Build PES for Track A (PID 770, stream 0xBD, PTS 1000)
        let payloadA = Data([0x0B, 0x77, 0x01, 0x02, 0x03, 0x04])
        let pesA = makeAudioPESPacket(streamID: 0xBD, pts90k: 1000, payload: payloadA)

        // Build PES for Track B (PID 772, stream 0xBD, PTS 2000)
        let payloadB = Data([0x0B, 0x77, 0xAA, 0xBB, 0xCC, 0xDD])
        let pesB = makeAudioPESPacket(streamID: 0xBD, pts90k: 2000, payload: payloadB)

        // Interleave feeds across PID 770 and PID 772:
        assembler.feed(payload: pesA.prefix(10), pid: 770, unitStart: true)
        assembler.feed(payload: pesB.prefix(10), pid: 772, unitStart: true)
        assembler.feed(payload: pesA.dropFirst(10), pid: 770, unitStart: false)
        assembler.feed(payload: pesB.dropFirst(10), pid: 772, unitStart: false)

        assembler.flush()

        #expect(sink.emittedPackets.count == 2)

        let trackAPacket = sink.emittedPackets.first { $0.pid == 770 }
        let trackBPacket = sink.emittedPackets.first { $0.pid == 772 }

        #expect(trackAPacket != nil)
        #expect(trackAPacket?.pts90k == 1000)
        #expect(trackAPacket?.payload == payloadA)

        #expect(trackBPacket != nil)
        #expect(trackBPacket?.pts90k == 2000)
        #expect(trackBPacket?.payload == payloadB)
    }

    // MARK: - 4. Audio Continuity Counter Jump Detection

    @Test func tracksContinuityErrorsOnAudioPID() throws {
        let parser = TSPacketParser()
        let sink = MockTSAudioSink()
        parser.delegate = sink

        // Register PID 770 as audio
        let pat = makePATPacket(programNumber: 1, pmtPID: 98)
        let pmtStreams = Data([0x06, 0xE3, 0x02, 0xF0, 0x02, 0x6A, 0x00])
        let pmt = makePMTPacket(pmtPID: 98, programNumber: 1, pcrPID: 770, streamData: pmtStreams)
        parser.feed(data: pat)
        parser.feed(data: pmt)

        // Packet 1: CC = 0
        let p1 = makeTSPacket(pid: 770, cc: 0, unitStart: true, payload: Data([0x00, 0x01]))
        // Packet 2: CC = 1 (valid)
        let p2 = makeTSPacket(pid: 770, cc: 1, unitStart: false, payload: Data([0x02, 0x03]))
        // Packet 3: CC = 5 (jump / error!)
        let p3 = makeTSPacket(pid: 770, cc: 5, unitStart: false, payload: Data([0x04, 0x05]))

        parser.feed(data: p1)
        parser.feed(data: p2)
        parser.feed(data: p3)

        #expect(sink.continuityErrors.count == 1)
        if let err = sink.continuityErrors.first {
            #expect(err.pid == 770)
            #expect(err.expected == 2)
            #expect(err.actual == 5)
        }
    }

    // MARK: - 5. 33-Bit PTS Wrapping & Timeline Normalization

    @Test func normalizerHandles33BitPTSWrapWithoutNegativeJumps() throws {
        var normalizer = PTS33BitNormalizer()

        let nearWrapPTS: UInt64 = 8_589_934_500 // 92 ticks before 2^33 (8,589,934,592)
        let wrappedPTS: UInt64  = 100           // 100 ticks after 0 wrap

        let unwrapped1 = normalizer.unwrap(rawPTS: nearWrapPTS)
        let unwrapped2 = normalizer.unwrap(rawPTS: wrappedPTS)

        #expect(unwrapped1 == nearWrapPTS)
        #expect(unwrapped2 == 8_589_934_592 + 100)

        let delta = unwrapped2 - unwrapped1
        #expect(delta == 192) // 92 ticks to boundary + 100 ticks after = 192 ticks

        // Verify CMTime normalization is strictly monotonic
        normalizer.reset()
        let t1 = normalizer.normalize(rawPTS: nearWrapPTS)
        let t2 = normalizer.normalize(rawPTS: wrappedPTS)

        #expect(t1.seconds == 0.0)
        #expect(t2.seconds > 0.0)
        #expect(t2.value == 192)
    }

    // MARK: - 6. Real Stream Audio Track & PES Demuxing

    @Test func demuxesRealBroadcastAudioFromTSCapture() throws {
        let data = PULS24CaptureFixture.data
        #expect(!data.isEmpty, "Embedded PULS24 capture fixture must not be empty")

        let parser = TSPacketParser()
        let assembler = AudioPESAssembler()
        let sink = MockAudioPESSink()
        assembler.delegate = sink

        let bridge = RealStreamBridge(parser: parser, audioAssembler: assembler)
        parser.delegate = bridge

        parser.feed(data: data)
        assembler.flush()

        #expect(parser.videoPID != nil)
        #expect(!parser.audioTracks.isEmpty)

        // PULS 24 HD carries AC-3 audio (PID 1283, deu)
        if let ac3Track = parser.audioTracks.first(where: { $0.codec == .ac3 }) {
            #expect(ac3Track.pid == 1283)
            #expect(ac3Track.language == "deu")
        }

        #expect(!sink.emittedPackets.isEmpty)
        if sink.emittedPackets.count >= 2 {
            let p1 = sink.emittedPackets[0]
            let p2 = sink.emittedPackets[1]
            if let pts1 = p1.pts90k, let pts2 = p2.pts90k {
                #expect(pts2 >= pts1)
            }
        }
    }

    // MARK: - 7. Production AudioPESAssembler 33-Bit PTS Wrap Verification

    @Test func audioPESAssemblerUnwrapsPTSAcrossBoundaryInProductionPath() throws {
        let assembler = AudioPESAssembler()
        let sink = MockAudioPESSink()
        assembler.delegate = sink

        let nearWrapPTS: UInt64 = 8_589_934_500
        let wrappedPTS: UInt64  = 100

        let payload1 = Data([0x0B, 0x77, 0x01, 0x02])
        let pes1 = makeAudioPESPacket(streamID: 0xBD, pts90k: nearWrapPTS, payload: payload1)
        assembler.feed(payload: pes1, pid: 770, unitStart: true)

        let payload2 = Data([0x0B, 0x77, 0x03, 0x04])
        let pes2 = makeAudioPESPacket(streamID: 0xBD, pts90k: wrappedPTS, payload: payload2)
        assembler.feed(payload: pes2, pid: 770, unitStart: true)

        assembler.flush(pid: 770)

        #expect(sink.emittedPackets.count == 2)
        if sink.emittedPackets.count == 2 {
            let p1 = sink.emittedPackets[0]
            let p2 = sink.emittedPackets[1]

            #expect(p1.pts90k == nearWrapPTS)
            #expect(p2.pts90k == 8_589_934_592 + 100) // Unwrapped across 33-bit boundary
            #expect(p2.pts90k! > p1.pts90k!)
            #expect(p2.pts!.seconds > p1.pts!.seconds)
        }
    }
}

// MARK: - Test Helpers & Mocks

private func makeTSPacket(pid: UInt16, cc: UInt8, unitStart: Bool, payload: Data) -> Data {
    var packet = Data(repeating: 0xFF, count: 188)
    packet[0] = 0x47
    let byte1 = (unitStart ? 0x40 : 0x00) | UInt8((pid >> 8) & 0x1F)
    let byte2 = UInt8(pid & 0xFF)
    packet[1] = byte1
    packet[2] = byte2

    let payloadLen = payload.count
    if payloadLen >= 184 {
        packet[3] = 0x10 | (cc & 0x0F) // payload only
        packet.replaceSubrange(4..<188, with: payload.prefix(184))
    } else {
        let stuffingLen = 184 - payloadLen
        packet[3] = 0x30 | (cc & 0x0F) // adaptation field + payload
        packet[4] = UInt8(stuffingLen - 1) // adaptation_field_length
        if stuffingLen > 1 {
            packet[5] = 0x00 // flags
            for i in 6..<(4 + stuffingLen) {
                packet[i] = 0xFF // stuffing
            }
        }
        let payloadStart = 4 + stuffingLen
        packet.replaceSubrange(payloadStart..<188, with: payload)
    }
    return packet
}

private func makePATPacket(programNumber: UInt16, pmtPID: UInt16) -> Data {
    var pat = Data(repeating: 0xFF, count: 188)
    pat[0] = 0x47
    pat[1] = 0x40 // unitStart, PID 0
    pat[2] = 0x00
    pat[3] = 0x10 // CC = 0
    pat[4] = 0x00 // pointer field
    pat[5] = 0x00 // table_id = 0 (PAT)
    pat[6] = 0xB0 // section_syntax_indicator & length
    pat[7] = 0x0D // length = 13
    pat[8] = 0x00
    pat[9] = 0x01
    pat[10] = 0xC1
    pat[11] = 0x00
    pat[12] = 0x00
    pat[13] = UInt8((programNumber >> 8) & 0xFF)
    pat[14] = UInt8(programNumber & 0xFF)
    pat[15] = 0xE0 | UInt8((pmtPID >> 8) & 0x1F)
    pat[16] = UInt8(pmtPID & 0xFF)
    // 4 dummy CRC bytes
    pat[17] = 0x00
    pat[18] = 0x00
    pat[19] = 0x00
    pat[20] = 0x00
    return pat
}

private func makePMTPacket(pmtPID: UInt16, programNumber: UInt16, pcrPID: UInt16, streamData: Data) -> Data {
    var pmt = Data(repeating: 0xFF, count: 188)
    pmt[0] = 0x47
    pmt[1] = 0x40 | UInt8((pmtPID >> 8) & 0x1F)
    pmt[2] = UInt8(pmtPID & 0xFF)
    pmt[3] = 0x10 // CC = 0
    pmt[4] = 0x00 // pointer field
    pmt[5] = 0x02 // table_id = 2 (PMT)

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
    pmt[15] = 0xF0 // program_info_length = 0
    pmt[16] = 0x00

    let streamOffset = 17
    pmt.replaceSubrange(streamOffset..<(streamOffset + streamData.count), with: streamData)
    return pmt
}

private func makeAudioPESPacket(streamID: UInt8, pts90k: UInt64?, payload: Data) -> Data {
    var pes = Data()
    pes.append(contentsOf: [0x00, 0x00, 0x01, streamID])

    var headerData = Data()
    var flags2: UInt8 = 0x00

    if let pts = pts90k {
        flags2 = 0x80 // PTS present
        let b0 = UInt8(0x21 | (((pts >> 30) & 0x07) << 1))
        let b1 = UInt8((pts >> 22) & 0xFF)
        let b2 = UInt8(0x01 | (((pts >> 15) & 0x7F) << 1))
        let b3 = UInt8((pts >> 7) & 0xFF)
        let b4 = UInt8(0x01 | ((pts & 0x7F) << 1))
        headerData.append(contentsOf: [b0, b1, b2, b3, b4])
    }

    let pesLength = 3 + headerData.count + payload.count
    let lenHigh = UInt8((pesLength >> 8) & 0xFF)
    let lenLow = UInt8(pesLength & 0xFF)
    pes.append(contentsOf: [lenHigh, lenLow])

    pes.append(contentsOf: [0x80, flags2, UInt8(headerData.count)])
    pes.append(headerData)
    pes.append(payload)
    return pes
}

private final class MockTSAudioSink: TSPacketParserDelegate, @unchecked Sendable {
    var discoveredVideoPID: UInt16?
    var discoveredTracks: [AudioTrackInfo] = []
    var continuityErrors: [(pid: UInt16, expected: UInt8, actual: UInt8)] = []

    func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16) {
        self.discoveredVideoPID = pid
    }

    func tsParser(_ parser: TSPacketParser, didDiscoverAudioTracks tracks: [AudioTrackInfo]) {
        self.discoveredTracks = tracks
    }

    func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool) {}

    func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8) {
        self.continuityErrors.append((pid: pid, expected: expected, actual: actual))
    }
}

private final class MockAudioPESSink: AudioPESAssemblerDelegate, @unchecked Sendable {
    var emittedPackets: [AudioPESData] = []
    var errors: [(reason: String, pid: UInt16)] = []

    func audioPESAssembler(_ assembler: AudioPESAssembler, didEmitAudioPES payload: AudioPESData) {
        emittedPackets.append(payload)
    }

    func audioPESAssembler(_ assembler: AudioPESAssembler, didEncounterPESError reason: String, onPID pid: UInt16) {
        errors.append((reason: reason, pid: pid))
    }
}

private final class TSRouter: TSPacketParserDelegate, @unchecked Sendable {
    let audioAssembler: AudioPESAssembler

    init(audioAssembler: AudioPESAssembler) {
        self.audioAssembler = audioAssembler
    }

    func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16) {}
    func tsParser(_ parser: TSPacketParser, didDiscoverAudioTracks tracks: [AudioTrackInfo]) {}
    func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool) {
        audioAssembler.feed(payload: data, pid: pid, unitStart: unitStart)
    }
    func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8) {}
}

private final class RealStreamBridge: TSPacketParserDelegate, @unchecked Sendable {
    let parser: TSPacketParser
    let audioAssembler: AudioPESAssembler
    var selectedAudioPID: UInt16?

    init(parser: TSPacketParser, audioAssembler: AudioPESAssembler) {
        self.parser = parser
        self.audioAssembler = audioAssembler
    }

    func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16) {}

    func tsParser(_ parser: TSPacketParser, didDiscoverAudioTracks tracks: [AudioTrackInfo]) {
        if selectedAudioPID == nil, let first = tracks.first {
            selectedAudioPID = first.pid
        }
    }

    func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool) {
        if let audioPID = selectedAudioPID, pid == audioPID {
            audioAssembler.feed(payload: data, pid: pid, unitStart: unitStart)
        }
    }

    func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8) {}
}
