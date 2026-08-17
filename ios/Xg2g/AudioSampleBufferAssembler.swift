// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AudioToolbox
import CoreMedia
import Foundation

public protocol AudioSampleBufferAssemblerDelegate: AnyObject, Sendable {
    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didUpdateFormat formatDescription: CMAudioFormatDescription, codec: AudioStreamCodec, sampleRate: Int, channels: Int, bitrateKbps: Int)
    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didEmitSampleBuffer sampleBuffer: CMSampleBuffer, codec: AudioStreamCodec, duration: CMTime)
    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didEncounterError reason: String)
}

/// Converts parsed AC-3, E-AC-3, and AAC audio syncframes into valid CoreMedia `CMSampleBuffer`s.
public final class AudioSampleBufferAssembler: @unchecked Sendable, AC3FrameParserDelegate, AACFrameParserDelegate {

    public private(set) var currentFormatDescription: CMAudioFormatDescription?
    public private(set) var currentAC3FrameInfo: AC3FrameInfo?
    public private(set) var currentAACFrameInfo: AACFrameInfo?

    public weak var delegate: AudioSampleBufferAssemblerDelegate?

    public init() {}

    public func reset() {
        currentFormatDescription = nil
        currentAC3FrameInfo = nil
        currentAACFrameInfo = nil
    }

    // MARK: - AC3FrameParserDelegate

    public func ac3FrameParser(_ parser: AC3FrameParser, didEmitFrame frame: ParsedAudioFrame) {
        let info = frame.info

        // Update format description if audio properties changed
        if currentFormatDescription == nil || currentAC3FrameInfo != info {
            if let newFormat = createAC3FormatDescription(for: info) {
                self.currentFormatDescription = newFormat
                self.currentAC3FrameInfo = info
                self.currentAACFrameInfo = nil
                let codec: AudioStreamCodec = info.isEnhanced ? .eac3 : .ac3
                delegate?.audioSampleBufferAssembler(self, didUpdateFormat: newFormat, codec: codec, sampleRate: info.sampleRate, channels: info.channelCount, bitrateKbps: info.bitrateKbps)
            } else {
                delegate?.audioSampleBufferAssembler(self, didEncounterError: "Failed to create CMAudioFormatDescription for \(info.isEnhanced ? "E-AC-3" : "AC-3")")
                return
            }
        }

        guard let formatDesc = currentFormatDescription else { return }

        emitSampleBuffer(data: frame.data, formatDescription: formatDesc, pts: frame.pts, codec: info.isEnhanced ? .eac3 : .ac3, duration: info.duration)
    }

    public func ac3FrameParser(_ parser: AC3FrameParser, didEncounterError reason: String) {
        delegate?.audioSampleBufferAssembler(self, didEncounterError: reason)
    }

    // MARK: - AACFrameParserDelegate

    public func aacFrameParser(_ parser: AACADTSFrameParser, didEmitFrame frame: ParsedAACFrame) {
        let info = frame.info

        if currentFormatDescription == nil || currentAACFrameInfo != info {
            if let newFormat = createAACFormatDescription(for: info) {
                self.currentFormatDescription = newFormat
                self.currentAACFrameInfo = info
                self.currentAC3FrameInfo = nil
                delegate?.audioSampleBufferAssembler(self, didUpdateFormat: newFormat, codec: .aac, sampleRate: info.sampleRate, channels: info.channelCount, bitrateKbps: 0)
            } else {
                delegate?.audioSampleBufferAssembler(self, didEncounterError: "Failed to create CMAudioFormatDescription for AAC")
                return
            }
        }

        guard let formatDesc = currentFormatDescription else { return }

        emitSampleBuffer(data: frame.rawPayload, formatDescription: formatDesc, pts: frame.pts, codec: .aac, duration: info.duration)
    }

    public func aacFrameParser(_ parser: AACADTSFrameParser, didEncounterError reason: String) {
        delegate?.audioSampleBufferAssembler(self, didEncounterError: reason)
    }

    // MARK: - SampleBuffer Construction

