// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AudioToolbox
import CoreMedia
import Foundation

public protocol AudioSampleBufferAssemblerDelegate: AnyObject, Sendable {
    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didUpdateFormat formatDescription: CMAudioFormatDescription, info: AC3FrameInfo)
    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didEmitSampleBuffer sampleBuffer: CMSampleBuffer, info: AC3FrameInfo)
    func audioSampleBufferAssembler(_ assembler: AudioSampleBufferAssembler, didEncounterError reason: String)
}

/// Converts parsed audio syncframes into valid CoreMedia `CMSampleBuffer`s.
public final class AudioSampleBufferAssembler: @unchecked Sendable, AC3FrameParserDelegate {

    public private(set) var currentFormatDescription: CMAudioFormatDescription?
    public private(set) var currentFrameInfo: AC3FrameInfo?

    public weak var delegate: AudioSampleBufferAssemblerDelegate?

    public init() {}

    public func reset() {
        currentFormatDescription = nil
        currentFrameInfo = nil
    }

    // MARK: - AC3FrameParserDelegate

    public func ac3FrameParser(_ parser: AC3FrameParser, didEmitFrame frame: ParsedAudioFrame) {
        let info = frame.info

        // Update format description if audio properties changed
        if currentFormatDescription == nil || currentFrameInfo != info {
            if let newFormat = createAudioFormatDescription(for: info) {
                self.currentFormatDescription = newFormat
                self.currentFrameInfo = info
                delegate?.audioSampleBufferAssembler(self, didUpdateFormat: newFormat, info: info)
            } else {
                delegate?.audioSampleBufferAssembler(self, didEncounterError: "Failed to create CMAudioFormatDescription for \(info.isEnhanced ? "E-AC-3" : "AC-3")")
                return
            }
        }

        guard let formatDesc = currentFormatDescription else { return }

        // Create CMBlockBuffer
        var blockBuffer: CMBlockBuffer?
        let status = CMBlockBufferCreateWithMemoryBlock(
            allocator: kCFAllocatorDefault,
            memoryBlock: nil,
            blockLength: frame.data.count,
            blockAllocator: kCFAllocatorDefault,
            customBlockSource: nil,
            offsetToData: 0,
            dataLength: frame.data.count,
            flags: 0,
            blockBufferOut: &blockBuffer
        )

        guard status == noErr, let buffer = blockBuffer else {
            delegate?.audioSampleBufferAssembler(self, didEncounterError: "Failed to create CMBlockBuffer (status \(status))")
            return
        }

        // Copy frame bytes into block buffer
        frame.data.withUnsafeBytes { rawBuffer in
            if let base = rawBuffer.baseAddress {
                CMBlockBufferReplaceDataBytes(
                    with: base,
                    blockBuffer: buffer,
                    offsetIntoDestination: 0,
                    dataLength: frame.data.count
                )
            }
        }

        // Prepare packet descriptions and timing info
        var packetDesc = AudioStreamPacketDescription(
            mStartOffset: 0,
            mVariableFramesInPacket: 0,
            mDataByteSize: UInt32(frame.data.count)
        )

        let pts = frame.pts ?? .invalid
        var sampleBuffer: CMSampleBuffer?
        let sampleBufferStatus = withUnsafePointer(to: &packetDesc) { descPtr in
            CMAudioSampleBufferCreateWithPacketDescriptions(
                allocator: kCFAllocatorDefault,
                dataBuffer: buffer,
                dataReady: true,
                makeDataReadyCallback: nil,
                refcon: nil,
                formatDescription: formatDesc,
                sampleCount: 1,
                presentationTimeStamp: pts,
                packetDescriptions: descPtr,
                sampleBufferOut: &sampleBuffer
            )
        }

        guard sampleBufferStatus == noErr, let sb = sampleBuffer else {
            delegate?.audioSampleBufferAssembler(self, didEncounterError: "Failed to create CMSampleBuffer (status \(sampleBufferStatus))")
            return
        }

        delegate?.audioSampleBufferAssembler(self, didEmitSampleBuffer: sb, info: info)
    }

    public func ac3FrameParser(_ parser: AC3FrameParser, didEncounterError reason: String) {
        delegate?.audioSampleBufferAssembler(self, didEncounterError: reason)
    }

    // MARK: - Format Description Helper

    private func createAudioFormatDescription(for info: AC3FrameInfo) -> CMAudioFormatDescription? {
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

        if status == noErr {
            return format
        }
        return nil
    }
}
