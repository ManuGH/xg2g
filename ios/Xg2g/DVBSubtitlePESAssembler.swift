// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation

/// A complete DVB Subtitling PES packet extracted from MPEG-TS elementary stream.
///
/// Contains the extracted 33-bit / 90 kHz presentation timestamp (PTS), the stream ID,
/// and the bounded DVB subtitle segment payload (stripped of PES, DVB data field headers, and 0xFF end marker).
public struct DVBSubtitlePESPacket: Sendable, Equatable {
    public let pts90k: UInt64?
    public let pts: CMTime?
    public let subtitleStreamID: UInt8
    public let payload: Data

    public init(pts90k: UInt64?, pts: CMTime? = nil, subtitleStreamID: UInt8 = 0x00, payload: Data) {
        self.pts90k = pts90k
        if let pts = pts {
            self.pts = pts
        } else if let pts90k = pts90k {
            self.pts = CMTime(value: CMTimeValue(pts90k), timescale: 90000)
        } else {
            self.pts = nil
        }
        self.subtitleStreamID = subtitleStreamID
        self.payload = payload
    }
}

public protocol DVBSubtitlePESAssemblerDelegate: AnyObject, Sendable {
    func dvbSubtitleAssembler(_ assembler: DVBSubtitlePESAssembler, didEmitPacket packet: DVBSubtitlePESPacket)
    func dvbSubtitleAssembler(_ assembler: DVBSubtitlePESAssembler, didEncounterError reason: String)
}

/// Assembles Packetized Elementary Stream (PES) packets from DVB Subtitle TS payloads (ETSI EN 300 743).
///
/// Features & Invariants:
/// - Reassembles fragmented payloads across multiple TS packets.
/// - Validates PES start code prefix (0x000001) and stream ID (0xBD private_stream_1).
/// - Enforces non-zero PES_packet_length as mandated by ISO/IEC 13818-1 for private_stream_1.
/// - Bounds-safe optional PES header parsing and strict 33-bit 90 kHz PTS timestamp validation (rejecting forbidden '01' flags and invalid marker bits).
/// - Validates DVB subtitling data_identifier (0x20) and extracts subtitle_stream_id.
/// - Enforces and validates the 0xFF end_of_PES_data_field_marker (ETSI EN 300 743 Clause 5.1).
/// - Handles PUSI restarts, packet drops, continuity loss, and clean resets upon track switching.
public final class DVBSubtitlePESAssembler: @unchecked Sendable {

    private var buffer = Data()
    private var isDroppingUntilNextPUSI = false
    private var normalizer = PTS33BitNormalizer()
    public weak var delegate: DVBSubtitlePESAssemblerDelegate?

    public init() {}

    /// Clears any pending PES fragments and resets normalizer state.
    public func reset() {
        buffer.removeAll(keepingCapacity: true)
        isDroppingUntilNextPUSI = false
        normalizer.reset()
    }

    /// Handles a transport stream continuity error on the subtitle PID.
    ///
    /// Drops the current incomplete PES buffer and rejects continuation packets until the next PUSI.
    public func handleContinuityError() {
        buffer.removeAll(keepingCapacity: true)
        isDroppingUntilNextPUSI = true
        delegate?.dvbSubtitleAssembler(self, didEncounterError: "Continuity error on subtitle PID — dropping PES until next PUSI")
    }

    /// Ingests a raw TS payload chunk for the selected DVB subtitle PID.
    public func feed(payload: Data, unitStart: Bool) {
        if unitStart {
            if !buffer.isEmpty {
                // An incomplete previous PES was in flight when a new PUSI arrived
                parseAndEmitCurrentBuffer(incomplete: true)
            }
            isDroppingUntilNextPUSI = false
            buffer = payload
            checkAndParseIfComplete()
        } else {
            guard !isDroppingUntilNextPUSI, !buffer.isEmpty else { return }
            buffer.append(payload)
            checkAndParseIfComplete()
        }
    }

    /// Flushes any pending PES buffer.
    public func flush() {
        if !buffer.isEmpty {
            parseAndEmitCurrentBuffer(incomplete: false)
        }
    }

    private func checkAndParseIfComplete() {
        guard buffer.count >= 6 else { return }
        let pesPacketLength = Int(buffer[4]) << 8 | Int(buffer[5])
        if pesPacketLength == 0 || buffer.count >= 6 + pesPacketLength {
            parseAndEmitCurrentBuffer(incomplete: false)
        }
    }

