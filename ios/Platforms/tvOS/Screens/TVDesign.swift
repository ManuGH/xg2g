// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

/// The television's own type and layout scale.
///
/// Three text sizes, one card size, one gutter. A screen in a TV app is read
/// from three metres and navigated one focus step at a time, so it carries
/// far fewer distinctions than a phone screen: the hierarchy lives in weight
/// and colour, not in a ladder of sizes.
enum TVDesign {

    enum Font {
        /// The one big line on a screen: the hero title.
        static let hero = SwiftUI.Font.system(size: 44, weight: .bold)
        /// Shelf titles.
        static let heading = SwiftUI.Font.system(size: 28, weight: .semibold)
        /// Programme titles, button labels.
        static let body = SwiftUI.Font.system(size: 24, weight: .semibold)
        /// Everything that qualifies a title: channel, time, duration.
        static let meta = SwiftUI.Font.system(size: 20, weight: .regular)
    }

    enum Layout {
        static let gutter: CGFloat = 24
        static let cardWidth: CGFloat = 340
        static let cardHeight: CGFloat = 200
        static let tileWidth: CGFloat = 320
        static let tileHeight: CGFloat = 200
        static let cornerRadius: CGFloat = 16
    }
}

/// Focus for cards: the theme surface stays, focus adds a border, a lift and
/// a shadow. The system `.card` style would put the label on a white platter.
struct TVCardButtonStyle: ButtonStyle {

    @Environment(\.isFocused) private var isFocused

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .background(
                RoundedRectangle(cornerRadius: TVDesign.Layout.cornerRadius, style: .continuous)
                    .fill(isFocused ? Theme.Colors.surfaceElevated : Theme.Colors.surfaceElevated.opacity(0.6))
            )
            .overlay(
                RoundedRectangle(cornerRadius: TVDesign.Layout.cornerRadius, style: .continuous)
                    .strokeBorder(isFocused ? Theme.Colors.accentAction : Theme.Colors.borderSubtle, lineWidth: isFocused ? 3 : 1)
            )
            .scaleEffect(isFocused ? 1.05 : 1)
            .shadow(color: .black.opacity(isFocused ? 0.5 : 0), radius: 24, y: 12)
            .opacity(configuration.isPressed ? 0.85 : 1)
            .animation(.easeOut(duration: 0.18), value: isFocused)
    }
}

/// A thin progress line for a running programme.
struct TVProgressLine: View {
    let progress: Double

    var body: some View {
        GeometryReader { geometry in
            ZStack(alignment: .leading) {
                Capsule().fill(Theme.Colors.borderSubtle)
                Capsule()
                    .fill(Theme.Colors.accentLive)
                    .frame(width: max(4, geometry.size.width * min(max(progress, 0), 1)))
            }
        }
        .frame(height: 4)
    }
}

/// A horizontal shelf with a heading, the way TV apps lay out rows.
struct TVShelf<Content: View>: View {
    let title: String
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text(title)
                .font(TVDesign.Font.heading)
                .foregroundStyle(Theme.Colors.textPrimary)
            ScrollView(.horizontal, showsIndicators: false) {
                LazyHStack(spacing: TVDesign.Layout.gutter) {
                    content()
                }
                // Room for the focused card's lift and shadow.
                .padding(.vertical, 16)
            }
        }
    }
}
