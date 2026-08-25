// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import CoreGraphics
import CoreMedia
import Foundation

/// A rendered DVB subtitle frame ready to be composited over video playback at its presentation timestamp.
public struct DVBSubtitleRenderedFrame: Sendable, Equatable {
    public let image: CGImage?
    public let presentationPTS: CMTime?
    public let presentationPTS90k: UInt64?
    public let timeoutDeadlinePTS: CMTime?
    public let displayWidth: Int
    public let displayHeight: Int

    public var isVisible: Bool {
        image != nil
    }

    public init(
        image: CGImage?,
        presentationPTS: CMTime?,
        presentationPTS90k: UInt64?,
        timeoutDeadlinePTS: CMTime?,
        displayWidth: Int,
        displayHeight: Int
    ) {
        self.image = image
        self.presentationPTS = presentationPTS
        self.presentationPTS90k = presentationPTS90k
        self.timeoutDeadlinePTS = timeoutDeadlinePTS
        self.displayWidth = displayWidth
        self.displayHeight = displayHeight
    }

    /// Evaluates whether this subtitle frame should be displayed at the given video presentation PTS.
    public func isValid(at mediaPTS: CMTime) -> Bool {
        guard isVisible else { return false }
        if let pPTS = presentationPTS {
            if mediaPTS < pPTS { return false }
        }
        if let deadline = timeoutDeadlinePTS {
            return mediaPTS < deadline
        }
        return true
    }
}

/// Renders fully resolved `DVBSubtitlePageSnapshot` models into composite 32-bit RGBA CGImages.
public final class DVBSubtitleImageRenderer: @unchecked Sendable {

    private let rleDecoder = DVBSubtitleRLEDecoder()
    private let clutConverter = DVBSubtitleCLUTConverter()

    public init() {}

    /// Renders a page snapshot into a full-frame `DVBSubtitleRenderedFrame`.
    ///
    /// - Parameter snapshot: The active subtitle page snapshot from `DVBSubtitlePageContext`.
    /// - Returns: A rendered frame containing a composite `CGImage` or `nil` if the page is blank.
    public func render(snapshot: DVBSubtitlePageSnapshot) -> DVBSubtitleRenderedFrame {
        guard snapshot.isVisible, !snapshot.regions.isEmpty else {
            return DVBSubtitleRenderedFrame(
                image: nil,
                presentationPTS: snapshot.presentationPTS,
                presentationPTS90k: snapshot.presentationPTS90k,
                timeoutDeadlinePTS: snapshot.timeoutDeadlinePTS,
                displayWidth: snapshot.displayWidth,
                displayHeight: snapshot.displayHeight
            )
        }

        let displayW = max(snapshot.displayWidth, 1)
        let displayH = max(snapshot.displayHeight, 1)

        let colorSpace = CGColorSpaceCreateDeviceRGB()
        let bitmapInfo = CGBitmapInfo.byteOrder32Little.rawValue | CGImageAlphaInfo.premultipliedLast.rawValue

        guard let context = CGContext(
            data: nil,
            width: displayW,
            height: displayH,
            bitsPerComponent: 8,
            bytesPerRow: displayW * 4,
            space: colorSpace,
            bitmapInfo: bitmapInfo
        ) else {
            return DVBSubtitleRenderedFrame(
                image: nil,
                presentationPTS: snapshot.presentationPTS,
                presentationPTS90k: snapshot.presentationPTS90k,
                timeoutDeadlinePTS: snapshot.timeoutDeadlinePTS,
                displayWidth: displayW,
                displayHeight: displayH
            )
        }

        // Clear canvas with transparent pixels
        context.clear(CGRect(x: 0, y: 0, width: displayW, height: displayH))

        // DVB Subtitling uses top-left origin (0, 0 at top-left). CoreGraphics uses bottom-left origin.
        // We flip coordinates so (x, y) maps accurately to TV screen coordinates:
        // CGY = displayH - (regionY + regionH)
        for region in snapshot.regions {
            guard region.width > 0 && region.height > 0 else { continue }

            // 1. Create a raster for the region initialized with background pixel code or 0
            var regionRaster = [UInt8](repeating: region.fillFlag ? region.pixelCode8Bit : 0, count: region.width * region.height)

            // 2. Decode and overlay all objects inside this region
            for object in region.objects {
                let objRaster = rleDecoder.decode(
                    objectData: object.objectData,
                    width: region.width,
                    height: region.height,
                    depth: region.depth
                )

                // Composite object raster over region raster
                for y in 0..<region.height {
                    let srcRow = y * region.width
                    let dstY = y + object.verticalPosition
                    guard dstY >= 0 && dstY < region.height else { continue }

                    for x in 0..<region.width {
                        let dstX = x + object.horizontalPosition
                        guard dstX >= 0 && dstX < region.width else { continue }

                        let pixel = objRaster[srcRow + x]
                        if pixel != 0 {
                            regionRaster[dstY * region.width + dstX] = pixel
                        }
                    }
                }
            }

            // 3. Convert region raster to RGBA CGImage using CLUT
            if let regionImage = clutConverter.createCGImage(
                raster: regionRaster,
                clut: region.clut,
                width: region.width,
                height: region.height
            ) {
                // Invert Y for CoreGraphics context coordinates
                let cgY = displayH - (region.verticalAddress + region.height)
                let rect = CGRect(
                    x: region.horizontalAddress,
                    y: cgY,
                    width: region.width,
                    height: region.height
                )
                context.draw(regionImage, in: rect)
            }
        }

        let fullImage = context.makeImage()

        return DVBSubtitleRenderedFrame(
            image: fullImage,
            presentationPTS: snapshot.presentationPTS,
            presentationPTS90k: snapshot.presentationPTS90k,
            timeoutDeadlinePTS: snapshot.timeoutDeadlinePTS,
            displayWidth: displayW,
            displayHeight: displayH
        )
    }
}
