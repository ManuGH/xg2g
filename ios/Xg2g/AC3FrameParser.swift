// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation

/// Parsed metadata for a discrete AC-3 or E-AC-3 audio syncframe.
public struct AC3FrameInfo: Sendable, Equatable {
    public let isEnhanced: Bool
    public let sampleRate: Int
    public let channelCount: Int
    public let isLFEOn: Bool
    public let bitrateKbps: Int
    public let frameSizeBytes: Int
    public let samplesPerFrame: Int
    public let duration: CMTime

    public init(
        isEnhanced: Bool,
        sampleRate: Int,
        channelCount: Int,
        isLFEOn: Bool,
        bitrateKbps: Int,
        frameSizeBytes: Int,
        samplesPerFrame: Int
    ) {
        self.isEnhanced = isEnhanced
        self.sampleRate = sampleRate
        self.channelCount = channelCount
        self.isLFEOn = isLFEOn
        self.bitrateKbps = bitrateKbps
        self.frameSizeBytes = frameSizeBytes
        self.samplesPerFrame = samplesPerFrame
        self.duration = CMTime(value: CMTimeValue(samplesPerFrame), timescale: CMTimeScale(sampleRate))
    }
}

/// A discrete, sliced audio syncframe with its metadata and estimated PTS.
public struct ParsedAudioFrame: Sendable {
    public let info: AC3FrameInfo
    public let data: Data
    public let pts: CMTime?
    public let pts90k: UInt64?

    public init(info: AC3FrameInfo, data: Data, pts: CMTime?, pts90k: UInt64?) {
        self.info = info
        self.data = data
        self.pts = pts
        self.pts90k = pts90k
    }
}

public protocol AC3FrameParserDelegate: AnyObject, Sendable {
    func ac3FrameParser(_ parser: AC3FrameParser, didEmitFrame frame: ParsedAudioFrame)
    func ac3FrameParser(_ parser: AC3FrameParser, didEncounterError reason: String)
}

/// Parses raw AC-3 / E-AC-3 elementary bitstreams into discrete audio syncframes.
///
/// Principles:
/// - PES packet boundaries DO NOT equal audio frame boundaries.
/// - Ingests continuous chunks, synchronizes to `0x0B77`, and extracts complete frames.
/// - Calculates frame durations dynamically (especially for E-AC-3 variable block structures).
public final class AC3FrameParser: @unchecked Sendable {

    private var buffer = Data()
    private var runningPTS: CMTime?
    private var runningPTS90k: UInt64?
    private var normalizer = PTS33BitNormalizer()

    public weak var delegate: AC3FrameParserDelegate?

    public init() {}

    public func reset() {
        buffer.removeAll(keepingCapacity: true)
        runningPTS = nil
        runningPTS90k = nil
        normalizer.reset()
    }

    /// Ingests elementary stream data chunk from an audio PES packet.
    public func feed(data: Data, pts: CMTime? = nil, pts90k: UInt64? = nil) {
        if let pts = pts, pts.isValid {
            if let running = runningPTS, running.isValid {
                let drift = abs(pts.seconds - running.seconds)
                // If stream timestamps jump by > 80 ms (e.g. channel zap or splice), resync timeline
                if drift > 0.080 {
                    self.runningPTS = pts
                    self.runningPTS90k = pts90k
                }
            } else {
                self.runningPTS = pts
                self.runningPTS90k = pts90k
            }
        }
        buffer.append(data)
        processBuffer()
    }

