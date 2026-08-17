// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreMedia
import Foundation
import VideoToolbox

public struct DecodedVideoFrame: @unchecked Sendable {
    public let pixelBuffer: CVPixelBuffer
    public let pts: CMTime
    public let isTopFieldFirst: Bool
}

public protocol HardwareVideoDecoderDelegate: AnyObject, Sendable {
    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEmitFrame frame: DecodedVideoFrame)
    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didChangeHWActiveState isHWActive: Bool)
    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didChangeVTDeinterlaceAccepted isAccepted: Bool)
    func hardwareDecoder(_ decoder: HardwareVideoDecoder, didEncounterDecodeError error: OSStatus)
}

/// Hardware-accelerated H.264 video decoder using Apple Silicon VideoToolbox.
///
/// Features:
/// - Enforces `kVTVideoDecoderSpecification_RequireHardwareAcceleratedVideoDecoder = true`.
/// - Verifies `kVTDecompressionPropertyKey_UsingHardwareAcceleratedVideoDecoder`.
/// - Outputs Bi-Planar NV12 `CVPixelBuffer`s.
/// - Supports Path A (Native Metal shader deinterlace) vs Path B (`kVTDecompressionPropertyKey_DeinterlaceMode`).
public final class HardwareVideoDecoder: @unchecked Sendable {

    private var decompressionSession: VTDecompressionSession?
    private var formatDescription: CMVideoFormatDescription?
    public var useNativeVTDeinterlace: Bool = false {
        didSet {
            if oldValue != useNativeVTDeinterlace {
                recreateSession()
            }
        }
    }

    public weak var delegate: HardwareVideoDecoderDelegate?

    public init() {}

    deinit {
        invalidateSession()
    }

    public func reset() {
        invalidateSession()
        formatDescription = nil
    }

    public func configure(with formatDescription: CMVideoFormatDescription) {
        if let current = self.formatDescription, CMFormatDescriptionEqual(current, otherFormatDescription: formatDescription), decompressionSession != nil {
            return
        }
        self.formatDescription = formatDescription
        recreateSession()
    }

    private func recreateSession() {
        invalidateSession()
        guard let format = formatDescription else { return }

        #if targetEnvironment(simulator)
        let decoderSpecification: [CFString: Any] = [
            kVTVideoDecoderSpecification_EnableHardwareAcceleratedVideoDecoder: kCFBooleanTrue as Any
        ]
        #else
        let decoderSpecification: [CFString: Any] = [
            kVTVideoDecoderSpecification_RequireHardwareAcceleratedVideoDecoder: kCFBooleanTrue as Any,
            kVTVideoDecoderSpecification_EnableHardwareAcceleratedVideoDecoder: kCFBooleanTrue as Any
        ]
        #endif

        let destinationImageBufferAttributes: [CFString: Any] = [
            kCVPixelBufferPixelFormatTypeKey: kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange,
            kCVPixelBufferMetalCompatibilityKey: kCFBooleanTrue as Any,
            kCVPixelBufferIOSurfacePropertiesKey: [:] as CFDictionary
        ]

        var callbackRecord = VTDecompressionOutputCallbackRecord(
            decompressionOutputCallback: decompressionCallback,
            decompressionOutputRefCon: Unmanaged.passUnretained(self).toOpaque()
        )

        var session: VTDecompressionSession?
        let status = VTDecompressionSessionCreate(
            allocator: kCFAllocatorDefault,
            formatDescription: format,
            decoderSpecification: decoderSpecification as CFDictionary,
            imageBufferAttributes: destinationImageBufferAttributes as CFDictionary,
            outputCallback: &callbackRecord,
            decompressionSessionOut: &session
        )

        guard status == noErr, let activeSession = session else {
            delegate?.hardwareDecoder(self, didEncounterDecodeError: status)
            return
        }

        // Apply Path B native VideoToolbox deinterlace property if requested and verify
        var isVTDeinterlaceAccepted = false
        if useNativeVTDeinterlace {
            let deinterlacePropKey = "DeinterlaceMode" as CFString
            let deinterlacePropVal = "Temporal" as CFString
            let deintStatus = VTSessionSetProperty(activeSession, key: deinterlacePropKey, value: deinterlacePropVal)
            if deintStatus == noErr {
                var readVal: CFTypeRef?
                let copyStatus = VTSessionCopyProperty(activeSession, key: deinterlacePropKey, allocator: kCFAllocatorDefault, valueOut: &readVal)
                if copyStatus == noErr, let val = readVal, CFGetTypeID(val) == CFStringGetTypeID() {
                    let str = unsafeBitCast(val, to: CFString.self) as String
                    if str == "Temporal" || str == "VerticalFilter" {
                        isVTDeinterlaceAccepted = true
                    }
                }
            }
        }

        // Verify if Hardware Acceleration is active
        var isHWActive: Bool = false
        var usingHW: CFTypeRef?
        let hwStatus = VTSessionCopyProperty(
            activeSession,
            key: kVTDecompressionPropertyKey_UsingHardwareAcceleratedVideoDecoder,
            allocator: kCFAllocatorDefault,
            valueOut: &usingHW
        )
        if hwStatus == noErr, let val = usingHW, CFGetTypeID(val) == CFBooleanGetTypeID() {
            let cfBool = unsafeBitCast(val, to: CFBoolean.self)
            isHWActive = CFBooleanGetValue(cfBool)
        } else {
            // Since RequireHardwareAcceleratedVideoDecoder was set to true, session creation success implies HW is active
            isHWActive = true
        }

        self.decompressionSession = activeSession
        delegate?.hardwareDecoder(self, didChangeHWActiveState: isHWActive)
        delegate?.hardwareDecoder(self, didChangeVTDeinterlaceAccepted: isVTDeinterlaceAccepted)
    }

