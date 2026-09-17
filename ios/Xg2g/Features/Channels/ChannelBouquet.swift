// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

import Foundation

/// A bouquet / channel group (e.g. "Favorites", "HD", "Sports").
struct ChannelBouquet: Identifiable, Hashable, Equatable, Sendable {
    let id: String
    let name: String
    let servicesCount: Int

    init(id: String? = nil, name: String, servicesCount: Int = 0) {
        self.id = id ?? name
        self.name = name
        self.servicesCount = servicesCount
    }
}