    private func processBuffer() {
        while buffer.count >= 7 { // Minimum header bytes to parse AC-3 / E-AC-3
            // Search for 0x0B 0x77 syncword
            guard let syncIndex = findSyncword(in: buffer) else {
                if buffer.count > 1 {
                    // Keep last byte in case it's 0x0B
                    buffer.removeSubrange(buffer.startIndex..<(buffer.endIndex - 1))
                }
                return
            }

            if syncIndex > buffer.startIndex {
                buffer.removeSubrange(buffer.startIndex..<syncIndex)
                if buffer.count < 7 { return }
            }

            // Try parsing frame header
            guard let frameInfo = parseHeader(at: buffer.startIndex) else {
                // False syncword, skip 0x0B byte and keep searching
                buffer.removeFirst()
                continue
            }

            guard buffer.count >= frameInfo.frameSizeBytes else {
                // Waiting for remainder of frame to arrive
                return
            }

            let frameData = buffer.subdata(in: buffer.startIndex..<(buffer.startIndex + frameInfo.frameSizeBytes))
            buffer.removeSubrange(buffer.startIndex..<(buffer.startIndex + frameInfo.frameSizeBytes))

            let framePTS = runningPTS
            let framePTS90k = runningPTS90k

            if let pts = runningPTS {
                // Advance continuous sample-accurate audio timebase by exact frame duration
                self.runningPTS = pts + frameInfo.duration
                if let p90 = runningPTS90k {
                    let ticks = UInt64((Int64(frameInfo.samplesPerFrame) * 90000) / Int64(frameInfo.sampleRate))
                    self.runningPTS90k = p90 + ticks
                }
            }

            let frame = ParsedAudioFrame(
                info: frameInfo,
                data: frameData,
                pts: framePTS,
                pts90k: framePTS90k
            )
            delegate?.ac3FrameParser(self, didEmitFrame: frame)
        }
    }

    private func findSyncword(in data: Data) -> Data.Index? {
        guard data.count >= 2 else { return nil }
        var i = data.startIndex
        let maxIndex = data.endIndex - 1
        while i < maxIndex {
            if data[i] == 0x0B && data[i + 1] == 0x77 {
                return i
            }
            i += 1
        }
        return nil
    }

    private func parseHeader(at start: Data.Index) -> AC3FrameInfo? {
        guard buffer.distance(from: start, to: buffer.endIndex) >= 7 else { return nil }

        let b0 = buffer[start]
        let b1 = buffer[start + 1]
        guard b0 == 0x0B && b1 == 0x77 else { return nil }

        let b4 = buffer[start + 4]
        let b5 = buffer[start + 5]

        let bsid = Int((b5 >> 3) & 0x1F)

        if bsid <= 8 {
            // Standard AC-3 (ATSC A/52)
            return parseStandardAC3Header(at: start)
        } else if bsid == 16 {
            // Enhanced AC-3 (E-AC-3 / Dolby Digital Plus)
            return parseEAC3Header(at: start)
        } else {
            // Other bsid variants (e.g. alternate bitstreams 9..10)
            return parseStandardAC3Header(at: start)
        }
    }

    private func parseStandardAC3Header(at start: Data.Index) -> AC3FrameInfo? {
        let b4 = buffer[start + 4]
        let fscod = Int((b4 >> 6) & 0x03)
        let frmsizecod = Int(b4 & 0x3F)

        guard fscod < 3, frmsizecod < 38 else { return nil }

        let sampleRate: Int
        switch fscod {
        case 0: sampleRate = 48000
        case 1: sampleRate = 44100
        case 2: sampleRate = 32000
        default: return nil
        }

        let frameSizeBytes = Self.ac3FrameSizes[fscod][frmsizecod]
        guard frameSizeBytes > 0 else { return nil }

        let bitrate = Self.ac3BitratesKbps[frmsizecod / 2]

        // Parse channel config from byte 6 (bsmod/acmod)
        let b6 = buffer[start + 6]
        let acmod = Int((b6 >> 5) & 0x07)

        var channelCount: Int
        switch acmod {
        case 0: channelCount = 2 // 1+1 Dual mono
        case 1: channelCount = 1 // 1/0 Mono
        case 2: channelCount = 2 // 2/0 Stereo
        case 3: channelCount = 3 // 3/0 L, C, R
        case 4: channelCount = 3 // 2/1 L, R, S
        case 5: channelCount = 4 // 3/1 L, C, R, S
        case 6: channelCount = 4 // 2/2 L, R, SL, SR
        case 7: channelCount = 5 // 3/2 L, C, R, SL, SR
        default: channelCount = 2
        }

        // Check LFE bit
        var bitOffset = 6 * 8 + 3 // After acmod (3 bits into byte 6)
        if (acmod & 0x01) != 0 && acmod != 1 {
            bitOffset += 2 // cmixlev
        }
        if (acmod & 0x04) != 0 {
            bitOffset += 2 // surmixlev
        }
        if acmod == 0x02 {
            bitOffset += 2 // dsurmod
        }

        var isLFEOn = false
        let lfeByteIdx = start + (bitOffset / 8)
        if lfeByteIdx < buffer.endIndex {
            let lfeBitShift = 7 - (bitOffset % 8)
            let lfeBit = (buffer[lfeByteIdx] >> lfeBitShift) & 0x01
            if lfeBit == 1 {
                isLFEOn = true
                channelCount += 1
            }
        }

        return AC3FrameInfo(
            isEnhanced: false,
            sampleRate: sampleRate,
            channelCount: channelCount,
            isLFEOn: isLFEOn,
            bitrateKbps: bitrate,
            frameSizeBytes: frameSizeBytes,
            samplesPerFrame: 1536 // Always 1536 for AC-3
        )
    }

