// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
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

/// The video codec a programme's PMT names for its video elementary stream.
///
/// The audio side has had this distinction from the start; the video side
/// classified four stream types as "video" and handed all of them to the H.264
/// access unit assembler. For anything that is not H.264 that produces no
/// picture at all, with nothing anywhere saying why — the same silent failure
/// `AudioStreamCodec.isDecodableOnDevice` exists to prevent.
public enum VideoStreamCodec: Sendable, Equatable, CustomStringConvertible {
    case h264
    case mpeg2
    case mpeg1
    case hevc
    case unknown(UInt8)

    public init(streamType: UInt8) {
        switch streamType {
        case 0x1B: self = .h264
        case 0x02: self = .mpeg2
        case 0x01: self = .mpeg1
        case 0x24: self = .hevc
        default: self = .unknown(streamType)
        }
    }

    public var description: String {
        switch self {
        case .h264: return "H.264"
        case .mpeg2: return "MPEG-2"
        case .mpeg1: return "MPEG-1"
        case .hevc: return "HEVC"
        case .unknown(let type): return "Unknown (0x\(String(type, radix: 16, uppercase: true)))"
        }
    }

    /// The VideoToolbox codec type, for the formats that have one here.
    ///
    /// MPEG-1 is excluded rather than assumed: its syntax is close enough to
    /// MPEG-2 that the same assembler would likely carry it, but no broadcast
    /// here sends it, so claiming support would be claiming something untested.
    public var videoToolboxCodecType: CMVideoCodecType? {
        switch self {
        case .h264: return kCMVideoCodecType_H264
        case .mpeg2: return kCMVideoCodecType_MPEG2Video
        case .hevc: return kCMVideoCodecType_HEVC
        case .mpeg1, .unknown: return nil
        }
    }

    /// Whether this pipeline can turn the stream into pictures.
    ///
    /// Two conditions, and both have to hold. There has to be an assembler here
    /// that cuts the elementary stream into access units — that part is our
    /// code, and it exists for H.264, MPEG-2 and HEVC. And VideoToolbox has to
    /// have a decoder to hand them to, which is asked at runtime rather than
    /// tabulated: iOS has never shipped an MPEG-2 decoder despite defining the
    /// constant, and HEVC is present on every device since the A9 but absent
    /// from the Simulator. Any fixed table is wrong on one of those.
    public var isDecodableOnDevice: Bool {
        guard let codecType = videoToolboxCodecType else { return false }
        return VideoDecoderAvailability.canDecode(codecType)
    }

