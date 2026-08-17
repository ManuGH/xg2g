// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation

/// Parsed metadata for an AAC ADTS audio frame.
public struct AACFrameInfo: Sendable, Equatable {
    public let profile: UInt8             // 0 = Main, 1 = LC, 2 = SSR, 3 = LTP
    public let sampleRate: Int
    public let sampleRateIndex: UInt8
    public let channelCount: Int
    public let channelConfig: UInt8
    public let frameSizeBytes: Int        // Full ADTS frame size (header + payload)
    public let headerSizeBytes: Int       // 7 or 9 bytes
    public let samplesPerFrame: Int       // Usually 1024
    public let duration: CMTime
    public let audioSpecificConfig: Data  // 2 bytes for CMAudioFormatDescription magic cookie

    public init(
        profile: UInt8,
        sampleRate: Int,
        sampleRateIndex: UInt8,
        channelCount: Int,
        channelConfig: UInt8,
        frameSizeBytes: Int,
        headerSizeBytes: Int,
        samplesPerFrame: Int
    ) {
        self.profile = profile
        self.sampleRate = sampleRate
        self.sampleRateIndex = sampleRateIndex
        self.channelCount = channelCount
        self.channelConfig = channelConfig
        self.frameSizeBytes = frameSizeBytes
        self.headerSizeBytes = headerSizeBytes
        self.samplesPerFrame = samplesPerFrame
        self.duration = CMTime(value: CMTimeValue(samplesPerFrame), timescale: CMTimeScale(sampleRate))

        // Build 2-byte AudioSpecificConfig (ISO/IEC 14496-3)
        // audioObjectType = profile + 1 (5 bits)
        // samplingFrequencyIndex (4 bits)
        // channelConfiguration (4 bits)
        let audioObjectType = profile + 1
        let byte0 = (audioObjectType << 3) | ((sampleRateIndex >> 1) & 0x07)
        let byte1 = ((sampleRateIndex & 0x01) << 7) | ((channelConfig & 0x0F) << 3)
        self.audioSpecificConfig = Data([byte0, byte1])
    }
}

public struct ParsedAACFrame: Sendable {
    public let info: AACFrameInfo
    public let rawPayload: Data          // Raw AAC elementary data (ADTS header stripped)
    public let adtsData: Data            // Full ADTS frame including header
    public let pts: CMTime?
    public let pts90k: UInt64?

    public init(info: AACFrameInfo, rawPayload: Data, adtsData: Data, pts: CMTime?, pts90k: UInt64?) {
        self.info = info
        self.rawPayload = rawPayload
        self.adtsData = adtsData
        self.pts = pts
        self.pts90k = pts90k
    }
}

public protocol AACFrameParserDelegate: AnyObject, Sendable {
    func aacFrameParser(_ parser: AACADTSFrameParser, didEmitFrame frame: ParsedAACFrame)
    func aacFrameParser(_ parser: AACADTSFrameParser, didEncounterError reason: String)
}

/// Parses continuous AAC ADTS bitstreams into discrete audio frames.
public final class AACADTSFrameParser: @unchecked Sendable {

    private var buffer = Data()
    private var pendingPTS: CMTime?
    private var pendingPTS90k: UInt64?

    private static let sampleRateTable: [Int] = [
        96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350
    ]

    public weak var delegate: AACFrameParserDelegate?

    public init() {}

    public func reset() {
        buffer.removeAll(keepingCapacity: true)
        pendingPTS = nil
        pendingPTS90k = nil
    }

    public func feed(data: Data, pts: CMTime? = nil, pts90k: UInt64? = nil) {
        if let pts = pts, let pts90k = pts90k {
            self.pendingPTS = pts
            self.pendingPTS90k = pts90k
        }
        buffer.append(data)
        processBuffer()
    }