    private func invalidateSession() {
        if let session = decompressionSession {
            VTDecompressionSessionInvalidate(session)
            decompressionSession = nil
        }
    }

    public func decode(sampleBuffer: CMSampleBuffer, isTopFieldFirst: Bool) {
        guard let session = decompressionSession else { return }

        var flagsOut: VTDecodeInfoFlags = []
        let context = FrameContext(isTopFieldFirst: isTopFieldFirst, pts: CMSampleBufferGetPresentationTimeStamp(sampleBuffer))
        let contextPtr = Unmanaged.passRetained(context).toOpaque()

        let status = VTDecompressionSessionDecodeFrame(
            session,
            sampleBuffer: sampleBuffer,
            flags: [._EnableAsynchronousDecompression],
            frameRefcon: contextPtr,
            infoFlagsOut: &flagsOut
        )

        if status != noErr {
            _ = Unmanaged<FrameContext>.fromOpaque(contextPtr).takeRetainedValue()
            print("[1080i50-DECODE-FAIL] VTDecompressionSessionDecodeFrame error: \(status)")
            delegate?.hardwareDecoder(self, didEncounterDecodeError: status)
        }
    }

    fileprivate func handleDecodedFrame(status: OSStatus, imageBuffer: CVImageBuffer?, context: FrameContext) {
        guard status == noErr, let buffer = imageBuffer else {
            print("[1080i50-DECODE-FAIL] handleDecodedFrame async error: \(status)")
            delegate?.hardwareDecoder(self, didEncounterDecodeError: status)
            return
        }

        let frame = DecodedVideoFrame(
            pixelBuffer: buffer,
            pts: context.pts,
            isTopFieldFirst: context.isTopFieldFirst
        )
        delegate?.hardwareDecoder(self, didEmitFrame: frame)
    }
}

private final class FrameContext {
    let isTopFieldFirst: Bool
    let pts: CMTime

    init(isTopFieldFirst: Bool, pts: CMTime) {
        self.isTopFieldFirst = isTopFieldFirst
        self.pts = pts
    }
}

private func decompressionCallback(
    decompressionOutputRefCon: UnsafeMutableRawPointer?,
    sourceFrameRefCon: UnsafeMutableRawPointer?,
    status: OSStatus,
    infoFlags: VTDecodeInfoFlags,
    imageBuffer: CVImageBuffer?,
    presentationTimeStamp: CMTime,
    presentationDuration: CMTime
) {
    guard let refCon = decompressionOutputRefCon, let frameRefCon = sourceFrameRefCon else { return }
    let decoder = Unmanaged<HardwareVideoDecoder>.fromOpaque(refCon).takeUnretainedValue()
    let context = Unmanaged<FrameContext>.fromOpaque(frameRefCon).takeRetainedValue()

    decoder.handleDecodedFrame(status: status, imageBuffer: imageBuffer, context: context)
}
