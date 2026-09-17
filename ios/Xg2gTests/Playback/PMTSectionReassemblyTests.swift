// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation
import Testing

@testable import Xg2g

/// A PMT section is not obliged to fit in one transport packet.
///
/// Modelled on Das Erste HD, whose table announces twelve elementary streams in
/// 191 bytes against the 183 a packet carries. The decodable AC-3 track sits
/// sixth, past the split, so a parser that reads only the first packet finds
/// three tracks it cannot play and misses the one it can.
@Suite struct PMTSectionReassemblyTests {

    private func tsPackets(pid: UInt16, section: [UInt8]) -> Data {
        var remaining: [UInt8] = [0x00] + section     // pointer_field, then the section
        var out = Data()
        var first = true
        var continuity: UInt8 = 0
        while !remaining.isEmpty {
            var p = [UInt8](repeating: 0xFF, count: 188)
            p[0] = 0x47
            p[1] = UInt8((first ? 0x40 : 0x00) | Int((pid >> 8) & 0x1F))
            p[2] = UInt8(pid & 0xFF)
            p[3] = 0x10 | continuity
            continuity = (continuity + 1) & 0x0F
            let take = min(184, remaining.count)
            for i in 0..<take { p[4 + i] = remaining[i] }
            remaining.removeFirst(take)
            out.append(contentsOf: p)
            first = false
        }
        return out
    }

    private func patSection(pmtPID: UInt16) -> [UInt8] {
        let header: [UInt8] = [0x00, 0x01, 0xC1, 0x00, 0x00]   // tsid, version, section numbers
        let programs: [UInt8] = [0x00, 0x01, UInt8(0xE0 | (pmtPID >> 8)), UInt8(pmtPID & 0xFF)]
        let length = header.count + programs.count + 4          // + CRC
        var section: [UInt8] = [0x00, UInt8(0xB0 | ((length >> 8) & 0x0F)), UInt8(length & 0xFF)]
        section += header + programs + [0xDE, 0xAD, 0xBE, 0xEF]
        return section
    }

    private func pmtSection(streams: [(UInt8, UInt16, [UInt8])]) -> [UInt8] {
        var body: [UInt8] = []
        for (type, pid, descriptors) in streams {
            body.append(type)
            body.append(UInt8(0xE0 | ((pid >> 8) & 0x1F)))
            body.append(UInt8(pid & 0xFF))
            body.append(UInt8(0xF0 | ((descriptors.count >> 8) & 0x0F)))
            body.append(UInt8(descriptors.count & 0xFF))
            body += descriptors
        }
        let header: [UInt8] = [0x00, 0x01, 0xC1, 0x00, 0x00, 0xE0, 0x64, 0xF0, 0x00]
        let length = header.count + body.count + 4
        var section: [UInt8] = [0x02, UInt8(0xB0 | ((length >> 8) & 0x0F)), UInt8(length & 0xFF)]
        section += header + body + [0xDE, 0xAD, 0xBE, 0xEF]
        return section
    }

    private func iso639(_ lang: String, audioType: UInt8) -> [UInt8] {
        [0x0A, 0x04] + Array(lang.utf8) + [audioType]
    }

    private var streamIdentifier: [UInt8] { [0x52, 0x01, 0x01] }

    /// The real shape of the table, with the playable track past the split.
    private var dasErsteStreams: [(UInt8, UInt16, [UInt8])] {
        [
            (0x1B, 5101, streamIdentifier + [0x0E, 0x03, 0x00, 0x00, 0x00]),      // H.264
            (0x03, 5102, streamIdentifier + iso639("deu", audioType: 0x00)),      // Layer II
            (0x03, 5103, [0x7F, 0x02, 0x06, 0x00] + streamIdentifier + iso639("mis", audioType: 0x00)),
            (0x03, 5107, [0x7F, 0x02, 0x06, 0x00] + streamIdentifier + iso639("qks", audioType: 0x02)),
            (0x06, 5104, streamIdentifier + [0x56, 0x0A] + [UInt8](repeating: 0x00, count: 10)), // teletext
            (0x06, 5106, [0x6A, 0x01, 0x00] + streamIdentifier + iso639("deu", audioType: 0x00)), // AC-3
            (0x05, 1170, [0x6F, 0x04, 0x00, 0x00, 0x00, 0x00]),
            (0x0C, 1176, streamIdentifier),
            (0x0B, 2171, streamIdentifier + [0x66, 0x04, 0x00, 0x00, 0x00, 0x00]),
            (0x06, 5105, streamIdentifier + [0x59, 0x08] + [UInt8](repeating: 0x00, count: 8)),   // subtitles
            (0x06, 5108, []),
            (0x0B, 5172, streamIdentifier + [0x66, 0x04, 0x00, 0x00, 0x00, 0x00])
        ]
    }

