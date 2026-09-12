// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import UIKit
import ImageIO

final class LogoImageCache: @unchecked Sendable {
    static let shared = LogoImageCache()
    private let cache = NSCache<NSString, UIImage>()

    init() {
        cache.countLimit = 500
        cache.totalCostLimit = 32 * 1024 * 1024
    }

    /// Rounds a required pixel size up to a shared bucket.
    ///
    /// The app draws logos at six different point sizes; keyed verbatim that
    /// would be six decoded copies of every logo. Two buckets cover all of them
    /// and still decode far below the source resolution.
    static func bucket(forPointSize points: CGFloat, scale: CGFloat) -> Int {
        let pixels = Int((points * scale).rounded(.up))
        let rounded = ((pixels + 63) / 64) * 64
        return max(128, rounded)
    }

    /// Every bucket `bucket(forPointSize:scale:)` can currently produce, ascending.
    /// Used only to look up "whatever we already have" — a miss just means no
    /// artwork yet, which is what an uncached logo produced before as well.
    private static let knownBuckets = [128, 192, 256, 512]

    func image(for url: URL, bucket: Int) -> UIImage? {
        cache.object(forKey: Self.key(url, bucket))
    }

    /// The largest cached rendition for this URL, for consumers that do not draw
    /// at a fixed size — Now Playing artwork, for instance.
    func anyImage(for url: URL) -> UIImage? {
        for bucket in Self.knownBuckets.reversed() {
            if let image = cache.object(forKey: Self.key(url, bucket)) {
                return image
            }
        }
        return nil
    }

    /// Stores with an explicit `cost`. Without one every entry counts as zero and
    /// `totalCostLimit` never evicts anything — only `countLimit` applied.
    func store(_ image: UIImage, for url: URL, bucket: Int) {
        let cost = image.cgImage.map { $0.bytesPerRow * $0.height } ?? bucket * bucket * 4
        cache.setObject(image, forKey: Self.key(url, bucket), cost: cost)
    }

    private static func key(_ url: URL, _ bucket: Int) -> NSString {
        "\(url.absoluteString)|\(bucket)" as NSString
    }

    /// Decodes straight to the target size via ImageIO.
    ///
    /// `UIImage(data:)` defers decoding until draw time, which put a full
    /// resolution bitmap decode on the main thread during scrolling. The
    /// thumbnail path decodes once, off the main thread, at the size actually
    /// drawn.
    static func downsampledImage(from data: Data, bucket: Int) -> UIImage? {
        let sourceOptions = [kCGImageSourceShouldCache: false] as CFDictionary
        guard let source = CGImageSourceCreateWithData(data as CFData, sourceOptions) else {
            return nil
        }

        let thumbnailOptions = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceShouldCacheImmediately: true,
            kCGImageSourceThumbnailMaxPixelSize: bucket
        ] as CFDictionary

        guard let cgImage = CGImageSourceCreateThumbnailAtIndex(source, 0, thumbnailOptions) else {
            return nil
        }
        return UIImage(cgImage: cgImage)
    }
}
