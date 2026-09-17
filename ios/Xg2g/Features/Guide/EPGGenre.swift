// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// EPG Genre classification for filtering and highlighting (TV Pro style)
enum EPGGenre: String, CaseIterable, Identifiable, Sendable {
    case all = "Alle"
    case movie = "Spielfilme"
    case series = "Serien"
    case sport = "Sport"
    case docu = "Doku & Wissen"
    case show = "Unterhaltung"
    case news = "Nachrichten"
    case kids = "Kinder"

    var id: String { rawValue }

    var icon: String {
        switch self {
        case .all: return "square.grid.2x2"
        case .movie: return "film"
        case .series: return "tv"
        case .sport: return "sportscourt"
        case .docu: return "globe.europe.africa"
        case .show: return "sparkles.tv"
        case .news: return "newspaper"
        case .kids: return "teddybear"
        }
    }
}