    @Test func aSectionSpanningTwoPacketsIsStillParsed() {
        let section = pmtSection(streams: dasErsteStreams)
        // The premise of the test: if this fits in one packet it proves nothing.
        #expect(section.count > 184)

        let parser = TSPacketParser()
        let probe = TrackProbe()
        parser.delegate = probe

        parser.feed(data: tsPackets(pid: 0, section: patSection(pmtPID: 5100)))
        parser.feed(data: tsPackets(pid: 5100, section: section))

        #expect(parser.videoPID == 5101)
        #expect(probe.tracks.contains { $0.pid == 5106 && $0.codec == .ac3 })
        #expect(probe.tracks.count == 4)   // three Layer II plus the AC-3
    }

    @Test func theTrackChosenFromASplitSectionIsTheDecodableOne() {
        let parser = TSPacketParser()
        let probe = TrackProbe()
        parser.delegate = probe
        parser.feed(data: tsPackets(pid: 0, section: patSection(pmtPID: 5100)))
        parser.feed(data: tsPackets(pid: 5100, section: pmtSection(streams: dasErsteStreams)))

        let picked = NativeTSVideoPipeline.preferredAudioTrack(from: probe.tracks)
        #expect(picked?.pid == 5106)
        #expect(picked?.codec == .ac3)
    }

    @Test func aSectionFittingOnePacketStillWorks() {
        let streams: [(UInt8, UInt16, [UInt8])] = [
            (0x1B, 1279, streamIdentifier),
            (0x06, 1283, [0x6A, 0x01, 0x00] + iso639("deu", audioType: 0x00))
        ]
        let section = pmtSection(streams: streams)
        #expect(section.count <= 184)

        let parser = TSPacketParser()
        let probe = TrackProbe()
        parser.delegate = probe
        parser.feed(data: tsPackets(pid: 0, section: patSection(pmtPID: 5100)))
        parser.feed(data: tsPackets(pid: 5100, section: section))

        #expect(parser.videoPID == 1279)
        #expect(probe.tracks.map(\.pid) == [1283])
    }

    /// Joining a transport mid-section is the normal case at tune-in, and the
    /// tail on its own carries no offsets that can be trusted.
    @Test func aSectionJoinedAfterItsStartIsIgnoredUntilTheNextOne() {
        let section = pmtSection(streams: dasErsteStreams)
        let packets = tsPackets(pid: 5100, section: section)
        let tailOnly = packets.suffix(from: 188)      // drop the packet carrying the head

        let parser = TSPacketParser()
        let probe = TrackProbe()
        parser.delegate = probe
        parser.feed(data: tsPackets(pid: 0, section: patSection(pmtPID: 5100)))
        parser.feed(data: Data(tailOnly))
        #expect(probe.tracks.isEmpty)

        // The next complete repetition is picked up normally.
        parser.feed(data: packets)
        #expect(probe.tracks.contains { $0.pid == 5106 })
    }
}

private final class TrackProbe: TSPacketParserDelegate, @unchecked Sendable {
    var tracks: [AudioTrackInfo] = []
    func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16) {}
    func tsParser(_ parser: TSPacketParser, didDiscoverAudioTracks tracks: [AudioTrackInfo]) {
        self.tracks = tracks
    }
    func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool) {}
    func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8) {}
}
