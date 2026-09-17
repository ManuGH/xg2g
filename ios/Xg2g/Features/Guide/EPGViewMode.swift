// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// View presentation mode: Compact List vs Magazine Grid (TV Pro style)
enum EPGViewMode: String, CaseIterable, Identifiable, Sendable {
    case list = "Senderliste"
    case magazine = "Magazin"

    var id: String { rawValue }

    var icon: String {
        switch self {
        case .list: return "list.bullet"
        case .magazine: return "square.grid.2x2"
        }
    }
}
