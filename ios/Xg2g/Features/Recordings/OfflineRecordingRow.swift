// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import SwiftUI

// MARK: - Offline Recording Row

struct OfflineRecordingRow: View {

    let offline: OfflineRecording

    var body: some View {
        HStack(spacing: 12) {
            ZStack {
                RoundedRectangle(cornerRadius: 10)
                    .fill(Theme.Colors.statusSuccess.opacity(0.15))
                    .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(Theme.Colors.statusSuccess.opacity(0.3), lineWidth: 1))

                Image(systemName: "arrow.down.circle.fill")
                    .font(.title3)
                    .foregroundStyle(Theme.Colors.statusSuccess)
            }
            .frame(width: 46, height: 46)

            VStack(alignment: .leading, spacing: 3) {
                Text(offline.title)
                    .font(.system(size: 15, weight: .bold))
                    .foregroundStyle(Theme.Colors.textPrimary)
                    .lineLimit(1)

                HStack(spacing: 6) {
                    if let q = offline.quality {
                        Label(q.title, systemImage: q.icon)
                            .font(.system(size: 10, weight: .bold))
                            .foregroundStyle(Theme.Colors.accentLive)
                            .padding(.horizontal, 6)
                            .padding(.vertical, 2)
                            .background(Theme.Colors.surfaceElevated, in: Capsule())

                        Text("•")
                            .font(.caption2)
                            .foregroundStyle(Theme.Colors.textTertiary)
                    }

                    Text(offline.formattedSize)
                        .font(.caption.monospaced())
                        .foregroundStyle(Theme.Colors.textTertiary)

                    Text("•")
                        .font(.caption2)
                        .foregroundStyle(Theme.Colors.textTertiary)

                    Text(offline.formattedDuration)
                        .font(.caption.monospaced())
                        .foregroundStyle(Theme.Colors.textTertiary)
                }
            }

            Spacer()

            Image(systemName: "play.circle.fill")
                .font(.title2)
                .foregroundStyle(Theme.Colors.accentAction)
        }
        .padding(14)
        .background(Theme.Gradients.cardSurface, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 16, style: .continuous).strokeBorder(Theme.Gradients.specularBorder, lineWidth: 1))
        .shadow(color: Color.black.opacity(0.18), radius: 6, y: 2)
    }
}
