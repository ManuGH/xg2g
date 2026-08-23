// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation

/// A complete DVB Subtitling PES packet extracted from MPEG-TS elementary stream.
///
/// Contains the extracted 33-bit / 90 kHz presentation timestamp (PTS), the stream ID,
/// and the bounded DVB subtitle segment payload (stripped of PES and DVB data field headers).
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
/// - Bounds-safe optional PES header parsing and 33-bit 90 kHz PTS timestamp unwrapping.
/// - Validates DVB subtitling data_identifier (0x20) and extracts subtitle_stream_id.
/// - Bounds DVB segment payload up to the 0xFF end-of-PES data field marker.
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
        if pesPacketLength > 0 && buffer.count >= 6 + pesPacketLength {
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
        var validData = buffer
        if pesPacketLength > 0 {
            let totalLength = 6 + pesPacketLength
            if buffer.count < totalLength && incomplete {
                delegate?.dvbSubtitleAssembler(self, didEncounterError: "Incomplete PES packet truncated by next PUSI (expected \(totalLength) bytes, got \(buffer.count))")
                return
            }
            if buffer.count >= totalLength {
                validData = buffer.prefix(totalLength)
            }
        }

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

        let headerEnd = 9 + headerDataLength
        guard validData.count >= headerEnd else {
            delegate?.dvbSubtitleAssembler(self, didEncounterError: "PES packet truncated within header data (headerEnd: \(headerEnd), size: \(validData.count))")
            return
        }

        var pts90k: UInt64? = nil
        var pts: CMTime? = nil

        if (ptsDtsFlags == 0x02 || ptsDtsFlags == 0x03) && headerDataLength >= 5 {
            let rawPTS = decode33BitTimestamp(data: validData, offset: 9)
            let unwrappedPTS = normalizer.unwrap(rawPTS: rawPTS)
            pts90k = unwrappedPTS
            pts = CMTime(value: CMTimeValue(unwrappedPTS), timescale: 90000)
        }

        // 4. DVB Subtitle Data Field (starts at headerEnd)
        let dataField = validData.dropFirst(headerEnd)
        guard dataField.count >= 2 else {
            delegate?.dvbSubtitleAssembler(self, didEncounterError: "PES packet carries no DVB data field")
            return
        }

        let dataIdentifier = dataField[dataField.startIndex]
        guard dataIdentifier == 0x20 else {
            delegate?.dvbSubtitleAssembler(self, didEncounterError: "Invalid DVB data_identifier 0x\(String(format: "%02X", dataIdentifier)): expected 0x20")
            return
        }

        let subtitleStreamID = dataField[dataField.startIndex + 1]

        // 5. Extract Subtitle Segments up to End Marker 0xFF
        var segmentBytes = dataField.dropFirst(2)
        if let lastByte = segmentBytes.last, lastByte == 0xFF {
            segmentBytes = segmentBytes.dropLast(1)
        }

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

    private func decode33BitTimestamp(data: Data, offset: Int) -> UInt64 {
        let bytes = [UInt8](data.dropFirst(offset).prefix(5))
        guard bytes.count == 5 else { return 0 }
        let b0 = UInt64(bytes[0])
        let b1 = UInt64(bytes[1])
        let b2 = UInt64(bytes[2])
        let b3 = UInt64(bytes[3])
        let b4 = UInt64(bytes[4])

        let pts32_30 = (b0 & 0x0E) >> 1
        let pts29_15 = ((b1 & 0xFF) << 7) | ((b2 & 0xFE) >> 1)
        let pts14_0  = ((b3 & 0xFF) << 7) | ((b4 & 0xFE) >> 1)

        return (pts32_30 << 30) | (pts29_15 << 15) | pts14_0
    }
}