    private func parseAndEmitCurrentBuffer(incomplete: Bool) {
        defer {
            buffer.removeAll(keepingCapacity: true)
        }

        guard buffer.count >= 6 else {
            if incomplete {
                delegate?.dvbSubtitleAssembler(self, didEncounterError: "PES packet too short (< 6 bytes)")
            }
            return
        }

        // 1. Verify Start Code Prefix: 0x00 0x00 0x01
        guard buffer[0] == 0x00 && buffer[1] == 0x00 && buffer[2] == 0x01 else {
            delegate?.dvbSubtitleAssembler(self, didEncounterError: "Invalid PES start code prefix")
            return
        }

        // 2. Verify Stream ID: 0xBD (private_stream_1)
        guard buffer[3] == 0xBD else {
            delegate?.dvbSubtitleAssembler(self, didEncounterError: "Invalid stream_id 0x\(String(format: "%02X", buffer[3])): expected 0xBD (private_stream_1)")
            return
        }

        let pesPacketLength = Int(buffer[4]) << 8 | Int(buffer[5])

        // ISO/IEC 13818-1 Clause 2.4.3.7: PES_packet_length = 0 is ONLY allowed in video PES packets.
        guard pesPacketLength > 0 else {
            delegate?.dvbSubtitleAssembler(self, didEncounterError: "Invalid PES_packet_length 0 for private_stream_1 (0xBD)")
            return
        }

        let totalLength = 6 + pesPacketLength
        if buffer.count < totalLength && incomplete {
            delegate?.dvbSubtitleAssembler(self, didEncounterError: "Incomplete PES packet truncated by next PUSI (expected \(totalLength) bytes, got \(buffer.count))")
            return
        }
        guard buffer.count >= totalLength else { return }
        let validData = buffer.prefix(totalLength)

        // 3. Optional PES Header (starts at byte 6)
        guard validData.count >= 9 else {
            delegate?.dvbSubtitleAssembler(self, didEncounterError: "PES packet truncated before optional header")
            return
        }

        let flags1 = validData[6]
        guard (flags1 & 0xC0) == 0x80 else {
            delegate?.dvbSubtitleAssembler(self, didEncounterError: "Invalid PES header marker bits")
            return
        }

        let flags2 = validData[7]
        let ptsDtsFlags = (flags2 & 0xC0) >> 6
        let headerDataLength = Int(validData[8])

        // ISO/IEC 13818-1 Clause 2.4.3.7 Table 2-17: '01' is forbidden
        guard ptsDtsFlags != 0x01 else {
            delegate?.dvbSubtitleAssembler(self, didEncounterError: "Forbidden PTS_DTS_flags value 0x01 ('01')")
            return
        }

        let headerEnd = 9 + headerDataLength
        guard validData.count >= headerEnd else {
            delegate?.dvbSubtitleAssembler(self, didEncounterError: "PES packet truncated within header data (headerEnd: \(headerEnd), size: \(validData.count))")
            return
        }

        var pts90k: UInt64? = nil
        var pts: CMTime? = nil

        if ptsDtsFlags == 0x02 || ptsDtsFlags == 0x03 {
            let requiredHeaderLen = (ptsDtsFlags == 0x03) ? 10 : 5
            guard headerDataLength >= requiredHeaderLen else {
                delegate?.dvbSubtitleAssembler(self, didEncounterError: "PTS flag set but headerDataLength \(headerDataLength) < \(requiredHeaderLen)")
                return
            }

            let expectedPrefix: UInt8 = (ptsDtsFlags == 0x02) ? 0x02 : 0x03
            guard let rawPTS = decodeAndValidatePTS(data: validData, offset: 9, expectedPrefix: expectedPrefix) else {
                delegate?.dvbSubtitleAssembler(self, didEncounterError: "Malformed PTS timestamp marker bits or prefix")
                return
            }
            let unwrappedPTS = normalizer.unwrap(rawPTS: rawPTS)
            pts90k = unwrappedPTS
            pts = CMTime(value: CMTimeValue(unwrappedPTS), timescale: 90000)
        }

        // 4. DVB Subtitle Data Field (starts at headerEnd)
        let dataField = validData.dropFirst(headerEnd)
        guard dataField.count >= 3 else {
            delegate?.dvbSubtitleAssembler(self, didEncounterError: "PES packet carries insufficient DVB data field bytes (< 3 bytes)")
            return
        }

        let dataIdentifier = dataField[dataField.startIndex]
        guard dataIdentifier == 0x20 else {
            delegate?.dvbSubtitleAssembler(self, didEncounterError: "Invalid DVB data_identifier 0x\(String(format: "%02X", dataIdentifier)): expected 0x20")
            return
        }

        let subtitleStreamID = dataField[dataField.startIndex + 1]

        // 5. Enforce and validate End Marker 0xFF (ETSI EN 300 743 Clause 5.1 / Table 1)
        guard let endMarker = dataField.last, endMarker == 0xFF else {
            delegate?.dvbSubtitleAssembler(self, didEncounterError: "Missing or invalid DVB end_of_PES_data_field_marker 0xFF")
            return
        }

        let segmentBytes = dataField.dropFirst(2).dropLast(1)
        guard !segmentBytes.isEmpty else {
            delegate?.dvbSubtitleAssembler(self, didEncounterError: "DVB subtitle payload has empty segment data")
            return
        }

        let packet = DVBSubtitlePESPacket(
            pts90k: pts90k,
            pts: pts,
            subtitleStreamID: subtitleStreamID,
            payload: Data(segmentBytes)
        )
        delegate?.dvbSubtitleAssembler(self, didEmitPacket: packet)
    }

    private func decodeAndValidatePTS(data: Data, offset: Int, expectedPrefix: UInt8) -> UInt64? {
        let bytes = [UInt8](data.dropFirst(offset).prefix(5))
        guard bytes.count == 5 else { return nil }
        let b0 = bytes[0]
        let b1 = bytes[1]
        let b2 = bytes[2]
        let b3 = bytes[3]
        let b4 = bytes[4]

        // 1. Verify 4-bit prefix: '0010' (0x02) for PTS only, '0011' (0x03) for PTS with DTS
        guard (b0 >> 4) == expectedPrefix else { return nil }

        // 2. Verify marker bits (bit 0 of b0, b2, b4 must be 1)
        guard (b0 & 0x01) == 1, (b2 & 0x01) == 1, (b4 & 0x01) == 1 else { return nil }

        let pts32_30 = UInt64((b0 & 0x0E) >> 1)
        let pts29_15 = UInt64(((UInt16(b1) << 7) | (UInt16(b2) >> 1)) & 0x7FFF)
        let pts14_0  = UInt64(((UInt16(b3) << 7) | (UInt16(b4) >> 1)) & 0x7FFF)

        return (pts32_30 << 30) | (pts29_15 << 15) | pts14_0
    }
}