    /// What to tell the viewer, in their terms, when this cannot be played.
    public var viewerDescription: String {
        switch self {
        case .h264: return "H.264"
        case .mpeg2, .mpeg1: return "ein älteres Format (MPEG-2)"
        case .hevc: return "ein hochauflösendes Format (HEVC)"
        case .unknown: return "ein unbekanntes Format"
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

    public var displayName: String {
        let langName: String
        switch (language ?? "").lowercased() {
        case "deu", "ger": langName = "Deutsch"
        case "eng": langName = "Englisch"
        case "fra", "fre": langName = "Französisch"
        case "ita": langName = "Italienisch"
        case "spa": langName = "Spanisch"
        case "qaa", "mis", "mul": langName = "Originalton"
        default:
            langName = language?.uppercased() ?? "Tonspur"
        }

        let typeSuffix: String
        switch audioType {
        case 1: typeSuffix = " (Clean Audio)"
        case 2: typeSuffix = " (Hörgeschädigte)"
        case 3: typeSuffix = " (Audiodeskription)"
        default: typeSuffix = ""
        }

        return "\(langName) – \(codec.description)\(typeSuffix)"
    }
}

/// The format and page identification of a DVB or Teletext subtitle track.
public enum SubtitleFormat: Sendable, Equatable, CustomStringConvertible {
    case dvb(compositionPageID: UInt16, ancillaryPageID: UInt16)
    case teletext(page: Int)

    public var description: String {
        switch self {
        case .dvb(let comp, let anc):
            return "DVB(comp: \(comp), anc: \(anc))"
        case .teletext(let page):
            return "Teletext(\(page))"
        }
    }
}

/// A subtitle track discovered in a programme's PMT (DVB Subtitles or Teletext Subtitles).
public struct SubtitleTrackInfo: Sendable, Equatable, Identifiable {
    public var id: String { "\(pid)_\(format.description)_\(language ?? "und")" }
    public let pid: UInt16
    public let format: SubtitleFormat
    public let language: String?
    public let isHearingImpaired: Bool

    public init(
        pid: UInt16,
        format: SubtitleFormat,
        language: String? = nil,
        isHearingImpaired: Bool = false
    ) {
        self.pid = pid
        self.format = format
        self.language = language
        self.isHearingImpaired = isHearingImpaired
    }

    public var displayName: String {
        let langName: String
        switch (language ?? "").lowercased() {
        case "deu", "ger": langName = "Deutsch"
        case "eng": langName = "Englisch"
        case "fra", "fre": langName = "Französisch"
        case "ita": langName = "Italienisch"
        case "spa": langName = "Spanisch"
        case "qaa", "mis", "mul": langName = "Originalton"
        default:
            langName = language?.uppercased() ?? "Untertitel"
        }

        var details: [String] = []
        switch format {
        case .dvb:
            details.append("DVB")
        case .teletext(let page):
            details.append("Teletext \(page)")
        }

        if isHearingImpaired {
            details.append("Hörgeschädigte")
        }

        return "\(langName) (\(details.joined(separator: ", ")))"
    }
}

/// An elementary stream the PMT named that could be audio, but that the parser
/// could not classify.
///
/// A channel whose only sound arrives this way plays silent, and until this
/// existed it did so without a single line anywhere: `parsePMT` only notifies the
/// delegate when it has produced at least one track, so "no track" and "no audio
/// on this service" were the same event. Streams a descriptor positively explains
/// as something other than audio - teletext, subtitles, application signalling,
/// data carousels - are not reported, because naming those would bury the case
/// that matters in noise.
public struct UnclassifiedStreamInfo: Sendable, Equatable {
    public let pid: UInt16
    public let streamType: UInt8
    public let descriptorTags: [UInt8]
    public let language: String?

    public init(pid: UInt16, streamType: UInt8, descriptorTags: [UInt8], language: String?) {
        self.pid = pid
        self.streamType = streamType
        self.descriptorTags = descriptorTags
        self.language = language
    }

    /// Descriptor tags that positively identify a non-audio elementary stream.
    ///
    /// `0x7F` is deliberately absent: the DVB extension descriptor is how AC-4 is
    /// signalled, so a stream carrying it is a candidate rather than an exclusion.
    static let nonAudioDescriptorTags: Set<UInt8> = [
        0x13, // carousel_identifier
        0x56, // teletext
        0x59, // subtitling
        0x6F  // application_signalling (HbbTV/AIT)
    ]

    /// Stream types that carry no elementary audio and need no explanation.
    ///
    /// `0x86` is SCTE-35 splice information, carried by 24 of the 144 services in
    /// the measured bouquet. It is signalling, not sound, and reporting it would
    /// have buried the one case this exists for under two dozen false positives on
    /// every channel change.
    static let nonAudioStreamTypes: Set<UInt8> = [
        0x05, // private sections
        0x0B, // DSM-CC sections
        0x0C, // DSM-CC descriptors
        0x86  // SCTE-35 splice information
    ]

    /// Registration descriptor format identifiers that name a non-audio stream.
    static let nonAudioRegistrations: Set<String> = ["CUEI", "SCTE"]

    static func isAudioCandidate(streamType: UInt8, descriptorTags: [UInt8], registration: String? = nil) -> Bool {
        if nonAudioStreamTypes.contains(streamType) { return false }
        if descriptorTags.contains(where: { nonAudioDescriptorTags.contains($0) }) { return false }
        if let registration, nonAudioRegistrations.contains(registration) { return false }
        return true
    }
}

public protocol TSPacketParserDelegate: AnyObject, Sendable {
    func tsParser(_ parser: TSPacketParser, didDiscoverVideoPID pid: UInt16)
    func tsParser(_ parser: TSPacketParser, didDetermineVideoCodec codec: VideoStreamCodec)
    func tsParser(_ parser: TSPacketParser, didDiscoverAudioTracks tracks: [AudioTrackInfo])
    func tsParser(_ parser: TSPacketParser, didDiscoverSubtitleTracks tracks: [SubtitleTrackInfo])
    func tsParser(_ parser: TSPacketParser, didEmitPayload data: Data, pid: UInt16, unitStart: Bool)
    func tsParser(_ parser: TSPacketParser, didEncounterContinuityErrorOnPID pid: UInt16, expected: UInt8, actual: UInt8)
    func tsParser(_ parser: TSPacketParser, didEncounterScrambledPacketOnPID pid: UInt16)
    func tsParser(_ parser: TSPacketParser, didObserveUnclassifiedStreams streams: [UnclassifiedStreamInfo])
}

public extension TSPacketParserDelegate {
    func tsParser(_ parser: TSPacketParser, didDetermineVideoCodec codec: VideoStreamCodec) {}
    func tsParser(_ parser: TSPacketParser, didDiscoverAudioTracks tracks: [AudioTrackInfo]) {}
    func tsParser(_ parser: TSPacketParser, didDiscoverSubtitleTracks tracks: [SubtitleTrackInfo]) {}
    func tsParser(_ parser: TSPacketParser, didEncounterScrambledPacketOnPID pid: UInt16) {}
    func tsParser(_ parser: TSPacketParser, didObserveUnclassifiedStreams streams: [UnclassifiedStreamInfo]) {}
}

/// MPEG-TS (ISO/IEC 13818-1) transport stream packet parser.
///
/// Features:
/// - 188-byte TS packet synchronization and recovery.
/// - PAT (Program Association Table) demuxing.
/// - PMT (Program Map Table) parsing to discover Video and Audio PIDs with DVB descriptors.
/// - Multi-track audio discovery (AC-3, E-AC-3, AAC, MPEG-Audio) and ISO 639 language tagging.
/// - Subtitle discovery (DVB bitmap subtitles & Teletext subtitles).
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

    /// The codec the PMT names for `videoPID`, when a PMT named it.
    ///
    /// `nil` when the video PID came from the PES start-code fallback, which
    /// identifies a video stream without saying what is in it.
    public private(set) var videoCodec: VideoStreamCodec?
    public private(set) var audioTracks: [AudioTrackInfo] = []
    public private(set) var audioPIDs = Set<UInt16>()
    public private(set) var subtitleTracks: [SubtitleTrackInfo] = []

    /// Streams the PMT named that could be audio but could not be classified.
    public private(set) var unclassifiedStreams: [UnclassifiedStreamInfo] = []
    private var continuityCounters: [UInt16: UInt8] = [:]

    /// Partially received PMT sections, keyed by the PID carrying them.
    private var pmtSectionBuffers: [UInt16: [UInt8]] = [:]

    /// Packets skipped on the video or audio PID because their payload was
    /// still scrambled.
    ///
    /// Deliberately not every scrambled packet in the stream. A receiver hands
    /// over the whole transport, and the services this app is not playing stay
    /// encrypted for their own reasons — counting those produced 24512 on a
    /// measured tune whose picture and sound were flawless, which is a number
    /// that can only mislead whoever reads it.
    public private(set) var scrambledPackets: Int = 0

    public weak var delegate: TSPacketParserDelegate?

    public init() {}

    public func reset() {
        buffer.removeAll(keepingCapacity: true)
        pmtPIDs.removeAll()
        videoPID = nil
        videoCodec = nil
        audioTracks.removeAll()
        audioPIDs.removeAll()
        subtitleTracks.removeAll()
        unclassifiedStreams.removeAll()
        continuityCounters.removeAll()
        pmtSectionBuffers.removeAll()
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
            // Skipped whatever the PID — a scrambled payload is not elementary
            // stream — but only reported for the two PIDs being played, so the
            // count means "your programme is not coming through" and nothing
            // else. Before the PMT names them there is nothing to compare
            // against; PSI itself is never scrambled and arrives in tens of
            // milliseconds, so that window costs at most a handful of packets.
            if pid == videoPID || audioPIDs.contains(pid) {
                scrambledPackets += 1
                delegate?.tsParser(self, didEncounterScrambledPacketOnPID: pid)
            }
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
            parsePMT(payload: payload, unitStart: payloadUnitStart, pid: pid)
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

    /// Collects a PMT section across as many transport packets as it takes.
    ///
    /// A section is not obliged to fit in one packet, and on a channel with many
    /// elementary streams it does not: Das Erste HD announces twelve, a 191-byte
    /// section against the 183 bytes one packet can carry. Requiring a single
    /// packet dropped that table whole, and silently — a table that never parses
    /// reports nothing, so no counter moved and no log line appeared.
    ///
    /// From outside it did not look like a table problem. Video played, because
    /// the video PID is also found by sniffing PES headers rather than from the
    /// table, so the picture came up in 1.6 s and looked correct. Then the master
    /// clock, which starts on audio, had no audio to start on. The display layer
    /// never reported readiness — 121 pulls, ready 0 — the field queue filled to
    /// its cap of 180 and shed everything after it: 1394 fields dropped against
    /// 13 delivered. The channel showed one still frame in silence while every
    /// metric in the app read healthy.
    ///
    /// Buffered per PID: a transport may carry several programmes, and their
    /// sections interleave.
    private func parsePMT(payload: Data, unitStart: Bool, pid: UInt16) {
        if unitStart {
            guard let pointerField = payload.first else { return }
            let start = 1 + Int(pointerField)
            guard start < payload.count else { pmtSectionBuffers[pid] = nil; return }
            pmtSectionBuffers[pid] = [UInt8](payload[start...])
        } else {
            // The tail of a section whose head was never seen: its offsets cannot
            // be trusted, and a stream joined mid-table is the normal case at
            // tune-in.
            guard pmtSectionBuffers[pid] != nil else { return }
            pmtSectionBuffers[pid]?.append(contentsOf: payload)
        }

        guard var section = pmtSectionBuffers[pid], section.count >= 3 else { return }
        guard section[0] == 0x02 else { pmtSectionBuffers[pid] = nil; return }

        let sectionLength = Int(UInt16(section[1] & 0x0F) << 8 | UInt16(section[2]))
        let sectionTotal = 3 + sectionLength
        // Still arriving.
        guard section.count >= sectionTotal else { return }

        section = Array(section.prefix(sectionTotal))
        pmtSectionBuffers[pid] = nil
        parsePMTSection(Data(section))
    }

    private func parsePMTSection(_ payload: Data) {
        let offset = 0
        guard offset + 12 < payload.count else { return }

        let sectionLength = Int(UInt16(payload[offset + 1] & 0x0F) << 8 | UInt16(payload[offset + 2]))

        let programInfoLength = Int(UInt16(payload[offset + 10] & 0x0F) << 8 | UInt16(payload[offset + 11]))
        var streamOffset = offset + 12 + programInfoLength
        let endOffset = offset + 3 + sectionLength - 4 // minus 4 CRC bytes

        var discoveredVideo: UInt16?
        var discoveredVideoCodec: VideoStreamCodec?
        var tracks: [AudioTrackInfo] = []
        var subtitleTracks: [SubtitleTrackInfo] = []
        var unclassified: [UnclassifiedStreamInfo] = []

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
            var registrationID: String?

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
                        registrationID = regStr
                        if regStr == "AC-3" {
                            isAC3Descriptor = true
                        } else if regStr == "EAC3" {
                            isEAC3Descriptor = true
                        }
                    }
                } else if tag == 0x59 {
                    // DVB Subtitling Descriptor (ETSI EN 300 468 Clause 6.2.41)
                    var entryOffset = tagDataStart
                    while entryOffset + 8 <= tagDataEnd {
                        let langBytes = [payload[entryOffset], payload[entryOffset + 1], payload[entryOffset + 2]]
                        let langStr = String(bytes: langBytes, encoding: .ascii)?.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
                        let subType = payload[entryOffset + 3]
                        let compPage = UInt16(payload[entryOffset + 4]) << 8 | UInt16(payload[entryOffset + 5])
                        let ancPage = UInt16(payload[entryOffset + 6]) << 8 | UInt16(payload[entryOffset + 7])

                        let isNormal = (0x10...0x15).contains(subType)
                        let isHardOfHearing = (0x20...0x25).contains(subType)
                        guard isNormal || isHardOfHearing else {
                            entryOffset += 8
                            continue
                        }

                        let subTrack = SubtitleTrackInfo(
                            pid: elementaryPID,
                            format: .dvb(compositionPageID: compPage, ancillaryPageID: ancPage),
                            language: (langStr?.isEmpty == false) ? langStr : nil,
                            isHearingImpaired: isHardOfHearing
                        )
                        subtitleTracks.append(subTrack)
                        entryOffset += 8
                    }
                } else if tag == 0x56 {
                    // Teletext Descriptor (ETSI EN 300 468 Clause 6.2.43 / ETSI EN 300 706)
                    var entryOffset = tagDataStart
                    while entryOffset + 5 <= tagDataEnd {
                        let langBytes = [payload[entryOffset], payload[entryOffset + 1], payload[entryOffset + 2]]
                        let langStr = String(bytes: langBytes, encoding: .ascii)?.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
                        let typeAndMag = payload[entryOffset + 3]
                        let ttxType = (typeAndMag >> 3) & 0x1F
                        let mag = Int(typeAndMag & 0x07)
                        let pageBCD = payload[entryOffset + 4]

                        // Subtitle pages are type 0x02 (normal) or 0x05 (hearing impaired)
                        if ttxType == 0x02 || ttxType == 0x05 {
                            let tens = Int((pageBCD >> 4) & 0x0F)
                            let units = Int(pageBCD & 0x0F)
                            guard tens <= 9 && units <= 9 else {
                                entryOffset += 5
                                continue
                            }

                            let magNumber = (mag == 0) ? 8 : mag
                            let pageNumber = magNumber * 100 + tens * 10 + units

                            let isHardOfHearing = (ttxType == 0x05)
                            let subTrack = SubtitleTrackInfo(
                                pid: elementaryPID,
                                format: .teletext(page: pageNumber),
                                language: (langStr?.isEmpty == false) ? langStr : nil,
                                isHearingImpaired: isHardOfHearing
                            )
                            subtitleTracks.append(subTrack)
                        }
                        entryOffset += 5
                    }
                }

                descOffset += 2 + len
            }

