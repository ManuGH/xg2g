// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

public protocol TSPacketParserDelegate: AnyObject, Sendable {
    func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16)
    func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool)
    func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8)
}

/// MPEG-TS (ISO/IEC 13818-1) transport stream packet parser.
///
/// Features:
/// - 188-byte TS packet synchronization and recovery.
/// - PAT (Program Association Table) demuxing.
/// - PMT (Program Map Table) parsing to discover the H.264 video PID (`stream_type = 0x1B`).
/// - Continuity counter tracking per PID.
/// - Adaptation field skipping and payload extraction.
public final class TSPacketParser: @unchecked Sendable {

    private static let packetSize = 188
    private static let syncByte: UInt8 = 0x47

    private var buffer = Data()
    private var pmtPIDs = Set<UInt16>()
    public private(set) var videoPID: UInt16?
    private var continuityCounters: [UInt16: UInt8] = [:]

    public weak var delegate: TSPacketParserDelegate?

    public init() {}

    public func reset() {
        buffer.removeAll(keepingCapacity: true)
        pmtPIDs.removeAll()
        videoPID = nil
        continuityCounters.removeAll()
    }

    /// Feeds raw MPEG-TS chunk from network or file stream.
    public func feed(data: Data) {
        buffer.append(data)
        processBuffer()
    }

    private func processBuffer() {
        while buffer.count >= Self.packetSize {
            // Find 0x47 sync byte
            guard let syncIndex = buffer.firstIndex(of: Self.syncByte) else {
                buffer.removeAll(keepingCapacity: true)
                return
            }

            if syncIndex > buffer.startIndex {
                buffer.removeSubrange(buffer.startIndex..<syncIndex)
                if buffer.count < Self.packetSize {
                    return
                }
            }

            // Check if next packet boundary also has 0x47 or if this is true packet
            let packetRange = buffer.startIndex..<(buffer.startIndex + Self.packetSize)
            let packet = buffer.subdata(in: packetRange)

            // Validate next packet sync byte if enough data is available
            if buffer.count >= Self.packetSize * 2 {
                let nextSync = buffer[buffer.startIndex + Self.packetSize]
                if nextSync != Self.syncByte {
                    // False sync byte, drop 1 byte and scan again
                    buffer.removeFirst(1)
                    continue
                }
            }

            buffer.removeSubrange(packetRange)
            parsePacket(packet)
        }
    }

    private func parsePacket(_ packet: Data) {
        guard packet.count == Self.packetSize, packet[0] == Self.syncByte else { return }

        let byte1 = packet[1]
        let byte2 = packet[2]
        let byte3 = packet[3]

        let transportError = (byte1 & 0x80) != 0
        if transportError { return }

        let payloadUnitStart = (byte1 & 0x40) != 0
        let pid = UInt16(byte1 & 0x1F) << 8 | UInt16(byte2)
        let adaptationControl = (byte3 & 0x30) >> 4
        let continuityCounter = byte3 & 0x0F

        // Check continuity counter
        if adaptationControl == 0x01 || adaptationControl == 0x03 { // Has payload
            if let lastCC = continuityCounters[pid] {
                let expectedCC = (lastCC + 1) & 0x0F
                if continuityCounter != expectedCC && continuityCounter != lastCC {
                    delegate?.tsParser(self, didEncounterContinuityErrorOnPID: pid, expected: expectedCC, actual: continuityCounter)
                }
            }
            continuityCounters[pid] = continuityCounter
        }

        // Calculate payload offset
        var payloadOffset = 4
        if adaptationControl == 0x02 || adaptationControl == 0x03 { // Adaptation field present
            let adaptationLength = Int(packet[4])
            payloadOffset = 5 + adaptationLength
            if payloadOffset > Self.packetSize { return }
        }

        // Check if there is payload
        guard (adaptationControl == 0x01 || adaptationControl == 0x03), payloadOffset < Self.packetSize else {
            return
        }

        let payload = packet.subdata(in: payloadOffset..<Self.packetSize)

        if pid == 0 {
            // PAT
            parsePAT(payload: payload, unitStart: payloadUnitStart)
        } else if pmtPIDs.contains(pid) {
            // PMT
            parsePMT(payload: payload, unitStart: payloadUnitStart)
        } else if let vPid = videoPID, pid == vPid {
            // Video Elementary Stream
            delegate?.tsParser(self, didEmitPayload: payload, pid: pid, unitStart: payloadUnitStart)
        } else if videoPID == nil && payloadUnitStart && payload.count >= 4 {
            // Fast-track auto-detection of video PID from PES video start code (0x000001E0 - 0x000001EF)
            if payload[0] == 0x00 && payload[1] == 0x00 && payload[2] == 0x01 && (payload[3] >= 0xE0 && payload[3] <= 0xEF) {
                self.videoPID = pid
                delegate?.tsParser(self, didDiscoverVideoPID: pid)
                delegate?.tsParser(self, didEmitPayload: payload, pid: pid, unitStart: payloadUnitStart)
            }
        }
    }

