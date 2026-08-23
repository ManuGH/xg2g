// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing
@testable import Xg2g

struct SubtitleDiscoveryTests {

    @Test("SubtitleTrackInfo: Formatting display names for DVB and Teletext")
    func subtitleTrackInfoDisplayNameFormatting() {
        // DVB Subtitles
        let dvbNormal = SubtitleTrackInfo(
            pid: 301,
            format: .dvb(compositionPageID: 1, ancillaryPageID: 1),
            language: "deu",
            isHearingImpaired: false
        )
        #expect(dvbNormal.displayName == "Deutsch (DVB)")

        let dvbHI = SubtitleTrackInfo(
            pid: 301,
            format: .dvb(compositionPageID: 2, ancillaryPageID: 2),
            language: "deu",
            isHearingImpaired: true
        )
        #expect(dvbHI.displayName == "Deutsch (DVB, Hörgeschädigte)")

        let dvbEng = SubtitleTrackInfo(
            pid: 302,
            format: .dvb(compositionPageID: 1, ancillaryPageID: 1),
            language: "eng",
            isHearingImpaired: false
        )
        #expect(dvbEng.displayName == "Englisch (DVB)")

        // Teletext Subtitles
        let ttx777 = SubtitleTrackInfo(
            pid: 303,
            format: .teletext(page: 777),
            language: "deu",
            isHearingImpaired: true
        )
        #expect(ttx777.displayName == "Deutsch (Teletext 777, Hörgeschädigte)")

        let ttx150 = SubtitleTrackInfo(
            pid: 303,
            format: .teletext(page: 150),
            language: "deu",
            isHearingImpaired: false
        )
        #expect(ttx150.displayName == "Deutsch (Teletext 150)")
    }

