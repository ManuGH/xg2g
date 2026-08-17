// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation

/// Elementary Stream video data chunk extracted from a PES packet.
public struct PESVideoData: Sendable {
    public let data: Data
    public let pts: CMTime?
    public let dts: CMTime?
}

public protocol PESPacketAssemblerDelegate: AnyObject, Sendable {
    func pesAssembler(_ assembler: PESPacketAssembler, didEmitVideoPayload payload: PESVideoData)
    func pesAssembler(_ assembler: PESPacketAssembler, didEncounterPESError reason: String)
}

/// Assembles Packetized Elementary Stream (PES) packets from TS payloads.
///
/// Features:
/// - Reassembles fragmented payloads across TS packets.
/// - Parses PES header, PTS and DTS flags.
/// - Decodes 33-bit MPEG-2 90 kHz timestamps into `CMTime`.
/// - Emits continuous Elementary Stream data chunks to the Access Unit Assembler.
public final class PESPacketAssembler: @unchecked Sendable {

    private var currentPESBuffer = Data()
    public weak var delegate: PESPacketAssemblerDelegate?

    public init() {}

    public func reset() {
        currentPESBuffer.removeAll(keepingCapacity: true)
    }

    /// Ingests a video TS payload fragment.
    public func feed(payload: Data, unitStart: Bool) {
        if unitStart {
            if !currentPESBuffer.isEmpty {
                parseAndEmitCurrentPES()
            }
            currentPESBuffer = payload
        } else {
            if !currentPESBuffer.isEmpty {
                currentPESBuffer.append(payload)
            }
        }
    }

    private func parseAndEmitCurrentPES() {
        let data = currentPESBuffer
        currentPESBuffer.removeAll(keepingCapacity: true)

        guard data.count >= 6 else { return }

        // Start code prefix: 0x00 0x00 0x01
        guard data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x01 else {
            delegate?.pesAssembler(self, didEncounterPESError: "Invalid PES start code prefix")
            return
        }

        let streamID = data[3]
        // Video stream IDs: 0xE0 - 0xEF or 0xFD (extended) or 0xBD (private stream)
        guard (streamID >= 0xE0 && streamID <= 0xEF) || streamID == 0xFD || streamID == 0xBD else {
            return
        }

        // Optional PES header starts at byte 6
        guard data.count >= 9 else { return }

        let flags2 = data[7]
        let headerDataLength = Int(data[8])
        let ptsDtsFlags = (flags2 & 0xC0) >> 6

        var pts: CMTime? = nil
        var dts: CMTime? = nil

        let headerEnd = 9 + headerDataLength
        guard data.count >= headerEnd else { return }

        if (ptsDtsFlags == 0x02 || ptsDtsFlags == 0x03) && headerDataLength >= 5 {
            // PTS present
            let ptsVal = decode33BitTimestamp(data: data, offset: 9)
            pts = CMTime(value: CMTimeValue(ptsVal), timescale: 90000)
        }

        if ptsDtsFlags == 0x03 && headerDataLength >= 10 {
            // DTS present
            let dtsVal = decode33BitTimestamp(data: data, offset: 14)
            dts = CMTime(value: CMTimeValue(dtsVal), timescale: 90000)
        }

        let esData = data.subdata(in: headerEnd..<data.count)
        guard !esData.isEmpty else { return }

        let payload = PESVideoData(data: esData, pts: pts, dts: dts)
        delegate?.pesAssembler(self, didEmitVideoPayload: payload)
    }

    private func decode33BitTimestamp(data: Data, offset: Int) -> UInt64 {
        let b0 = UInt64(data[offset])
        let b1 = UInt64(data[offset + 1])
        let b2 = UInt64(data[offset + 2])
        let b3 = UInt64(data[offset + 3])
        let b4 = UInt64(data[offset + 4])

        let pts32_30 = (b0 & 0x0E) >> 1
        let pts29_15 = ((b1 & 0xFF) << 7) | ((b2 & 0xFE) >> 1)
        let pts14_0  = ((b3 & 0xFF) << 7) | ((b4 & 0xFE) >> 1)

        return (pts32_30 << 30) | (pts29_15 << 15) | pts14_0
    }
}