    private func processBuffer() {
        while buffer.count >= 7 {
            guard let syncIndex = findSyncword(in: buffer) else {
                if buffer.count > 1 {
                    buffer.removeSubrange(buffer.startIndex..<(buffer.endIndex - 1))
                }
                return
            }

            if syncIndex > buffer.startIndex {
                buffer.removeSubrange(buffer.startIndex..<syncIndex)
                if buffer.count < 7 { return }
            }

            guard let frameInfo = parseHeader(at: buffer.startIndex) else {
                buffer.removeFirst()
                continue
            }

            guard buffer.count >= frameInfo.frameSizeBytes else {
                // Wait for the full ADTS frame to arrive
                return
            }

            let fullFrame = buffer.subdata(in: buffer.startIndex..<(buffer.startIndex + frameInfo.frameSizeBytes))
            let rawPayload = fullFrame.dropFirst(frameInfo.headerSizeBytes)
            buffer.removeSubrange(buffer.startIndex..<(buffer.startIndex + frameInfo.frameSizeBytes))

            let framePTS = pendingPTS
            let framePTS90k = pendingPTS90k

            if let pts = pendingPTS {
                self.pendingPTS = pts + frameInfo.duration
                if let p90 = pendingPTS90k {
                    let ticks = UInt64((Int64(frameInfo.samplesPerFrame) * 90000) / Int64(frameInfo.sampleRate))
                    self.pendingPTS90k = p90 + ticks
                }
            }

            let frame = ParsedAACFrame(
                info: frameInfo,
                rawPayload: rawPayload,
                adtsData: fullFrame,
                pts: framePTS,
                pts90k: framePTS90k
            )
            delegate?.aacFrameParser(self, didEmitFrame: frame)
        }
    }

    private func findSyncword(in data: Data) -> Data.Index? {
        guard data.count >= 2 else { return nil }
        var i = data.startIndex
        let maxIndex = data.endIndex - 1
        while i < maxIndex {
            if data[i] == 0xFF && (data[i + 1] & 0xF0) == 0xF0 {
                return i
            }
            i += 1
        }
        return nil
    }

    private func parseHeader(at start: Data.Index) -> AACFrameInfo? {
        guard buffer.distance(from: start, to: buffer.endIndex) >= 7 else { return nil }

        let b0 = buffer[start]
        let b1 = buffer[start + 1]
        guard b0 == 0xFF && (b1 & 0xF0) == 0xF0 else { return nil }

        let protectionAbsent = (b1 & 0x01) == 1
        let headerSize = protectionAbsent ? 7 : 9

        let b2 = buffer[start + 2]
        let b3 = buffer[start + 3]
        let b4 = buffer[start + 4]
        let b5 = buffer[start + 5]
        let b6 = buffer[start + 6]

        let profile = (b2 >> 6) & 0x03
        let sampleRateIdx = (b2 >> 2) & 0x0F
        guard Int(sampleRateIdx) < Self.sampleRateTable.count else { return nil }
        let sampleRate = Self.sampleRateTable[Int(sampleRateIdx)]

        let channelConfig = ((b2 & 0x01) << 2) | ((b3 >> 6) & 0x03)
        let channelCount: Int
        switch channelConfig {
        case 1: channelCount = 1
        case 2: channelCount = 2
        case 3: channelCount = 3
        case 4: channelCount = 4
        case 5: channelCount = 5
        case 6: channelCount = 6
        case 7: channelCount = 8
        default: channelCount = 2
        }

        let frameLength = ((Int(b3) & 0x03) << 11) | (Int(b4) << 3) | ((Int(b5) >> 5) & 0x07)
        guard frameLength >= headerSize else { return nil }

        let numBlocks = Int(b6 & 0x03) + 1
        let samplesPerFrame = numBlocks * 1024

        return AACFrameInfo(
            profile: profile,
            sampleRate: sampleRate,
            sampleRateIndex: sampleRateIdx,
            channelCount: channelCount,
            channelConfig: channelConfig,
            frameSizeBytes: frameLength,
            headerSizeBytes: headerSize,
            samplesPerFrame: samplesPerFrame
        )
    }
}
