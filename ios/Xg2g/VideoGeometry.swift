// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreGraphics
import CoreVideo
import Foundation

/// Defines whether the video is letterboxed/pillarboxed to preserve all content (.fit)
/// or zoomed/cropped to completely fill the viewport (.fill).
public enum VideoScalingMode: String, CaseIterable, Sendable {
    case fit
    case fill
}

/// Display Aspect Ratio (DAR) of the video source.
public enum VideoAspectRatio: Equatable, Sendable {
    case auto
    case override(Double)

    public static let r4_3   = VideoAspectRatio.override(4.0 / 3.0)
    public static let r5_4   = VideoAspectRatio.override(5.0 / 4.0)
    public static let r16_9  = VideoAspectRatio.override(16.0 / 9.0)
    public static let r16_10 = VideoAspectRatio.override(16.0 / 10.0)
    public static let r2_21  = VideoAspectRatio.override(2.21)
    public static let r2_35  = VideoAspectRatio.override(2.35)
    public static let r2_39  = VideoAspectRatio.override(2.39)
}

/// Complete VLC-compatible aspect ratio and scaling cycle preset list.
public enum VideoViewPreset: String, CaseIterable, Identifiable, Sendable {
    case standard     = "Standard"
    case fillScreen   = "Bildschirm füllen"
    case r16_9        = "16:9"
    case r4_3         = "4:3"
    case r5_4         = "5:4"
    case r16_10       = "16:10"
    case r2_21        = "2.21:1"
    case r2_35        = "2.35:1"
    case r2_39        = "2.39:1"

    public var id: String { rawValue }

    /// Default 2026 clean cycle: Standard (Auto Fit) <-> Bildschirm füllen (Auto Fill).
    public static let standardPresets: [VideoViewPreset] = [
        .standard,
        .fillScreen
    ]

    /// Advanced cycle for power users and problem streams.
    public static let advancedPresets: [VideoViewPreset] = [
        .standard,
        .fillScreen,
        .r16_9,
        .r4_3,
        .r5_4,
        .r16_10,
        .r2_21,
        .r2_35,
        .r2_39
    ]

    public var scalingMode: VideoScalingMode {
        switch self {
        case .fillScreen:
            return .fill
        default:
            return .fit
        }
    }

    public var aspectRatio: VideoAspectRatio {
        switch self {
        case .standard, .fillScreen:
            return .auto
        case .r4_3:
            return .r4_3
        case .r5_4:
            return .r5_4
        case .r16_9:
            return .r16_9
        case .r16_10:
            return .r16_10
        case .r2_21:
            return .r2_21
        case .r2_35:
            return .r2_35
        case .r2_39:
            return .r2_39
        }
    }

    /// Cycles to the next preset, honoring whether advanced aspect ratios are enabled.
    public func next(includeAdvanced: Bool = false) -> VideoViewPreset {
        let list = includeAdvanced ? Self.advancedPresets : Self.standardPresets
        guard let index = list.firstIndex(of: self) else { return .standard }
        let nextIndex = (index + 1) % list.count
        return list[nextIndex]
    }
}

/// Uniforms passed to the Metal vertex shader for rendering geometry.
public struct MetalGeometryUniforms: Sendable, Equatable {
    public var positionScaleX: Float = 1.0
    public var positionScaleY: Float = 1.0
    public var uvScaleX: Float = 1.0
    public var uvScaleY: Float = 1.0
    public var uvOffsetX: Float = 0.0
    public var uvOffsetY: Float = 0.0

    public init(
        positionScaleX: Float = 1.0,
        positionScaleY: Float = 1.0,
        uvScaleX: Float = 1.0,
        uvScaleY: Float = 1.0,
        uvOffsetX: Float = 0.0,
        uvOffsetY: Float = 0.0
    ) {
        self.positionScaleX = positionScaleX
        self.positionScaleY = positionScaleY
        self.uvScaleX = uvScaleX
        self.uvScaleY = uvScaleY
        self.uvOffsetX = uvOffsetX
        self.uvOffsetY = uvOffsetY
    }

    public static let identity = MetalGeometryUniforms()
}