    // MARK: - PSI Parsing (PAT & PMT)

    private func parsePAT(payload: Data, unitStart: Bool) {
        var offset = 0
        if unitStart {
            let pointerField = Int(payload[0])
            offset = 1 + pointerField
        }
        guard offset + 8 < payload.count else { return }

        let tableID = payload[offset]
        guard tableID == 0x00 else { return } // PAT table_id is 0x00

        let sectionLength = Int(UInt16(payload[offset + 1] & 0x0F) << 8 | UInt16(payload[offset + 2]))
        guard offset + 3 + sectionLength <= payload.count else { return }

        // Programs start at offset + 8 (after transport_stream_id, version, section_number, etc.)
        var progOffset = offset + 8
        let endOffset = offset + 3 + sectionLength - 4 // minus 4 CRC bytes

        while progOffset + 4 <= endOffset {
            let programNumber = UInt16(payload[progOffset]) << 8 | UInt16(payload[progOffset + 1])
            let programPID = UInt16(payload[progOffset + 2] & 0x1F) << 8 | UInt16(payload[progOffset + 3])

            if programNumber != 0 {
                self.pmtPIDs.insert(programPID)
            }
            progOffset += 4
        }
    }

    private func parsePMT(payload: Data, unitStart: Bool) {
        var offset = 0
        if unitStart {
            let pointerField = Int(payload[0])
            offset = 1 + pointerField
        }
        guard offset + 12 < payload.count else { return }

        let tableID = payload[offset]
        guard tableID == 0x02 else { return } // PMT table_id is 0x02

        let sectionLength = Int(UInt16(payload[offset + 1] & 0x0F) << 8 | UInt16(payload[offset + 2]))
        guard offset + 3 + sectionLength <= payload.count else { return }

        let programInfoLength = Int(UInt16(payload[offset + 10] & 0x0F) << 8 | UInt16(payload[offset + 11]))
        var streamOffset = offset + 12 + programInfoLength
        let endOffset = offset + 3 + sectionLength - 4 // minus 4 CRC bytes

        while streamOffset + 5 <= endOffset {
            let streamType = payload[streamOffset]
            let elementaryPID = UInt16(payload[streamOffset + 1] & 0x1F) << 8 | UInt16(payload[streamOffset + 2])
            let esInfoLength = Int(UInt16(payload[streamOffset + 3] & 0x0F) << 8 | UInt16(payload[streamOffset + 4]))

            // stream_type 0x1B = H.264 / AVC Video
            // stream_type 0x24 = HEVC / H.265 Video
            // stream_type 0x02 = MPEG-2 Video
            if streamType == 0x1B || streamType == 0x24 || streamType == 0x02 {
                if self.videoPID != elementaryPID {
                    self.videoPID = elementaryPID
                    delegate?.tsParser(self, didDiscoverVideoPID: elementaryPID)
                }
                break
            }

            streamOffset += 5 + esInfoLength
        }
    }
}
