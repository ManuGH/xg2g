// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

public enum AudioStreamCodec: Sendable, Equatable, CustomStringConvertible {
    case ac3
    case eac3
    case aac
    case mpegAudio
    case unknown(UInt8)

    public var description: String {
        switch self {
        case .ac3: return "AC-3"
        case .eac3: return "E-AC-3"
        case .aac: return "AAC"
        case .mpegAudio: return "MPEG Audio"
        case .unknown(let type): return "Unknown (0x\(String(type, radix: 16, uppercase: true)))"
        }
    }

    /// Whether the platform can actually decode this, measured on device rather
    /// than assumed.
    ///
    /// `AVSampleBufferAudioRenderer` plays AC-3, E-AC-3 and AAC; MPEG Layer II —
    /// stream type 0x03/0x04, still carried by most German public broadcasters —
    /// has no decoder on iOS. Selecting it yields a silent stream with no error
    /// anywhere: the parser runs, buffers are produced, the renderer accepts
    /// them, and nothing is heard.
    public var isDecodableOnDevice: Bool {
        switch self {
        case .ac3, .eac3, .aac: return true
        case .mpegAudio, .unknown: return false
        }
    }
}

public struct AudioTrackInfo: Sendable, Equatable, Identifiable {
    public var id: UInt16 { pid }
    public let pid: UInt16
    public let streamType: UInt8
    public let codec: AudioStreamCodec
    public let language: String?
    public let audioType: UInt8
    public let descriptorTags: [UInt8]

    public init(
        pid: UInt16,
        streamType: UInt8,
        codec: AudioStreamCodec,
        language: String? = nil,
        audioType: UInt8 = 0,
        descriptorTags: [UInt8] = []
    ) {
        self.pid = pid
        self.streamType = streamType
        self.codec = codec
        self.language = language
        self.audioType = audioType
        self.descriptorTags = descriptorTags
    }
}

public protocol TSPacketParserDelegate: AnyObject, Sendable {
    func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16)
    func tsParser(_ parser: TSPacketParser, didDiscoverAudioTracks tracks: [AudioTrackInfo])
    func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool)
    func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8)
    func tsParser(_ parser: TSPacketParser, didEncounterScrambledPacketOnPID pid: UInt16)
}

public extension TSPacketParserDelegate {
    func tsParser(_ parser: TSPacketParser, didDiscoverAudioTracks tracks: [AudioTrackInfo]) {}
    func tsParser(_ parser: TSPacketParser, didEncounterScrambledPacketOnPID pid: UInt16) {}
}

/// MPEG-TS (ISO/IEC 13818-1) transport stream packet parser.
///
/// Features:
/// - 188-byte TS packet synchronization and recovery.
/// - PAT (Program Association Table) demuxing.
/// - PMT (Program Map Table) parsing to discover Video and Audio PIDs with DVB descriptors.
/// - Multi-track audio discovery (AC-3, E-AC-3, AAC, MPEG-Audio) and ISO 639 language tagging.
/// - Continuity counter tracking per PID.
/// - Adaptation field skipping and payload extraction.
public final class TSPacketParser: @unchecked Sendable {

    private static let packetSize = 188
    private static let syncByte: UInt8 = 0x47

    /// Stuffing packets, inserted to pad the multiplex to its constant rate.
    ///
    /// ISO/IEC 13818-1 leaves their continuity counter undefined, so checking it
    /// reports a fault on padding: a clean F1 multiplex measured 516 "errors" on
    /// this PID against 1 on 50.070 video packets, and the telemetry showed the
    /// sum as if the broadcast were breaking up. They carry nothing, so they are
    /// dropped before anything else looks at them.
    private static let nullPacketPID: UInt16 = 0x1FFF

    private var buffer = Data()
    private var pmtPIDs = Set<UInt16>()
    public private(set) var videoPID: UInt16?
    public private(set) var audioTracks: [AudioTrackInfo] = []
    public private(set) var audioPIDs = Set<UInt16>()
    private var continuityCounters: [UInt16: UInt8] = [:]

    /// Packets skipped because their payload was still scrambled.
    public private(set) var scrambledPackets: Int = 0

    public weak var delegate: TSPacketParserDelegate?

    public init() {}