    @Test("PMT Parser: Discovers DVB (0x59) and Teletext (0x56) Subtitle Descriptors")
    func pmtParserDiscoversSubtitles() {
        let parser = TSPacketParser()
        let sink = MockSubtitleSink()
        parser.delegate = sink

        // 1. Send PAT
        parser.feed(data: makePAT(programNumber: 1, pmtPID: 100))

        // 2. Build PMT with Video (H.264 PID 256), Audio (AC-3 PID 257), DVB Subtitles (PID 258) and Teletext (PID 259)
        var streams = Data()

        // Video: Stream Type 0x1B, PID 256
        streams.append(contentsOf: [0x1B, 0xE1, 0x00, 0xF0, 0x00])

        // Audio: Stream Type 0x06, PID 257, AC-3 Descriptor (0x6A), Language (0x0A: deu)
        streams.append(contentsOf: [
            0x06, 0xE1, 0x01, 0xF0, 0x09,
            0x6A, 0x01, 0x00,
            0x0A, 0x04, UInt8(ascii: "d"), UInt8(ascii: "e"), UInt8(ascii: "u"), 0x00
        ])

        // DVB Subtitles: Stream Type 0x06, PID 258, DVB Subtitling Descriptor (0x59)
        // Length 16: 2 entries of 8 bytes each
        // Entry 1: deu, type 0x20 (Hearing Impaired), compPage 1, ancPage 1
        // Entry 2: eng, type 0x10 (Normal), compPage 2, ancPage 2
        streams.append(contentsOf: [
            0x06, 0xE1, 0x02, 0xF0, 0x12,
            0x59, 0x10,
            UInt8(ascii: "d"), UInt8(ascii: "e"), UInt8(ascii: "u"), 0x20, 0x00, 0x01, 0x00, 0x01,
            UInt8(ascii: "e"), UInt8(ascii: "n"), UInt8(ascii: "g"), 0x10, 0x00, 0x02, 0x00, 0x02
        ])

        // Teletext: Stream Type 0x06, PID 259, Teletext Descriptor (0x56)
        // Length 15: 3 entries of 5 bytes each
        // Entry 1: deu, type 0x05 (Hearing Impaired Subtitle), mag 7, page 0x77 (777)
        // Entry 2: deu, type 0x02 (Normal Subtitle), mag 1, page 0x50 (150)
        // Entry 3: deu, type 0x01 (Initial page -> must be ignored)
        streams.append(contentsOf: [
            0x06, 0xE1, 0x03, 0xF0, 0x11,
            0x56, 0x0F,
            UInt8(ascii: "d"), UInt8(ascii: "e"), UInt8(ascii: "u"), (0x05 << 3) | 0x07, 0x77,
            UInt8(ascii: "d"), UInt8(ascii: "e"), UInt8(ascii: "u"), (0x02 << 3) | 0x01, 0x50,
            UInt8(ascii: "d"), UInt8(ascii: "e"), UInt8(ascii: "u"), (0x01 << 3) | 0x01, 0x00
        ])

        let pmt = makePMT(pmtPID: 100, programNumber: 1, pcrPID: 256, streamData: streams)
        parser.feed(data: pmt)

        #expect(parser.subtitleTracks.count == 4)
        #expect(sink.discoveredSubtitleTracks.count == 4)

        // Verify DVB tracks
        let dvb1 = parser.subtitleTracks[0]
        #expect(dvb1.pid == 258)
        #expect(dvb1.format == .dvb(compositionPageID: 1, ancillaryPageID: 1))
        #expect(dvb1.language == "deu")
        #expect(dvb1.isHearingImpaired == true)
        #expect(dvb1.displayName == "Deutsch (DVB, Hörgeschädigte)")

        let dvb2 = parser.subtitleTracks[1]
        #expect(dvb2.pid == 258)
        #expect(dvb2.format == .dvb(compositionPageID: 2, ancillaryPageID: 2))
        #expect(dvb2.language == "eng")
        #expect(dvb2.isHearingImpaired == false)
        #expect(dvb2.displayName == "Englisch (DVB)")

        // Verify Teletext tracks
        let ttx1 = parser.subtitleTracks[2]
        #expect(ttx1.pid == 259)
        #expect(ttx1.format == .teletext(page: 777))
        #expect(ttx1.language == "deu")
        #expect(ttx1.isHearingImpaired == true)
        #expect(ttx1.displayName == "Deutsch (Teletext 777, Hörgeschädigte)")

        let ttx2 = parser.subtitleTracks[3]
        #expect(ttx2.pid == 259)
        #expect(ttx2.format == .teletext(page: 150))
        #expect(ttx2.language == "deu")
        #expect(ttx2.isHearingImpaired == false)
        #expect(ttx2.displayName == "Deutsch (Teletext 150)")

        // Verify Reset
        parser.reset()
        #expect(parser.subtitleTracks.isEmpty)
    }

    @Test("PMT Validation: Invalid Teletext BCD nibbles are strictly discarded")
    func invalidBCDTeletextPagesAreDiscarded() {
        let parser = TSPacketParser()
        parser.feed(data: makePAT(programNumber: 1, pmtPID: 100))

        // Teletext Descriptor with invalid BCD nibbles: 0xFA, 0x1F, 0xAF
        var streams = Data()
        streams.append(contentsOf: [0x1B, 0xE1, 0x00, 0xF0, 0x00]) // Video
        streams.append(contentsOf: [
            0x06, 0xE1, 0x03, 0xF0, 0x11,
            0x56, 0x0F,
            UInt8(ascii: "d"), UInt8(ascii: "e"), UInt8(ascii: "u"), (0x02 << 3) | 0x01, 0xFA,
            UInt8(ascii: "d"), UInt8(ascii: "e"), UInt8(ascii: "u"), (0x05 << 3) | 0x07, 0x1F,
            UInt8(ascii: "d"), UInt8(ascii: "e"), UInt8(ascii: "u"), (0x02 << 3) | 0x02, 0xAF
        ])

        let pmt = makePMT(pmtPID: 100, programNumber: 1, pcrPID: 256, streamData: streams)
        parser.feed(data: pmt)

        #expect(parser.subtitleTracks.isEmpty, "Invalid BCD nibbles must not produce subtitle tracks")
    }

