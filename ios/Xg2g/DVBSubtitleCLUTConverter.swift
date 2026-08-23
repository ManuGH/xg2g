// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreGraphics
import Foundation

/// Converts DVB Subtitle color indices (YCbCr + Transparency) to 32-bit RGBA pixel buffers and CGImages (ITU-R BT.601).
public final class DVBSubtitleCLUTConverter: @unchecked Sendable {

    public init() {}

    /// Converts a pixel index raster into 32-bit RGBA pixel buffer (4 bytes per pixel: [R, G, B, A]).
    ///
    /// - Parameters:
    ///   - raster: 1D array of palette color indices (width * height).
    ///   - clut: Active CLUT definition segment.
    ///   - width: Image width in pixels.
    ///   - height: Image height in pixels.
    /// - Returns: Data buffer containing 32-bit RGBA pixel bytes.
    public func convertToRGBA(
        raster: [UInt8],
        clut: DVBCLUTDefinitionSegment?,
        width: Int,
        height: Int
    ) -> Data {
        guard width > 0 && height > 0, raster.count >= width * height else { return Data() }

        // Build quick lookup table for 256 possible palette entry IDs
        var lookupTable = [UInt32](repeating: 0x00000000, count: 256) // default: transparent black

        if let entries = clut?.entries {
            for entry in entries {
                let idx = Int(entry.entryID)
                guard idx < 256 else { continue }

                if entry.isFullyTransparent {
                    lookupTable[idx] = 0x00000000
                } else {
                    let y = Double(entry.y)
                    let cr = Double(entry.cr) - 128.0
                    let cb = Double(entry.cb) - 128.0

                    // ITU-R BT.601 standard matrix
                    let r = UInt8(clamping: Int(round(y + 1.402 * cr)))
                    let g = UInt8(clamping: Int(round(y - 0.344136 * cb - 0.714136 * cr)))
                    let b = UInt8(clamping: Int(round(y + 1.772 * cb)))
                    let a = UInt8(255 - Int(entry.t)) // DVB T=0 is opaque (alpha=255), T=255 is transparent (alpha=0)

                    // RGBA in memory: byte 0 = R, byte 1 = G, byte 2 = B, byte 3 = A
                    let rgba = (UInt32(a) << 24) | (UInt32(b) << 16) | (UInt32(g) << 8) | UInt32(r)
                    lookupTable[idx] = rgba
                }
            }
        }

        var rgbaData = Data(count: width * height * 4)
        rgbaData.withUnsafeMutableBytes { rawPtr in
            guard let dstPtr = rawPtr.bindMemory(to: UInt32.self).baseAddress else { return }
            for i in 0..<(width * height) {
                let colorIndex = Int(raster[i])
                dstPtr[i] = lookupTable[colorIndex]
            }
        }

        return rgbaData
    }

    /// Creates a CGImage from a pixel index raster and its CLUT definition.
    public func createCGImage(
        raster: [UInt8],
        clut: DVBCLUTDefinitionSegment?,
        width: Int,
        height: Int
    ) -> CGImage? {
        let rgbaData = convertToRGBA(raster: raster, clut: clut, width: width, height: height)
        guard !rgbaData.isEmpty else { return nil }

        let colorSpace = CGColorSpaceCreateDeviceRGB()
        let bitmapInfo = CGBitmapInfo.byteOrder32Little.rawValue | CGImageAlphaInfo.premultipliedLast.rawValue

        guard let provider = CGDataProvider(data: rgbaData as CFData) else { return nil }

        return CGImage(
            width: width,
            height: height,
            bitsPerComponent: 8,
            bitsPerPixel: 32,
            bytesPerRow: width * 4,
            space: colorSpace,
            bitmapInfo: CGBitmapInfo(rawValue: bitmapInfo),
            provider: provider,
            decode: nil,
            shouldInterpolate: false,
            intent: .defaultIntent
        )
    }
}
