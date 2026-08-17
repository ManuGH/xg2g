// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation

/// Audio elementary stream chunk extracted from a PES packet.
public struct AudioPESData: Sendable, Equatable {
    public let pid: UInt16
    public let streamID: UInt8
    public let pts90k: UInt64?
    public let pts: CMTime?
    public let payload: Data

    public init(pid: UInt16, streamID: UInt8, pts90k: UInt64?, pts: CMTime?, payload: Data) {
        self.pid = pid
        self.streamID = streamID
        self.pts90k = pts90k
        self.pts = pts
        self.payload = payload
    }
}

public protocol AudioPESAssemblerDelegate: AnyObject, Sendable {
    func audioPESAssembler(_ assembler: AudioPESAssembler, didEmitAudioPES payload: AudioPESData)
    func audioPESAssembler(_ assembler: AudioPESAssembler, didEncounterPESError reason: String, onPID pid: UInt16)
}

/// Normalizes 33-bit MPEG-2 90 kHz timestamps across the 2^33 - 1 wrap boundary (~26.5 hours).
public struct PTS33BitNormalizer: Sendable {
    private static let maxPTS: UInt64 = 1 << 33 // 8,589,934,592
    private static let halfPTS: UInt64 = 1 << 32 // 4,294,967,296

    private var epochOffset: UInt64 = 0
    private var lastRawPTS: UInt64?
    private var baseUnwrappedPTS: UInt64?

    public init() {}

    public mutating func reset() {
        epochOffset = 0
        lastRawPTS = nil
        baseUnwrappedPTS = nil
    }

    /// Unwraps a 33-bit raw PTS value to a continuous 64-bit timestamp.
    public mutating func unwrap(rawPTS: UInt64) -> UInt64 {
        let maskedPTS = rawPTS & 0x1_FFFF_FFFF
        guard let last = lastRawPTS else {
            lastRawPTS = maskedPTS
            if baseUnwrappedPTS == nil {
                baseUnwrappedPTS = maskedPTS
            }
            return maskedPTS + epochOffset
        }

        // Detect forward wrap (last near 2^33-1, new near 0)
        if last > Self.halfPTS && maskedPTS < (last - Self.halfPTS) {
            epochOffset += Self.maxPTS
        }
        // Detect backward wrap / out-of-order jump near boundary
        else if maskedPTS > Self.halfPTS && last < (maskedPTS - Self.halfPTS) {
            if epochOffset >= Self.maxPTS {
                epochOffset -= Self.maxPTS
            }
        }

        lastRawPTS = maskedPTS
        return maskedPTS + epochOffset
    }

    /// Converts raw 33-bit PTS to a normalized CMTime starting at basePTS (or first received PTS).
    public mutating func normalize(rawPTS: UInt64, basePTS: UInt64? = nil) -> CMTime {
        let unwrapped = unwrap(rawPTS: rawPTS)
        let base = basePTS ?? (baseUnwrappedPTS ?? unwrapped)
        let diff = Int64(unwrapped) - Int64(base)
        return CMTime(value: CMTimeValue(diff), timescale: 90000)
    }
}

/// Assembles Packetized Elementary Stream (PES) packets from audio TS payloads.
///
/// Features:
/// - Supports reassembly of fragmented payloads across multiple TS packets per PID.
/// - Isolates multiple concurrent audio PIDs without buffer collision.
/// - Parses standard MPEG audio PES headers (stream IDs 0xBD, 0xC0...0xDF, 0xFD).
/// - Decodes 33-bit MPEG-2 90 kHz PTS timestamps.
/// - Emits structured `AudioPESData` chunks for codec frame parsing.
public final class AudioPESAssembler: @unchecked Sendable {

    private var buffersPerPID: [UInt16: Data] = [:]
    public weak var delegate: AudioPESAssemblerDelegate?

    public init() {}

    public func reset(pid: UInt16? = nil) {
        if let pid = pid {
            buffersPerPID.removeValue(forKey: pid)
        } else {
            buffersPerPID.removeAll(keepingCapacity: true)
        }
    }

    /// Ingests an audio TS payload chunk.
    public func feed(payload: Data, pid: UInt16, unitStart: Bool) {
        if unitStart {
            if let existing = buffersPerPID[pid], !existing.isEmpty {
                parseAndEmitCurrentPES(for: pid)
            }
            buffersPerPID[pid] = payload
        } else {
            if buffersPerPID[pid] != nil {
                buffersPerPID[pid]?.append(payload)
            }
        }
    }

    /// Flushes any pending PES buffer for a given PID or all active PIDs.
    public func flush(pid: UInt16? = nil) {
        if let pid = pid {
            if buffersPerPID[pid] != nil {
                parseAndEmitCurrentPES(for: pid)
            }
        } else {
            for activePID in Array(buffersPerPID.keys) {
                parseAndEmitCurrentPES(for: activePID)
            }
        }
    }

    private func parseAndEmitCurrentPES(for pid: UInt16) {
        guard let data = buffersPerPID[pid] else { return }
        buffersPerPID[pid]?.removeAll(keepingCapacity: true)

        guard data.count >= 6 else { return }

        // Start code prefix: 0x00 0x00 0x01
        guard data[0] == 0x00 && data[1] == 0x00 && data[2] == 0x01 else {
            delegate?.audioPESAssembler(self, didEncounterPESError: "Invalid PES start code prefix", onPID: pid)
            return
        }

        let streamID = data[3]
        // Audio stream IDs:
        // 0xBD (private stream 1 / AC-3 / E-AC-3)
        // 0xC0 ... 0xDF (ISO/IEC 13818-3 / 11172-3 MPEG audio)
        // 0xFD (extended stream ID)
        let isAudioStreamID = (streamID == 0xBD) || (streamID >= 0xC0 && streamID <= 0xDF) || (streamID == 0xFD)
        guard isAudioStreamID else {
            return
        }

        let pesPacketLength = Int(data[4]) << 8 | Int(data[5])
        var validData = data
        if pesPacketLength > 0 && data.count >= 6 + pesPacketLength {
            validData = data.prefix(6 + pesPacketLength)
        }

        // Optional PES header starts at byte 6
        guard validData.count >= 9 else { return }

        let flags2 = validData[7]
        let headerDataLength = Int(validData[8])
        let ptsDtsFlags = (flags2 & 0xC0) >> 6

        var pts90k: UInt64? = nil
        var pts: CMTime? = nil

        let headerEnd = 9 + headerDataLength
        guard validData.count >= headerEnd else { return }

        if (ptsDtsFlags == 0x02 || ptsDtsFlags == 0x03) && headerDataLength >= 5 {
            let ptsVal = decode33BitTimestamp(data: validData, offset: 9)
            pts90k = ptsVal
            pts = CMTime(value: CMTimeValue(ptsVal), timescale: 90000)
        }

        let esData = Data(validData.dropFirst(headerEnd))
        guard !esData.isEmpty else { return }

        let audioData = AudioPESData(
            pid: pid,
            streamID: streamID,
            pts90k: pts90k,
            pts: pts,
            payload: esData
        )
        delegate?.audioPESAssembler(self, didEmitAudioPES: audioData)
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