    @Test("PMT Validation: Reserved DVB subtitling types are discarded")
    func reservedDVBSubtitlingTypesAreDiscarded() {
        let parser = TSPacketParser()
        parser.feed(data: makePAT(programNumber: 1, pmtPID: 100))

        // DVB Subtitling Descriptor with reserved/unknown types: 0x00, 0x05, 0x18, 0x30, 0xFF
        var streams = Data()
        streams.append(contentsOf: [0x1B, 0xE1, 0x00, 0xF0, 0x00]) // Video
        streams.append(contentsOf: [
            0x06, 0xE1, 0x02, 0xF0, 0x1A,
            0x59, 0x18,
            UInt8(ascii: "d"), UInt8(ascii: "e"), UInt8(ascii: "u"), 0x00, 0x00, 0x01, 0x00, 0x01,
            UInt8(ascii: "d"), UInt8(ascii: "e"), UInt8(ascii: "u"), 0x18, 0x00, 0x02, 0x00, 0x02,
            UInt8(ascii: "d"), UInt8(ascii: "e"), UInt8(ascii: "u"), 0x30, 0x00, 0x03, 0x00, 0x03
        ])

        let pmt = makePMT(pmtPID: 100, programNumber: 1, pcrPID: 256, streamData: streams)
        parser.feed(data: pmt)

        #expect(parser.subtitleTracks.isEmpty, "Reserved DVB subtitling types must not produce subtitle tracks")
    }

    @Test("Dynamic PMT Update: Broadcaster removing subtitle streams updates parser and pipeline")
    func dynamicPMTUpdateClearsAndUpdatesSubtitleTracks() async {
        let parser = TSPacketParser()
        let pipeline = NativeTSVideoPipeline()
        let sink = MockSubtitleSink()
        parser.delegate = sink

        parser.feed(data: makePAT(programNumber: 1, pmtPID: 100))

        // 1. PMT Version 1 with Subtitles
        var streamsV1 = Data()
        streamsV1.append(contentsOf: [0x1B, 0xE1, 0x00, 0xF0, 0x00]) // Video
        streamsV1.append(contentsOf: [
            0x06, 0xE1, 0x02, 0xF0, 0x0A,
            0x59, 0x08,
            UInt8(ascii: "d"), UInt8(ascii: "e"), UInt8(ascii: "u"), 0x20, 0x00, 0x01, 0x00, 0x01
        ])
        streamsV1.append(contentsOf: [
            0x06, 0xE1, 0x03, 0xF0, 0x07,
            0x56, 0x05,
            UInt8(ascii: "d"), UInt8(ascii: "e"), UInt8(ascii: "u"), (0x05 << 3) | 0x07, 0x77
        ])

        parser.feed(data: makePMT(pmtPID: 100, programNumber: 1, pcrPID: 256, version: 1, streamData: streamsV1))
        #expect(parser.subtitleTracks.count == 2)
        #expect(sink.discoveredSubtitleTracks.count == 2)

        pipeline.tsParser(parser, didDiscoverSubtitleTracks: parser.subtitleTracks)
        #expect(pipeline.availableSubtitleTracks.count == 2)

        let ttxTrack = pipeline.availableSubtitleTracks.first(where: { $0.format == .teletext(page: 777) })!
        pipeline.selectSubtitleTrack(ttxTrack)
        try? await Task.sleep(nanoseconds: 50_000_000)
        #expect(pipeline.selectedSubtitleTrack == ttxTrack)

        // 2. PMT Version 2: Channel removes all subtitle streams
        var streamsV2 = Data()
        streamsV2.append(contentsOf: [0x1B, 0xE1, 0x00, 0xF0, 0x00]) // Video only

        parser.feed(data: makePMT(pmtPID: 100, programNumber: 1, pcrPID: 256, version: 2, streamData: streamsV2))
        #expect(parser.subtitleTracks.isEmpty)
        #expect(sink.discoveredSubtitleTracks.isEmpty)

        pipeline.tsParser(parser, didDiscoverSubtitleTracks: parser.subtitleTracks)
        #expect(pipeline.availableSubtitleTracks.isEmpty)
        #expect(pipeline.selectedSubtitleTrack == nil, "Removed subtitle track must be deselected")
    }

