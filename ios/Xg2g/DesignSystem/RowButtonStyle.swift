// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import SwiftUI

#if os(tvOS)
/// Focus for rows that carry text.
///
/// tvOS's `.plain` style lifts a focused label onto a light platter, and the
/// theme's white text disappears on it. This style keeps the theme surface
/// and signals focus with a border and a small scale, which is what the
/// system's own list rows do in dark interfaces.
struct TVFocusRowButtonStyle: ButtonStyle {

    @Environment(\.isFocused) private var isFocused

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .padding(.horizontal, 14)
            .padding(.vertical, 8)
            .background(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(isFocused ? Theme.Colors.surfaceElevated : Color.clear)
            )
            .overlay(
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .strokeBorder(isFocused ? Theme.Colors.accentAction : Color.clear, lineWidth: 2)
            )
            .scaleEffect(isFocused ? 1.02 : 1)
            .opacity(configuration.isPressed ? 0.85 : 1)
            .animation(.easeOut(duration: 0.15), value: isFocused)
    }
}
#endif

extension View {
    /// The button style for a tappable row: `.plain` on iPhone and iPad, and
    /// on tvOS a focus style that keeps the theme's text readable.
    @ViewBuilder
    func rowButtonStyle() -> some View {
#if os(tvOS)
        buttonStyle(TVFocusRowButtonStyle())
#else
        buttonStyle(.plain)
#endif
    }
}
