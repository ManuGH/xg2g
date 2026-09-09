// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

struct ChannelLogo: View {
    let url: URL?
    let name: String
    var size: CGFloat = 48

    @State private var loadedImage: UIImage?

    private var bucket: Int {
        LogoImageCache.bucket(forPointSize: size, scale: UIScreen.main.scale)
    }

    var body: some View {
        let currentImage = loadedImage ?? (url.flatMap { LogoImageCache.shared.image(for: $0, bucket: bucket) })

        ZStack {
            if let img = currentImage {
                Image(uiImage: img)
                    .resizable()
                    .scaledToFit()
                    .frame(width: size, height: size)
                    .shadow(color: Color.black.opacity(0.55), radius: 3, y: 1.5)
            } else if let url {
                fallbackBadge
                    .task(id: url) {
                        await loadLogo(from: url)
                    }
            } else {
                fallbackBadge
            }
        }
        .frame(width: size, height: size)
    }

    private func loadLogo(from url: URL) async {
        let target = bucket

        if let cached = LogoImageCache.shared.image(for: url, bucket: target) {
            loadedImage = cached
            return
        }

        guard let data = await MediaFetcher.imageData(from: url), !Task.isCancelled else {
            return
        }

        // Decoding is CPU work on a scrolling list's critical path, so it runs
        // off the main actor rather than implicitly during draw.
        let image = await Task.detached(priority: .utility) {
            LogoImageCache.downsampledImage(from: data, bucket: target)
        }.value

        guard let image, !Task.isCancelled else { return }
        LogoImageCache.shared.store(image, for: url, bucket: target)
        loadedImage = image
    }

    private var fallbackBadge: some View {
        ZStack {
            RoundedRectangle(cornerRadius: size * 0.22, style: .continuous)
                .fill(Color.white.opacity(0.06))
                .overlay(
                    RoundedRectangle(cornerRadius: size * 0.22, style: .continuous)
                        .strokeBorder(Color.white.opacity(0.12), lineWidth: 1)
                )

            Text(String(name.prefix(2)).uppercased())
                .font(.system(size: size * 0.35, weight: .bold, design: .rounded))
                .foregroundStyle(Theme.Colors.textSecondary)
        }
        .frame(width: size, height: size)
        .shadow(color: Color.black.opacity(0.3), radius: 3, y: 1.5)
    }
}
