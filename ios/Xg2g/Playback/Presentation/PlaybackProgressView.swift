// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Custom Precision Scrubber with gradient progress bar and start/end/remaining timestamps.
struct PlaybackProgressView: View {
    let progress: Double // 0.0 to 1.0
    let startTime: String
    let endTime: String
    let remainingText: String?

    var body: some View {
        VStack(spacing: 5) {
            GeometryReader { geo in
                ZStack(alignment: .leading) {
                    Capsule()
                        .fill(Color.white.opacity(0.18))
                        .frame(height: 3)

                    Capsule()
                        .fill(
                            LinearGradient(
                                colors: [Theme.Colors.accentAction, Theme.Colors.accentLive],
                                startPoint: .leading,
                                endPoint: .trailing
                            )
                        )
                        .frame(width: max(0, min(geo.size.width, geo.size.width * CGFloat(progress))), height: 3)

                    Circle()
                        .fill(Color.white)
                        .frame(width: 8, height: 8)
                        .shadow(color: Theme.Colors.accentLive.opacity(0.8), radius: 3)
                        .offset(x: max(0, min(geo.size.width - 8, geo.size.width * CGFloat(progress) - 4)))
                }
            }
            .frame(height: 8)

            HStack {
                Text(startTime)
                    .font(.app(size: 10, weight: .medium, design: .monospaced))
                    .foregroundStyle(Theme.Colors.textTertiary)

                Spacer()

                if let remainingText {
                    Text(remainingText)
                        .font(.app(size: 10, weight: .semibold, design: .monospaced))
                        .foregroundStyle(Theme.Colors.accentLive)
                }

                Spacer()

                Text(endTime)
                    .font(.app(size: 10, weight: .medium, design: .monospaced))
                    .foregroundStyle(Theme.Colors.textTertiary)
            }
        }
    }
}