    @Test("Dynamic PMT Update: Removing Teletext retains remaining DVB subtitle track")
    func dynamicPMTUpdateRemovesTeletextLeavesDVB() async {
        let parser = TSPacketParser()
        let pipeline = NativeTSVideoPipeline()

        parser.feed(data: makePAT(programNumber: 1, pmtPID: 100))

        // PMT Version 1: DVB + Teletext
        var streamsV1 = Data()
        streamsV1.append(contentsOf: [0x1B, 0xE1, 0x00, 0xF0, 0x00]) // Video
        streamsV1.append(contentsOf: [
            0x06, 0xE1, 0x02, 0xF0, 0x0A,
            0x59, 0x08,
            UInt8(ascii: "d"), UInt8(ascii: "e"), UInt8(ascii: "u"), 0x20, 0x00, 0x01, 0x00, 0x01
        ])
        streamsV1.append(contentsOf: [
            0x06, 0xE1, 0x03, 0xF0, 0x07,
            0x56, 0x05,
            UInt8(ascii: "d"), UInt8(ascii: "e"), UInt8(ascii: "u"), (0x05 << 3) | 0x07, 0x77
        ])

        parser.feed(data: makePMT(pmtPID: 100, programNumber: 1, pcrPID: 256, version: 1, streamData: streamsV1))
        pipeline.tsParser(parser, didDiscoverSubtitleTracks: parser.subtitleTracks)

        let dvbTrack = pipeline.availableSubtitleTracks.first(where: { $0.format == .dvb(compositionPageID: 1, ancillaryPageID: 1) })!
        pipeline.selectSubtitleTrack(dvbTrack)
        try? await Task.sleep(nanoseconds: 50_000_000)
        #expect(pipeline.selectedSubtitleTrack == dvbTrack)

        // PMT Version 2: Only DVB retained
        var streamsV2 = Data()
        streamsV2.append(contentsOf: [0x1B, 0xE1, 0x00, 0xF0, 0x00]) // Video
        streamsV2.append(contentsOf: [
            0x06, 0xE1, 0x02, 0xF0, 0x0A,
            0x59, 0x08,
            UInt8(ascii: "d"), UInt8(ascii: "e"), UInt8(ascii: "u"), 0x20, 0x00, 0x01, 0x00, 0x01
        ])

        parser.feed(data: makePMT(pmtPID: 100, programNumber: 1, pcrPID: 256, version: 2, streamData: streamsV2))
        pipeline.tsParser(parser, didDiscoverSubtitleTracks: parser.subtitleTracks)

        #expect(pipeline.availableSubtitleTracks.count == 1)
        #expect(pipeline.availableSubtitleTracks[0].format == .dvb(compositionPageID: 1, ancillaryPageID: 1))
        #expect(pipeline.selectedSubtitleTrack == dvbTrack, "Retained DVB track must remain selected")
    }