    public func reset() {
        buffer.removeAll(keepingCapacity: true)
        pmtPIDs.removeAll()
        videoPID = nil
        audioTracks.removeAll()
        audioPIDs.removeAll()
        continuityCounters.removeAll()
        scrambledPackets = 0
    }

    /// Feeds raw MPEG-TS chunk from network or file stream.
    public func feed(data: Data) {
        buffer.append(data)
        processBuffer()
    }

    private func processBuffer() {
        guard !buffer.isEmpty else { return }

        var consumedBytes = 0
        buffer.withUnsafeBytes { rawBuffer in
            guard let baseAddress = rawBuffer.baseAddress else { return }
            let ptr = baseAddress.assumingMemoryBound(to: UInt8.self)
            var offset = 0
            let count = rawBuffer.count

            while count - offset >= Self.packetSize {
                // Find 0x47 sync byte
                var foundSync = false
                while offset < count {
                    if ptr[offset] == Self.syncByte {
                        foundSync = true
                        break
                    }
                    offset += 1
                }

                guard foundSync, count - offset >= Self.packetSize else {
                    break
                }

                let packetPtr = ptr.advanced(by: offset)
                parsePacket(bytes: packetPtr, length: Self.packetSize)
                offset += Self.packetSize
            }

            consumedBytes = offset
        }

        if consumedBytes > 0 {
            if consumedBytes >= buffer.count {
                buffer.removeAll(keepingCapacity: true)
            } else {
                buffer.removeSubrange(0..<consumedBytes)
            }
        }
    }

    private func parsePacket(bytes: UnsafePointer<UInt8>, length: Int) {
        guard length == Self.packetSize, bytes[0] == Self.syncByte else { return }

        let byte1 = bytes[1]
        let byte2 = bytes[2]
        let byte3 = bytes[3]

        let transportError = (byte1 & 0x80) != 0
        if transportError { return }

        let payloadUnitStart = (byte1 & 0x40) != 0
        let pid = UInt16(byte1 & 0x1F) << 8 | UInt16(byte2)
        guard pid != Self.nullPacketPID else { return }

        // A scrambled payload is not elementary stream yet, and parsing it as one
        // produces exactly what was being counted as a PES error.
        //
        // The receiver descrambles, but not instantly: on an encrypted service the
        // first packets after a tune arrive before the CA module has the key, and
        // they are marked as such. Feeding them to the PES assembler turns a
        // normal part of tune-in into a burst of parse failures — 262 of them in
        // one measured session, against 3 continuity errors, which is what said
        // the data itself was arriving intact.
        let scramblingControl = (byte3 & 0xC0) >> 6
        guard scramblingControl == 0 else {
            scrambledPackets += 1
            delegate?.tsParser(self, didEncounterScrambledPacketOnPID: pid)
            return
        }

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
            let adaptationLength = Int(bytes[4])
            payloadOffset = 5 + adaptationLength
            if payloadOffset > Self.packetSize { return }
        }

        // Check if there is payload
        guard (adaptationControl == 0x01 || adaptationControl == 0x03), payloadOffset < Self.packetSize else {
            return
        }

        let payloadLength = Self.packetSize - payloadOffset
        let payload = Data(bytes: bytes.advanced(by: payloadOffset), count: payloadLength)