    private func emitSampleBuffer(data: Data, formatDescription: CMAudioFormatDescription, pts: CMTime?, codec: AudioStreamCodec, duration: CMTime) {
        var blockBuffer: CMBlockBuffer?
        let status = CMBlockBufferCreateWithMemoryBlock(
            allocator: kCFAllocatorDefault,
            memoryBlock: nil,
            blockLength: data.count,
            blockAllocator: kCFAllocatorDefault,
            customBlockSource: nil,
            offsetToData: 0,
            dataLength: data.count,
            flags: 0,
            blockBufferOut: &blockBuffer
        )

        guard status == noErr, let buffer = blockBuffer else {
            delegate?.audioSampleBufferAssembler(self, didEncounterError: "Failed to create CMBlockBuffer (status \(status))")
            return
        }

        data.withUnsafeBytes { rawBuffer in
            if let base = rawBuffer.baseAddress {
                CMBlockBufferReplaceDataBytes(
                    with: base,
                    blockBuffer: buffer,
                    offsetIntoDestination: 0,
                    dataLength: data.count
                )
            }
        }

        var packetDesc = AudioStreamPacketDescription(
            mStartOffset: 0,
            mVariableFramesInPacket: 0,
            mDataByteSize: UInt32(data.count)
        )

        let presentationPTS = pts ?? .invalid
        var sampleBuffer: CMSampleBuffer?
        let sampleBufferStatus = withUnsafePointer(to: &packetDesc) { descPtr in
            CMAudioSampleBufferCreateWithPacketDescriptions(
                allocator: kCFAllocatorDefault,
                dataBuffer: buffer,
                dataReady: true,
                makeDataReadyCallback: nil,
                refcon: nil,
                formatDescription: formatDescription,
                sampleCount: 1,
                presentationTimeStamp: presentationPTS,
                packetDescriptions: descPtr,
                sampleBufferOut: &sampleBuffer
            )
        }

        guard sampleBufferStatus == noErr, let sb = sampleBuffer else {
            delegate?.audioSampleBufferAssembler(self, didEncounterError: "Failed to create CMSampleBuffer (status \(sampleBufferStatus))")
            return
        }

        delegate?.audioSampleBufferAssembler(self, didEmitSampleBuffer: sb, codec: codec, duration: duration)
    }

    // MARK: - Format Description Helpers

    private func createAC3FormatDescription(for info: AC3FrameInfo) -> CMAudioFormatDescription? {
        let formatID: AudioFormatID = info.isEnhanced ? kAudioFormatEnhancedAC3 : kAudioFormatAC3

        var asbd = AudioStreamBasicDescription(
            mSampleRate: Float64(info.sampleRate),
            mFormatID: formatID,
            mFormatFlags: 0,
            mBytesPerPacket: 0,
            mFramesPerPacket: UInt32(info.samplesPerFrame),
            mBytesPerFrame: 0,
            mChannelsPerFrame: UInt32(info.channelCount),
            mBitsPerChannel: 0,
            mReserved: 0
        )

        var format: CMAudioFormatDescription?
        let status = CMAudioFormatDescriptionCreate(
            allocator: kCFAllocatorDefault,
            asbd: &asbd,
            layoutSize: 0,
            layout: nil,
            magicCookieSize: 0,
            magicCookie: nil,
            extensions: nil,
            formatDescriptionOut: &format
        )

        return status == noErr ? format : nil
    }

    private func createAACFormatDescription(for info: AACFrameInfo) -> CMAudioFormatDescription? {
        var asbd = AudioStreamBasicDescription(
            mSampleRate: Float64(info.sampleRate),
            mFormatID: kAudioFormatMPEG4AAC,
            mFormatFlags: AudioFormatFlags(MPEG4ObjectID.AAC_LC.rawValue),
            mBytesPerPacket: 0,
            mFramesPerPacket: UInt32(info.samplesPerFrame),
            mBytesPerFrame: 0,
            mChannelsPerFrame: UInt32(info.channelCount),
            mBitsPerChannel: 0,
            mReserved: 0
        )

        var format: CMAudioFormatDescription?
        let cookieData = info.audioSpecificConfig
        let status = cookieData.withUnsafeBytes { rawCookie in
            CMAudioFormatDescriptionCreate(
                allocator: kCFAllocatorDefault,
                asbd: &asbd,
                layoutSize: 0,
                layout: nil,
                magicCookieSize: cookieData.count,
                magicCookie: rawCookie.baseAddress,
                extensions: nil,
                formatDescriptionOut: &format
            )
        }

        return status == noErr ? format : nil
    }
}