            // Stream classification
            if streamType == 0x1B || streamType == 0x24 || streamType == 0x02 || streamType == 0x01 {
                // Video stream
                if discoveredVideo == nil {
                    discoveredVideo = elementaryPID
                    discoveredVideoCodec = VideoStreamCodec(streamType: streamType)
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
                } else if UnclassifiedStreamInfo.isAudioCandidate(streamType: streamType, descriptorTags: descriptorTags, registration: registrationID) {
                    // Not a track - selecting it would be guessing - but it must not
                    // vanish. This is the only record that the PMT named something
                    // here at all.
                    unclassified.append(UnclassifiedStreamInfo(
                        pid: elementaryPID,
                        streamType: streamType,
                        descriptorTags: descriptorTags,
                        language: language
                    ))
                }
            }

            streamOffset += 5 + esInfoLength
        }

        if let vPid = discoveredVideo, self.videoPID != vPid {
            self.videoPID = vPid
            self.videoCodec = discoveredVideoCodec
            delegate?.tsParser(self, didDiscoverVideoPID: vPid)
            if let codec = discoveredVideoCodec {
                delegate?.tsParser(self, didDetermineVideoCodec: codec)
            }
        }

        if !tracks.isEmpty && self.audioTracks != tracks {
            self.audioTracks = tracks
            self.audioPIDs = Set(tracks.map(\.pid))
            delegate?.tsParser(self, didDiscoverAudioTracks: tracks)
        }

        if self.subtitleTracks != subtitleTracks {
            self.subtitleTracks = subtitleTracks
            delegate?.tsParser(self, didDiscoverSubtitleTracks: subtitleTracks)
        }

        // Reported independently of the track list, and precisely when that list is
        // empty: a service whose every audio stream is unclassifiable produces no
        // track, so the delegate call above never happens and nothing else would
        // ever say why the channel is silent.
        if !unclassified.isEmpty && self.unclassifiedStreams != unclassified {
            self.unclassifiedStreams = unclassified
            delegate?.tsParser(self, didObserveUnclassifiedStreams: unclassified)
        }
    }
}