        if pid == 0 {
            // PAT
            parsePAT(payload: payload, unitStart: payloadUnitStart)
        } else if pmtPIDs.contains(pid) {
            // PMT
            parsePMT(payload: payload, unitStart: payloadUnitStart)
        } else if let vPid = videoPID, pid == vPid {
            // Video Elementary Stream
            delegate?.tsParser(self, didEmitPayload: payload, pid: pid, unitStart: payloadUnitStart)
        } else if audioPIDs.contains(pid) {
            // Audio Elementary Stream
            delegate?.tsParser(self, didEmitPayload: payload, pid: pid, unitStart: payloadUnitStart)
        } else if videoPID == nil && payloadUnitStart && payloadLength >= 4 {
            // Fast-track auto-detection of video PID from PES video start code (0x000001E0 - 0x000001EF)
            if bytes[payloadOffset] == 0x00 && bytes[payloadOffset + 1] == 0x00 && bytes[payloadOffset + 2] == 0x01 && (bytes[payloadOffset + 3] >= 0xE0 && bytes[payloadOffset + 3] <= 0xEF) {
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

        var discoveredVideo: UInt16?
        var tracks: [AudioTrackInfo] = []

        while streamOffset + 5 <= endOffset {
            let streamType = payload[streamOffset]
            let elementaryPID = UInt16(payload[streamOffset + 1] & 0x1F) << 8 | UInt16(payload[streamOffset + 2])
            let esInfoLength = Int(UInt16(payload[streamOffset + 3] & 0x0F) << 8 | UInt16(payload[streamOffset + 4]))
            let descStart = streamOffset + 5
            let descEnd = min(descStart + esInfoLength, endOffset)

            // Parse ES descriptors
            var descOffset = descStart
            var descriptorTags: [UInt8] = []
            var language: String?
            var audioType: UInt8 = 0
            var isAC3Descriptor = false
            var isEAC3Descriptor = false

            while descOffset + 2 <= descEnd {
                let tag = payload[descOffset]
                let len = Int(payload[descOffset + 1])
                descriptorTags.append(tag)

                let tagDataStart = descOffset + 2
                let tagDataEnd = min(tagDataStart + len, descEnd)

                if tag == 0x6A {
                    // DVB AC-3 descriptor
                    isAC3Descriptor = true
                } else if tag == 0x7A {
                    // DVB Enhanced AC-3 descriptor
                    isEAC3Descriptor = true
                } else if tag == 0x0A && tagDataStart + 3 <= tagDataEnd {
                    // ISO 639-2 language descriptor
                    let langBytes = [payload[tagDataStart], payload[tagDataStart + 1], payload[tagDataStart + 2]]
                    if let langStr = String(bytes: langBytes, encoding: .ascii)?.trimmingCharacters(in: .whitespacesAndNewlines), !langStr.isEmpty {
                        language = langStr.lowercased()
                    }
                    if tagDataStart + 4 <= tagDataEnd {
                        audioType = payload[tagDataStart + 3]
                    }
                } else if tag == 0x05 && tagDataStart + 4 <= tagDataEnd {
                    // Registration descriptor
                    let regBytes = [payload[tagDataStart], payload[tagDataStart + 1], payload[tagDataStart + 2], payload[tagDataStart + 3]]
                    if let regStr = String(bytes: regBytes, encoding: .ascii) {
                        if regStr == "AC-3" {
                            isAC3Descriptor = true
                        } else if regStr == "EAC3" {
                            isEAC3Descriptor = true
                        }
                    }
                }

                descOffset += 2 + len
            }

            // Stream classification
            if streamType == 0x1B || streamType == 0x24 || streamType == 0x02 || streamType == 0x01 {
                // Video stream
                if discoveredVideo == nil {
                    discoveredVideo = elementaryPID
                }
            } else {
                // Audio stream
                let codec: AudioStreamCodec?
                switch streamType {
                case 0x03, 0x04:
                    codec = .mpegAudio
                case 0x0F, 0x11:
                    codec = .aac
                case 0x81:
                    codec = .ac3
                case 0x87:
                    codec = .eac3
                case 0x06:
                    // DVB Private PES: Check descriptors
                    if isEAC3Descriptor {
                        codec = .eac3
                    } else if isAC3Descriptor {
                        codec = .ac3
                    } else {
                        codec = nil
                    }
                default:
                    codec = nil
                }

                if let codec = codec {
                    let track = AudioTrackInfo(
                        pid: elementaryPID,
                        streamType: streamType,
                        codec: codec,
                        language: language,
                        audioType: audioType,
                        descriptorTags: descriptorTags
                    )
                    tracks.append(track)
                }
            }

            streamOffset += 5 + esInfoLength
        }

        if let vPid = discoveredVideo, self.videoPID != vPid {
            self.videoPID = vPid
            delegate?.tsParser(self, didDiscoverVideoPID: vPid)
        }

        if !tracks.isEmpty && self.audioTracks != tracks {
            self.audioTracks = tracks
            self.audioPIDs = Set(tracks.map(\.pid))
            delegate?.tsParser(self, didDiscoverAudioTracks: tracks)
        }
    }
}