    private func parseEAC3Header(at start: Data.Index) -> AC3FrameInfo? {
        let b2 = buffer[start + 2]
        let b3 = buffer[start + 3]
        let b4 = buffer[start + 4]

        let frmsiz = (Int(b2 & 0x07) << 8) | Int(b3)
        let frameSizeBytes = (frmsiz + 1) * 2

        let fscod = Int((b4 >> 6) & 0x03)
        var sampleRate = 48000
        var numblkscod = 3 // default: 6 blocks (1536 samples)

        if fscod == 3 {
            let fscod2 = Int((b4 >> 4) & 0x03)
            numblkscod = 3
            switch fscod2 {
            case 0: sampleRate = 24000
            case 1: sampleRate = 22050
            case 2: sampleRate = 16000
            default: sampleRate = 24000
            }
        } else {
            numblkscod = Int((b4 >> 4) & 0x03)
            switch fscod {
            case 0: sampleRate = 48000
            case 1: sampleRate = 44100
            case 2: sampleRate = 32000
            default: sampleRate = 48000
            }
        }

        let blocks: Int
        switch numblkscod {
        case 0: blocks = 1
        case 1: blocks = 2
        case 2: blocks = 3
        case 3: blocks = 6
        default: blocks = 6
        }
        let samplesPerFrame = blocks * 256

        let acmod = Int((b4 >> 1) & 0x07)
        let lfeon = Int(b4 & 0x01)

        var channelCount: Int
        switch acmod {
        case 0: channelCount = 2
        case 1: channelCount = 1
        case 2: channelCount = 2
        case 3: channelCount = 3
        case 4: channelCount = 3
        case 5: channelCount = 4
        case 6: channelCount = 4
        case 7: channelCount = 5
        default: channelCount = 2
        }
        let isLFE = (lfeon == 1)
        if isLFE {
            channelCount += 1
        }

        let bitrate = (frameSizeBytes * 8 * sampleRate) / (samplesPerFrame * 1000)

        return AC3FrameInfo(
            isEnhanced: true,
            sampleRate: sampleRate,
            channelCount: channelCount,
            isLFEOn: isLFE,
            bitrateKbps: bitrate,
            frameSizeBytes: frameSizeBytes,
            samplesPerFrame: samplesPerFrame
        )
    }

    // MARK: - ATSC A/52 Table 5.18 Lookup Tables

    private static let ac3BitratesKbps = [
        32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384, 448, 512, 576, 640
    ]

    // Frame size in bytes for fscod (0: 48k, 1: 44.1k, 2: 32k) and frmsizecod (0..37)
    private static let ac3FrameSizes: [[Int]] = [
        // 48 kHz
        [
            128, 128, 160, 160, 192, 192, 224, 224, 256, 256,
            320, 320, 384, 384, 448, 448, 512, 512, 640, 640,
            768, 768, 896, 896, 1024, 1024, 1280, 1280, 1536, 1536,
            1792, 1792, 2048, 2048, 2304, 2304, 2560, 2560
        ],
        // 44.1 kHz
        [
            138, 140, 174, 174, 208, 210, 242, 244, 278, 280,
            348, 348, 416, 418, 486, 488, 556, 558, 696, 696,
            834, 836, 974, 976, 1114, 1114, 1392, 1394, 1670, 1672,
            1950, 1950, 2228, 2230, 2506, 2508, 2786, 2786
        ],
        // 32 kHz
        [
            192, 192, 240, 240, 288, 288, 336, 336, 384, 384,
            480, 480, 576, 576, 672, 672, 768, 768, 960, 960,
            1152, 1152, 1344, 1344, 1536, 1536, 1920, 1920, 2304, 2304,
            2688, 2688, 3072, 3072, 3456, 3456, 3840, 3840
        ]
    ]
}