public enum VideoGeometry {

    /// Finds the best rational fraction (numerator/denominator) approximating a floating point ratio.
    public static func rationalFraction(from value: Double, maxDenominator: Int = 1000) -> (numerator: Int, denominator: Int) {
        guard value > 0, value.isFinite else { return (1, 1) }
        var bestNum = 1
        var bestDen = 1
        var minDiff = Double.infinity
        for d in 1...maxDenominator {
            let n = Int(round(value * Double(d)))
            if n > 0 {
                let diff = abs(Double(n) / Double(d) - value)
                if diff < minDiff {
                    minDiff = diff
                    bestNum = n
                    bestDen = d
                    if diff < 1e-6 { break }
                }
            }
        }
        return (bestNum, bestDen)
    }

    /// Extracts Sample Aspect Ratio (SAR) from CVPixelBuffer attachments if present.
    public static func extractSAR(from pixelBuffer: CVPixelBuffer) -> (numerator: Int, denominator: Int) {
        if #available(iOS 15.0, *) {
            if let dict = CVBufferCopyAttachment(pixelBuffer, kCVImageBufferPixelAspectRatioKey, nil) as? [CFString: Any] {
                let hSpacing = (dict[kCVImageBufferPixelAspectRatioHorizontalSpacingKey] as? NSNumber)?.intValue ?? (dict[kCVImageBufferPixelAspectRatioHorizontalSpacingKey] as? Int ?? 1)
                let vSpacing = (dict[kCVImageBufferPixelAspectRatioVerticalSpacingKey] as? NSNumber)?.intValue ?? (dict[kCVImageBufferPixelAspectRatioVerticalSpacingKey] as? Int ?? 1)
                return (max(1, hSpacing), max(1, vSpacing))
            }
            if let dict = CVBufferCopyAttachment(pixelBuffer, kCVImageBufferPixelAspectRatioKey, nil) as? [String: Any] {
                let hSpacing = (dict[kCVImageBufferPixelAspectRatioHorizontalSpacingKey as String] as? NSNumber)?.intValue ?? (dict[kCVImageBufferPixelAspectRatioHorizontalSpacingKey as String] as? Int ?? 1)
                let vSpacing = (dict[kCVImageBufferPixelAspectRatioVerticalSpacingKey as String] as? NSNumber)?.intValue ?? (dict[kCVImageBufferPixelAspectRatioVerticalSpacingKey as String] as? Int ?? 1)
                return (max(1, hSpacing), max(1, vSpacing))
            }
        }
        if let dict = CVBufferGetAttachment(pixelBuffer, kCVImageBufferPixelAspectRatioKey, nil)?.takeUnretainedValue() as? NSDictionary {
            let h = (dict[kCVImageBufferPixelAspectRatioHorizontalSpacingKey as String] as? NSNumber)?.intValue ?? 1
            let v = (dict[kCVImageBufferPixelAspectRatioVerticalSpacingKey as String] as? NSNumber)?.intValue ?? 1
            return (max(1, h), max(1, v))
        }
        return (1, 1)
    }

    /// Applies or updates Sample Aspect Ratio (SAR) attachment on a CVPixelBuffer based on an aspect ratio override.
    public static func applyPixelAspectRatio(
        to pixelBuffer: CVPixelBuffer,
        aspectRatioOverride: VideoAspectRatio
    ) {
        switch aspectRatioOverride {
        case .auto:
            // Retain existing stream attachment (no modification)
            break
        case .override(let targetDAR):
            let width = CVPixelBufferGetWidth(pixelBuffer)
            let height = CVPixelBufferGetHeight(pixelBuffer)
            guard width > 0, height > 0, targetDAR > 0 else { return }
            let rawPAR = targetDAR * Double(height) / Double(width)
            let (hNum, vDen) = rationalFraction(from: rawPAR)
            let sarDict: [CFString: Any] = [
                kCVImageBufferPixelAspectRatioHorizontalSpacingKey: hNum,
                kCVImageBufferPixelAspectRatioVerticalSpacingKey: vDen
            ]
            CVBufferSetAttachment(pixelBuffer, kCVImageBufferPixelAspectRatioKey, sarDict as CFDictionary, .shouldPropagate)
        }
    }

    /// Calculates the effective Display Aspect Ratio (DAR) from pixel dimensions and Sample Aspect Ratio (SAR).
    public static func effectiveDAR(
        width: Int,
        height: Int,
        sarNumerator: Int = 1,
        sarDenominator: Int = 1,
        override: VideoAspectRatio = .auto
    ) -> Double {
        switch override {
        case .override(let ratio):
            return ratio > 0 ? ratio : (16.0 / 9.0)
        case .auto:
            guard width > 0, height > 0 else { return 16.0 / 9.0 }
            let num = sarNumerator > 0 ? Double(sarNumerator) : 1.0
            let den = sarDenominator > 0 ? Double(sarDenominator) : 1.0
            let par = num / den
            return (Double(width) / Double(height)) * par
        }
    }

    /// Computes Metal geometry uniforms (vertex quad scale for .fit, or UV crop for .fill).
    public static func calculateMetalGeometry(
        viewportSize: CGSize,
        sourceWidth: Int,
        sourceHeight: Int,
        sarNumerator: Int = 1,
        sarDenominator: Int = 1,
        aspectRatioOverride: VideoAspectRatio = .auto,
        scalingMode: VideoScalingMode = .fit
    ) -> MetalGeometryUniforms {
        guard viewportSize.width > 0, viewportSize.height > 0, sourceWidth > 0, sourceHeight > 0 else {
            return .identity
        }

        let targetDAR = effectiveDAR(
            width: sourceWidth,
            height: sourceHeight,
            sarNumerator: sarNumerator,
            sarDenominator: sarDenominator,
            override: aspectRatioOverride
        )
        let viewDAR = Double(viewportSize.width / viewportSize.height)
        guard targetDAR > 0, viewDAR > 0 else { return .identity }

        switch scalingMode {
        case .fit:
            // Aspect Fit: Scale vertex quad down within [-1..1], leaving clean black borders
            if targetDAR > viewDAR {
                // Video is wider than viewport -> Letterbox top & bottom (height scale < 1.0)
                let heightScale = Float(viewDAR / targetDAR)
                return MetalGeometryUniforms(
                    positionScaleX: 1.0,
                    positionScaleY: heightScale,
                    uvScaleX: 1.0,
                    uvScaleY: 1.0,
                    uvOffsetX: 0.0,
                    uvOffsetY: 0.0
                )
            } else {
                // Video is narrower than viewport -> Pillarbox left & right (width scale < 1.0)
                let widthScale = Float(targetDAR / viewDAR)
                return MetalGeometryUniforms(
                    positionScaleX: widthScale,
                    positionScaleY: 1.0,
                    uvScaleX: 1.0,
                    uvScaleY: 1.0,
                    uvOffsetX: 0.0,
                    uvOffsetY: 0.0
                )
            }

        case .fill:
            // Aspect Fill: Full quad [-1..1], crop texture UVs symmetrically (Center Crop)
            if targetDAR > viewDAR {
                // Video is wider than viewport -> Crop left & right edges of the video
                let visibleRatio = Float(viewDAR / targetDAR)
                let cropX = (1.0 - visibleRatio) * 0.5
                return MetalGeometryUniforms(
                    positionScaleX: 1.0,
                    positionScaleY: 1.0,
                    uvScaleX: visibleRatio,
                    uvScaleY: 1.0,
                    uvOffsetX: cropX,
                    uvOffsetY: 0.0
                )
            } else {
                // Video is taller/narrower than viewport -> Crop top & bottom edges of the video
                let visibleRatio = Float(targetDAR / viewDAR)
                let cropY = (1.0 - visibleRatio) * 0.5
                return MetalGeometryUniforms(
                    positionScaleX: 1.0,
                    positionScaleY: 1.0,
                    uvScaleX: 1.0,
                    uvScaleY: visibleRatio,
                    uvOffsetX: 0.0,
                    uvOffsetY: cropY
                )
            }
        }
    }
}
