// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// Broadcast Console 2026 Design Tokens & Component Styles.
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

    enum Gradients {
        /// Specular lighting border: Top-left light source giving cards physical depth (VisionOS style)
        static let specularBorder = LinearGradient(
            colors: [Color.white.opacity(0.20), Color.white.opacity(0.05)],
            startPoint: .topLeading,
            endPoint: .bottomTrailing
        )

        /// Live aura border for on-air broadcast programs
        static let liveAuraBorder = LinearGradient(
            colors: [Colors.accentLive.opacity(0.60), Colors.accentLive.opacity(0.15)],
            startPoint: .topLeading,
            endPoint: .bottomTrailing
        )

        /// Ambient background card gradient
        static let cardSurface = LinearGradient(
            colors: [Colors.surfaceElevated.opacity(0.88), Colors.surfaceElevated.opacity(0.65)],
            startPoint: .top,
            endPoint: .bottom
        )

        /// Recording alert border
        static let recordingAlertBorder = LinearGradient(
            colors: [Colors.statusError.opacity(0.85), Colors.statusError.opacity(0.35)],
            startPoint: .topLeading,
            endPoint: .bottomTrailing
        )

        /// Active sidebar selection highlight
        static let sidebarActiveSelection = LinearGradient(
            colors: [Colors.accentAction.opacity(0.22), Colors.accentAction.opacity(0.08)],
            startPoint: .leading,
            endPoint: .trailing
        )
    }

    struct GlassCardModifier: ViewModifier {
        var cornerRadius: CGFloat = 12
        var isLive: Bool = false

        func body(content: Content) -> some View {
            content
                .background(.ultraThinMaterial)
                .background(Gradients.cardSurface)
                .clipShape(RoundedRectangle(cornerRadius: cornerRadius, style: .continuous))
                .overlay(
                    RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
                        .strokeBorder(isLive ? Gradients.liveAuraBorder : Gradients.specularBorder, lineWidth: 1)
                )
                .shadow(
                    color: isLive ? Colors.accentLive.opacity(0.18) : Color.black.opacity(0.25),
                    radius: isLive ? 10 : 4,
                    y: isLive ? 2 : 2
                )
        }
    }

    struct FadingHorizontalEdgesModifier: ViewModifier {
        var fadeWidth: CGFloat = 16

        func body(content: Content) -> some View {
            content
                .overlay(alignment: .leading) {
                    LinearGradient(
                        colors: [Colors.bgBase.opacity(0.85), Color.clear],
                        startPoint: .leading,
                        endPoint: .trailing
                    )
                    .frame(width: fadeWidth)
                    .allowsHitTesting(false)
                }
                .overlay(alignment: .trailing) {
                    LinearGradient(
                        colors: [Color.clear, Colors.bgBase.opacity(0.85)],
                        startPoint: .leading,
                        endPoint: .trailing
                    )
                    .frame(width: fadeWidth)
                    .allowsHitTesting(false)
                }
        }
    }
}

/// An amber dot with ambient glow for live status indicators (CPU/GPU-friendly).
struct PulsingLiveDot: View {

    var size: CGFloat = 8

    var body: some View {
        Circle()
            .fill(Theme.Colors.accentLive)
            .frame(width: size, height: size)
            .shadow(color: Theme.Colors.accentLive.opacity(0.8), radius: size * 0.5)
    }
}

/// Apple-native squircle icon container for settings rows (iOS Settings style).
struct SettingsIconBadge: View {
    let systemName: String
    let backgroundColor: Color

    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: 7, style: .continuous)
                .fill(backgroundColor)
                .frame(width: 28, height: 28)

            Image(systemName: systemName)
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(.white)
        }
        .frame(width: 28, height: 28)
    }
}

/// Deterministic channel color generator for vibrant, non-monotone fallback logo badges.
enum ChannelColorGenerator {
    private static let gradients: [(Color, Color)] = [
        (Color(red: 0.08, green: 0.22, blue: 0.45), Color(red: 0.04, green: 0.12, blue: 0.28)), // Navy / Cobalt
        (Color(red: 0.45, green: 0.12, blue: 0.18), Color(red: 0.25, green: 0.05, blue: 0.09)), // Crimson / Wine
        (Color(red: 0.08, green: 0.38, blue: 0.32), Color(red: 0.03, green: 0.20, blue: 0.16)), // Emerald / Teal
        (Color(red: 0.35, green: 0.14, blue: 0.45), Color(red: 0.18, green: 0.06, blue: 0.25)), // Violet / Plum
        (Color(red: 0.48, green: 0.28, blue: 0.08), Color(red: 0.25, green: 0.14, blue: 0.03)), // Amber / Bronze
        (Color(red: 0.15, green: 0.32, blue: 0.48), Color(red: 0.07, green: 0.18, blue: 0.28)), // Steel / Cyan
    ]

    static func gradient(for name: String) -> LinearGradient {
        let hash = abs(name.utf8.reduce(0) { ($0 &* 31) &+ Int($1) })
        let pair = gradients[hash % gradients.count]
        return LinearGradient(
            colors: [pair.0, pair.1],
            startPoint: .topLeading,
            endPoint: .bottomTrailing
        )
    }
}

extension View {
    func glassCard(cornerRadius: CGFloat = 12, isLive: Bool = false) -> some View {
        modifier(Theme.GlassCardModifier(cornerRadius: cornerRadius, isLive: isLive))
    }

    func fadingHorizontalEdges(fadeWidth: CGFloat = 16) -> some View {
        modifier(Theme.FadingHorizontalEdgesModifier(fadeWidth: fadeWidth))
    }
}
