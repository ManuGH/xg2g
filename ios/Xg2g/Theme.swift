// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Broadcast Console 2026 Design Tokens
/// Aligned 1:1 with `apps/webui/src/index.css`.
enum Theme {

    enum Colors {
        /// Base canvas background (#070B10)
        static let bgBase = Color(red: 0.027, green: 0.043, blue: 0.063)
        /// Pure OLED black for video stage (#000000)
        static let bgVideoStage = Color.black
        /// Elevated card/surface (#0E1724 / rgba(14, 23, 36, 0.85))
        static let surfaceElevated = Color(red: 0.055, green: 0.090, blue: 0.141)
        /// Translucent glass panel
        static let surfaceGlass = Color(red: 0.067, green: 0.110, blue: 0.153).opacity(0.65)
        /// Ultra subtle border
        static let borderSubtle = Color.white.opacity(0.08)
        static let borderElevated = Color.white.opacity(0.12)

        /// Action Accent (Apple Blue #0A84FF)
        static let accentAction = Color(red: 0.039, green: 0.518, blue: 1.0)
        /// Live / Recording Accent (Broadcast Amber #FFB24A)
        static let accentLive = Color(red: 1.0, green: 0.698, blue: 0.290)

        /// Text colors
        static let textPrimary = Color(red: 0.969, green: 0.984, blue: 0.965)
        static let textSecondary = Color(red: 0.718, green: 0.776, blue: 0.769)
        static let textTertiary = Color(red: 0.604, green: 0.675, blue: 0.659)
        static let textDisabled = Color(red: 0.345, green: 0.416, blue: 0.408)

        /// Semantic status colors
        static let statusSuccess = Color(red: 0.204, green: 0.827, blue: 0.596)
        static let statusWarning = Color(red: 0.984, green: 0.749, blue: 0.141)
        static let statusError = Color(red: 0.973, green: 0.443, blue: 0.443)
    }

    struct GlassCardModifier: ViewModifier {
        var cornerRadius: CGFloat = 12

        func body(content: Content) -> some View {
            content
                .background(Colors.surfaceGlass)
                .clipShape(RoundedRectangle(cornerRadius: cornerRadius, style: .continuous))
                .overlay(
                    RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
                        .strokeBorder(Colors.borderSubtle, lineWidth: 1)
                )
        }
    }
}

extension View {
    func glassCard(cornerRadius: CGFloat = 12) -> some View {
        modifier(Theme.GlassCardModifier(cornerRadius: cornerRadius))
    }
}
