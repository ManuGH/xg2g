// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import AVKit
import SwiftUI

enum RecordingArtworkTheme {
    struct Palette {
        let gradient: LinearGradient
        let accent: Color
        let icon: String
        let label: String
    }

    static func palette(for recording: Recording) -> Palette {
        palette(for: recording.genre)
    }

    static func palette(for genre: EPGGenre) -> Palette {
        switch genre {
        case .movie:
            return Palette(
                gradient: LinearGradient(
                    colors: [Color(red: 0.18, green: 0.05, blue: 0.10), Color(red: 0.05, green: 0.02, blue: 0.04)],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                ),
                accent: Color(red: 0.95, green: 0.35, blue: 0.45),
                icon: "film.stack",
                label: "Spielfilm"
            )
        case .series:
            return Palette(
                gradient: LinearGradient(
                    colors: [Color(red: 0.08, green: 0.12, blue: 0.24), Color(red: 0.02, green: 0.04, blue: 0.08)],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                ),
                accent: Color(red: 0.35, green: 0.65, blue: 1.0),
                icon: "tv",
                label: "Serie"
            )
        case .sport:
            return Palette(
                gradient: LinearGradient(
                    colors: [Color(red: 0.04, green: 0.16, blue: 0.12), Color(red: 0.01, green: 0.05, blue: 0.04)],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                ),
                accent: Color(red: 0.25, green: 0.85, blue: 0.55),
                icon: "sportscourt",
                label: "Sport"
            )
        case .docu:
            return Palette(
                gradient: LinearGradient(
                    colors: [Color(red: 0.04, green: 0.14, blue: 0.18), Color(red: 0.01, green: 0.04, blue: 0.06)],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                ),
                accent: Color(red: 0.20, green: 0.80, blue: 0.90),
                icon: "globe.europe.africa",
                label: "Doku"
            )
        case .news:
            return Palette(
                gradient: LinearGradient(
                    colors: [Color(red: 0.14, green: 0.10, blue: 0.04), Color(red: 0.04, green: 0.03, blue: 0.01)],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                ),
                accent: Color(red: 1.0, green: 0.70, blue: 0.25),
                icon: "newspaper",
                label: "Nachrichten"
            )
        case .kids:
            return Palette(
                gradient: LinearGradient(
                    colors: [Color(red: 0.16, green: 0.08, blue: 0.18), Color(red: 0.04, green: 0.02, blue: 0.05)],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                ),
                accent: Color(red: 0.90, green: 0.50, blue: 0.95),
                icon: "sparkles",
                label: "Kinder"
            )
        case .all, .show:
            return Palette(
                gradient: LinearGradient(
                    colors: [Color(red: 0.08, green: 0.10, blue: 0.16), Color(red: 0.02, green: 0.03, blue: 0.05)],
                    startPoint: .topLeading,
                    endPoint: .bottomTrailing
                ),
                accent: Theme.Colors.accentAction,
                icon: "play.rectangle.on.rectangle",
                label: "Aufnahme"
            )
        }
    }
}