    @Test("Pipeline: Subtitle Track selection and Zap teardown")
    func pipelineSubtitleTrackSelection() async {
        let pipeline = NativeTSVideoPipeline()
        let parser = TSPacketParser()

        let tracks = [
            SubtitleTrackInfo(pid: 258, format: .dvb(compositionPageID: 1, ancillaryPageID: 1), language: "deu", isHearingImpaired: true),
            SubtitleTrackInfo(pid: 259, format: .teletext(page: 777), language: "deu", isHearingImpaired: true)
        ]

        pipeline.tsParser(parser, didDiscoverSubtitleTracks: tracks)
        #expect(pipeline.availableSubtitleTracks.count == 2)
        #expect(pipeline.selectedSubtitleTrack == nil, "Subtitles should be Off by default")

        // Select DVB Subtitle
        pipeline.selectSubtitleTrack(tracks[0])
        try? await Task.sleep(nanoseconds: 50_000_000)
        #expect(pipeline.selectedSubtitleTrack == tracks[0])

        // Turn Off
        pipeline.selectSubtitleTrack(nil)
        try? await Task.sleep(nanoseconds: 50_000_000)
        #expect(pipeline.selectedSubtitleTrack == nil)

        // Select Teletext Subtitle
        pipeline.selectSubtitleTrack(tracks[1])
        try? await Task.sleep(nanoseconds: 50_000_000)
        #expect(pipeline.selectedSubtitleTrack == tracks[1])

        // Channel Zap / Teardown
        pipeline.stopStreaming()
        #expect(pipeline.availableSubtitleTracks.isEmpty)
        #expect(pipeline.selectedSubtitleTrack == nil)
    }
}

// MARK: - Test Helpers

private final class MockSubtitleSink: TSPacketParserDelegate, @unchecked Sendable {
    var discoveredSubtitleTracks: [SubtitleTrackInfo] = []

    func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16) {}
    func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool) {}
    func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8) {}
    func tsParser(_ parser: TSPacketParser, didDiscoverSubtitleTracks tracks: [SubtitleTrackInfo]) {
        discoveredSubtitleTracks = tracks
    }
}

private func makePAT(programNumber: UInt16, pmtPID: UInt16) -> Data {
    var pat = Data(repeating: 0xFF, count: 188)
    pat[0] = 0x47
    pat[1] = 0x40
    pat[2] = 0x00
    pat[3] = 0x10

    pat[4] = 0x00 // pointer field
    pat[5] = 0x00 // table_id = 0 (PAT)
    pat[6] = 0xB0 // section_syntax_indicator
    pat[7] = 0x0D // section_length (13 bytes)
    pat[8] = 0x00
    pat[9] = 0x01 // transport_stream_id
    pat[10] = 0xC1
    pat[11] = 0x00
    pat[12] = 0x00

    pat[13] = UInt8((programNumber >> 8) & 0xFF)
    pat[14] = UInt8(programNumber & 0xFF)
    pat[15] = 0xE0 | UInt8((pmtPID >> 8) & 0x1F)
    pat[16] = UInt8(pmtPID & 0xFF)

    return pat
}

private func makePMT(pmtPID: UInt16, programNumber: UInt16, pcrPID: UInt16, version: UInt8 = 0, streamData: Data) -> Data {
    var pmt = Data(repeating: 0xFF, count: 188)
    pmt[0] = 0x47
    pmt[1] = 0x40 | UInt8((pmtPID >> 8) & 0x1F)
    pmt[2] = UInt8(pmtPID & 0xFF)
    pmt[3] = 0x10

    pmt[4] = 0x00 // pointer field
    pmt[5] = 0x02 // table_id = 2 (PMT)

    let sectionLength = 9 + streamData.count + 4
    pmt[6] = 0xB0 | UInt8((sectionLength >> 8) & 0x0F)
    pmt[7] = UInt8(sectionLength & 0xFF)
    pmt[8] = UInt8((programNumber >> 8) & 0xFF)
    pmt[9] = UInt8(programNumber & 0xFF)
    pmt[10] = 0xC1 | ((version & 0x1F) << 1)
    pmt[11] = 0x00
    pmt[12] = 0x00
    pmt[13] = 0xE0 | UInt8((pcrPID >> 8) & 0x1F)
    pmt[14] = UInt8(pcrPID & 0xFF)
    pmt[15] = 0xF0
    pmt[16] = 0x00

    pmt.replaceSubrange(17..<(17 + streamData.count), with: streamData)
    return pmt
}
